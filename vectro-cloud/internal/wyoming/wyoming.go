// Package wyoming implements the framing of the Wyoming protocol used by
// Home Assistant voice services and satellites.
//
// Each event is one JSON header line, optionally followed by data_length
// bytes of JSON data (merged into the header's "data") and payload_length
// bytes of binary payload (e.g. PCM audio).
package wyoming

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Version is the protocol version we report in headers.
const Version = "1.5.4"

// maxLine bounds a header line so a bad peer can't make us buffer forever.
const maxLine = 1 << 20

// Event is a single Wyoming message.
type Event struct {
	Type    string
	Data    map[string]any
	Payload []byte
}

type header struct {
	Type          string         `json:"type"`
	Version       string         `json:"version,omitempty"`
	Data          map[string]any `json:"data,omitempty"`
	DataLength    int            `json:"data_length,omitempty"`
	PayloadLength int            `json:"payload_length,omitempty"`
}

// Read reads the next event. It returns io.EOF when the peer closes cleanly.
func Read(r *bufio.Reader) (*Event, error) {
	line, err := readLine(r)
	if err != nil {
		return nil, err
	}
	var h header
	if err := json.Unmarshal(line, &h); err != nil {
		return nil, fmt.Errorf("wyoming: bad header: %w", err)
	}
	ev := &Event{Type: h.Type, Data: h.Data}
	if ev.Data == nil {
		ev.Data = map[string]any{}
	}
	if h.DataLength > 0 {
		buf := make([]byte, h.DataLength)
		if _, err := io.ReadFull(r, buf); err != nil {
			return nil, err
		}
		var extra map[string]any
		if err := json.Unmarshal(buf, &extra); err != nil {
			return nil, fmt.Errorf("wyoming: bad data: %w", err)
		}
		for k, v := range extra {
			ev.Data[k] = v
		}
	}
	if h.PayloadLength > 0 {
		ev.Payload = make([]byte, h.PayloadLength)
		if _, err := io.ReadFull(r, ev.Payload); err != nil {
			return nil, err
		}
	}
	return ev, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, isPrefix, err := r.ReadLine()
		if err != nil {
			return nil, err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, fmt.Errorf("wyoming: header line too long")
		}
		if !isPrefix {
			return line, nil
		}
	}
}

// Write writes one event. Callers sharing a writer must serialize calls.
func Write(w io.Writer, ev *Event) error {
	h := header{Type: ev.Type, Version: Version}
	var data []byte
	if len(ev.Data) > 0 {
		var err error
		if data, err = json.Marshal(ev.Data); err != nil {
			return err
		}
		h.DataLength = len(data)
	}
	h.PayloadLength = len(ev.Payload)
	line, err := json.Marshal(h)
	if err != nil {
		return err
	}
	buf := make([]byte, 0, len(line)+1+len(data)+len(ev.Payload))
	buf = append(buf, line...)
	buf = append(buf, '\n')
	buf = append(buf, data...)
	buf = append(buf, ev.Payload...)
	_, err = w.Write(buf)
	return err
}

// String returns a data field, or "" if missing or not a string.
func (e *Event) String(key string) string {
	s, _ := e.Data[key].(string)
	return s
}

// Int returns a numeric data field, or def if missing.
func (e *Event) Int(key string, def int) int {
	if f, ok := e.Data[key].(float64); ok {
		return int(f)
	}
	return def
}
