package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Camera handles video capture and recording
type Camera struct {
	config        *Config
	logger        *Logger
	done          chan struct{}
	streamManager *StreamManager
	lastErrorTime time.Time
	recordCmd     *exec.Cmd
	cmdMu         sync.Mutex
	videoEncoder  string
}

func NewCamera(config *Config, logger *Logger) (*Camera, error) {
	camera := &Camera{
		config: config,
		logger: logger,
		done:   make(chan struct{}),
	}
	
	// Detect available encoder on startup
	camera.videoEncoder = detectVideoEncoder(logger)
	logger.Printf("Using video encoder: %s", camera.videoEncoder)
	
	return camera, nil
}

// SetStreamManager connects the camera to a stream manager
func (c *Camera) SetStreamManager(sm *StreamManager) {
	c.streamManager = sm
}

// Start begins continuous recording and streaming
func (c *Camera) Start(videoDir string) error {
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		return fmt.Errorf("failed to create video directory: %w", err)
	}

	// Start background frame extraction to cache frames for faster /api/stream/frame responses
	go c.backgroundFrameUpdate(videoDir)

	for {
		select {
		case <-c.done:
			return nil
		default:
		}

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		// Record to MJPEG (Motion JPEG) - supports real-time streaming and safe interruption recovery
		// Each frame is a complete JPEG, so files remain readable during recording
		filename := filepath.Join(videoDir, fmt.Sprintf("dashcam_%s.mjpeg", timestamp))

		c.logger.Debugf("Starting recording segment: %s", filename)

		if err := c.recordAndStreamSegment(filename); err != nil {
			if time.Since(c.lastErrorTime) > 5*time.Second {
				c.logger.Printf("Recording error: %v", err)
				c.lastErrorTime = time.Now()
			}
		}

		select {
		case <-c.done:
			return nil
		default:
			c.logger.Debugf("Segment completed, starting next recording...")
		}
	}
}

// backgroundFrameUpdate continuously extracts and caches frames from the latest segment
// This ensures fresh frames are always available for the /api/stream/frame endpoint
// Runs at 10 Hz (100ms) for near-realtime performance
func (c *Camera) backgroundFrameUpdate(videoDir string) {
	ticker := time.NewTicker(100 * time.Millisecond) // Update frame at 10 Hz
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			frameData := c.ExtractFrameFromLatestSegment(videoDir)
			if len(frameData) > 0 && c.streamManager != nil {
				c.streamManager.UpdateFrame(frameData)
			}
		}
	}
}

