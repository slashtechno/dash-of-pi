package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type APIServer struct {
	config      *Config
	camera      *Camera
	storage     *StorageManager
	logger      *Logger
	auth        *AuthMiddleware
	server      *http.Server
	indexHTML   string
	streamMgr   *StreamManager
	exportInfo  *ExportInfo
	exportMutex sync.RWMutex
}

type ExportInfo struct {
	Filename       string    `json:"filename"`
	StartTime      time.Time `json:"start_time"`
	EndTime        time.Time `json:"end_time"`
	Size           int64     `json:"size"`
	Available      bool      `json:"available"`
	InProgress     bool      `json:"in_progress"`
	Progress       string    `json:"progress"`        // Human-readable progress message
	CurrentSizeMB  float64   `json:"current_size_mb"` // Current size during encoding
	TotalSegments  int       `json:"total_segments"`  // Total number of segments to process
	ProcessedFiles int       `json:"processed_files"` // Number of files copied so far
}

type VideoInfo struct {
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Duration int       `json:"duration"` // seconds (approximate)
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
	camera.SetStreamManager(streamMgr)

	server := &APIServer{
		config:     config,
		camera:     camera,
		storage:    storage,
		logger:     logger,
		auth:       auth,
		streamMgr:  streamMgr,
		exportInfo: &ExportInfo{Available: false},
	}

	// Check for existing export on startup
	server.checkExistingExport()

	return server
}

func (s *APIServer) checkExistingExport() {
	// First, clean up any leftover temporary export directories
	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d temporary export director%s", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", "current_export.mp4")
	infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")

	if info, err := os.Stat(exportPath); err == nil {
		if infoData, err := os.ReadFile(infoPath); err == nil {
			var exportInfo ExportInfo
			if err := json.Unmarshal(infoData, &exportInfo); err == nil {
				// Only mark as available if it was completed (not in progress)
				if !exportInfo.InProgress {
					exportInfo.Size = info.Size()
					exportInfo.Available = true
					s.exportMutex.Lock()
					s.exportInfo = &exportInfo
					s.exportMutex.Unlock()
					s.logger.Printf("Found existing export: %.2f MB from %s to %s",
						float64(info.Size())/BytesPerMB,
						exportInfo.StartTime.Format(time.RFC3339),
						exportInfo.EndTime.Format(time.RFC3339))
				} else {
					// Export was interrupted, clean it up
					s.logger.Printf("Found interrupted export, cleaning up...")
					os.Remove(exportPath)
					os.Remove(infoPath)
					s.exportMutex.Lock()
					s.exportInfo = &ExportInfo{
						Available:  false,
						InProgress: false,
						Progress:   "Previous export was interrupted",
					}
					s.exportMutex.Unlock()
				}
			}
		}
	}
}

