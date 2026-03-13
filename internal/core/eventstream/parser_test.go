package eventstream

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"io"
	"testing"
)

// buildFrame constructs a valid AWS EventStream binary frame for testing.
func buildFrame(headers map[string]string, payload []byte) []byte {
	crc32cTable := crc32.MakeTable(crc32.Castagnoli)

	// Encode headers
	var headerBuf bytes.Buffer
	for name, value := range headers {
		headerBuf.WriteByte(byte(len(name)))
		headerBuf.WriteString(name)
		headerBuf.WriteByte(7) // string type
		binary.Write(&headerBuf, binary.BigEndian, uint16(len(value)))
		headerBuf.WriteString(value)
	}
	headersData := headerBuf.Bytes()

	totalLen := uint32(12 + len(headersData) + len(payload) + 4) // prelude + headers + payload + message_crc
	headersLen := uint32(len(headersData))

	var frame bytes.Buffer

	// Prelude (first 8 bytes)
	binary.Write(&frame, binary.BigEndian, totalLen)
	binary.Write(&frame, binary.BigEndian, headersLen)
	// Prelude CRC (over first 8 bytes)
	prelude := frame.Bytes()
	preludeCRC := crc32.Checksum(prelude, crc32cTable)
	binary.Write(&frame, binary.BigEndian, preludeCRC)

	// Headers
	frame.Write(headersData)

	// Payload
	frame.Write(payload)

	// Message CRC (over entire frame so far)
	frameSoFar := frame.Bytes()
	messageCRC := crc32.Checksum(frameSoFar, crc32cTable)
	binary.Write(&frame, binary.BigEndian, messageCRC)

	return frame.Bytes()
}

// ============================================================
// Parse - single frame
// ============================================================

