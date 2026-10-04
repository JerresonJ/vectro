// Command sdkprobe is a debug aid: it streams robot_state and prints head
// angle, body pitch, accelerometer and status flags as CSV for -secs seconds.
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
	secs := flag.Int("secs", 20, "how long to record")
	every := flag.Duration("every", 100*time.Millisecond, "sample interval")
	flag.Parse()

	v, err := vars.GetVec()
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*secs)*time.Second)
	defer cancel()
	s, err := v.Conn.EventStream(ctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	start := time.Now()
	var last time.Time
	fmt.Println("t_s,head_deg,pitch_deg,lift_mm,ax,ay,az,picked_up,held,on_charger,animating")
	for {
		r, err := s.Recv()
		if err != nil {
			return
		}
		rs := r.GetEvent().GetRobotState()
		if rs == nil || time.Since(last) < *every {
			continue
		}
		last = time.Now()
		a := rs.GetAccel()
		bit := func(b uint32) int {
			if rs.Status&b != 0 {
				return 1
			}
			return 0
		}
		fmt.Printf("%.1f,%.1f,%.1f,%.0f,%.0f,%.0f,%.0f,%d,%d,%d,%d\n",
			time.Since(start).Seconds(),
			float64(rs.HeadAngleRad)*180/math.Pi, float64(rs.PosePitchRad)*180/math.Pi, rs.LiftHeightMm,
			a.GetX(), a.GetY(), a.GetZ(), bit(8), bit(65536), bit(4096), bit(64))
	}
}
