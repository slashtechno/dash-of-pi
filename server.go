package main

import (
	"dash-of-pi/camera"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type APIServer struct {
	config        *Config
	cameraManager *camera.CameraManager
	storage       *StorageManager
	logger        *Logger
	auth          *AuthMiddleware
	server        *http.Server
	indexHTML     string
	exportInfo    *ExportInfo
	exportMutex   sync.RWMutex
	configPath    string
}

type ExportInfo struct {
	Filename       string    `json:"filename"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Size           int64     `json:"size"`
	Available      bool      `json:"available"`
	InProgress     bool      `json:"in_progress"`
	Progress       string    `json:"progress"`
	CurrentSizeMB  float64   `json:"current_size_mb"`
	TotalSegments  int       `json:"total_segments"`
	ProcessedFiles int       `json:"processed_files"`
}

type VideoInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Duration int       `json:"duration"`
	CameraID string    `json:"camera_id"`
}

type StorageStats struct {
	UsedBytes int64   `json:"used_bytes"`
	CapBytes  int64   `json:"cap_bytes"`
	UsedGB    float64 `json:"used_gb"`
	CapGB     int     `json:"cap_gb"`
	Percent   int     `json:"percent"`
}

type StatusResponse struct {
	Status  string       `json:"status"`
	Storage StorageStats `json:"storage"`
	Videos  []VideoInfo  `json:"videos"`
	Uptime  string       `json:"uptime"`
}

var startTime = time.Now()

func NewAPIServer(config *Config, cameraManager *camera.CameraManager, storage *StorageManager, logger *Logger, configPath string) *APIServer {
	auth := NewAuthMiddleware(config.AuthToken)

	server := &APIServer{
		config:        config,
		cameraManager: cameraManager,
		storage:       storage,
		logger:        logger,
		auth:          auth,
		exportInfo:    &ExportInfo{Available: false},
		configPath:    configPath,
	}

	// Check for existing export on startup
	server.checkExistingExport()

	return server
}

func (s *APIServer) Start() error {
	mux := http.NewServeMux()

	// Health check (no auth)
	mux.HandleFunc("/health", s.handleHealth)

	// UI endpoints (no auth for now)
	mux.HandleFunc("/", s.handleUI)

	// Serve static files from web directory
	possibleWebDirs := []string{
		"./web",
		"/var/lib/dash-of-pi/web",
		filepath.Join(filepath.Dir(os.Args[0]), "../web"),
	}

	for _, webDir := range possibleWebDirs {
		if _, err := os.Stat(webDir); err == nil {
			fs := http.FileServer(http.Dir(webDir))
			mux.Handle("/web/", http.StripPrefix("/web/", fs))
			s.logger.Printf("Serving static files from: %s", webDir)
			break
		}
	}

	// API endpoints (with auth)
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/status", s.handleStatus)
	apiMux.HandleFunc("/api/videos", s.handleListVideos)
	apiMux.HandleFunc("/api/video/download", s.handleDownloadVideo)
	apiMux.HandleFunc("/api/video/latest", s.handleLatestVideo)
	apiMux.HandleFunc("/api/videos/generate-export", s.handleGenerateExport)
	apiMux.HandleFunc("/api/videos/export-status", s.handleExportStatus)
	apiMux.HandleFunc("/api/videos/download-export", s.handleDownloadExport)
	apiMux.HandleFunc("/api/videos/delete-export", s.handleDeleteExport)
	apiMux.HandleFunc("/api/videos/", s.handleServeSegment)
	apiMux.HandleFunc("/api/auth/token", s.handleGetAuthToken)
	apiMux.HandleFunc("/api/config", s.handleGetConfig)
	apiMux.HandleFunc("/api/config/update", s.handleUpdateConfig)
	apiMux.HandleFunc("/api/cameras", s.handleListCameras)
	apiMux.HandleFunc("/api/cameras/add", s.handleAddCamera)
	apiMux.HandleFunc("/api/cameras/update", s.handleUpdateCamera)
	apiMux.HandleFunc("/api/cameras/delete", s.handleDeleteCamera)
	apiMux.HandleFunc("/api/stream/frame", s.handleStreamFrame)
	apiMux.HandleFunc("/api/stream/mjpeg", s.handleStreamMJPEG)

	mux.Handle("/api/", s.auth.Check(apiMux))

	s.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", s.config.Port),
		Handler:           mux,
		ReadTimeout:       ServerReadTimeout,
		WriteTimeout:      ServerWriteTimeout,
		IdleTimeout:       ServerIdleTimeout,
		ReadHeaderTimeout: ServerReadHeaderTimeout,
		MaxHeaderBytes:    HTTPMaxHeaderBytes,
	}

	s.logger.Printf("HTTP server starting on port %d", s.config.Port)
	return s.server.ListenAndServe()
}

func (s *APIServer) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
