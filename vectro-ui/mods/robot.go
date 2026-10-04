package mods

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"vectro/ui/vars"
)

// robotAPI is Vectro vic-cloud's local robot API: movement, camera, speech,
// eye color and volume, shared with Home Assistant (one control session).
const robotAPI = "http://127.0.0.1:8090"

// Robot proxies /api/mods/Robot/<path> to vic-cloud's local robot API.
type Robot struct {
	vars.Modification
}

func NewRobot() *Robot { return &Robot{} }

func (m *Robot) Name() string        { return "Robot" }
func (m *Robot) Description() string { return "Drive, camera, eye color and volume." }
func (m *Robot) Load() error         { return nil }

var robotClient = &http.Client{Timeout: 50 * time.Second}

var robotPaths = map[string]bool{
	"options": true, "command": true, "say": true, "eye_color": true,
	"volume": true, "snapshot": true, "state": true,
}

func (m *Robot) HTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/mods/Robot/")
	if !robotPaths[path] {
		vars.HTTPError(w, r, "404 not found")
		return
	}
	r.ParseForm()
	var resp *http.Response
	var err error
	if r.Method == http.MethodPost {
		resp, err = robotClient.PostForm(robotAPI+"/"+path, r.Form)
	} else {
		resp, err = robotClient.Get(robotAPI + "/" + path + "?" + url.Values(r.Form).Encode())
	}
	if err != nil {
		vars.HTTPError(w, r, "Vectro vic-cloud isn't answering: "+err.Error())
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		vars.HTTPError(w, r, strings.TrimSpace(string(msg)))
		return
	}
	if resp.StatusCode == http.StatusNoContent {
		vars.HTTPSuccess(w, r)
		return
	}
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-store")
	io.Copy(w, resp.Body)
}
