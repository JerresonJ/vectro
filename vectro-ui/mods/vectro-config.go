package mods

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"vectro/ui/vars"
)

// vectroConfigPath is read by Vectro's vic-cloud (voice and MQTT).
const vectroConfigPath = "/data/vectro/config.json"

type vectroConfig struct {
	Voice struct {
		HAURL    string `json:"ha_url"`
		HAToken  string `json:"ha_token"`
		Whisper  string `json:"whisper"`
		Language string `json:"language,omitempty"`
		Agent    string `json:"agent,omitempty"`
	} `json:"voice"`
	MQTT struct {
		Broker          string `json:"broker"`
		Username        string `json:"username,omitempty"`
		Password        string `json:"password,omitempty"`
		DiscoveryPrefix string `json:"discovery_prefix,omitempty"`
	} `json:"mqtt"`
}

func loadVectroConfig() (vectroConfig, error) {
	var c vectroConfig
	b, err := os.ReadFile(vectroConfigPath)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

// VectroConfig edits the Home Assistant voice and MQTT settings.
type VectroConfig struct {
	vars.Modification
}

func NewVectroConfig() *VectroConfig { return &VectroConfig{} }

func (m *VectroConfig) Name() string        { return "VectroConfig" }
func (m *VectroConfig) Description() string { return "Home Assistant voice and MQTT settings." }
func (m *VectroConfig) Load() error         { return nil }

func (m *VectroConfig) HTTP(w http.ResponseWriter, r *http.Request) {
	switch strings.TrimPrefix(r.URL.Path, "/api/mods/VectroConfig/") {
	case "get":
		c, err := loadVectroConfig()
		if err != nil && !os.IsNotExist(err) {
			vars.HTTPError(w, r, err.Error())
			return
		}
		// Never send secrets back to the browser.
		c.Voice.HAToken = mask(c.Voice.HAToken)
		c.MQTT.Password = mask(c.MQTT.Password)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(c)
	case "set":
		if r.Method != http.MethodPost {
			vars.HTTPError(w, r, "POST required")
			return
		}
		var in vectroConfig
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			vars.HTTPError(w, r, "bad JSON: "+err.Error())
			return
		}
		if err := saveVectroConfig(in); err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		if err := restartVectroCloud(); err != nil {
			vars.HTTPError(w, r, "saved, but restarting voice services failed: "+err.Error())
			return
		}
		vars.HTTPSuccess(w, r)
	default:
		vars.HTTPError(w, r, "404 not found")
	}
}

// mask hides a secret, keeping its last 4 characters as a hint.
func mask(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "••••"
	}
	return "••••" + s[len(s)-4:]
}

// saveVectroConfig writes in, keeping existing secrets when the browser
// sends back the masked placeholder or nothing.
func saveVectroConfig(in vectroConfig) error {
	in.Voice.HAURL = strings.TrimRight(strings.TrimSpace(in.Voice.HAURL), "/")
	in.Voice.Whisper = strings.TrimSpace(in.Voice.Whisper)
	in.MQTT.Broker = strings.TrimSpace(in.MQTT.Broker)
	if in.Voice.HAURL != "" && !strings.HasPrefix(in.Voice.HAURL, "http") {
		return errors.New("Home Assistant URL must start with http:// or https://")
	}

	old, _ := loadVectroConfig()
	if in.Voice.HAToken == "" || strings.HasPrefix(in.Voice.HAToken, "••••") {
		in.Voice.HAToken = old.Voice.HAToken
	}
	if in.MQTT.Password == "" || strings.HasPrefix(in.MQTT.Password, "••••") {
		in.MQTT.Password = old.MQTT.Password
	}

	data, err := json.MarshalIndent(in, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll("/data/vectro", 0755); err != nil {
		return err
	}
	tmp := vectroConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return err
	}
	// vic-cloud runs as user "cloud" in group "anki".
	if u, err := user.Lookup("cloud"); err == nil {
		if g, err := user.LookupGroup("anki"); err == nil {
			uid, _ := strconv.Atoi(u.Uid)
			gid, _ := strconv.Atoi(g.Gid)
			os.Chown(tmp, uid, gid)
		}
	}
	return os.Rename(tmp, vectroConfigPath)
}

// restartVectroCloud reloads vic-cloud's config. vic-switchboard has to
// restart with it (its datagram socket stays connected to the old process);
// the engine, anim and robot processes reconnect on their own.
func restartVectroCloud() error {
	return exec.Command("sh", "-c", "systemctl stop vic-cloud && systemctl restart vic-switchboard && systemctl start vic-cloud").Run()
}
