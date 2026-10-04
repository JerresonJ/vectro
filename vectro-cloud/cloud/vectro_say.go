package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"vectro/cloud/internal/log"
	extint "vectro/cloud/internal/proto/external_interface"
)

// engineVoice makes Vector speak through the engine, the same way the SDK
// does: take behavior control, SayText in his own voice, release control.
type engineVoice struct{}

// controlHold is how long behavior control is kept after a command, so a
// burst of commands (driving with HA buttons) runs back to back instead of
// re-acquiring control, which takes seconds each time.
const controlHold = 5 * time.Second

// control is Vectro's behavior control session. Everything Vectro does
// under control (speech, movement) goes through withControl, one at a time.
var control struct {
	mu       sync.Mutex // held while a command runs
	connID   uint64
	active   bool
	lost     chan struct{} // closed when the engine takes control back
	release  *time.Timer
	cancelCh func() // removes the response listener
}

// withControl runs fn while holding behavior control at DEFAULT priority,
// acquiring it if needed and releasing it controlHold after the last use.
func withControl(ctx context.Context, fn func(ctx context.Context) error) error {
	control.mu.Lock()
	defer control.mu.Unlock()

	if control.active {
		select {
		case <-control.lost:
			endControlLocked(false)
		default:
		}
	}
	if !control.active {
		if err := acquireControlLocked(ctx); err != nil {
			return err
		}
	}
	if control.release != nil {
		control.release.Stop()
	}
	err := fn(ctx)
	control.release = time.AfterFunc(controlHold, func() {
		control.mu.Lock()
		defer control.mu.Unlock()
		if control.active {
			endControlLocked(true)
		}
	})
	return err
}

func acquireControlLocked(ctx context.Context) error {
	connID := rand.Uint64()
	cancel, responses := engineProtoManager.CreateChannel(&extint.GatewayWrapper_BehaviorControlResponse{}, 8)
	if _, err := engineProtoManager.WriteWithID(&extint.GatewayWrapper{
		OneofMessageType: &extint.GatewayWrapper_ControlRequest{
			ControlRequest: &extint.ControlRequest{Priority: extint.ControlRequest_DEFAULT},
		},
	}, connID); err != nil {
		cancel()
		return err
	}

	requested := time.Now()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case msg, ok := <-responses:
			if !ok {
				cancel()
				return errors.New("engine closed behavior control channel")
			}
			if msg.GetBehaviorControlResponse().GetControlGrantedResponse() == nil {
				continue
			}
			log.Println(fmt.Sprintf("vectro: behavior control granted in %dms", time.Since(requested).Milliseconds()))
			lost := make(chan struct{})
			control.connID, control.active, control.lost, control.cancelCh = connID, true, lost, cancel
			// Watch for the engine taking control back (e.g. he's picked up).
			go func() {
				for msg := range responses {
					if msg.GetBehaviorControlResponse().GetControlLostEvent() != nil {
						close(lost)
						return
					}
				}
			}()
			return nil
		case <-timeout:
			releaseControl(connID)
			cancel()
			return errors.New("timed out waiting for behavior control")
		case <-ctx.Done():
			releaseControl(connID)
			cancel()
			return ctx.Err()
		}
	}
}

// endControlLocked releases control (if we still hold it) and resets the
// session.
func endControlLocked(release bool) {
	if release {
		releaseControl(control.connID)
	}
	if control.cancelCh != nil {
		control.cancelCh()
	}
	control.active = false
	control.cancelCh = nil
}

func releaseControl(connID uint64) {
	engineProtoManager.WriteWithID(&extint.GatewayWrapper{
		OneofMessageType: &extint.GatewayWrapper_ControlRelease{ControlRelease: &extint.ControlRelease{}},
	}, connID)
}

func (engineVoice) Say(ctx context.Context, text string) error {
	return withControl(ctx, func(ctx context.Context) error {
		g, said := engineProtoManager.CreateChannel(&extint.GatewayWrapper_SayTextResponse{}, 4)
		defer g()
		if _, _, err := engineProtoManager.Write(&extint.GatewayWrapper{
			OneofMessageType: &extint.GatewayWrapper_SayTextRequest{
				SayTextRequest: &extint.SayTextRequest{Text: text, UseVectorVoice: true, DurationScalar: 1},
			},
		}); err != nil {
			return err
		}
		for {
			select {
			case msg, ok := <-said:
				if !ok {
					return errors.New("engine closed say text channel")
				}
				switch msg.GetSayTextResponse().GetState() {
				case extint.SayTextResponse_FINISHED:
					return nil
				case extint.SayTextResponse_INVALID:
					return errors.New("engine rejected say text")
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	})
}
