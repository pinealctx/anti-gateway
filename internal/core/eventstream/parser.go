package eventstream

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Event represents a parsed AWS EventStream event.
type Event struct {
	EventType   string
	ContentType string
	MessageType string // "event" or "exception"
	Payload     json.RawMessage
}

// Parse reads a complete AWS EventStream frame from the reader.
// Frame layout:
//
//	[total_length: 4B][headers_length: 4B][prelude_crc: 4B][headers: NB][payload: MB][message_crc: 4B]
func Parse(r io.Reader) (*Event, error) {
	// Read prelude (12 bytes)
	prelude := make([]byte, 12)
	if _, err := io.ReadFull(r, prelude); err != nil {
		return nil, fmt.Errorf("read prelude: %w", err)
	}

	totalLen := binary.BigEndian.Uint32(prelude[0:4])
	headersLen := binary.BigEndian.Uint32(prelude[4:8])
	// prelude_crc at prelude[8:12] — skip CRC validation for now

	if totalLen < 16 {
		return nil, fmt.Errorf("invalid frame: total_length=%d too small", totalLen)
	}

	// Read remaining bytes (total - 12 prelude bytes)
	remaining := make([]byte, totalLen-12)
	if _, err := io.ReadFull(r, remaining); err != nil {
		return nil, fmt.Errorf("read frame body: %w", err)
	}

	// Parse headers
	headersData := remaining[:headersLen]
	payloadLen := totalLen - 12 - headersLen - 4 // minus prelude, headers, message_crc
	payloadData := remaining[headersLen : headersLen+payloadLen]

	event := &Event{}
	parseHeaders(headersData, event)

	event.Payload = json.RawMessage(payloadData)

	return event, nil
}

// parseHeaders extracts named string headers from the header block.
func parseHeaders(data []byte, event *Event) {
	offset := 0
	for offset < len(data) {
		if offset >= len(data) {
			break
		}
		nameLen := int(data[offset])
		offset++
		if offset+nameLen > len(data) {
			break
		}
		name := string(data[offset : offset+nameLen])
		offset += nameLen

		if offset >= len(data) {
			break
		}
		valueType := data[offset]
		offset++

		if valueType == 7 { // string type
			if offset+2 > len(data) {
				break
			}
			valueLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
			offset += 2
			if offset+valueLen > len(data) {
				break
			}
			value := string(data[offset : offset+valueLen])
			offset += valueLen

			switch name {
			case ":event-type":
				event.EventType = value
			case ":content-type":
				event.ContentType = value
			case ":message-type":
				event.MessageType = value
			}
		} else {
			// Skip unknown value types
			break
		}
	}
}

// ParseStreamingResponse reads events from an AWS EventStream byte stream.
// It yields events through the channel and closes it when the stream ends.
func ParseStreamingResponse(reader io.Reader, events chan<- Event) error {
	defer close(events)

	for {
		event, err := Parse(reader)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return nil
			}
			return fmt.Errorf("parse event: %w", err)
		}
		events <- *event
	}
}