func TestParse_SimpleEvent(t *testing.T) {
	payload := []byte(`{"content":"hello"}`)
	headers := map[string]string{
		":event-type":   "assistantResponseEvent",
		":content-type": "application/json",
		":message-type": "event",
	}
	data := buildFrame(headers, payload)
	reader := bytes.NewReader(data)

	event, err := Parse(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.EventType != "assistantResponseEvent" {
		t.Errorf("EventType = %q, want assistantResponseEvent", event.EventType)
	}
	if event.ContentType != "application/json" {
		t.Errorf("ContentType = %q", event.ContentType)
	}
	if event.MessageType != "event" {
		t.Errorf("MessageType = %q, want event", event.MessageType)
	}
	if string(event.Payload) != `{"content":"hello"}` {
		t.Errorf("Payload = %q", string(event.Payload))
	}
}

func TestParse_ExceptionEvent(t *testing.T) {
	payload := []byte(`{"message":"throttled"}`)
	headers := map[string]string{
		":event-type":   "exception",
		":message-type": "exception",
	}
	data := buildFrame(headers, payload)

	event, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.MessageType != "exception" {
		t.Errorf("MessageType = %q, want exception", event.MessageType)
	}
	if event.EventType != "exception" {
		t.Errorf("EventType = %q", event.EventType)
	}
}

func TestParse_EmptyPayload(t *testing.T) {
	headers := map[string]string{
		":event-type":   "ping",
		":message-type": "event",
	}
	data := buildFrame(headers, []byte{})

	event, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.EventType != "ping" {
		t.Errorf("EventType = %q", event.EventType)
	}
	if len(event.Payload) != 0 {
		t.Errorf("Payload should be empty, got %d bytes", len(event.Payload))
	}
}

func TestParse_EmptyReader_EOF(t *testing.T) {
	_, err := Parse(bytes.NewReader([]byte{}))
	if err == nil {
		t.Error("expected error on empty reader")
	}
}

func TestParse_TruncatedPrelude(t *testing.T) {
	_, err := Parse(bytes.NewReader([]byte{0, 0, 0}))
	if err == nil {
		t.Error("expected error on truncated prelude")
	}
}

// ============================================================
// ParseStreamingResponse - multi-frame
// ============================================================

func TestParseStreamingResponse_MultipleEvents(t *testing.T) {
	var buf bytes.Buffer

	// Frame 1
	p1 := []byte(`{"content":"hello "}`)
	buf.Write(buildFrame(map[string]string{
		":event-type":   "assistantResponseEvent",
		":message-type": "event",
	}, p1))

	// Frame 2
	p2 := []byte(`{"content":"world"}`)
	buf.Write(buildFrame(map[string]string{
		":event-type":   "assistantResponseEvent",
		":message-type": "event",
	}, p2))

	events := make(chan Event, 10)
	err := ParseStreamingResponse(&buf, events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var collected []Event
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 2 {
		t.Fatalf("expected 2 events, got %d", len(collected))
	}

	var content1, content2 struct{ Content string }
	json.Unmarshal(collected[0].Payload, &content1)
	json.Unmarshal(collected[1].Payload, &content2)
	if content1.Content != "hello " || content2.Content != "world" {
		t.Errorf("contents = %q, %q", content1.Content, content2.Content)
	}
}

func TestParseStreamingResponse_EmptyStream(t *testing.T) {
	events := make(chan Event, 10)
	err := ParseStreamingResponse(bytes.NewReader([]byte{}), events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var collected []Event
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 0 {
		t.Errorf("expected 0 events, got %d", len(collected))
	}
}

func TestParseStreamingResponse_MixedEventTypes(t *testing.T) {
	var buf bytes.Buffer

	// Text event
	buf.Write(buildFrame(map[string]string{
		":event-type":   "assistantResponseEvent",
		":message-type": "event",
	}, []byte(`{"content":"hi"}`)))

	// Tool use event
	buf.Write(buildFrame(map[string]string{
		":event-type":   "toolUseEvent",
		":message-type": "event",
	}, []byte(`{"toolUseId":"t1","name":"calc","input":""}`)))

	// Exception
	buf.Write(buildFrame(map[string]string{
		":event-type":   "exception",
		":message-type": "exception",
	}, []byte(`{"message":"err"}`)))

	events := make(chan Event, 10)
	err := ParseStreamingResponse(&buf, events)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var collected []Event
	for e := range events {
		collected = append(collected, e)
	}
	if len(collected) != 3 {
		t.Fatalf("expected 3 events, got %d", len(collected))
	}
	if collected[0].EventType != "assistantResponseEvent" {
		t.Errorf("[0] EventType = %q", collected[0].EventType)
	}
	if collected[1].EventType != "toolUseEvent" {
		t.Errorf("[1] EventType = %q", collected[1].EventType)
	}
	if collected[2].MessageType != "exception" {
		t.Errorf("[2] MessageType = %q", collected[2].MessageType)
	}
}

// ============================================================
// parseHeaders edge cases
// ============================================================

func TestParseHeaders_EmptyHeaders(t *testing.T) {
	// Frame with 0 headers
	data := buildFrame(map[string]string{}, []byte(`{}`))
	event, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event.EventType != "" {
		t.Errorf("EventType should be empty, got %q", event.EventType)
	}
}

// ============================================================
// Round-trip: build frame → parse → verify
// ============================================================

func TestRoundTrip_LargePayload(t *testing.T) {
	// 64KB payload
	payload := bytes.Repeat([]byte("x"), 64*1024)
	headers := map[string]string{
		":event-type":   "assistantResponseEvent",
		":message-type": "event",
	}
	data := buildFrame(headers, payload)

	event, err := Parse(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(event.Payload) != 64*1024 {
		t.Errorf("payload size = %d, want %d", len(event.Payload), 64*1024)
	}
}

// Ensure Parse properly returns error on partial body read
func TestParse_TruncatedBody(t *testing.T) {
	payload := []byte(`{"content":"hello"}`)
	headers := map[string]string{":event-type": "test", ":message-type": "event"}
	full := buildFrame(headers, payload)

	// Truncate body — cut off 10 bytes from end
	truncated := full[:len(full)-10]
	_, err := Parse(bytes.NewReader(truncated))
	if err == nil {
		t.Error("expected error on truncated body")
	}
}

// Sequential reads from a single reader
func TestParse_Sequential(t *testing.T) {
	var buf bytes.Buffer
	for i := 0; i < 5; i++ {
		buf.Write(buildFrame(map[string]string{
			":event-type":   "assistantResponseEvent",
			":message-type": "event",
		}, []byte(`{"i":`+string(rune('0'+i))+`}`)))
	}

	reader := &buf
	for i := 0; i < 5; i++ {
		event, err := Parse(reader)
		if err != nil {
			t.Fatalf("frame %d: unexpected error: %v", i, err)
		}
		if event.EventType != "assistantResponseEvent" {
			t.Errorf("frame %d: EventType = %q", i, event.EventType)
		}
	}
	// Next read should be EOF
	_, err := Parse(reader)
	if err == nil {
		t.Error("expected EOF error after all frames consumed")
	}
	if err != nil && err != io.EOF && !bytes.Contains([]byte(err.Error()), []byte("EOF")) {
		t.Logf("got expected error: %v", err)
	}
}
