package mods

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

// Diagnostics reports the robot's health on one page: system, network,
// services, live sensors, Vectro's Home Assistant links and recent voice
// requests.
type Diagnostics struct {
	vars.Modification
}

func NewDiagnostics() *Diagnostics { return &Diagnostics{} }

func (m *Diagnostics) Name() string        { return "Diagnostics" }
func (m *Diagnostics) Description() string { return "Robot health and Vectro status." }
func (m *Diagnostics) Load() error         { return nil }

// robotServices are the units that make up Vector's software.
var robotServices = []string{"vic-robot", "vic-anim", "vic-engine", "vic-cloud", "vic-switchboard", "wired"}

func (m *Diagnostics) HTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/api/mods/Diagnostics/") {
	case "report":
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
		defer cancel()
		// Sections run in parallel, each with its own deadline, so one slow
		// source can't hang the page.
		sections := map[string]func(context.Context) any{
			"system":   func(context.Context) any { return systemInfo() },
			"network":  func(ctx context.Context) any { return networkInfo(ctx) },
			"services": func(ctx context.Context) any { return serviceStates(ctx) },
			"robot":    func(ctx context.Context) any { return robotInfo(ctx) },
			"vectro":   func(ctx context.Context) any { return vectroLinks(ctx) },
			"voice":    func(ctx context.Context) any { return recentVoice(ctx) },
		}
		report := map[string]any{
			"time":        time.Now().Format(time.RFC3339),
			"fault":       pendingFault(),
			"head_resync": resyncInfo(),
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		for name, fn := range sections {
			wg.Add(1)
			go func(name string, fn func(context.Context) any) {
				defer wg.Done()
				start := time.Now()
				v := runWithin(ctx, 6*time.Second, fn)
				if d := time.Since(start); d > time.Second {
					fmt.Printf("vectro-ui: diagnostics %s took %v\n", name, d.Round(time.Millisecond))
				}
				mu.Lock()
				report[name] = v
				mu.Unlock()
			}(name, fn)
		}
		wg.Wait()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(report)
	case "logs":
		unit := r.FormValue("unit")
		if !contains(robotServices, unit) {
			vars.HTTPError(w, r, "unknown unit")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		out, _ := exec.CommandContext(ctx, "journalctl", "-u", unit, "-n", "150", "--no-pager", "-o", "short").Output()
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(out)
	default:
		vars.HTTPError(w, r, "404 not found")
	}
}

// runWithin runs fn with a deadline and returns its result, or a timeout
// marker if it doesn't finish in time.
func runWithin(parent context.Context, d time.Duration, fn func(context.Context) any) any {
	ctx, cancel := context.WithTimeout(parent, d)
	defer cancel()
	done := make(chan any, 1)
	go func() { done <- fn(ctx) }()
	select {
	case v := <-done:
		return v
	case <-ctx.Done():
		return map[string]any{"error": "timed out"}
	}
}

func systemInfo() map[string]any {
	info := map[string]any{}
	info["hostname"], _ = os.Hostname()
	info["os_version"] = strings.TrimSpace(readOr("/etc/os-version", ""))
	if up, err := strconv.ParseFloat(strings.Fields(readOr("/proc/uptime", "0"))[0], 64); err == nil {
		info["uptime_s"] = int(up)
	}
	if f := strings.Fields(readOr("/proc/loadavg", "")); len(f) >= 3 {
		info["load"] = f[:3]
	}
	mem := meminfo()
	info["mem_total_mb"] = mem["MemTotal"] / 1024
	info["mem_available_mb"] = mem["MemAvailable"] / 1024
	info["disk_root"] = diskUsage("/")
	info["disk_data"] = diskUsage("/data")
	info["cpu_temp_c"] = cpuTemp()
	if khz, err := strconv.Atoi(strings.TrimSpace(readOr("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq", ""))); err == nil {
		info["cpu_mhz"] = khz / 1000
	}
	return info
}

func meminfo() map[string]int {
	out := map[string]int{}
	for _, line := range strings.Split(readOr("/proc/meminfo", ""), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 {
			v, _ := strconv.Atoi(f[1])
			out[strings.TrimSuffix(f[0], ":")] = v
		}
	}
	return out
}

