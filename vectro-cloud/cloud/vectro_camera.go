package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"vectro/cloud/internal/assist"
	"vectro/cloud/internal/log"
	extint "vectro/cloud/internal/proto/external_interface"
)

// cameraHub shares the robot's camera between the live stream, HA still
// images and MQTT snapshots. The engine streams camera frames only while
// someone holds a subscription, so the camera (and its CPU cost) is idle
// when nobody is watching.
type cameraHub struct {
	mu      sync.Mutex
	subs    map[chan []byte]struct{}
	stop    chan struct{}
	latest  []byte
	latestT time.Time
}

var camera = &cameraHub{subs: map[chan []byte]struct{}{}}

// subscribe returns a channel of JPEG frames; call the returned func when
// done. The first subscriber turns the camera stream on, the last one off.
func (h *cameraHub) subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 2)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	if len(h.subs) == 1 {
		h.stop = make(chan struct{})
		go h.run(h.stop)
	}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			if len(h.subs) == 0 && h.stop != nil {
				close(h.stop)
				h.stop = nil
			}
			h.mu.Unlock()
		})
	}
}

func (h *cameraHub) run(stop chan struct{}) {
	svc := &rpcService{}
	f, chunks := engineProtoManager.CreateChannel(&extint.GatewayWrapper_ImageChunk{}, 1024)
	defer f()
	if _, err := svc.EnableImageStreaming(nil, &extint.EnableImageStreamingRequest{Enable: true}); err != nil {
		log.Println("vectro: couldn't start camera stream:", err)
		return
	}
	log.Println("vectro: camera stream on")
	defer func() {
		svc.EnableImageStreaming(nil, &extint.EnableImageStreamingRequest{Enable: false})
		log.Println("vectro: camera stream off")
	}()

	cache := CameraFeedCache{ImageId: -1}
	for {
		select {
		case <-stop:
			return
		case msg, ok := <-chunks:
			if !ok {
				return
			}
			chunk := msg.GetImageChunk()
			if !UnpackCameraImageChunk(chunk, &cache) {
				continue
			}
			switch chunk.GetImageEncoding() {
			case extint.ImageChunk_JPEG_COLOR, extint.ImageChunk_JPEG_GRAY, extint.ImageChunk_JPEG_COLOR_HALF_WIDTH:
			default:
				continue
			}
			frame := append([]byte(nil), cache.Data[:cache.Size]...)
			h.mu.Lock()
			h.latest, h.latestT = frame, time.Now()
			for sub := range h.subs {
				select {
				case sub <- frame:
				default: // slow viewer: skip this frame
				}
			}
			h.mu.Unlock()
		}
	}
}

// lowBatteryVolts: below this, and not charging, the camera stays off. A
// worn battery can't take the extra load and the robot can brown out.
const lowBatteryVolts = 3.7

// batteryTooLow reports whether the camera should stay off to protect a weak
// battery.
func batteryTooLow(ctx context.Context) bool {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	b, err := (&rpcService{}).BatteryState(cctx, &extint.BatteryStateRequest{})
	if err != nil {
		return false // don't block the camera on a failed read
	}
	return !b.GetIsCharging() && b.GetBatteryVolts() > 0 && b.GetBatteryVolts() < lowBatteryVolts
}

