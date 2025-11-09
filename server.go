package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type APIServer struct {
	config    *Config
	camera    *Camera
	storage   *StorageManager
	logger    *Logger
	auth      *AuthMiddleware
	server    *http.Server
	indexHTML string
	streamMgr *StreamManager
}

type VideoInfo struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Size      int64     `json:"size"`
	ModTime   time.Time `json:"mod_time"`
	Duration  int       `json:"duration"` // seconds (approximate)
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

func NewAPIServer(config *Config, camera *Camera, storage *StorageManager, logger *Logger) *APIServer {
	auth := NewAuthMiddleware(config.AuthToken)
	streamMgr := NewStreamManager(config, logger)
	streamMgr.videoDir = config.VideoDir
	camera.SetStreamManager(streamMgr)
	return &APIServer{
		config:    config,
		camera:    camera,
		storage:   storage,
		logger:    logger,
		auth:      auth,
		streamMgr: streamMgr,
	}
}

func (s *APIServer) Start() error {
	mux := http.NewServeMux()

	// Health check (no auth)
	mux.HandleFunc("/health", s.handleHealth)

	// UI endpoints (no auth for now, add auth if frontend served elsewhere)
	mux.HandleFunc("/", s.handleUI)
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir("/tmp/dash-of-pi-assets"))))

	// API endpoints (with auth)
	apiMux := http.NewServeMux()
	apiMux.HandleFunc("/api/status", s.handleStatus)
	apiMux.HandleFunc("/api/videos", s.handleListVideos)
	apiMux.HandleFunc("/api/video/download", s.handleDownloadVideo)
	apiMux.HandleFunc("/api/video/latest", s.handleLatestVideo)
	apiMux.HandleFunc("/api/videos/", s.handleServeSegment)
	apiMux.HandleFunc("/api/auth/token", s.handleGetAuthToken)
	apiMux.HandleFunc("/api/config", s.handleGetConfig)
	apiMux.HandleFunc("/api/stream/frame", s.handleStreamFrame)
	apiMux.HandleFunc("/api/stream/ts", s.handleStreamTS)
	apiMux.HandleFunc("/api/stream/m3u8", s.handleStreamM3U8)

	mux.Handle("/api/", s.auth.Check(apiMux))

	s.server = &http.Server{
		Addr:           fmt.Sprintf(":%d", s.config.Port),
		Handler:        mux,
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   15 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	// Start stream manager
	s.streamMgr.Start()

	s.logger.Printf("HTTP server starting on port %d", s.config.Port)
	return s.server.ListenAndServe()
}

func (s *APIServer) Stop() error {
	s.streamMgr.Stop()
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}

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

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, getEmbeddedHTML())
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
			UsedGB:    float64(used) / (1024 * 1024 * 1024),
			CapGB:     s.config.StorageCapGB,
			Percent:   percent,
		},
		Videos: videos,
		Uptime: fmt.Sprintf("%d seconds", int(time.Since(startTime).Seconds())),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *APIServer) handleListVideos(w http.ResponseWriter, r *http.Request) {
	videos, err := s.listVideoFiles()
	if err != nil {
		http.Error(w, "Failed to list videos", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"videos": videos,
	})
}

