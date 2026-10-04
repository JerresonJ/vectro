// Command headdrive is a debug aid: with behavior control, it drives the head
// motor by speed (ignoring the position estimate) for a few seconds, then
// stops, printing what the robot thinks the head angle is as it goes.
package main

import (
	"context"
	"flag"
	"fmt"
	"math"
	"time"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

func main() {
	speed := flag.Float64("speed", -1.0, "head speed in rad/s (negative = down)")
	secs := flag.Float64("secs", 3, "how long to drive")
	flag.Parse()

	v, err := vars.GetVec()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	stream, _ := v.Conn.EventStream(sctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	report := func(label string) {
		for {
			r, err := stream.Recv()
			if err != nil {
				return
			}
			if rs := r.GetEvent().GetRobotState(); rs != nil {
				fmt.Printf("%-8s head %6.1f°  pitch %6.1f°\n", label,
					float64(rs.HeadAngleRad)*180/math.Pi, float64(rs.PosePitchRad)*180/math.Pi)
				return
			}
		}
	}

	report("start")
	v.Conn.MoveHead(ctx, &vectorpb.MoveHeadRequest{SpeedRadPerSec: float32(*speed)})
	end := time.Now().Add(time.Duration(*secs * float64(time.Second)))
	for time.Now().Before(end) {
		time.Sleep(500 * time.Millisecond)
		report("driving")
	}
	v.Conn.MoveHead(ctx, &vectorpb.MoveHeadRequest{SpeedRadPerSec: 0})
	time.Sleep(time.Second)
	report("stopped")
}
