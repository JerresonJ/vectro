// Package hamqtt connects Vector to Home Assistant over MQTT.
//
// It publishes MQTT discovery config so HA creates a "Vectro <id>" device
// with:
//   - Say: a notify entity and a text box; the text is spoken in Vector's voice
//   - a camera, a "Take snapshot" button and an auto-snapshot interval
//   - movement buttons (drive, turn, head, lift, dock/undock)
//   - eye color and volume selects
//   - battery, charging, docked and activity sensors
//   - desk presence from face recognition: occupied, last seen, and a
//     "face seen" event for automations
//
// Availability is tracked with an online/offline status topic and a will.
package hamqtt

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"

	"vectro/cloud/internal/log"
)

// Config is the "mqtt" section of /data/vectro/config.json.
type Config struct {
	// Broker is the broker URL, e.g. tcp://10.0.0.3:1883.
	Broker   string `json:"broker"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	// DiscoveryPrefix is HA's MQTT discovery prefix; "homeassistant" if empty.
	DiscoveryPrefix string `json:"discovery_prefix,omitempty"`
}

// LoadConfig reads the mqtt section of the config file.
func LoadConfig(path string) (Config, error) {
	var file struct {
		MQTT Config `json:"mqtt"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return Config{}, err
	}
	if file.MQTT.Broker == "" {
		return Config{}, errors.New("mqtt config needs broker")
	}
	if file.MQTT.DiscoveryPrefix == "" {
		file.MQTT.DiscoveryPrefix = "homeassistant"
	}
	return file.MQTT, nil
}

// Command is a movement or action button.
type Command struct {
	ID, Name, Icon string
}

// State is what the robot reports to HA's sensors.
type State struct {
	BatteryVolts float64 `json:"battery_volts"`
	BatteryLevel string  `json:"battery_level"`
	Charging     bool    `json:"charging"`
	OnCharger    bool    `json:"on_charger"`
	Activity     string  `json:"activity"`
	CameraActive bool    `json:"camera_active"`
}

// Robot is what the bridge needs from the robot.
type Robot interface {
	Say(ctx context.Context, text string) error
	Commands() []Command
	Command(ctx context.Context, id string) error
	Snapshot(ctx context.Context) ([]byte, error)
	EyeColors() []string
	SetEyeColor(ctx context.Context, name string) error
	Volumes() []string
	SetVolume(ctx context.Context, name string) error
	State(ctx context.Context) (State, error)
	// WatchFaces calls fn for every face observation ("" = unknown person)
	// until ctx is done.
	WatchFaces(ctx context.Context, fn func(name string))
}

// RobotInfo identifies the robot to Home Assistant.
type RobotInfo struct {
	// ID is a short lowercase id used in topics, e.g. "x9c2".
	ID string
	// Name is the device name shown in HA, e.g. "Vectro X9C2".
	Name    string
	Version string
}

// RobotFromHostname builds a RobotInfo from a hostname like "Vector-X9C2".
func RobotFromHostname(host, version string) RobotInfo {
	short := strings.TrimPrefix(host, "Vector-")
	if short == "" {
		short = "vector"
	}
	return RobotInfo{ID: strings.ToLower(short), Name: "Vectro " + short, Version: version}
}

const (
	maxQueued     = 8 // pending requests; more are dropped
	stateInterval = 30 * time.Second
	maxSnapEvery  = 3600
)

type bridge struct {
	cfg   Config
	info  RobotInfo
	robot Robot
	c     mqtt.Client
	base  string
	jobs  chan job

	mu        sync.Mutex
	snapEvery int // auto-snapshot interval in seconds; 0 = off
	snapReset chan struct{}

	presence presence
}

// presence turns the robot's stream of face observations into an occupancy
// state, a "last seen" sensor and throttled "face seen" events.
type presence struct {
	mu         sync.Mutex
	timeout    time.Duration // no faces for this long = unoccupied
	occupied   bool
	lastFace   time.Time // any face
	lastNamed  time.Time // a recognized face
	unknownAt  time.Time // start of the current run of unknown observations
	lastName   string
	lastPubAt  time.Time            // last "last seen" publish
	lastEvents map[string]time.Time // last "face seen" event per name

	timeoutSeen atomic.Bool // a timeout value arrived from MQTT
}