// recordAndStreamSegment records video to MJPEG (Motion JPEG) format
// MJPEG supports real-time streaming and safe recovery from interrupted recordings
// Each frame is a complete JPEG, so the file is always readable even while recording
func (c *Camera) recordAndStreamSegment(filename string) error {
	inputFormat, inputDevice := c.getCameraInput()

	args := []string{
		"-y",
		"-loglevel", "warning",
		"-f", inputFormat,
	}

	if inputFormat == "video4linux2" || inputFormat == "v4l2" {
		args = append(args,
			"-input_format", "mjpeg",
			"-video_size", fmt.Sprintf("%dx%d", c.config.VideoResWidth, c.config.VideoResHeight),
		)
	}

	args = append(args,
		"-framerate", fmt.Sprintf("%d", c.config.VideoFPS),
		"-rtbufsize", "5M",
		"-thread_queue_size", "16",
		"-i", inputDevice,
	)

	// Build video filters
	var videoFilters []string
	if inputFormat != "video4linux2" && inputFormat != "v4l2" {
		videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d", c.config.VideoResWidth, c.config.VideoResHeight))
	}
	if c.config.EmbedTimestamp {
		timestampFilter := "drawtext=text='%{gmtime\\:%Y-%m-%d %H\\\\\\:%M\\\\\\:%S} \\\\(UTC\\\\)':fontcolor=white:fontsize=24:box=1:boxcolor=black@0.5:boxborderw=5:x=10:y=10"
		videoFilters = append(videoFilters, timestampFilter)
	}
	if len(videoFilters) > 0 {
		args = append(args, "-vf", strings.Join(videoFilters, ","))
	}

	// Encode to MJPEG (Motion JPEG) for real-time streaming and robust recovery
	args = append(args,
		"-c:v", "mjpeg",
		"-q:v", "8",  // JPEG quality (2-31, lower=better, 8=good balance)
		"-r", fmt.Sprintf("%d", c.config.VideoFPS),
		"-t", fmt.Sprintf("%d", c.config.SegmentLengthS),
		"-f", "mjpeg",
		filename,
	)

	recordCmd := exec.Command("ffmpeg", args...)

	stderr, err := recordCmd.StderrPipe()
	if err != nil {
		return err
	}

	c.cmdMu.Lock()
	c.recordCmd = recordCmd
	c.cmdMu.Unlock()

	if err := recordCmd.Start(); err != nil {
		c.cmdMu.Lock()
		c.recordCmd = nil
		c.cmdMu.Unlock()
		return err
	}

	// Capture stderr for debugging
	var stderrOutput strings.Builder
	go func() {
		buf := make([]byte, FFmpegStderrBufferKB*BytesPerKB)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				stderrOutput.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for recording to complete
	recordErr := recordCmd.Wait()

	c.cmdMu.Lock()
	c.recordCmd = nil
	c.cmdMu.Unlock()

	if recordErr != nil && stderrOutput.Len() > 0 {
		c.logger.Printf("FFmpeg error output: %s", stderrOutput.String())
	}

	return recordErr
}

// ExtractFrameFromLatestSegment extracts a JPEG frame from the most recent MJPEG segment
// MJPEG is just concatenated JPEGs, so we read the last JPEG directly from the file
// This is near-instantaneous (no FFmpeg overhead) and works even while recording
func (c *Camera) ExtractFrameFromLatestSegment(videoDir string) []byte {
	// Find the latest MJPEG file
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		c.logger.Printf("[WARN] Failed to read video directory '%s': %v", videoDir, err)
		return nil
	}

	var latestFile string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".mjpeg") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			c.logger.Debugf("Failed to get info for file '%s': %v", name, err)
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = filepath.Join(videoDir, name)
		}
	}

	if latestFile == "" {
		c.logger.Debugf("No video segments found in '%s' - recording may be initializing", videoDir)
		return nil
	}

	// Extract the last JPEG frame directly from the MJPEG file
	// MJPEG = concatenated JPEGs with markers: FFD8 (start) ... FFD9 (end)
	frameData := extractLastJPEGFromMJPEG(latestFile)
	if len(frameData) == 0 {
		c.logger.Debugf("Could not extract JPEG frame from '%s'", filepath.Base(latestFile))
		return nil
	}

	return frameData
}

