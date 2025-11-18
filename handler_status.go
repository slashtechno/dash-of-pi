package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

func (s *APIServer) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Try to serve from web directory first
	// Check multiple locations for the web directory
	possiblePaths := []string{
		"./web/index.html",                                           // Relative to working directory
		"/var/lib/dash-of-pi/web/index.html",                         // Systemd service location
		filepath.Join(filepath.Dir(os.Args[0]), "../web/index.html"), // Relative to binary
	}

	for _, indexPath := range possiblePaths {
		if data, err := os.ReadFile(indexPath); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			w.Write(data)
			return
		}
	}

	// Fallback to a simple error if web directory doesn't exist
	http.Error(w, "UI not found. Please ensure the 'web' directory is present and contains index.html.", http.StatusNotFound)
}

func (s *APIServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	used, cap, err := s.storage.GetStorageStats()
	if err != nil {
		http.Error(w, "Failed to get storage stats", http.StatusInternalServerError)
		return
	}

	videos, err := s.listVideoFiles()
	if err != nil {
		http.Error(w, "Failed to list videos", http.StatusInternalServerError)
		return
	}

	percent := 0
	if cap > 0 {
		percent = int((used * 100) / cap)
	}

	status := StatusResponse{
		Status: "recording",
		Storage: StorageStats{
			UsedBytes: used,
			CapBytes:  cap,
			UsedGB:    float64(used) / BytesPerGB,
			CapGB:     s.config.StorageCapGB,
			Percent:   percent,
		},
		Videos: videos,
		Uptime: fmt.Sprintf("%d seconds", int(time.Since(startTime).Seconds())),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *APIServer) handleGetAuthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": s.config.AuthToken,
	})
}
