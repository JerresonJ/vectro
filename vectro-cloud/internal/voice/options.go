package voice

import (
	"vectro/cloud/internal/token"
	"vectro/cloud/internal/util"
)

// Handler selected a speech service on DDL's chipper server. Vectro keeps the
// type so existing flags still parse, but backends are chosen with WithBackend.
type Handler int

const (
	// HandlerDefault lets the backend decide
	HandlerDefault Handler = iota
	// HandlerGoogle is kept for flag compatibility
	HandlerGoogle
	// HandlerMicrosoft is kept for flag compatibility
	HandlerMicrosoft
	// HandlerAmazon is kept for flag compatibility
	HandlerAmazon
)

// Option defines an option that can be set on the cloud process
type Option func(o *options)

type options struct {
	backend      Backend
	compress     bool
	chunkMs      uint
	handler      Handler
	saveAudio    bool
	tokener      token.Accessor
	requireToken bool
	errListener  util.ErrorListener
}

// WithBackend sets the service that turns utterances into intents. Without
// one, Process uses NullBackend.
func WithBackend(value Backend) Option {
	return func(o *options) {
		o.backend = value
	}
}

// WithCompression sets whether compression will be performed on audio
// before uploading (and returns the same Options struct to allow method
// chaining)
func WithCompression(value bool) Option {
	return func(o *options) {
		o.compress = value
	}
}

// WithChunkMs determines how often the cloud process will stream data to the cloud
func WithChunkMs(value uint) Option {
	return func(o *options) {
		o.chunkMs = value
	}
}

// WithHandler records the requested speech service (unused by Vectro backends)
func WithHandler(value Handler) Option {
	return func(o *options) {
		o.handler = value
	}
}

// WithSaveAudio sets whether the backend may save the audio we send to it
func WithSaveAudio(value bool) Option {
	return func(o *options) {
		o.saveAudio = value
	}
}

// WithRequireToken specifies that a failure to obtain an access token from the TMS
// should be treated as a fatal error for requests
func WithRequireToken() Option {
	return func(o *options) {
		o.requireToken = true
	}
}

// WithTokener sets the accessor that should be used to obtain tokens
func WithTokener(value token.Accessor) Option {
	return func(o *options) {
		o.tokener = value
	}
}

// WithErrorListener specifies that the given ErrorListener should be passed errors
// that result from voice requests
func WithErrorListener(value util.ErrorListener) Option {
	return func(o *options) {
		o.errListener = value
	}
}