const (
	defaultPresenceMin = 5
	maxPresenceMin     = 120
	// unknownDelay: recognition takes a moment, so the first frames of a
	// known face arrive unnamed; only report "Unknown" if no name follows.
	unknownDelay   = 3 * time.Second
	lastSeenEvery  = 30 * time.Second // refresh "last seen" while in view
	faceEventEvery = 10 * time.Minute // one "face seen" event per person per this
)

// job is one request run on the bridge's worker, so the robot only does one
// thing at a time.
type job struct {
	name string
	run  func(ctx context.Context) error
}

func (b *bridge) topic(parts ...string) string {
	return b.base + "/" + strings.Join(parts, "/")
}

// Run connects to the broker and serves until ctx is done. Paho reconnects
// on its own, and discovery is republished each time we connect.
func Run(ctx context.Context, cfg Config, info RobotInfo, robot Robot) {
	b := &bridge{
		cfg: cfg, info: info, robot: robot,
		base:      "vectro/" + info.ID,
		jobs:      make(chan job, maxQueued),
		snapReset: make(chan struct{}, 1),
		presence: presence{
			timeout:    defaultPresenceMin * time.Minute,
			lastEvents: map[string]time.Time{},
		},
	}
	opts := mqtt.NewClientOptions().
		AddBroker(cfg.Broker).
		SetClientID("vectro-"+info.ID).
		SetUsername(cfg.Username).
		SetPassword(cfg.Password).
		SetAutoReconnect(true).
		SetConnectRetry(true).
		SetConnectRetryInterval(10*time.Second).
		SetMaxReconnectInterval(time.Minute).
		SetWill(b.topic("status"), "offline", 1, true)
	opts.SetOnConnectHandler(b.onConnect)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Println("vectro: MQTT connection lost:", err)
	})
	b.c = mqtt.NewClient(opts)
	b.c.Connect() // retries in the background (SetConnectRetry)

	go b.autoSnapshot(ctx)
	go b.stateLoop(ctx)
	go b.robot.WatchFaces(ctx, b.onFace)
	go b.presenceLoop(ctx)
	for {
		select {
		case <-ctx.Done():
			b.c.Publish(b.topic("status"), 1, true, "offline").WaitTimeout(time.Second)
			b.c.Disconnect(250)
			return
		case j := <-b.jobs:
			jctx, cancel := context.WithTimeout(ctx, time.Minute)
			if err := j.run(jctx); err != nil {
				log.Println("vectro: MQTT", j.name, "failed:", err)
			}
			cancel()
		}
	}
}

func (b *bridge) enqueue(name string, run func(ctx context.Context) error) {
	select {
	case b.jobs <- job{name, run}:
	default:
		log.Println("vectro: MQTT queue full, dropping", name)
	}
}