// extractLastJPEGFromMJPEG reads the last complete JPEG frame from an MJPEG file
// by scanning backwards for JPEG markers. This is near-instantaneous (no FFmpeg).
func extractLastJPEGFromMJPEG(filepath string) []byte {
	file, err := os.Open(filepath)
	if err != nil {
		return nil
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil
	}
	fileSize := fileInfo.Size()

	if fileSize < 4 {
		return nil // Too small to be a valid JPEG
	}

	// Read the last 1MB of the file (should contain at least one complete JPEG frame)
	readSize := int64(1024 * 1024) // 1MB
	if readSize > fileSize {
		readSize = fileSize
	}

	startPos := fileSize - readSize
	_, err = file.Seek(startPos, 0)
	if err != nil {
		return nil
	}

	buf := make([]byte, readSize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}
	buf = buf[:n]

	// Scan backwards for JPEG end marker (FFD9) followed by next start marker (FFD8) or EOF
	// Work backwards from the end to find the last complete JPEG frame
	var jpegEnd int64 = -1
	var jpegStart int64 = -1

	// Find the last FFD9 (JPEG end marker)
	for i := len(buf) - 1; i > 0; i-- {
		if buf[i] == 0xD9 && buf[i-1] == 0xFF {
			jpegEnd = int64(i) + 1
			break
		}
	}

	if jpegEnd == -1 {
		return nil // No JPEG end marker found
	}

	// Find the FFD8 (JPEG start marker) before the end
	// Limit search to MaxFrameSizeKB to avoid finding very old frames
	searchLimit := int(jpegEnd) - (MaxFrameSizeKB * BytesPerKB)
	if searchLimit < 0 {
		searchLimit = 0
	}

	for i := int(jpegEnd) - 2; i >= searchLimit; i-- {
		if buf[i] == 0xD8 && buf[i-1] == 0xFF {
			jpegStart = int64(i) - 1
			break
		}
	}

	if jpegStart == -1 {
		return nil // No JPEG start marker found
	}

	// Return the JPEG frame
	return buf[jpegStart:jpegEnd]
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// getCameraInput returns the format and device based on OS
func (c *Camera) getCameraInput() (string, string) {
	switch runtime.GOOS {
	case "darwin":
		return "avfoundation", "0"
	case "windows":
		return "dshow", "video=\"USB Video Device\""
	default:
		device := c.config.CameraDevice
		if device == "" {
			device = "/dev/video0"
		}
		return "v4l2", device
	}
}

// Stop halts the recording
func (c *Camera) Stop() {
	close(c.done)
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	if c.recordCmd != nil && c.recordCmd.Process != nil {
		c.recordCmd.Process.Kill()
	}
}

// detectVideoEncoder checks available encoders and returns the best one
// Priority: h264_v4l2m2m (Pi hardware) > h264_vaapi (generic hardware) > libopenh264 (open) > libx264 (fallback)
func detectVideoEncoder(logger *Logger) string {
	cmd := exec.Command("ffmpeg", "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Debugf("Failed to query FFmpeg encoders: %v", err)
		return "libopenh264"
	}
	
	encodersOutput := string(output)
	
	// Priority list of encoders to check
	preferredEncoders := []string{
		"h264_v4l2m2m",  // Raspberry Pi hardware encoder
		"h264_vaapi",    // Generic hardware encoder
		"libopenh264",   // Open-source H.264
		"libx264",       // Software fallback
	}
	
	for _, encoder := range preferredEncoders {
		if strings.Contains(encodersOutput, encoder) {
			// Test if encoder actually works with a quick validation
			if isEncoderUsable(encoder, logger) {
				return encoder
			}
		}
	}
	
	// Ultimate fallback
	logger.Printf("[WARN] No suitable H.264 encoders found, defaulting to libopenh264")
	return "libopenh264"
}

// isEncoderUsable tests if an encoder can actually be used
func isEncoderUsable(encoder string, logger *Logger) bool {
	// Skip hardware encoders that require specific hardware
	if encoder == "h264_v4l2m2m" || encoder == "h264_vaapi" {
		// Try a quick test to see if the encoder works
		testCmd := exec.Command("ffmpeg",
			"-f", "lavfi",
			"-i", "color=c=black:s=640x480:d=0.1",
			"-c:v", encoder,
			"-f", "null",
			"-",
		)
		
		if err := testCmd.Run(); err != nil {
			logger.Debugf("Encoder %s not usable: %v", encoder, err)
			return false
		}
	}
	
	return true
}

// StreamManager handles HTTP streaming of video to clients
type StreamManager struct {
	logger      *Logger
	config      *Config
	done        chan struct{}
	stopOnce    sync.Once
	mu          sync.RWMutex
	latestFrame []byte
}

func NewStreamManager(config *Config, logger *Logger) *StreamManager {
	return &StreamManager{
		logger: logger,
		config: config,
		done:   make(chan struct{}),
	}
}

// Start initializes the stream manager
func (sm *StreamManager) Start() error {
	return nil
}

// Stop halts the stream manager
func (sm *StreamManager) Stop() {
	sm.stopOnce.Do(func() {
		close(sm.done)
	})
}

// UpdateFrame stores the latest frame
func (sm *StreamManager) UpdateFrame(frameData []byte) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if len(frameData) > 0 {
		sm.latestFrame = make([]byte, len(frameData))
		copy(sm.latestFrame, frameData)
	}
}

// ServeJPEG returns the latest frame as JPEG
func (sm *StreamManager) ServeJPEG(w http.ResponseWriter, r *http.Request) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.latestFrame) == 0 {
		http.Error(w, "No frame available", http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(sm.latestFrame)))
	w.Write(sm.latestFrame)
}

// GetLatestFrame returns the latest JPEG frame
func (sm *StreamManager) GetLatestFrame() []byte {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.latestFrame) == 0 {
		return nil
	}

	frame := make([]byte, len(sm.latestFrame))
	copy(frame, sm.latestFrame)
	return frame
}
