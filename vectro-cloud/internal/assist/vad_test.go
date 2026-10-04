package assist

import "testing"

const testRate = 16000

// frame returns 120 ms of a square wave with the given amplitude.
func frame(amp int16) []int16 {
	f := make([]int16, testRate*120/1000)
	for i := range f {
		if (i/20)%2 == 0 {
			f[i] = amp
		} else {
			f[i] = -amp
		}
	}
	return f
}

// feed pushes frames and returns how many were consumed before the
// endpointer said the utterance was over (or -1 if it never did).
func feed(e *endpointer, frames ...[]int16) int {
	for i, f := range frames {
		if e.push(f) {
			return i + 1
		}
	}
	return -1
}

func repeat(f []int16, n int) [][]int16 {
	out := make([][]int16, n)
	for i := range out {
		out[i] = f
	}
	return out
}

func TestEndsAfterSpeechThenPause(t *testing.T) {
	e := newEndpointer(testRate)
	quiet, loud := frame(100), frame(4000)
	var frames [][]int16
	frames = append(frames, repeat(quiet, 2)...) // room noise
	frames = append(frames, repeat(loud, 10)...) // 1.2 s of speech
	frames = append(frames, repeat(quiet, 20)...)
	got := feed(e, frames...)
	// 700 ms of silence at 120 ms frames ends on the 6th quiet frame.
	if want := 2 + 10 + 6; got != want {
		t.Fatalf("ended after %d frames, want %d", got, want)
	}
}

func TestShortPauseDoesNotEnd(t *testing.T) {
	e := newEndpointer(testRate)
	quiet, loud := frame(100), frame(4000)
	var frames [][]int16
	frames = append(frames, repeat(quiet, 2)...)
	frames = append(frames, repeat(loud, 4)...)
	frames = append(frames, repeat(quiet, 3)...) // 360 ms gap between words
	frames = append(frames, repeat(loud, 4)...)
	if got := feed(e, frames...); got != -1 {
		t.Fatalf("ended early after %d frames", got)
	}
}

func TestSilenceAloneDoesNotEndBeforeCap(t *testing.T) {
	e := newEndpointer(testRate)
	if got := feed(e, repeat(frame(100), 20)...); got != -1 {
		t.Fatalf("ended after %d frames of silence", got)
	}
}

func TestHardCap(t *testing.T) {
	e := newEndpointer(testRate)
	got := feed(e, repeat(frame(4000), 100)...)
	if want := vadMaxMs / 120; got < want || got > want+1 {
		t.Fatalf("ended after %d frames, want about %d", got, want)
	}
}
