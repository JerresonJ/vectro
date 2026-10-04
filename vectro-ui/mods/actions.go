package mods

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vector"
	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

// Actions are one-click robot commands for the Overview page.
type Actions struct {
	vars.Modification
}

func NewActions() *Actions { return &Actions{} }

func (m *Actions) Name() string        { return "Actions" }
func (m *Actions) Description() string { return "Robot commands: head re-sync, say, dock, restarts." }
func (m *Actions) Load() error {
	go headGuard(context.Background())
	return nil
}

// actionMu keeps actions (including the head guard) from overlapping.
var actionMu sync.Mutex

func (m *Actions) HTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		vars.HTTPError(w, r, "POST required")
		return
	}
	action := strings.TrimPrefix(r.URL.Path, "/api/mods/Actions/")
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	var err error
	switch action {
	case "headResync":
		err = withRobot(ctx, true, headSweep)
		if err == nil {
			recordResync("manual")
		}
	case "calibrateHead":
		// Head and lift calibrate when vic-robot starts; restarting the
		// robot software is the only way to trigger that from outside.
		vars.HTTPSuccess(w, r)
		go func() {
			recordResync("manual calibration")
			calibrateHead()
		}()
		return
	case "restartVoice":
		err = restartVectroCloud()
	case "restartRobot":
		// Respond first: the restart takes the gateway down for a while.
		vars.HTTPSuccess(w, r)
		go vars.RestartVic()
		return
	case "reboot":
		vars.HTTPSuccess(w, r)
		go func() {
			time.Sleep(time.Second)
			exec.Command("/bin/sh", "-c", "sync; reboot").Run()
		}()
		return
	default:
		vars.HTTPError(w, r, "unknown action")
		return
	}
	if err != nil {
		vars.HTTPError(w, r, err.Error())
		return
	}
	vars.HTTPSuccess(w, r)
}

// withRobot connects to the SDK and, if control is set, holds behavior
// control while fn runs so the robot's own behaviors don't interfere.
func withRobot(ctx context.Context, control bool, fn func(context.Context, *vector.Vector) error) error {
	actionMu.Lock()
	defer actionMu.Unlock()
	v, err := vars.GetVec()
	if err != nil {
		return err
	}
	if !control {
		return fn(ctx, v)
	}

	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := v.Conn.BehaviorControl(cctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
		ControlRequest: &vectorpb.ControlRequest{Priority: vectorpb.ControlRequest_OVERRIDE_BEHAVIORS},
	}}); err != nil {
		return err
	}
	granted := make(chan error, 1)
	go func() {
		for {
			r, err := stream.Recv()
			if err != nil {
				granted <- err
				return
			}
			if r.GetControlGrantedResponse() != nil {
				granted <- nil
				return
			}
		}
	}()
	select {
	case err := <-granted:
		if err != nil {
			return err
		}
	case <-time.After(10 * time.Second):
		return errors.New("timed out waiting for behavior control")
	}
	defer stream.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRelease{
		ControlRelease: &vectorpb.ControlRelease{},
	}})
	return fn(ctx, v)
}

// actionTag numbers SDK actions; the engine rejects actions without one.
var actionTag int32 = 2000100

// headSweep drives the head to the top, the bottom, the top again and the
// middle. When the head position estimate has drifted from the real head (a
// slipping head gear), some of these moves stall against an end stop the
// robot didn't expect; that's fine, because hitting both stops is what
// brings the estimate back in line. It fails only if no move worked.
func headSweep(ctx context.Context, v *vector.Vector) error {
	var lastErr error
	ok := 0
	for _, deg := range []float64{44, -22, 44, 10} {
		actionTag++
		resp, err := v.Conn.SetHeadAngle(ctx, &vectorpb.SetHeadAngleRequest{
			AngleRad:          float32(deg * math.Pi / 180),
			MaxSpeedRadPerSec: 2,
			AccelRadPerSec2:   10,
			IdTag:             actionTag,
			NumRetries:        0,
		})
		switch {
		case err != nil:
			if ctx.Err() != nil {
				return err
			}
			lastErr = err
		case resp.GetResult().GetCode() != vectorpb.ActionResult_ACTION_RESULT_SUCCESS:
			fmt.Printf("vectro-ui: head move to %.0f°: %s (expected while re-syncing)\n", deg, resp.GetResult().GetCode())
			ok++ // a stall at an end stop still moved the head there
		default:
			ok++
		}
		time.Sleep(300 * time.Millisecond)
	}
	if ok == 0 {
		return fmt.Errorf("head didn't move: %v", lastErr)
	}
	return nil
}

