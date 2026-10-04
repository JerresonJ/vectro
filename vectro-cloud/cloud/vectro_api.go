package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"vectro/cloud/internal/log"
)

// vectroAPIAddr is a local-only HTTP API the Vectro web UI uses to drive the
// robot through the same control session and commands as Home Assistant.
const vectroAPIAddr = "127.0.0.1:8090"

func serveVectroAPI(robot vectroRobot) {
	mux := http.NewServeMux()
	run := func(w http.ResponseWriter, r *http.Request, fn func(ctx context.Context) error) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}

	mux.HandleFunc("/options", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"commands":   robot.Commands(),
			"eye_colors": robot.EyeColors(),
			"volumes":    robot.Volumes(),
		})
	})
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, func(ctx context.Context) error { return robot.Command(ctx, r.FormValue("id")) })
	})
	mux.HandleFunc("/say", func(w http.ResponseWriter, r *http.Request) {
		text := strings.TrimSpace(r.FormValue("text"))
		run(w, r, func(ctx context.Context) error {
			log.Println("vectro: saying:", text)
			return robot.Say(ctx, text)
		})
	})
	mux.HandleFunc("/eye_color", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, func(ctx context.Context) error { return robot.SetEyeColor(ctx, r.FormValue("name")) })
	})
	mux.HandleFunc("/volume", func(w http.ResponseWriter, r *http.Request) {
		run(w, r, func(ctx context.Context) error { return robot.SetVolume(ctx, r.FormValue("name")) })
	})
	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()
		img, err := robot.Snapshot(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(img)
	})
	mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		st, err := robot.State(ctx)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(st)
	})

	srv := &http.Server{Addr: vectroAPIAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Println("vectro: local robot API on", vectroAPIAddr)
	if err := srv.ListenAndServe(); err != nil {
		log.Println("vectro: local robot API stopped:", err)
	}
}
