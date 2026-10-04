package voice

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"vectro/cloud/internal/clad/cloud"
)

// NullBackend is used when no voice service is configured. It answers every
// utterance with intent_system_noaudio (the engine's "silence" user intent),
// so Vector does a small "didn't hear anything" reaction instead of showing
// a cloud error face, and it reports connection checks as healthy.
type NullBackend struct {
	sessions atomic.Uint64
}

// Open implements Backend.
func (b *NullBackend) Open(ctx context.Context, req Request, sink Sink) (Session, error) {
	ctx, cancel := context.WithCancel(ctx)
	s := &nullSession{ctx: ctx, cancel: cancel, sink: sink, done: make(chan struct{})}
	id := fmt.Sprintf("vectro-null-%d", b.sessions.Add(1))

	if req.ConnectionCheck {
		go s.deliver(func() {
			expected := uint8(DefaultAudioLenMs / DefaultChunkMs)
			sink.OnConnectionResult(&cloud.ConnectionResult{
				Code:            cloud.ConnectionCode_Available,
				Status:          "vectro: no voice backend",
				NumPackets:      expected,
				ExpectedPackets: expected,
			})
		})
		return s, nil
	}

	go s.deliver(func() { sink.OnStreamOpen(id) })
	go func() {
		// Reply when the mic finishes, or after the usual voice timeout if it
		// never says so.
		select {
		case <-s.done:
		case <-time.After(DefaultTimeout):
		case <-ctx.Done():
			return
		}
		s.deliver(func() {
			sink.OnIntent(&cloud.IntentResult{Intent: "intent_system_noaudio"})
		})
	}()
	return s, nil
}

type nullSession struct {
	ctx      context.Context
	cancel   context.CancelFunc
	sink     Sink
	done     chan struct{}
	doneOnce sync.Once
}

// deliver runs fn unless the session has already been closed. Sink methods
// block until Process.Run reads them, so this is always called on its own
// goroutine.
func (s *nullSession) deliver(fn func()) {
	select {
	case <-s.ctx.Done():
		return
	default:
	}
	fn()
}

func (s *nullSession) AddSamples([]int16) {}

func (s *nullSession) CloseSend() error {
	s.doneOnce.Do(func() { close(s.done) })
	return nil
}

func (s *nullSession) Close() error {
	s.cancel()
	return nil
}
