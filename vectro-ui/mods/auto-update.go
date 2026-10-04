package mods

import (
	"fmt"
	"net/http"
	"os"

	"vectro/ui/vars"
)

type AutoUpdate struct {
	vars.Modification
}

func NewAutoUpdate() *AutoUpdate {
	return &AutoUpdate{}
}

func (modu *AutoUpdate) Name() string {
	return "AutoUpdate"
}

func (modu *AutoUpdate) Description() string {
	return "Modifies CPU/RAM frequency for faster operation."
}

func (modu *AutoUpdate) Load() error {
	return nil
}

func (m *AutoUpdate) HTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/mods/AutoUpdate/isSelfMadeBuild" {
		if _, err := os.Stat("/etc/do-not-auto-update"); err == nil {
			fmt.Fprintf(w, "true")
		} else {
			fmt.Fprintf(w, "false")
		}
		return
	} else if r.URL.Path == "/api/mods/AutoUpdate/isInhibitedByUser" {
		if _, err := os.Stat("/data/data/user-do-not-auto-update"); err == nil {
			fmt.Fprintf(w, "true")
		} else {
			fmt.Fprintf(w, "false")
		}
		return
	} else if r.URL.Path == "/api/mods/AutoUpdate/setInhibited" {
		os.WriteFile("/data/data/user-do-not-auto-update", []byte("true"), 0777)
		vars.HTTPSuccess(w, r)
		return
	} else if r.URL.Path == "/api/mods/AutoUpdate/setAllowed" {
		os.Remove("/data/data/user-do-not-auto-update")
		vars.HTTPSuccess(w, r)
		return
	} else {
		vars.HTTPError(w, r, "404 not found")
		return
	}
}
