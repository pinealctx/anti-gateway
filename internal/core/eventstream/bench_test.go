package eventstream

import (
	"bytes"
	"testing"
)

func BenchmarkParse_SingleFrame(b *testing.B) {
	payload := []byte(`{"content":"Hello, this is a test response from the model."}`)
	headers := map[string]string{
		":event-type":   "assistantResponseEvent",
		":content-type": "application/json",
		":message-type": "event",
	}
	data := buildFrame(headers, payload)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Parse(bytes.NewReader(data))
	}
}

func BenchmarkParseStreamingResponse_100Frames(b *testing.B) {
	var buf bytes.Buffer
	payload := []byte(`{"content":"chunk "}`)
	headers := map[string]string{
		":event-type":   "assistantResponseEvent",
		":message-type": "event",
	}
	for i := 0; i < 100; i++ {
		buf.Write(buildFrame(headers, payload))
	}
	data := buf.Bytes()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		events := make(chan Event, 100)
		ParseStreamingResponse(bytes.NewReader(data), events)
		for range events {
		}
	}
}