func (s *APIServer) Start() error {
	mux := http.NewServeMux()

	// Health check (no auth)
	mux.HandleFunc("/health", s.handleHealth)

	// UI endpoints (no auth for now, add auth if frontend served elsewhere)
	mux.HandleFunc("/", s.handleUI)

	// Serve static files from web directory
	// Check multiple locations for the web directory
	possibleWebDirs := []string{
		"./web",                   // Relative to working directory
		"/var/lib/dash-of-pi/web", // Systemd service location
		filepath.Join(filepath.Dir(os.Args[0]), "../web"), // Relative to binary
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
		if !IsPlayableVideo(name) {
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

	// Set content type based on file extension
	contentType := "video/mp4"
	if HasExtension(videoPath, ExtensionWebM) {
		contentType = "video/webm"
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
		"port":             s.config.Port,
		"storage_cap_gb":   s.config.StorageCapGB,
		"video_bitrate":    s.config.VideoBitrate,
		"video_fps":        s.config.VideoFPS,
		"video_width":      s.config.VideoResWidth,
		"video_height":     s.config.VideoResHeight,
		"segment_length_s": s.config.SegmentLengthS,
	})
}

// handleStreamFrame serves the latest JPEG frame from the live stream
func (s *APIServer) handleStreamFrame(w http.ResponseWriter, r *http.Request) {
	// Get latest frame from stream manager
	frameData := s.streamMgr.GetLatestFrame()
	if len(frameData) == 0 {
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

// handleStreamMJPEG serves continuous MJPEG stream (multipart)
func (s *APIServer) handleStreamMJPEG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "close")

	boundary := "frame"
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	s.logger.Printf("MJPEG stream client connected")
	defer s.logger.Printf("MJPEG stream client disconnected")

	// Stream frames continuously at target FPS
	ticker := time.NewTicker(time.Duration(MJPEGStreamIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	frameCount := 0
	noFrameCount := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			frameData := s.streamMgr.GetLatestFrame()
			if len(frameData) == 0 {
				noFrameCount++
				if noFrameCount > MJPEGNoFrameTimeout {
					s.logger.Printf("MJPEG stream: No frames timeout, closing connection")
					return
				}
				continue
			}
			noFrameCount = 0

			// Write frame to stream
			_, err := fmt.Fprintf(w, "--%s\r\n", boundary)
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "Content-Type: image/jpeg\r\n")
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(frameData))
			if err != nil {
				return
			}
			_, err = w.Write(frameData)
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "\r\n")
			if err != nil {
				return
			}

			flusher.Flush()
			frameCount++

			if frameCount%StreamLogInterval == 0 {
				s.logger.Debugf("MJPEG stream: sent %d frames", frameCount)
			}
		}
	}
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

		// Rough estimate: bytes / (bitrate * multiplier) = seconds
		duration := int(info.Size() / int64(s.config.VideoBitrate*BitrateToStorageMultiplier))

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

// handleGenerateExport generates an export and saves it to disk for later download
func (s *APIServer) handleGenerateExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	if startStr == "" || endStr == "" {
		http.Error(w, "Missing start or end parameter", http.StatusBadRequest)
		return
	}

	// Parse timestamps
	startTime, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		http.Error(w, "Invalid start time format", http.StatusBadRequest)
		return
	}

	endTime, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		http.Error(w, "Invalid end time format", http.StatusBadRequest)
		return
	}

	// Start generation in background
	go s.generateExportAsync(startTime, endTime)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Export generation started",
	})
}

