package wyoming

import (
	"bufio"
	"bytes"
	"strconv"
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := &Event{
		Type:    "audio-chunk",
		Data:    map[string]any{"rate": 16000, "width": 2, "channels": 1},
		Payload: []byte{1, 2, 3, 4},
	}
	if err := Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	if err := Write(&buf, &Event{Type: "audio-stop"}); err != nil {
		t.Fatal(err)
	}

	r := bufio.NewReader(&buf)
	out, err := Read(r)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != "audio-chunk" || out.Int("rate", 0) != 16000 || !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("unexpected event: %+v", out)
	}
	out, err = Read(r)
	if err != nil {
		t.Fatal(err)
	}
	if out.Type != "audio-stop" || len(out.Payload) != 0 {
		t.Fatalf("unexpected event: %+v", out)
	}
}

// Older peers put data inline in the header instead of using data_length.
func TestReadInlineData(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(`{"type":"transcript","data":{"text":"turn on the lights"}}` + "\n"))
	ev, err := Read(r)
	if err != nil {
		t.Fatal(err)
	}
	if ev.String("text") != "turn on the lights" {
		t.Fatalf("unexpected text %q", ev.String("text"))
	}
}

// Inline data and data_length bytes are merged, with the latter winning.
func TestReadMergedData(t *testing.T) {
	data := `{"text":"hello"}`
	hdr := `{"type":"synthesize","data":{"voice":{"name":"x"},"text":"old"},"data_length":` + strconv.Itoa(len(data)) + "}\n"
	ev, err := Read(bufio.NewReader(strings.NewReader(hdr + data)))
	if err != nil {
		t.Fatal(err)
	}
	if ev.String("text") != "hello" || ev.Data["voice"] == nil {
		t.Fatalf("unexpected data %+v", ev.Data)
	}
}
