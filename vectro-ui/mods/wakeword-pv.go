package mods

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"vectro/ui/vars"
)

type WakeWordPVRequest struct {
	Keyword string `json:"keyword"`
}

type WakeWordPVResponseFailure struct {
	Code  ErrorCode `json:"code"`
	Error string    `json:"error"`
}

type WakeWordPVResponseSuccess struct {
	File string `json:"file"`
}

// error codes for optimizer failures
type ErrorCode int

const (
	CodeIO ErrorCode = iota + 1
	CodePronunciation
	CodeUnknown
)

var WakeWordPVLocation = "/data/data/com.anki.victor/persistent/picovoice/custom_1-5-0.ppn"

type WakeWordPV struct {
	vars.Modification
}

func NewWakeWordPV() *WakeWordPV {
	return &WakeWordPV{}
}

func (modu *WakeWordPV) Name() string {
	return "WakeWordPV"
}

func (modu *WakeWordPV) Description() string {
	return "Train a new wake word with Picovoice."
}

func (modu *WakeWordPV) HTTP(w http.ResponseWriter, r *http.Request) {
	if vars.IsEndpoint(r, "request-model") {
		kw := r.FormValue("keyword")
		if kw == "" {
			vars.HTTPError(w, r, "keyword not given")
			return
		}
		var reqKW WakeWordPVRequest
		reqKW.Keyword = kw
		jsonKW, _ := json.Marshal(reqKW)
		resp, err := http.Post("http://pvic.xyz:8079/wakeword-pv/create-model", "application/json", bytes.NewReader(jsonKW))
		if err != nil {
			vars.HTTPError(w, r, "network error")
			return
		}
		defer resp.Body.Close()

		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			var kwFail WakeWordPVResponseFailure
			json.Unmarshal(b, &kwFail)
			if kwFail.Code == CodePronunciation {
				vars.HTTPError(w, r, "pronounciation not found in resource files, try using more common words.")
			} else {
				vars.HTTPError(w, r, kwFail.Error)
			}
			return
		}
		var kwSuccess WakeWordPVResponseSuccess
		json.Unmarshal(b, &kwSuccess)
		decoded, err := base64.StdEncoding.DecodeString(kwSuccess.File)
		if err != nil {
			vars.HTTPError(w, r, "Error decoding keyword file: "+err.Error())
			return
		}
		os.MkdirAll(filepath.Dir(WakeWordPVLocation), 0777)
		err = os.WriteFile(WakeWordPVLocation, decoded, 0777)
		if err != nil {
			vars.HTTPError(w, r, "Error writing model file to disk: "+err.Error())
			return
		}
		vars.HTTPSuccess(w, r)
	} else if vars.IsEndpoint(r, "delete-model") {
		os.Remove(WakeWordPVLocation)
		vars.HTTPSuccess(w, r)
	}
}

func (modu *WakeWordPV) Load() error {
	return nil
}