// generateExportAsync generates an export in the background
func (s *APIServer) generateExportAsync(startTime, endTime time.Time) {
	s.logger.Printf("Starting async export generation from %s to %s", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Clean up any leftover temporary export directories from previous runs
	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d temporary export director%s before starting export", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	// Set initial progress state
	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{
		Available:  false,
		InProgress: true,
		Progress:   "Scanning for video files...",
		StartTime:  startTime,
		EndTime:    endTime,
	}
	s.exportMutex.Unlock()

	// Ensure we clean up on panic or unexpected exit
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("Export generation panicked: %v", r)
			s.exportMutex.Lock()
			s.exportInfo = &ExportInfo{
				Available:  false,
				InProgress: false,
				Progress:   "Error: Export generation failed unexpectedly",
			}
			s.exportMutex.Unlock()
			// Clean up any partial export
			exportPath := filepath.Join(s.config.VideoDir, ".export", "current_export.mp4")
			infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")
			os.Remove(exportPath)
			os.Remove(infoPath)
		}
	}()

	// Get all MJPEG files in date range
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		s.logger.Printf("Failed to read video directory: %v", err)
		return
	}

	var mjpegFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsMJPEGFile(name) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		modTime := info.ModTime()
		// Include files within the time range (inclusive of boundaries)
		// Use After/Before for start, and not After for end to include files up to and including endTime
		if (modTime.After(startTime) || modTime.Equal(startTime)) && !modTime.After(endTime) {
			mjpegFiles = append(mjpegFiles, filepath.Join(s.config.VideoDir, name))
		}
	}

	if len(mjpegFiles) == 0 {
		s.logger.Printf("No videos found in the specified date range")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "No videos found in the specified date range",
		}
		s.exportMutex.Unlock()
		return
	}

	// Update progress with total segments found
	s.exportMutex.Lock()
	s.exportInfo.Progress = fmt.Sprintf("Found %d video segments, preparing to copy...", len(mjpegFiles))
	s.exportInfo.TotalSegments = len(mjpegFiles)
	s.exportMutex.Unlock()

	// Sort by modification time
	sort.Slice(mjpegFiles, func(i, j int) bool {
		iInfo, _ := os.Stat(mjpegFiles[i])
		jInfo, _ := os.Stat(mjpegFiles[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Create temporary directory for working files
	tempDir := filepath.Join(s.config.VideoDir, fmt.Sprintf(".temp_export_%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.logger.Printf("Failed to create temp directory: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Failed to create temporary directory",
		}
		s.exportMutex.Unlock()
		return
	}
	defer os.RemoveAll(tempDir)

	// Copy MJPEG files to temp directory
	s.logger.Printf("Copying %d MJPEG files to temporary directory...", len(mjpegFiles))
	var tempFiles []string
	for i, srcPath := range mjpegFiles {
		// Update progress every 10 files
		if i%10 == 0 {
			s.exportMutex.Lock()
			s.exportInfo.Progress = fmt.Sprintf("Copying files... %d/%d", i, len(mjpegFiles))
			s.exportInfo.ProcessedFiles = i
			s.exportMutex.Unlock()
		}
		tempPath := filepath.Join(tempDir, fmt.Sprintf("segment_%03d.mjpeg", i))

		src, err := os.Open(srcPath)
		if err != nil {
			s.logger.Printf("Warning: Could not open %s: %v", filepath.Base(srcPath), err)
			continue
		}

		dst, err := os.Create(tempPath)
		if err != nil {
			src.Close()
			s.logger.Printf("Failed to create temp file: %v", err)
			return
		}

		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()

		if copyErr != nil {
			s.logger.Printf("Failed to copy file: %v", copyErr)
			return
		}

		tempFiles = append(tempFiles, tempPath)
	}

	if len(tempFiles) == 0 {
		s.logger.Printf("No videos could be copied (may have been deleted)")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: No videos could be copied (files may have been deleted)",
		}
		s.exportMutex.Unlock()
		return
	}

	s.logger.Printf("Successfully copied %d/%d files", len(tempFiles), len(mjpegFiles))
	s.exportMutex.Lock()
	s.exportInfo.Progress = fmt.Sprintf("Copied %d files, preparing to encode...", len(tempFiles))
	s.exportInfo.ProcessedFiles = len(tempFiles)
	s.exportMutex.Unlock()

	// Create concat file
	concatFile := filepath.Join(tempDir, "concat_list.txt")
	var concatContent strings.Builder
	for _, file := range tempFiles {
		concatContent.WriteString(fmt.Sprintf("file '%s'\n", file))
	}

	if err := os.WriteFile(concatFile, []byte(concatContent.String()), 0644); err != nil {
		s.logger.Printf("Failed to create concat file: %v", err)
		return
	}

	// Create export directory
	exportDir := filepath.Join(s.config.VideoDir, ".export")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		s.logger.Printf("Failed to create export directory: %v", err)
		return
	}

	// Delete old export if exists
	oldExportPath := filepath.Join(exportDir, "current_export.mp4")
	os.Remove(oldExportPath)
	os.Remove(filepath.Join(exportDir, "export_info.json"))
	s.logger.Printf("Removed old export if it existed")

	// Generate MP4
	outputFile := filepath.Join(exportDir, "current_export.mp4")

	s.logger.Printf("Generating video from %d MJPEG segments at %dx%d@%dfps",
		len(tempFiles), s.config.VideoResWidth, s.config.VideoResHeight, s.config.VideoFPS)

	s.exportMutex.Lock()
	s.exportInfo.Progress = "Encoding video with FFmpeg..."
	s.exportMutex.Unlock()

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-loglevel", "error",
		"-fflags", "+discardcorrupt",
		"-err_detect", "ignore_err",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c:v", "mpeg4",
		"-q:v", fmt.Sprintf("%d", ExportVideoQuality),
		"-r", fmt.Sprintf("%d", s.config.VideoFPS),
		"-fps_mode", "cfr",
		"-movflags", "+faststart",
		"-f", "mp4",
		outputFile,
	)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	s.logger.Printf("Starting FFmpeg encoding of %d segments...", len(tempFiles))

	if err := cmd.Start(); err != nil {
		s.logger.Printf("Failed to start encoding: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Failed to start FFmpeg encoding",
		}
		s.exportMutex.Unlock()
		return
	}

	// Monitor progress
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	lastSize := int64(0)
	for {
		select {
		case err := <-done:
			if err != nil {
				s.logger.Printf("FFmpeg error: %s", stderrBuf.String())
				s.exportMutex.Lock()
				s.exportInfo = &ExportInfo{
					Available:  false,
					InProgress: false,
					Progress:   "Error: FFmpeg encoding failed",
				}
				s.exportMutex.Unlock()
				return
			}
			s.logger.Printf("FFmpeg encoding complete!")
			goto encodingDone
		case <-progressTicker.C:
			if info, err := os.Stat(outputFile); err == nil {
				sizeMB := float64(info.Size()) / BytesPerMB
				speedMBps := float64(info.Size()-lastSize) / BytesPerMB / 5.0
				s.logger.Printf("Encoding progress: %.1f MB (%.1f MB/s)", sizeMB, speedMBps)
				lastSize = info.Size()

				// Update progress for frontend
				s.exportMutex.Lock()
				s.exportInfo.Progress = fmt.Sprintf("Encoding... %.1f MB (%.1f MB/s)", sizeMB, speedMBps)
				s.exportInfo.CurrentSizeMB = sizeMB
				s.exportMutex.Unlock()
			}
		}
	}
encodingDone:

	// Verify output file
	info, err := os.Stat(outputFile)
	if err != nil {
		s.logger.Printf("Output file not found: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Output file not found",
		}
		s.exportMutex.Unlock()
		return
	}

	if info.Size() == 0 {
		s.logger.Printf("Output file is empty")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Output file is empty",
		}
		s.exportMutex.Unlock()
		return
	}

	s.logger.Printf("Generated export: %.2f MB at %dx%d@%dfps",
		float64(info.Size())/BytesPerMB,
		s.config.VideoResWidth, s.config.VideoResHeight, s.config.VideoFPS)

	// Save export info
	exportInfo := ExportInfo{
		Filename:      "current_export.mp4",
		StartTime:     startTime,
		EndTime:       endTime,
		Size:          info.Size(),
		Available:     true,
		InProgress:    false,
		Progress:      "Complete",
		CurrentSizeMB: float64(info.Size()) / BytesPerMB,
	}

	infoPath := filepath.Join(exportDir, "export_info.json")
	infoData, _ := json.Marshal(exportInfo)
	os.WriteFile(infoPath, infoData, 0644)

	s.exportMutex.Lock()
	s.exportInfo = &exportInfo
	s.exportMutex.Unlock()

	s.logger.Printf("Export ready for download")
}

// handleExportStatus returns the status of the current export
func (s *APIServer) handleExportStatus(w http.ResponseWriter, r *http.Request) {
	s.exportMutex.RLock()
	defer s.exportMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.exportInfo)
}

// handleDownloadExport serves the current export file
func (s *APIServer) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	s.exportMutex.RLock()
	available := s.exportInfo.Available
	s.exportMutex.RUnlock()

	if !available {
		http.Error(w, "No export available", http.StatusNotFound)
		return
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", "current_export.mp4")
	info, err := os.Stat(exportPath)
	if err != nil {
		http.Error(w, "Export file not found", http.StatusNotFound)
		return
	}

	file, err := os.Open(exportPath)
	if err != nil {
		http.Error(w, "Failed to open export file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dashcam_export_%s.mp4", time.Now().Format("2006-01-02")))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "no-cache")

	io.Copy(w, file)
	s.logger.Printf("Export downloaded by client")
}

// handleDeleteExport deletes the current export
func (s *APIServer) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", "current_export.mp4")
	infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")

	os.Remove(exportPath)
	os.Remove(infoPath)

	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{Available: false}
	s.exportMutex.Unlock()

	s.logger.Printf("Export deleted")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
	})
}

var startTime = time.Now()