// frame returns one fresh frame, using the running stream if there is one.
func (h *cameraHub) frame(ctx context.Context) ([]byte, error) {
	if batteryTooLow(ctx) {
		return nil, errors.New("battery too low for the camera")
	}
	h.mu.Lock()
	if len(h.subs) > 0 && time.Since(h.latestT) < time.Second {
		f := h.latest
		h.mu.Unlock()
		return f, nil
	}
	h.mu.Unlock()
	frames, done := h.subscribe()
	defer done()
	select {
	case f := <-frames:
		return f, nil
	case <-time.After(5 * time.Second):
		return nil, errors.New("no camera frame (is he asleep?)")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Live stream policy.
const (
	streamFPS        = 8
	hotFPS           = 1                // when the CPU is hot
	hotCPU           = 80.0             // °C
	undockedEvery    = 10 * time.Second // still frames when off the charger
	noFrameAfter     = 3 * time.Second  // show the sleeping frame
	dockCheckEvery   = 5 * time.Second
	cameraListenAddr = ":8091"
)

// cameraConfig is the "camera" section of /data/vectro/config.json.
type cameraConfig struct {
	Token string `json:"token"`
}

func loadCameraConfig() (cameraConfig, error) {
	var file struct {
		Camera cameraConfig `json:"camera"`
	}
	data, err := os.ReadFile(assist.DefaultConfigPath)
	if err != nil {
		return cameraConfig{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return cameraConfig{}, err
	}
	if len(file.Camera.Token) < 16 {
		return cameraConfig{}, errors.New("camera.token missing or shorter than 16 characters")
	}
	return file.Camera, nil
}

// serveCamera serves the live MJPEG stream and still images on the LAN for
// Home Assistant's MJPEG camera integration:
//
//	http://<robot>:8091/stream.mjpg?token=…
//	http://<robot>:8091/still.jpg?token=…
func serveCamera(robot vectroRobot) {
	cfg, err := loadCameraConfig()
	if err != nil {
		log.Println("vectro: camera stream disabled:", err)
		return
	}
	authorized := func(r *http.Request) bool {
		return subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(cfg.Token)) == 1
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/still.jpg", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		f, err := camera.frame(r.Context())
		if err != nil {
			f = sleepingFrame
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(f)
	})
	mux.HandleFunc("/stream.mjpg", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		streamMJPEG(w, r, robot)
	})
	srv := &http.Server{Addr: cameraListenAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Println("vectro: camera stream on", cameraListenAddr)
	if err := srv.ListenAndServe(); err != nil {
		log.Println("vectro: camera server stopped:", err)
	}
}

const mjpegBoundary = "vectroframe"

func streamMJPEG(w http.ResponseWriter, r *http.Request, robot vectroRobot) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary="+mjpegBoundary)
	w.Header().Set("Cache-Control", "no-store")
	write := func(f []byte) error {
		if _, err := fmt.Fprintf(w, "--%s\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", mjpegBoundary, len(f)); err != nil {
			return err
		}
		if _, err := w.Write(f); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\r\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	ctx := r.Context()
	log.Println("vectro: live view opened by", r.RemoteAddr)
	defer log.Println("vectro: live view closed by", r.RemoteAddr)

	var frames <-chan []byte
	var unsubscribe func()
	stopLive := func() {
		if unsubscribe != nil {
			unsubscribe()
			unsubscribe, frames = nil, nil
		}
	}
	defer stopLive()

	docked := isDocked(ctx, robot)
	dockCheck := time.NewTicker(dockCheckEvery)
	defer dockCheck.Stop()
	var lastSent time.Time
	lastFrame := time.Now()
	stillTick := time.NewTicker(undockedEvery)
	defer stillTick.Stop()

	if err := write(sleepingFrame); err != nil { // show something right away
		return
	}
	for {
		if docked && frames == nil {
			frames, unsubscribe = camera.subscribe()
			lastFrame = time.Now()
		} else if !docked {
			stopLive()
		}
		select {
		case <-ctx.Done():
			return
		case <-dockCheck.C:
			docked = isDocked(ctx, robot)
			if frames != nil && time.Since(lastFrame) > noFrameAfter {
				if write(sleepingFrame) != nil {
					return
				}
			}
		case f := <-frames:
			lastFrame = time.Now()
			fps := streamFPS
			if cpuTemp() >= hotCPU {
				fps = hotFPS
			}
			if time.Since(lastSent) < time.Second/time.Duration(fps) {
				continue
			}
			lastSent = time.Now()
			if write(f) != nil {
				return
			}
		case <-stillTick.C:
			if docked {
				continue
			}
			f, err := camera.frame(ctx)
			if err != nil {
				f = sleepingFrame
			}
			if write(f) != nil {
				return
			}
		}
	}
}

// isDocked reports whether live streaming is allowed: on the charger with a
// healthy battery. Errors count as not docked so the battery-saving path
// wins.
func isDocked(ctx context.Context, robot vectroRobot) bool {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	b, err := robot.svc.BatteryState(cctx, &extint.BatteryStateRequest{})
	if err != nil || !b.GetIsOnChargerPlatform() {
		return false
	}
	return b.GetIsCharging() || b.GetBatteryVolts() >= lowBatteryVolts
}

// cpuTemp returns the hottest CPU thermal zone in °C.
func cpuTemp() float64 {
	var hottest float64
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		t, _ := os.ReadFile(z + "/type")
		if !strings.HasPrefix(strings.TrimSpace(string(t)), "cpu") {
			continue
		}
		v, _ := os.ReadFile(z + "/temp")
		n, err := strconv.Atoi(strings.TrimSpace(string(v)))
		if err != nil {
			continue
		}
		c := float64(n)
		if c > 1000 {
			c /= 1000
		}
		if c > hottest {
			hottest = c
		}
	}
	return hottest
}

// sleepingFrame is shown while his camera is off: two closed amber eyes.
var sleepingFrame = func() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 640, 360))
	draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{14, 16, 19, 255}}, image.Point{}, draw.Src)
	amber := &image.Uniform{color.RGBA{255, 176, 0, 255}}
	draw.Draw(img, image.Rect(190, 196, 290, 212), amber, image.Point{}, draw.Src)
	draw.Draw(img, image.Rect(350, 196, 450, 212), amber, image.Point{}, draw.Src)
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80})
	return buf.Bytes()
}()
