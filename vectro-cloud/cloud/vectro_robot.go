package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"vectro/cloud/internal/hamqtt"
	extint "vectro/cloud/internal/proto/external_interface"
)

// vectroRobot gives the Home Assistant bridge access to the robot by calling
// the gateway's handlers in-process.
type vectroRobot struct {
	engineVoice
	svc *rpcService
}

var _ hamqtt.Robot = vectroRobot{}

func newVectroRobot() vectroRobot { return vectroRobot{svc: &rpcService{}} }

// Action tags must be in the engine's SDK range (2000001-3000000) or it
// rejects the action. vectro-ui counts up from 2000100; Vectro's vic-cloud
// uses the top half.
const (
	firstTag = 2500000
	lastTag  = 3000000
)

var actionTag atomic.Int32

func init() { actionTag.Store(firstTag) }

func nextTag() int32 {
	t := actionTag.Add(1)
	if t >= lastTag {
		actionTag.Store(firstTag)
		t = actionTag.Add(1)
	}
	return t
}

func deg(d float64) float32 { return float32(d * math.Pi / 180) }

// actionResult turns an action response's result code into an error.
func actionResult(name string, r interface{ GetResult() *extint.ActionResult }, err error) error {
	if err != nil {
		return err
	}
	if code := r.GetResult().GetCode(); code != extint.ActionResult_ACTION_RESULT_SUCCESS {
		return fmt.Errorf("%s: %s", name, code)
	}
	return nil
}

// Commands lists the movement commands Home Assistant gets buttons for.
var robotCommands = []hamqtt.Command{
	{ID: "forward", Name: "Drive forward", Icon: "mdi:arrow-up-bold"},
	{ID: "backward", Name: "Drive backward", Icon: "mdi:arrow-down-bold"},
	{ID: "turn_left", Name: "Turn left", Icon: "mdi:rotate-left"},
	{ID: "turn_right", Name: "Turn right", Icon: "mdi:rotate-right"},
	{ID: "head_up", Name: "Head up", Icon: "mdi:arrow-up-circle"},
	{ID: "head_middle", Name: "Head level", Icon: "mdi:circle-outline"},
	{ID: "head_down", Name: "Head down", Icon: "mdi:arrow-down-circle"},
	{ID: "lift_up", Name: "Lift up", Icon: "mdi:forklift"},
	{ID: "lift_down", Name: "Lift down", Icon: "mdi:arrow-collapse-down"},
	{ID: "dock", Name: "Go to charger", Icon: "mdi:battery-charging"},
	{ID: "undock", Name: "Drive off charger", Icon: "mdi:robot-outline"},
}

func (r vectroRobot) Commands() []hamqtt.Command { return robotCommands }

func (r vectroRobot) Command(ctx context.Context, id string) error {
	return withControl(ctx, func(ctx context.Context) error {
		s := r.svc
		switch id {
		case "forward", "backward":
			dist := float32(100)
			if id == "backward" {
				dist = -100
			}
			resp, err := s.DriveStraight(ctx, &extint.DriveStraightRequest{SpeedMmps: 80, DistMm: dist, IdTag: nextTag()})
			return actionResult(id, resp, err)
		case "turn_left", "turn_right":
			angle := deg(90)
			if id == "turn_right" {
				angle = -angle
			}
			resp, err := s.TurnInPlace(ctx, &extint.TurnInPlaceRequest{AngleRad: angle, SpeedRadPerSec: deg(120), AccelRadPerSec2: deg(240), TolRad: deg(2), IdTag: nextTag()})
			return actionResult(id, resp, err)
		case "head_up", "head_middle", "head_down":
			target := map[string]float64{"head_up": 44, "head_middle": 10, "head_down": -22}[id]
			resp, err := s.SetHeadAngle(ctx, &extint.SetHeadAngleRequest{AngleRad: deg(target), MaxSpeedRadPerSec: 3, AccelRadPerSec2: 10, IdTag: nextTag()})
			return actionResult(id, resp, err)
		case "lift_up", "lift_down":
			height := float32(92)
			if id == "lift_down" {
				height = 32
			}
			resp, err := s.SetLiftHeight(ctx, &extint.SetLiftHeightRequest{HeightMm: height, MaxSpeedRadPerSec: 3, AccelRadPerSec2: 10, IdTag: nextTag()})
			return actionResult(id, resp, err)
		case "dock":
			_, err := s.DriveOnCharger(ctx, &extint.DriveOnChargerRequest{})
			return err
		case "undock":
			_, err := s.DriveOffCharger(ctx, &extint.DriveOffChargerRequest{})
			return err
		}
		return fmt.Errorf("unknown command %q", id)
	})
}

// Snapshot captures one camera frame as JPEG, sharing the camera with the
// live stream (see cameraHub).
func (r vectroRobot) Snapshot(ctx context.Context) ([]byte, error) {
	return camera.frame(ctx)
}