func (b *bridge) onConnect(c mqtt.Client) {
	log.Println("vectro: connected to MQTT broker", b.cfg.Broker)
	b.publishDiscovery()
	c.Publish(b.topic("status"), 1, true, "online")

	sub := func(topic string, fn func(payload string)) {
		c.Subscribe(topic, 1, func(_ mqtt.Client, m mqtt.Message) {
			fn(strings.TrimSpace(string(m.Payload())))
		})
	}
	sub(b.topic("say"), func(text string) {
		if text == "" {
			return
		}
		b.enqueue("say", func(ctx context.Context) error {
			log.Println("vectro: saying:", text)
			return b.robot.Say(ctx, text)
		})
	})
	sub(b.topic("command"), func(id string) {
		if id == "snapshot" {
			b.enqueue("snapshot", b.snapshot)
			return
		}
		b.enqueue("command "+id, func(ctx context.Context) error {
			log.Println("vectro: MQTT command:", id)
			err := b.robot.Command(ctx, id)
			b.publishState(ctx)
			return err
		})
	})
	sub(b.topic("eye_color", "set"), func(name string) {
		b.enqueue("eye color", func(ctx context.Context) error {
			if err := b.robot.SetEyeColor(ctx, name); err != nil {
				return err
			}
			c.Publish(b.topic("eye_color"), 1, true, name)
			return nil
		})
	})
	sub(b.topic("volume", "set"), func(name string) {
		b.enqueue("volume", func(ctx context.Context) error {
			if err := b.robot.SetVolume(ctx, name); err != nil {
				return err
			}
			c.Publish(b.topic("volume"), 1, true, name)
			return nil
		})
	})
	for _, t := range []string{b.topic("presence_timeout", "set"), b.topic("presence_timeout")} {
		sub(t, func(v string) {
			n, err := strconv.Atoi(strings.TrimSuffix(v, ".0"))
			if err != nil || n < 1 || n > maxPresenceMin {
				return
			}
			b.presence.timeoutSeen.Store(true)
			b.presence.mu.Lock()
			changed := time.Duration(n)*time.Minute != b.presence.timeout
			b.presence.timeout = time.Duration(n) * time.Minute
			b.presence.mu.Unlock()
			if changed {
				c.Publish(b.topic("presence_timeout"), 1, true, strconv.Itoa(n))
			}
		})
	}
	// Give a retained timeout a moment to arrive; if none did, publish the
	// default so HA's number entity isn't "unknown".
	go func() {
		time.Sleep(3 * time.Second)
		b.presence.mu.Lock()
		minutes := int(b.presence.timeout / time.Minute)
		b.presence.mu.Unlock()
		if !b.presence.timeoutSeen.Load() {
			c.Publish(b.topic("presence_timeout"), 1, true, strconv.Itoa(minutes))
		}
	}()
	// Report the current presence state on (re)connect.
	b.presence.mu.Lock()
	occupied := b.presence.occupied
	b.presence.mu.Unlock()
	c.Publish(b.topic("presence"), 1, true, onOff(occupied))

	// The interval's retained state topic restores it after a restart.
	for _, t := range []string{b.topic("snapshot_interval", "set"), b.topic("snapshot_interval")} {
		sub(t, func(v string) {
			n, err := strconv.Atoi(strings.TrimSuffix(v, ".0"))
			if err != nil || n < 0 || n > maxSnapEvery {
				return
			}
			b.mu.Lock()
			changed := n != b.snapEvery
			b.snapEvery = n
			b.mu.Unlock()
			if changed {
				c.Publish(b.topic("snapshot_interval"), 1, true, strconv.Itoa(n))
				select {
				case b.snapReset <- struct{}{}:
				default:
				}
			}
		})
	}
}

func (b *bridge) snapshot(ctx context.Context) error {
	img, err := b.robot.Snapshot(ctx)
	if err != nil {
		return err
	}
	b.c.Publish(b.topic("camera"), 0, true, img)
	return nil
}

func (b *bridge) autoSnapshot(ctx context.Context) {
	for {
		b.mu.Lock()
		every := b.snapEvery
		b.mu.Unlock()
		var tick <-chan time.Time
		if every > 0 {
			tick = time.After(time.Duration(every) * time.Second)
		}
		select {
		case <-ctx.Done():
			return
		case <-b.snapReset:
		case <-tick:
			b.enqueue("auto snapshot", b.snapshot)
		}
	}
}

func (b *bridge) stateLoop(ctx context.Context) {
	t := time.NewTicker(stateInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			b.publishState(sctx)
			cancel()
		}
	}
}

func (b *bridge) publishState(ctx context.Context) {
	if b.c == nil || !b.c.IsConnected() {
		return
	}
	st, err := b.robot.State(ctx)
	if err != nil {
		return
	}
	payload, _ := json.Marshal(st)
	b.c.Publish(b.topic("state"), 0, true, payload)
}

func onOff(v bool) string {
	if v {
		return "ON"
	}
	return "OFF"
}

