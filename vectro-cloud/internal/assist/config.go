// Package assist answers Vector's voice requests with Home Assistant: the
// utterance is transcribed by a Wyoming speech-to-text service (Whisper),
// the text goes to HA's conversation API, and HA's reply is spoken by
// Vector in his own voice.
package assist

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
)

// DefaultConfigPath is where the robot keeps Vectro's settings. It lives on
// /data so it survives OS updates and stays out of source control.
const DefaultConfigPath = "/data/vectro/config.json"

// Config is the voice section of /data/vectro/config.json.
type Config struct {
	// HAURL is Home Assistant's base URL, e.g. http://10.0.0.3:8123.
	HAURL string `json:"ha_url"`
	// HAToken is a long-lived access token for HA.
	HAToken string `json:"ha_token"`
	// Whisper is the host:port of a Wyoming speech-to-text service.
	Whisper string `json:"whisper"`
	// Language is passed to speech-to-text and HA; "en" if empty.
	Language string `json:"language"`
	// Agent optionally selects an HA conversation agent (agent_id).
	Agent string `json:"agent,omitempty"`
}

// LoadConfig reads the config file.
func LoadConfig(path string) (Config, error) {
	var file struct {
		Voice Config `json:"voice"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return Config{}, err
	}
	cfg := file.Voice
	cfg.HAURL = strings.TrimRight(cfg.HAURL, "/")
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if cfg.HAURL == "" || cfg.HAToken == "" || cfg.Whisper == "" {
		return Config{}, errors.New("voice config needs ha_url, ha_token and whisper")
	}
	return cfg, nil
}