func (s *APIServer) handleDownloadVideo(w http.ResponseWriter, r *http.Request) {
	filename := r.URL.Query().Get("file")
	if filename == "" {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	if filepath.Dir(filename) != "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	filepath := filepath.Join(s.config.VideoDir, filename)

	// Verify file exists and is in video directory
	if _, err := os.Stat(filepath); err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	file, err := os.Open(filepath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

func (s *APIServer) handleLatestVideo(w http.ResponseWriter, r *http.Request) {
	// List all video files in directory
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		http.Error(w, "Failed to list videos", http.StatusInternalServerError)
		return
	}

	// Collect all video files sorted by modification time
	type fileInfo struct {
		name    string
		modTime time.Time
	}
	var files []fileInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Accept both mp4 and webm
		if !strings.HasSuffix(name, ".mp4") && !strings.HasSuffix(name, ".webm") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, fileInfo{name: name, modTime: info.ModTime()})
	}

	if len(files) == 0 {
		http.Error(w, "No videos available", http.StatusNotFound)
		return
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Serve the second-newest file (previous segment) to avoid serving incomplete current recording
	fileToServe := files[0].name
	if len(files) > 1 {
		fileToServe = files[1].name
	}

	videoPath := filepath.Join(s.config.VideoDir, fileToServe)

	// Detect format from file extension
	contentType := "video/webm"
	if strings.HasSuffix(videoPath, ".mp4") {
		contentType = "video/mp4"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeFile(w, r, videoPath)
}

func (s *APIServer) handleServeSegment(w http.ResponseWriter, r *http.Request) {
	// Extract filename from path /api/videos/filename
	filename := filepath.Base(r.URL.Path)
	if filename == "" || filename == "videos" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	if filepath.Dir(filename) != "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	videoPath := filepath.Join(s.config.VideoDir, filename)

	// Verify file exists
	if _, err := os.Stat(videoPath); err != nil {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, videoPath)
}

func (s *APIServer) handleGetAuthToken(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"token": s.config.AuthToken,
	})
}

func (s *APIServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"port":              s.config.Port,
		"stream_port":       s.config.StreamPort,
		"storage_cap_gb":    s.config.StorageCapGB,
		"video_bitrate":     s.config.VideoBitrate,
		"video_fps":         s.config.VideoFPS,
		"video_width":       s.config.VideoResWidth,
		"video_height":      s.config.VideoResHeight,
		"segment_length_s":  s.config.SegmentLengthS,
	})
}

// getLatestVideoFile returns the path to the most recent completed video segment
func (s *APIServer) getLatestVideoFile() string {
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		return ""
	}

	type fileInfo struct {
		name    string
		modTime time.Time
		path    string
	}
	var files []fileInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".mp4") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, fileInfo{
			name:    name,
			modTime: info.ModTime(),
			path:    filepath.Join(s.config.VideoDir, name),
		})
	}

	if len(files) == 0 {
		return ""
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Return the second-newest file (previous/completed segment) to avoid reading currently-recording file
	if len(files) > 1 {
		return files[1].path
	}
	return files[0].path
}

// handleStreamFrame serves the latest JPEG frame from the live stream
func (s *APIServer) handleStreamFrame(w http.ResponseWriter, r *http.Request) {
	// Get latest frame from stream manager
	frameData := s.streamMgr.GetLatestFrame()
	if frameData == nil || len(frameData) == 0 {
		http.Error(w, "No frame available", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(frameData)))
	w.Write(frameData)
}

// handleStreamTS serves the live MPEG-TS stream
func (s *APIServer) handleStreamTS(w http.ResponseWriter, r *http.Request) {
	s.streamMgr.ServeHTTP(w, r)
}

// handleStreamM3U8 serves HLS playlist for compatibility
func (s *APIServer) handleStreamM3U8(w http.ResponseWriter, r *http.Request) {
	s.streamMgr.ServeStreamM3U8(w, r)
}

func (s *APIServer) listVideoFiles() ([]VideoInfo, error) {
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		return nil, err
	}

	var videos []VideoInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if !isVideoFile(entry.Name()) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		// Rough estimate: bytes / (bitrate * 128) = seconds
		duration := int(info.Size() / int64(s.config.VideoBitrate*128))

		videos = append(videos, VideoInfo{
			Name:     entry.Name(),
			Path:     fmt.Sprintf("/api/video/download?file=%s&token=%s", entry.Name(), s.config.AuthToken),
			Size:     info.Size(),
			ModTime:  info.ModTime(),
			Duration: duration,
		})
	}

	// Sort by modification time (newest first)
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].ModTime.After(videos[j].ModTime)
	})

	return videos, nil
}



var startTime = time.Now()
