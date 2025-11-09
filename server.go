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
	apiMux.HandleFunc("/api/videos/generate-video", s.handleGenerateVideo)
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
		if !HasExtension(name, ExtensionMP4) {
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

func (s *APIServer) handleGenerateVideo(w http.ResponseWriter, r *http.Request) {
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

	// Get all MJPEG files in date range
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		http.Error(w, "Failed to read video directory", http.StatusInternalServerError)
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
		if modTime.After(startTime) && modTime.Before(endTime) {
			mjpegFiles = append(mjpegFiles, filepath.Join(s.config.VideoDir, name))
		}
	}

	if len(mjpegFiles) == 0 {
		http.Error(w, "No videos found in the specified date range", http.StatusNotFound)
		return
	}

	// Sort by modification time
	sort.Slice(mjpegFiles, func(i, j int) bool {
		iInfo, _ := os.Stat(mjpegFiles[i])
		jInfo, _ := os.Stat(mjpegFiles[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Create temporary directory for working files (prevents race with storage cleanup)
	tempDir := filepath.Join(s.config.VideoDir, fmt.Sprintf(".temp_export_%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		http.Error(w, "Failed to create temp directory", http.StatusInternalServerError)
		return
	}
	defer os.RemoveAll(tempDir)

	// Copy MJPEG files to temp directory to prevent deletion during export
	s.logger.Printf("Copying %d MJPEG files to temporary directory...", len(mjpegFiles))
	var tempFiles []string
	for i, srcPath := range mjpegFiles {
		tempPath := filepath.Join(tempDir, fmt.Sprintf("segment_%03d.mjpeg", i))
		
		src, err := os.Open(srcPath)
		if err != nil {
			// File may have been deleted by storage cleanup - continue with remaining files
			s.logger.Printf("Warning: Could not open %s: %v", filepath.Base(srcPath), err)
			continue
		}
		
		dst, err := os.Create(tempPath)
		if err != nil {
			src.Close()
			http.Error(w, "Failed to create temp file", http.StatusInternalServerError)
			return
		}
		
		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()
		
		if copyErr != nil {
			http.Error(w, "Failed to copy file", http.StatusInternalServerError)
			return
		}
		
		tempFiles = append(tempFiles, tempPath)
	}

	if len(tempFiles) == 0 {
		http.Error(w, "No videos could be copied (may have been deleted)", http.StatusNotFound)
		return
	}

	s.logger.Printf("Successfully copied %d/%d files", len(tempFiles), len(mjpegFiles))

	// Create concat file using temp files
	concatFile := filepath.Join(tempDir, "concat_list.txt")
	var concatContent strings.Builder
	for _, file := range tempFiles {
		concatContent.WriteString(fmt.Sprintf("file '%s'\n", file))
	}

	if err := os.WriteFile(concatFile, []byte(concatContent.String()), 0644); err != nil {
		http.Error(w, "Failed to create concat file", http.StatusInternalServerError)
		return
	}

	// Generate MP4 using ffmpeg concat with MPEG-4 encoding at native quality/FPS
	outputFile := filepath.Join(tempDir, "output.mp4")
	
	s.logger.Printf("Generating video from %d MJPEG segments at %dx%d@%dfps", 
		len(tempFiles), s.config.VideoResWidth, s.config.VideoResHeight, s.config.VideoFPS)
	
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-loglevel", "warning",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		// MPEG-4 encoding with high quality (native FFmpeg encoder)
		"-c:v", "mpeg4",
		"-q:v", fmt.Sprintf("%d", ExportVideoQuality), // Quality: 1-31 (lower=better)
		"-r", fmt.Sprintf("%d", s.config.VideoFPS), // Force output framerate to match recording
		"-fps_mode", "cfr", // Constant framerate (pad/drop frames as needed)
		"-movflags", "+faststart", // Enable streaming
		"-f", "mp4",
		outputFile,
	)

	// Capture stderr for debugging
	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	s.logger.Printf("Starting FFmpeg encoding of %d segments...", len(tempFiles))
	
	// Start FFmpeg
	if err := cmd.Start(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to start encoding: %v", err), http.StatusInternalServerError)
		return
	}

	// Monitor progress in background
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Log progress while encoding
	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()
	
	lastSize := int64(0)
	for {
		select {
		case err := <-done:
			// Encoding finished
			if err != nil {
				s.logger.Printf("FFmpeg error: %s", stderrBuf.String())
				http.Error(w, fmt.Sprintf("Failed to generate video: %v", err), http.StatusInternalServerError)
				return
			}
			s.logger.Printf("FFmpeg encoding complete!")
			goto encodingDone
		case <-progressTicker.C:
			// Check output file size for progress indication
			if info, err := os.Stat(outputFile); err == nil {
				sizeMB := float64(info.Size()) / BytesPerMB
				speedMBps := float64(info.Size()-lastSize) / BytesPerMB / 5.0
				s.logger.Printf("Encoding progress: %.1f MB (%.1f MB/s)", sizeMB, speedMBps)
				lastSize = info.Size()
			}
		}
	}
encodingDone:

	// Verify output file exists and has content
	info, err := os.Stat(outputFile)
	if err != nil {
		s.logger.Printf("Output file not found: %v", err)
		http.Error(w, "Generated video file not found", http.StatusInternalServerError)
		return
	}
	
	if info.Size() == 0 {
		s.logger.Printf("Output file is empty")
		http.Error(w, "Generated video file is empty", http.StatusInternalServerError)
		return
	}

	s.logger.Printf("Generated video: %.2f MB at %dx%d@%dfps", 
		float64(info.Size())/BytesPerMB,
		s.config.VideoResWidth, s.config.VideoResHeight, s.config.VideoFPS)

	// Open file for reading
	file, err := os.Open(outputFile)
	if err != nil {
		s.logger.Printf("Failed to open output file: %v", err)
		http.Error(w, "Failed to open generated video", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	// Set headers
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dashcam_%s.mp4", time.Now().Format("2006-01-02")))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	// Copy file to response
	written, err := io.Copy(w, file)
	if err != nil {
		s.logger.Printf("Error streaming video: %v (wrote %d bytes)", err, written)
		return
	}
	
	s.logger.Printf("Successfully streamed %d bytes to client", written)
}

var startTime = time.Now()
