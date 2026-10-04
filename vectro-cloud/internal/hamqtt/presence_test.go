package hamqtt

import (
	"testing"
	"time"
)

func newTestBridge() *bridge {
	return &bridge{
		base: "vectro/test",
		presence: presence{
			timeout:    time.Minute,
			lastEvents: map[string]time.Time{},
		},
	}
}

func TestKnownFaceMarksOccupied(t *testing.T) {
	b := newTestBridge()
	b.onFace("Jonathan")
	p := &b.presence
	if !p.occupied || p.lastName != "Jonathan" {
		t.Fatalf("occupied=%v lastName=%q", p.occupied, p.lastName)
	}
	if _, ok := p.lastEvents["Jonathan"]; !ok {
		t.Fatal("no face_seen event recorded")
	}
}

// Recognition takes a moment: unnamed frames right before a name must not
// be reported as an unknown person.
func TestUnknownWaitsForRecognition(t *testing.T) {
	b := newTestBridge()
	b.onFace("")
	if b.presence.occupied {
		t.Fatal("unnamed first frame reported immediately")
	}
	b.onFace("Jonathan")
	b.onFace("")
	if b.presence.lastName != "Jonathan" {
		t.Fatalf("lastName=%q, want Jonathan", b.presence.lastName)
	}
	if _, ok := b.presence.lastEvents["Unknown"]; ok {
		t.Fatal("reported Unknown while a named face was just seen")
	}
}

func TestUnknownAfterDelay(t *testing.T) {
	b := newTestBridge()
	b.onFace("")
	b.presence.unknownAt = time.Now().Add(-unknownDelay - time.Second)
	b.onFace("")
	if !b.presence.occupied || b.presence.lastName != "Unknown" {
		t.Fatalf("occupied=%v lastName=%q", b.presence.occupied, b.presence.lastName)
	}
}

func TestPresenceTimesOut(t *testing.T) {
	b := newTestBridge()
	b.onFace("Jonathan")
	b.checkPresence()
	if !b.presence.occupied {
		t.Fatal("unoccupied right after a sighting")
	}
	b.presence.lastFace = time.Now().Add(-2 * time.Minute)
	b.checkPresence()
	if b.presence.occupied {
		t.Fatal("still occupied after timeout")
	}
}

func TestFaceEventsThrottled(t *testing.T) {
	b := newTestBridge()
	b.onFace("Jonathan")
	first := b.presence.lastEvents["Jonathan"]
	b.onFace("Jonathan")
	if !b.presence.lastEvents["Jonathan"].Equal(first) {
		t.Fatal("second sighting within the window produced another event")
	}
}