func diskUsage(path string) map[string]int {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return nil
	}
	bs := uint64(st.Bsize)
	return map[string]int{
		"total_mb": int(st.Blocks * bs / (1 << 20)),
		"free_mb":  int(st.Bavail * bs / (1 << 20)),
	}
}

// cpuTemp returns the hottest CPU thermal zone in degrees C. (Other zones
// such as xo-therm report bogus values like 125 C.)
func cpuTemp() float64 {
	var hottest float64
	zones, _ := filepath.Glob("/sys/class/thermal/thermal_zone*")
	for _, z := range zones {
		if !strings.HasPrefix(strings.TrimSpace(readOr(z+"/type", "")), "cpu") {
			continue
		}
		v, err := strconv.Atoi(strings.TrimSpace(readOr(z+"/temp", "")))
		if err != nil {
			continue
		}
		c := float64(v)
		if c > 1000 { // millidegrees
			c /= 1000
		}
		hottest = math.Max(hottest, c)
	}
	return hottest
}

func networkInfo(ctx context.Context) map[string]any {
	info := map[string]any{}
	if iface, err := net.InterfaceByName("wlan0"); err == nil {
		info["mac"] = iface.HardwareAddr.String()
		if addrs, err := iface.Addrs(); err == nil {
			for _, a := range addrs {
				if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
					info["ip"] = ipn.IP.String()
				}
			}
		}
	}
	// /proc/net/wireless: "wlan0: 0000   70.  -40.  -256 ..."
	for _, line := range strings.Split(readOr("/proc/net/wireless", ""), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && strings.HasPrefix(f[0], "wlan0") {
			info["link_quality"] = strings.TrimSuffix(f[2], ".")
			info["signal_dbm"] = strings.TrimSuffix(f[3], ".")
		}
	}
	if out, err := exec.CommandContext(ctx, "wpa_cli", "-i", "wlan0", "status").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if k, v, ok := strings.Cut(line, "="); ok && (k == "ssid" || k == "bssid" || k == "freq") {
				info[k] = v
			}
		}
	}
	return info
}

func serviceStates(ctx context.Context) map[string]string {
	states := map[string]string{}
	out, _ := exec.CommandContext(ctx, "systemctl", append([]string{"is-active"}, robotServices...)...).Output()
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for i, s := range robotServices {
		if i < len(lines) {
			states[s] = strings.TrimSpace(lines[i])
		}
	}
	return states
}

var statusFlags = []struct {
	bit  uint32
	name string
}{
	{1, "moving"}, {2, "carrying block"}, {8, "picked up"}, {16, "button pressed"},
	{32, "falling"}, {64, "animating"}, {128, "pathing"}, {1024, "calm power mode"},
	{4096, "on charger"}, {8192, "charging"}, {16384, "cliff detected"},
	{32768, "wheels moving"}, {65536, "being held"}, {131072, "motion detected"},
}

