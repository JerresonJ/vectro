// Command headtest is a debug aid: it takes behavior control and moves the
// head down, up and to the middle, printing where the robot thinks the head
// is after each move, so a person can compare with the real head.
package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vector"
	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

func main() {
	v, err := vars.GetVec()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ctrl, err := v.Conn.BehaviorControl(ctx)
	if err != nil {
		fmt.Println("control error:", err)
		return
	}
	ctrl.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRequest{
		ControlRequest: &vectorpb.ControlRequest{Priority: vectorpb.ControlRequest_OVERRIDE_BEHAVIORS},
	}})
	for {
		r, err := ctrl.Recv()
		if err != nil {
			fmt.Println("control error:", err)
			return
		}
		if r.GetControlGrantedResponse() != nil {
			break
		}
	}
	defer ctrl.Send(&vectorpb.BehaviorControlRequest{RequestType: &vectorpb.BehaviorControlRequest_ControlRelease{
		ControlRelease: &vectorpb.ControlRelease{},
	}})
	fmt.Println("control granted")

	for i, target := range []float64{-22, 44, 10} {
		_, err := v.Conn.SetHeadAngle(ctx, &vectorpb.SetHeadAngleRequest{
			AngleRad:          float32(target * math.Pi / 180),
			MaxSpeedRadPerSec: 2, AccelRadPerSec2: 10, DurationSec: 0,
			IdTag: int32(2000001 + i), NumRetries: 1,
		})
		time.Sleep(3 * time.Second)
		fmt.Printf("commanded %4.0f deg -> robot thinks %s (err %v)\n", target, headAngle(ctx, v), err)
	}
}

func headAngle(parent context.Context, v *vector.Vector) string {
	// Close the stream when done; leaked streams pile up as stuck
	// listeners in the gateway.
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	s, err := v.Conn.EventStream(ctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	if err != nil {
		return "?"
	}
	for {
		r, err := s.Recv()
		if err != nil {
			return "?"
		}
		if rs := r.GetEvent().GetRobotState(); rs != nil {
			return fmt.Sprintf("%.1f deg (pitch %.1f)", float64(rs.HeadAngleRad)*180/math.Pi, float64(rs.PosePitchRad)*180/math.Pi)
		}
	}
}
