package mods

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

var ctx context.Context

type JdocSettings struct {
	vars.Modification
}

func NewJdocSettings() *JdocSettings {
	return &JdocSettings{}
}

func (modu *JdocSettings) Name() string {
	return "JdocSettings"
}

func (modu *JdocSettings) Description() string {
	return "A couple settings in case you are using the WireOS servers."
}

func (modu *JdocSettings) Load() error {
	ctx = context.Background()
	return nil
}

func (m *JdocSettings) HTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/mods/JdocSettings/") {
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/mods/JdocSettings/") {
	case "setLocation":
		location := r.FormValue("location")
		err := setLocation(location)
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
	case "setTimezone":
		timezone := r.FormValue("timezone")
		err := setTimezone(timezone)
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
	case "setFahrenheit":
		temp := r.FormValue("temp")
		if temp != "f" && temp != "c" {
			vars.HTTPError(w, r, "temp must be f or c")
			return
		}
		err := setFahrenheit(temp == "f")
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
	case "getLocation":
		location, err := getLocation()
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		w.Write([]byte(location))
		return
	case "getTimezone":
		timezone, err := getTimezone()
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		w.Write([]byte(timezone))
		return
	case "getFahrenheit":
		temp, err := getFahrenheit()
		if err != nil {
			vars.HTTPError(w, r, err.Error())
			return
		}
		var ret string = "c"
		if temp {
			ret = "f"
		}
		w.Write([]byte(ret))
		return
	default:
		vars.HTTPError(w, r, "404 not found")
	}
	vars.HTTPSuccess(w, r)
}

func setLocation(location string) error {
	if location == "" {
		return errors.New("empty location")
	}
	return setSettingSDKstring("default_location", location)
}

func setTimezone(timezone string) error {
	if timezone == "" {
		return errors.New("empty time zone")
	}
	return setSettingSDKstring("time_zone", timezone)
}

func setFahrenheit(isF bool) error {
	return setSettingSDKintbool("temp_is_fahrenheit", fmt.Sprint(isF))
}

func getLocation() (string, error) {
	v, err := vars.GetVec()
	if err != nil {
		return "", err
	}
	r, err := v.Conn.PullJdocs(ctx,
		&vectorpb.PullJdocsRequest{
			JdocTypes: []vectorpb.JdocType{
				vectorpb.JdocType_ROBOT_SETTINGS,
			},
		},
	)
	if err != nil {
		return "", err
	}
	doc := r.NamedJdocs[0].Doc.JsonDoc
	var decodedDoc robotSettingsJson
	json.Unmarshal([]byte(doc), &decodedDoc)
	return decodedDoc.DefaultLocation, nil
}

func getTimezone() (string, error) {
	v, err := vars.GetVec()
	if err != nil {
		return "", err
	}
	r, err := v.Conn.PullJdocs(ctx,
		&vectorpb.PullJdocsRequest{
			JdocTypes: []vectorpb.JdocType{
				vectorpb.JdocType_ROBOT_SETTINGS,
			},
		},
	)
	if err != nil {
		return "", err
	}
	doc := r.NamedJdocs[0].Doc.JsonDoc
	var decodedDoc robotSettingsJson
	json.Unmarshal([]byte(doc), &decodedDoc)
	return decodedDoc.TimeZone, nil
}

func getFahrenheit() (bool, error) {
	v, err := vars.GetVec()
	if err != nil {
		return false, err
	}
	r, err := v.Conn.PullJdocs(ctx,
		&vectorpb.PullJdocsRequest{
			JdocTypes: []vectorpb.JdocType{
				vectorpb.JdocType_ROBOT_SETTINGS,
			},
		},
	)
	if err != nil {
		return false, err
	}
	doc := r.NamedJdocs[0].Doc.JsonDoc
	var decodedDoc robotSettingsJson
	json.Unmarshal([]byte(doc), &decodedDoc)
	return decodedDoc.TempIsFahrenheit, nil
}

var transCfg = &http.Transport{
	TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, // ignore SSL warnings
}

func setSettingSDKstring(setting string, value string) error {
	url := "https://localhost:443/v1/update_settings"
	updateJSON, _ := json.Marshal(map[string]any{"update_settings": true, "settings": map[string]string{setting: value}})
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(updateJSON))
	guid, err := vars.GetGUID()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+guid)
	req.Header.Set("Content-Type", "application/json")
	return doSettingsRequest(req)
}

func setSettingSDKintbool(setting string, value string) error {
	url := "https://localhost:443/v1/update_settings"
	var updateJSON = []byte(`{"update_settings": true, "settings": {"` + setting + `": ` + value + ` } }`)
	req, _ := http.NewRequest("POST", url, bytes.NewBuffer(updateJSON))
	guid, err := vars.GetGUID()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+guid)
	req.Header.Set("Content-Type", "application/json")
	return doSettingsRequest(req)
}

// doSettingsRequest sends an update_settings request to the robot's gateway
// and reports a non-OK reply as an error.
func doSettingsRequest(req *http.Request) error {
	client := &http.Client{Transport: transCfg}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("robot rejected setting: %s", resp.Status)
	}
	return nil
}

type robotSettingsJson struct {
	ButtonWakeword int  `json:"button_wakeword"`
	Clock24Hour    bool `json:"clock_24_hour"`
	CustomEyeColor struct {
		Enabled    bool    `json:"enabled"`
		Hue        float64 `json:"hue"`
		Saturation float64 `json:"saturation"`
	} `json:"custom_eye_color"`
	DefaultLocation  string `json:"default_location"`
	DistIsMetric     bool   `json:"dist_is_metric"`
	EyeColor         int    `json:"eye_color"`
	Locale           string `json:"locale"`
	MasterVolume     int    `json:"master_volume"`
	TempIsFahrenheit bool   `json:"temp_is_fahrenheit"`
	TimeZone         string `json:"time_zone"`
}