var eyeColorNames = []struct {
	name  string
	color extint.EyeColor
}{
	{"Teal", extint.EyeColor_TIP_OVER_TEAL},
	{"Orange", extint.EyeColor_OVERFIT_ORANGE},
	{"Yellow", extint.EyeColor_UNCANNY_YELLOW},
	{"Lime", extint.EyeColor_NON_LINEAR_LIME},
	{"Sapphire", extint.EyeColor_SINGULARITY_SAPPHIRE},
	{"Purple", extint.EyeColor_FALSE_POSITIVE_PURPLE},
	{"Green", extint.EyeColor_CONFUSION_MATRIX_GREEN},
	{"Rainbow", extint.EyeColor_RAINBOW_EYES},
	{"Red", extint.EyeColor_ROBOT_RED},
}

var volumeNames = []struct {
	name  string
	level extint.MasterVolumeLevel
}{
	{"Low", extint.MasterVolumeLevel_VOLUME_LOW},
	{"Medium low", extint.MasterVolumeLevel_VOLUME_MEDIUM_LOW},
	{"Medium", extint.MasterVolumeLevel_VOLUME_MEDIUM},
	{"Medium high", extint.MasterVolumeLevel_VOLUME_MEDIUM_HIGH},
	{"High", extint.MasterVolumeLevel_VOLUME_HIGH},
}

func (r vectroRobot) EyeColors() []string {
	out := make([]string, len(eyeColorNames))
	for i, c := range eyeColorNames {
		out[i] = c.name
	}
	return out
}

func (r vectroRobot) Volumes() []string {
	out := make([]string, len(volumeNames))
	for i, v := range volumeNames {
		out[i] = v.name
	}
	return out
}

func (r vectroRobot) SetEyeColor(ctx context.Context, name string) error {
	for _, c := range eyeColorNames {
		if strings.EqualFold(c.name, name) {
			_, err := r.svc.UpdateSettings(ctx, &extint.UpdateSettingsRequest{Settings: &extint.RobotSettingsConfig{
				OneofEyeColor: &extint.RobotSettingsConfig_EyeColor{EyeColor: c.color},
			}})
			return err
		}
	}
	return fmt.Errorf("unknown eye color %q", name)
}

func (r vectroRobot) SetVolume(ctx context.Context, name string) error {
	for _, v := range volumeNames {
		if strings.EqualFold(v.name, name) {
			_, err := r.svc.SetMasterVolume(ctx, &extint.MasterVolumeRequest{VolumeLevel: v.level})
			return err
		}
	}
	return fmt.Errorf("unknown volume %q", name)
}

var robotStatusNames = []struct {
	bit  uint32
	name string
}{
	// Most specific first: State reports the first that applies.
	{32, "falling"}, {8, "picked up"}, {65536, "held"}, {16384, "cliff detected"},
	{128, "driving"}, {32768, "moving"}, {8192, "charging"}, {4096, "docked"},
	{1024, "sleeping"}, {64, "animating"},
}

// State reports battery and activity for Home Assistant's sensors.
func (r vectroRobot) State(ctx context.Context) (hamqtt.State, error) {
	var st hamqtt.State
	b, err := r.svc.BatteryState(ctx, &extint.BatteryStateRequest{})
	if err != nil {
		return st, err
	}
	st.BatteryVolts = math.Round(float64(b.GetBatteryVolts())*100) / 100
	st.BatteryLevel = strings.ToLower(strings.TrimPrefix(b.GetBatteryLevel().String(), "BATTERY_LEVEL_"))
	st.Charging = b.GetIsCharging()
	st.OnCharger = b.GetIsOnChargerPlatform()

	status, err := robotStatus(ctx)
	if err != nil {
		st.Activity = "unknown"
		return st, nil
	}
	// In calm power mode the camera is off, so he can't see faces.
	st.CameraActive = status&1024 == 0
	st.Activity = "idle"
	for _, s := range robotStatusNames {
		if status&s.bit != 0 {
			st.Activity = s.name
			break
		}
	}
	return st, nil
}

// robotStatus waits briefly for the engine's next robot_state event.
func robotStatus(ctx context.Context) (uint32, error) {
	f, events := engineProtoManager.CreateChannel(&extint.GatewayWrapper_Event{}, 64)
	defer f()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case msg, ok := <-events:
			if !ok {
				return 0, errors.New("event channel closed")
			}
			if rs := msg.GetEvent().GetRobotState(); rs != nil {
				return rs.GetStatus(), nil
			}
		case <-timeout:
			return 0, errors.New("no robot state")
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// WatchFaces calls fn with the name of every face the robot observes ("" for
// someone he doesn't know) until ctx is done. The engine reports faces many
// times a second while they're in view; callers throttle.
func (r vectroRobot) WatchFaces(ctx context.Context, fn func(name string)) {
	for ctx.Err() == nil {
		// Keep draining: a listener that falls behind gets dropped by the
		// IPC manager, and then we re-subscribe.
		f, events := engineProtoManager.CreateChannel(&extint.GatewayWrapper_Event{}, 512)
	drain:
		for {
			select {
			case <-ctx.Done():
				break drain
			case msg, ok := <-events:
				if !ok {
					break drain
				}
				if face := msg.GetEvent().GetRobotObservedFace(); face != nil {
					fn(face.GetName())
				}
			}
		}
		f()
		select {
		case <-ctx.Done():
		case <-time.After(time.Second):
		}
	}
}