// onFace handles one face observation. It's called many times a second
// while a face is in view, so it only publishes on changes.
func (b *bridge) onFace(name string) {
	now := time.Now()
	p := &b.presence
	p.mu.Lock()
	defer p.mu.Unlock()

	p.lastFace = now
	if name != "" {
		p.lastNamed = now
		p.unknownAt = time.Time{}
	} else {
		if p.unknownAt.IsZero() {
			p.unknownAt = now
		}
		// Wait to see whether recognition names this face.
		if now.Sub(p.unknownAt) < unknownDelay || now.Sub(p.lastNamed) < unknownDelay {
			return
		}
		name = "Unknown"
	}

	if !p.occupied {
		p.occupied = true
		log.Println("vectro: desk occupied (" + name + ")")
		b.publish(b.topic("presence"), true, "ON")
	}
	if name != p.lastName || now.Sub(p.lastPubAt) >= lastSeenEvery {
		p.lastName, p.lastPubAt = name, now
		payload, _ := json.Marshal(map[string]string{"name": name, "at": now.Format(time.RFC3339)})
		b.publish(b.topic("last_seen"), true, payload)
	}
	if now.Sub(p.lastEvents[name]) >= faceEventEvery {
		p.lastEvents[name] = now
		log.Println("vectro: face seen:", name)
		payload, _ := json.Marshal(map[string]string{"event_type": "face_seen", "name": name})
		b.publish(b.topic("face_event"), false, payload)
	}
}

// presenceLoop clears occupancy once no face has been seen for the timeout.
func (b *bridge) presenceLoop(ctx context.Context) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.checkPresence()
		}
	}
}

// checkPresence marks the desk unoccupied once no face has been seen for
// the timeout.
func (b *bridge) checkPresence() {
	p := &b.presence
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.occupied && time.Since(p.lastFace) > p.timeout {
		p.occupied = false
		log.Println("vectro: desk unoccupied")
		b.publish(b.topic("presence"), true, "OFF")
	}
}

func (b *bridge) publish(topic string, retain bool, payload any) {
	if b.c != nil && b.c.IsConnected() {
		b.c.Publish(topic, 1, retain, payload)
	}
}

