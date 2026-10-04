package mods

import (
	"fmt"
	"net/http"
	"strconv"

	"vectro/ui/vars"
)

var SensitivityPVLocation = "/data/data/com.anki.victor/persistent/picovoice/sensitivity2"
var DefaultSensitivity = "0.45"

type SensitivityPV struct {
	vars.Modification
}

func NewSensitivityPV() *SensitivityPV {
	return &SensitivityPV{}
}

func (modu *SensitivityPV) Name() string {
	return "SensitivityPV"
}

func (modu *SensitivityPV) Description() string {
	return "Set the sensitivity of the Picovoice model."
}

func (modu *SensitivityPV) HTTP(w http.ResponseWriter, r *http.Request) {
	if vars.IsEndpoint(r, "set") {
		value := r.FormValue("value")
		valueF, err := strconv.ParseFloat(value, 32)
		if err != nil {
			vars.HTTPError(w, r, "not a float")
			return
		}
		if !(valueF > 0.00 && valueF < 1.00) {
			vars.HTTPError(w, r, "float must be between 0.00 and 1.00")
			return
		}
		valueRounded := fmt.Sprintf("%.3f", valueF)
		vars.SaveFile(valueRounded, SensitivityPVLocation)
		vars.HTTPSuccess(w, r)
		return
	} else if vars.IsEndpoint(r, "get") {
		f, err := vars.ReadFile(SensitivityPVLocation)
		if err != nil {
			f = DefaultSensitivity
		}
		w.Write([]byte(f))
		return
	}
}

func (modu *SensitivityPV) Load() error {
	return nil
}