// calibrateHead restarts the robot software (the safe way, see
// vars.RestartVic) so vic-robot recalibrates the head and lift at startup,
// then waits for the gateway to come back.
func calibrateHead() {
	actionMu.Lock()
	defer actionMu.Unlock()
	vars.RestartVic()
	for i := 0; i < 30; i++ {
		if exec.Command("systemctl", "is-active", "--quiet", "vic-cloud", "vic-engine", "vic-anim", "vic-robot").Run() == nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	time.Sleep(15 * time.Second) // gateway and engine settle
}

// Head guard: the false "picked up" state comes from the head position
// estimate disagreeing with the real head (the head encoder drops counts).
// Detect it from robot_state and recalibrate automatically.

const (
	guardTilt     = 25.0             // degrees of body pitch that can't be real on the charger
	guardHold     = 6 * time.Second  // how long it must persist
	guardCooldown = 2 * time.Minute  // between automatic re-syncs
	guardBackoff  = 30 * time.Minute // after a re-sync that didn't help
	guardMaxFails = 3                // then stop until the UI restarts
	guardRetry    = 10 * time.Second // reconnect delay
)

// guardFails counts automatic re-syncs in a row that didn't clear the
// problem. Driving a stalling head motor over and over is hard on a worn
// gear, so the guard backs off and then gives up.
var guardFails int

var (
	resyncMu   sync.Mutex
	lastResync struct {
		At     time.Time `json:"at"`
		Reason string    `json:"reason"`
		Count  int       `json:"count"`
	}
)

func recordResync(reason string) {
	resyncMu.Lock()
	defer resyncMu.Unlock()
	lastResync.At = time.Now()
	lastResync.Reason = reason
	lastResync.Count++
	fmt.Println("vectro-ui: head re-synced (" + reason + ")")
}

// resyncInfo is shown on the Overview page.
func resyncInfo() map[string]any {
	resyncMu.Lock()
	defer resyncMu.Unlock()
	if lastResync.Count == 0 {
		return map[string]any{"count": 0}
	}
	b, _ := json.Marshal(lastResync)
	var out map[string]any
	json.Unmarshal(b, &out)
	out["guard_failures"] = guardFails
	out["guard_stopped"] = guardFails >= guardMaxFails
	return out
}

func headGuard(ctx context.Context) {
	for ctx.Err() == nil {
		if err := watchHead(ctx); err != nil && ctx.Err() == nil {
			time.Sleep(guardRetry)
		}
	}
}

func watchHead(ctx context.Context) error {
	v, err := vars.GetVec()
	if err != nil {
		return err
	}
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := v.Conn.EventStream(sctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	if err != nil {
		return err
	}
	var since time.Time
	for {
		r, err := stream.Recv()
		if err != nil {
			return err
		}
		rs := r.GetEvent().GetRobotState()
		if rs == nil {
			continue
		}
		const pickedUp, onCharger, wheelsMoving = 8, 4096, 32768
		pitch := math.Abs(float64(rs.PosePitchRad) * 180 / math.Pi)
		suspicious := rs.Status&onCharger != 0 && rs.Status&pickedUp != 0 &&
			rs.Status&wheelsMoving == 0 && pitch > guardTilt
		if !suspicious {
			since = time.Time{}
			continue
		}
		if since.IsZero() {
			since = time.Now()
			continue
		}
		resyncMu.Lock()
		wait := guardCooldown
		if guardFails > 0 {
			wait = guardBackoff
		}
		cooling := time.Since(lastResync.At) < wait
		gaveUp := guardFails >= guardMaxFails
		resyncMu.Unlock()
		if time.Since(since) < guardHold || cooling || gaveUp {
			continue
		}
		fmt.Printf("vectro-ui: on charger but 'picked up' at %.0f° pitch for %v; recalibrating head\n",
			pitch, time.Since(since).Round(time.Second))
		recordResync("automatic calibration")
		calibrateHead()
		// The restart dropped our SDK connection; judge by a fresh one.
		fixed := false
		if v2, err := vars.GetVec(); err == nil {
			fixed = !stillSuspicious(ctx, v2)
		}
		resyncMu.Lock()
		if fixed {
			guardFails = 0
		} else {
			guardFails++
			lastResync.Reason = fmt.Sprintf("automatic calibration, didn't help (%d/%d)", guardFails, guardMaxFails)
			if guardFails >= guardMaxFails {
				fmt.Println("vectro-ui: head recalibration isn't helping; likely a head encoder problem, guard stopped")
			}
		}
		resyncMu.Unlock()
		since = time.Time{}
	}
}

// stillSuspicious takes one fresh robot_state reading and reports whether
// the robot still thinks it's picked up at an impossible tilt on the charger.
func stillSuspicious(ctx context.Context, v *vector.Vector) bool {
	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stream, err := v.Conn.EventStream(sctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	if err != nil {
		return true
	}
	for {
		r, err := stream.Recv()
		if err != nil {
			return true
		}
		if rs := r.GetEvent().GetRobotState(); rs != nil {
			const pickedUp, onCharger = 8, 4096
			pitch := math.Abs(float64(rs.PosePitchRad) * 180 / math.Pi)
			return rs.Status&onCharger != 0 && rs.Status&pickedUp != 0 && pitch > guardTilt
		}
	}
}
