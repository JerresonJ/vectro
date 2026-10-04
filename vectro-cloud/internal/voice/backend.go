package voice

import (
	"context"

	"vectro/cloud/internal/clad/cloud"
)

// Backend turns one utterance of mic audio into an intent for the engine.
// Vectro replaced DDL's chipper cloud client with this interface so voice
// handling can be swapped out (no-op today, Home Assistant Assist later).
type Backend interface {
	// Open starts a session. Results must be reported to sink from another
	// goroutine (Process.Run is the reader of sink's channels) and must stop
	// once ctx is done.
	Open(ctx context.Context, req Request, sink Sink) (Session, error)
}

// Request describes what the engine asked for when it opened a session.
type Request struct {
	// ConnectionCheck is set when the engine/app is probing voice-service
	// health rather than sending a spoken request.
	ConnectionCheck bool
	Mode            cloud.StreamType
	Locale          string
	Timezone        string
	NoLogging       bool
}

// Session receives the audio for one utterance.
type Session interface {
	// AddSamples is called with 16 kHz mono PCM samples.
	AddSamples(samples []int16)
	// CloseSend signals that the mic has finished sending audio.
	CloseSend() error
	// Close ends the session; no further results will be read for it.
	Close() error
}

// Sink receives a session's results.
type Sink interface {
	OnError(cloud.ErrorType, error)
	OnStreamOpen(session string)
	OnIntent(*cloud.IntentResult)
	OnConnectionResult(*cloud.ConnectionResult)
}
