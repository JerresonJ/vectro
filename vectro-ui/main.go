// Command vectro-ui serves Vectro's settings and diagnostics web UI on
// port 8080. It replaces WireOS's wired and installs as /usr/bin/wired.
package main

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"vectro/ui/mods"
	"vectro/ui/vars"
)

//go:embed webroot
var webroot embed.FS

var EnabledMods []vars.Modification = []vars.Modification{
	mods.NewDiagnostics(),
	mods.NewActions(),
	mods.NewRobot(),
	mods.NewVectroConfig(),
	mods.NewFreqChange(),
	mods.NewWakeWordPV(),
	mods.NewAutoUpdate(),
	mods.NewSensitivityPV(),
	mods.NewJdocSettings(),
	mods.NewFaces(),
}

func main() {
	vars.EnabledMods = EnabledMods
	vars.InitMods()
	startweb()
}

func startweb() {
	fmt.Println("vectro-ui: serving on port 8080")
	root, _ := fs.Sub(webroot, "webroot")
	files := http.FileServer(http.FS(root))
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		files.ServeHTTP(w, r)
	})
	http.ListenAndServe(":8080", nil)
}