// robotInfo reads battery state and one robot_state event over the local
// SDK connection.
func robotInfo(ctx context.Context) map[string]any {
	info := map[string]any{}
	v, err := vars.GetVec()
	if err != nil {
		info["error"] = "SDK unavailable: " + err.Error()
		return info
	}
	if b, err := v.Conn.BatteryState(ctx, &vectorpb.BatteryStateRequest{}); err == nil {
		info["battery_volts"] = round(float64(b.BatteryVolts), 2)
		info["battery_level"] = strings.ToLower(strings.TrimPrefix(b.BatteryLevel.String(), "BATTERY_LEVEL_"))
		info["charging"] = b.IsCharging
		info["on_charger"] = b.IsOnChargerPlatform
	} else {
		info["battery_error"] = err.Error()
	}

	sctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stream, err := v.Conn.EventStream(sctx, &vectorpb.EventRequest{
		ListType: &vectorpb.EventRequest_WhiteList{WhiteList: &vectorpb.FilterList{List: []string{"robot_state"}}},
	})
	if err != nil {
		info["state_error"] = err.Error()
		return info
	}
	for {
		resp, err := stream.Recv()
		if err != nil {
			info["state_error"] = err.Error()
			return info
		}
		rs := resp.GetEvent().GetRobotState()
		if rs == nil {
			continue
		}
		info["head_angle_deg"] = round(float64(rs.HeadAngleRad)*180/math.Pi, 1)
		info["lift_height_mm"] = round(float64(rs.LiftHeightMm), 1)
		info["pitch_deg"] = round(float64(rs.PosePitchRad)*180/math.Pi, 1)
		if a := rs.Accel; a != nil {
			info["accel"] = []float64{round(float64(a.X), 0), round(float64(a.Y), 0), round(float64(a.Z), 0)}
		}
		if g := rs.Gyro; g != nil {
			info["gyro"] = []float64{round(float64(g.X), 3), round(float64(g.Y), 3), round(float64(g.Z), 3)}
		}
		if t := rs.TouchData; t != nil {
			info["touched"] = t.IsBeingTouched
		}
		if p := rs.ProxData; p != nil {
			info["prox_mm"] = p.DistanceMm
		}
		var flags []string
		for _, f := range statusFlags {
			if rs.Status&f.bit != 0 {
				flags = append(flags, f.name)
			}
		}
		info["status"] = flags
		return info
	}
}

// vectroLinks checks the services Vectro's vic-cloud depends on.
func vectroLinks(ctx context.Context) map[string]any {
	cfg, err := loadVectroConfig()
	if err != nil {
		return map[string]any{"error": "no Vectro config: " + err.Error()}
	}
	out := map[string]any{}
	if cfg.Voice.HAURL != "" {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Voice.HAURL+"/api/", nil)
		req.Header.Set("Authorization", "Bearer "+cfg.Voice.HAToken)
		start := time.Now()
		resp, err := (&http.Client{Timeout: 4 * time.Second}).Do(req)
		switch {
		case err != nil:
			out["home_assistant"] = link(false, err.Error(), 0)
		case resp.StatusCode == http.StatusOK:
			out["home_assistant"] = link(true, "API running", time.Since(start))
		default:
			out["home_assistant"] = link(false, resp.Status, time.Since(start))
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
	if cfg.Voice.Whisper != "" {
		out["whisper"] = tcpLink(cfg.Voice.Whisper)
	}
	if b := cfg.MQTT.Broker; b != "" {
		out["mqtt"] = tcpLink(strings.TrimPrefix(strings.TrimPrefix(b, "tcp://"), "mqtt://"))
	}
	return out
}

func tcpLink(addr string) map[string]any {
	start := time.Now()
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return link(false, err.Error(), 0)
	}
	c.Close()
	return link(true, "reachable", time.Since(start))
}

func link(ok bool, detail string, latency time.Duration) map[string]any {
	return map[string]any{"ok": ok, "detail": detail, "ms": latency.Milliseconds()}
}

// recentVoice returns Vectro's latest voice log lines, newest last.
func recentVoice(ctx context.Context) []string {
	out, err := exec.CommandContext(ctx, "journalctl", "-u", "vic-cloud", "-n", "600", "--no-pager", "-o", "short").Output()
	if err != nil {
		return nil
	}
	var lines []string
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := sc.Text()
		i := strings.Index(line, "vectro: ")
		if i < 0 {
			continue
		}
		ts := ""
		if f := strings.Fields(line); len(f) >= 3 {
			ts = f[2]
		}
		lines = append(lines, strings.TrimSpace(ts+" "+line[i+len("vectro: "):]))
	}
	if len(lines) > 30 {
		lines = lines[len(lines)-30:]
	}
	return lines
}

// pendingFault reports a fault code waiting to be shown on the face.
// /run/fault_code itself is a FIFO the fault display reads from; reading
// it here would block and could steal the code, so only look at the
// regular .pending file, if any.
func pendingFault() string {
	const pending = "/run/fault_code.pending"
	if fi, err := os.Stat(pending); err == nil && fi.Mode().IsRegular() {
		return strings.TrimSpace(readOr(pending, ""))
	}
	return ""
}

func readOr(path, def string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return def
	}
	return string(b)
}

func round(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
