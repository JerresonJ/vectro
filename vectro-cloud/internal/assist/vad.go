package assist

import "math"

// endpointer decides when the user has finished speaking, from mic audio
// as it streams in. Vector's own end-of-speech detection waits several
// seconds of silence; this ends the utterance after a short pause instead.
//
// It's energy based: the first frames set a noise floor, a frame counts as
// speech when it's clearly above that floor, and once speech has been heard
// a run of quiet frames marks the end.
type endpointer struct {
	floor      float64 // running estimate of background RMS
	frames     int
	speech     int // speech frames seen
	quietMs    int // trailing quiet since the last speech frame
	totalMs    int
	frameMs    int
	sampleRate int
}

const (
	vadCalibrateFrames = 2    // frames used to seed the noise floor
	vadMinFloor        = 150  // RMS floor so a silent mic doesn't make everything "speech"
	vadSpeechRatio     = 3.0  // speech is this many times louder than the floor
	vadMinSpeechFrames = 2    // need this much speech before ending
	vadEndSilenceMs    = 700  // pause that ends the utterance
	vadMaxMs           = 8000 // hard cap on an utterance
)

func newEndpointer(sampleRate int) *endpointer {
	return &endpointer{sampleRate: sampleRate}
}

// push adds a frame of samples and reports whether the utterance is over.
func (e *endpointer) push(samples []int16) bool {
	if len(samples) == 0 {
		return false
	}
	ms := len(samples) * 1000 / e.sampleRate
	e.totalMs += ms
	level := rms(samples)
	e.frames++

	if e.frames <= vadCalibrateFrames {
		// The wake word has just ended, so the first frames are usually
		// the room before the command starts.
		e.floor = math.Max(vadMinFloor, (e.floor*float64(e.frames-1)+level)/float64(e.frames))
		return false
	}

	if level > e.floor*vadSpeechRatio {
		e.speech++
		e.quietMs = 0
	} else {
		e.quietMs += ms
		// Track slow changes in background noise during pauses.
		e.floor = math.Max(vadMinFloor, 0.9*e.floor+0.1*level)
	}

	if e.totalMs >= vadMaxMs {
		return true
	}
	return e.speech >= vadMinSpeechFrames && e.quietMs >= vadEndSilenceMs
}

func rms(samples []int16) float64 {
	var sum float64
	for _, s := range samples {
		v := float64(s)
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(samples)))
}