func (b *bridge) publishDiscovery() {
	id := "vectro_" + b.info.ID
	device := map[string]any{
		"identifiers":  []string{id},
		"name":         b.info.Name,
		"manufacturer": "Vectro (Retro Vector)",
		"model":        "Anki Vector",
		"sw_version":   b.info.Version,
	}
	entity := func(key, name, icon string) map[string]any {
		e := map[string]any{
			"name":               name,
			"unique_id":          id + "_" + key,
			"object_id":          id + "_" + key,
			"availability_topic": b.topic("status"),
			"device":             device,
		}
		if icon != "" {
			e["icon"] = icon
		}
		return e
	}
	publish := func(component, key string, cfg map[string]any) {
		payload, _ := json.Marshal(cfg)
		b.c.Publish(b.cfg.DiscoveryPrefix+"/"+component+"/"+id+"/"+key+"/config", 1, true, payload)
	}

	// Speech: notify.vectro_<id>_say for automations, a text box for dashboards.
	say := entity("say", "Say", "mdi:robot")
	say["command_topic"] = b.topic("say")
	publish("notify", "say", say)
	sayText := entity("say_text", "Say text", "mdi:message-text")
	sayText["command_topic"] = b.topic("say")
	sayText["mode"] = "text"
	sayText["max"] = 255
	publish("text", "say_text", sayText)

	// Camera.
	cam := entity("camera", "Camera", "mdi:camera")
	cam["topic"] = b.topic("camera")
	publish("camera", "camera", cam)
	snap := entity("snapshot", "Take snapshot", "mdi:camera-iris")
	snap["command_topic"] = b.topic("command")
	snap["payload_press"] = "snapshot"
	publish("button", "snapshot", snap)
	every := entity("snapshot_interval", "Auto-snapshot interval", "mdi:timer-cog")
	every["command_topic"] = b.topic("snapshot_interval", "set")
	every["state_topic"] = b.topic("snapshot_interval")
	every["min"] = 0
	every["max"] = maxSnapEvery
	every["step"] = 5
	every["unit_of_measurement"] = "s"
	every["mode"] = "box"
	every["entity_category"] = "config"
	publish("number", "snapshot_interval", every)

	// Movement buttons.
	for _, cmd := range b.robot.Commands() {
		btn := entity(cmd.ID, cmd.Name, cmd.Icon)
		btn["command_topic"] = b.topic("command")
		btn["payload_press"] = cmd.ID
		publish("button", cmd.ID, btn)
	}

	// Eye color and volume.
	eyes := entity("eye_color", "Eye color", "mdi:eye")
	eyes["command_topic"] = b.topic("eye_color", "set")
	eyes["state_topic"] = b.topic("eye_color")
	eyes["options"] = b.robot.EyeColors()
	publish("select", "eye_color", eyes)
	vol := entity("volume", "Volume", "mdi:volume-high")
	vol["command_topic"] = b.topic("volume", "set")
	vol["state_topic"] = b.topic("volume")
	vol["options"] = b.robot.Volumes()
	publish("select", "volume", vol)

	// Sensors from the state topic.
	state := b.topic("state")
	volts := entity("battery_voltage", "Battery voltage", "")
	volts["state_topic"] = state
	volts["value_template"] = "{{ value_json.battery_volts }}"
	volts["device_class"] = "voltage"
	volts["unit_of_measurement"] = "V"
	volts["state_class"] = "measurement"
	volts["entity_category"] = "diagnostic"
	publish("sensor", "battery_voltage", volts)
	level := entity("battery_level", "Battery", "mdi:battery")
	level["state_topic"] = state
	level["value_template"] = "{{ value_json.battery_level }}"
	publish("sensor", "battery_level", level)
	activity := entity("activity", "Activity", "mdi:robot-happy")
	activity["state_topic"] = state
	activity["value_template"] = "{{ value_json.activity }}"
	publish("sensor", "activity", activity)
	charging := entity("charging", "Charging", "")
	charging["state_topic"] = state
	charging["value_template"] = "{{ 'ON' if value_json.charging else 'OFF' }}"
	charging["device_class"] = "battery_charging"
	publish("binary_sensor", "charging", charging)
	docked := entity("on_charger", "On charger", "mdi:home-battery")
	docked["state_topic"] = state
	docked["value_template"] = "{{ 'ON' if value_json.on_charger else 'OFF' }}"
	publish("binary_sensor", "on_charger", docked)
	camera := entity("camera_active", "Camera active", "mdi:eye-check")
	camera["state_topic"] = state
	camera["value_template"] = "{{ 'ON' if value_json.camera_active else 'OFF' }}"
	camera["entity_category"] = "diagnostic"
	publish("binary_sensor", "camera_active", camera)

	// Desk presence from face recognition.
	occupied := entity("desk_occupied", "Desk occupied", "")
	occupied["state_topic"] = b.topic("presence")
	occupied["device_class"] = "occupancy"
	publish("binary_sensor", "desk_occupied", occupied)
	seen := entity("last_seen", "Last seen", "mdi:face-recognition")
	seen["state_topic"] = b.topic("last_seen")
	seen["value_template"] = "{{ value_json.name }}"
	seen["json_attributes_topic"] = b.topic("last_seen")
	publish("sensor", "last_seen", seen)
	faceEvent := entity("face_seen", "Face seen", "mdi:account-eye")
	faceEvent["state_topic"] = b.topic("face_event")
	faceEvent["event_types"] = []string{"face_seen"}
	publish("event", "face_seen", faceEvent)
	timeout := entity("presence_timeout", "Presence timeout", "mdi:timer-sand")
	timeout["command_topic"] = b.topic("presence_timeout", "set")
	timeout["state_topic"] = b.topic("presence_timeout")
	timeout["min"] = 1
	timeout["max"] = maxPresenceMin
	timeout["step"] = 1
	timeout["unit_of_measurement"] = "min"
	timeout["mode"] = "box"
	timeout["entity_category"] = "config"
	publish("number", "presence_timeout", timeout)
}
