package mods

import (
	"net/http"
	"strings"

	"encoding/json"

	"github.com/digital-dream-labs/vector-go-sdk/pkg/vectorpb"
	"vectro/ui/vars"
)

type MoreJdocSettings struct {
	vars.Modification
}

func NewMoreJdocSettings() *MoreJdocSettings {
	return &MoreJdocSettings{}
}

func (modu *MoreJdocSettings) Name() string {
	return "MoreJdocSettings"
}

func (modu *MoreJdocSettings) Description() string {
	return "More Jdoc Settings"
}

func (modu *MoreJdocSettings) Load() error {
	return nil
}

type eyeColorResponse struct {
	IsCustom         bool    `json:"iscustom"`
	Preset           int     `json:"preset"`
	CustomHue        float32 `json:"hue"`
	CustomSaturation float32 `json:"saturation"`
}

func (m *MoreJdocSettings) HTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/mods/"+m.Name()) {
		return
	}
	switch strings.TrimPrefix(r.URL.Path, "/api/mods/"+m.Name()+"/") {
	case "getEyeColor":
	case "setEyeColor":
	case "getVolume":
	case "setVolume":
	// this could be where names go
	default:
		vars.HTTPError(w, r, "404 not found")
		return
	}
}

func getEyeColor() (eyeColorResponse, error) {
	var resp eyeColorResponse
	doc, err := getJdoc()
	if err != nil {
		return eyeColorResponse{}, err
	}
	resp.Preset = doc.EyeColor
	if doc.CustomEyeColor.Enabled {
		resp.IsCustom = true
		resp.CustomHue = float32(doc.CustomEyeColor.Hue)
		resp.CustomSaturation = float32(doc.CustomEyeColor.Saturation)
	}
	return resp, nil
}

func getVolume() (int, error) {
	doc, err := getJdoc()
	if err != nil {
		return 0, err
	}
	return doc.MasterVolume, nil
}

func getJdoc() (robotSettingsJson, error) {
	v, err := vars.GetVec()
	if err != nil {
		return robotSettingsJson{}, err
	}
	r, err := v.Conn.PullJdocs(ctx,
		&vectorpb.PullJdocsRequest{
			JdocTypes: []vectorpb.JdocType{
				vectorpb.JdocType_ROBOT_SETTINGS,
			},
		},
	)
	if err != nil {
		return robotSettingsJson{}, err
	}
	doc := r.NamedJdocs[0].Doc.JsonDoc
	var decodedDoc robotSettingsJson
	json.Unmarshal([]byte(doc), &decodedDoc)
	return decodedDoc, nil
}
