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

	"golang.org/x/exp/mmap"
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
}

func NewCamera(config *Config, logger *Logger) (*Camera, error) {
	return &Camera{
		config: config,
		logger: logger,
		done:   make(chan struct{}),
	}, nil
}

// SetStreamManager connects the camera to a stream manager for live streaming
func (c *Camera) SetStreamManager(sm *StreamManager) {
	c.streamManager = sm
}

// Start begins continuous recording and streaming from a single camera capture
func (c *Camera) Start(videoDir string) error {
	// Ensure video directory exists
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		return fmt.Errorf("failed to create video directory: %w", err)
	}

	// Single FFmpeg process captures once and outputs to both file and stream
	for {
		select {
		case <-c.done:
			return nil
		default:
		}

		timestamp := time.Now().Format("2006-01-02_15-04-05")
		filename := filepath.Join(videoDir, fmt.Sprintf("dashcam_%s.mjpeg", timestamp))

		c.logger.Debugf("Starting recording segment: %s", filename)

		// Record and stream simultaneously from single camera input
		if err := c.recordAndStreamSegment(filename); err != nil {
			// Avoid spamming logs with repeated errors
			if time.Since(c.lastErrorTime) > 5*time.Second {
				c.logger.Printf("Recording error: %v", err)
				c.lastErrorTime = time.Now()
			}
		}

		// Check if we should stop
		select {
		case <-c.done:
			return nil
		default:
			c.logger.Debugf("Segment completed, starting next recording...")
		}
	}
}

// recordAndStreamSegment records to file AND streams JPEG frames
func (c *Camera) recordAndStreamSegment(filename string) error {
	// Get camera input based on OS
	inputFormat, inputDevice := c.getCameraInput()

	// Build FFmpeg command with memory-efficient settings for Pi Zero 2W
	args := []string{
		"-y",
		"-loglevel", "warning",
		"-f", inputFormat,
	}

	// For v4l2 (Linux), request specific input format to reduce memory usage
	if inputFormat == "video4linux2" || inputFormat == "v4l2" {
		// Request MJPEG from camera if possible (reduces CPU/memory load)
		args = append(args,
			"-input_format", "mjpeg",
			"-video_size", fmt.Sprintf("%dx%d", c.config.VideoResWidth, c.config.VideoResHeight),
		)
	}

	args = append(args,
		"-framerate", fmt.Sprintf("%d", c.config.VideoFPS),
		// Memory-efficient buffer settings for Pi Zero 2W (512MB RAM)
		"-rtbufsize", "5M", // Reduce real-time buffer size (default can be 3GB!)
		"-thread_queue_size", "16", // Reduce thread queue (default 8, but keep minimal)
		"-i", inputDevice,
	)

	// Build video filter chain
	var videoFilters []string

	// Only scale if camera doesn't support native resolution
	if inputFormat != "video4linux2" && inputFormat != "v4l2" {
		videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d", c.config.VideoResWidth, c.config.VideoResHeight))
	}

	// Add timestamp overlay if enabled
	if c.config.EmbedTimestamp {
		// Format: YYYY-MM-DD HH:MM:SS (UTC)
		// Position: top-left with 10px padding
		// Font size: 24, white text with black shadow for readability
		// Note: In FFmpeg drawtext, colons in text need escaping, parentheses need double escaping
		// Use gmtime instead of localtime to display UTC
		timestampFilter := "drawtext=text='%{gmtime\\:%Y-%m-%d %H\\\\\\:%M\\\\\\:%S} \\\\(UTC\\\\)':fontcolor=white:fontsize=24:box=1:boxcolor=black@0.5:boxborderw=5:x=10:y=10"
		videoFilters = append(videoFilters, timestampFilter)
	}

	// Apply video filters if any
	if len(videoFilters) > 0 {
		args = append(args, "-vf", strings.Join(videoFilters, ","))
	}

	args = append(args,
		"-c:v", "mjpeg",
		"-q:v", fmt.Sprintf("%d", c.config.MJPEGQuality),
		"-r", fmt.Sprintf("%d", c.config.VideoFPS),
		"-huffman", "optimal", // Use optimal Huffman tables (better compression)
		"-force_duplicated_matrix", "1", // Ensure proper quantization matrices
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

	// Capture ffmpeg stderr to help diagnose issues
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

	// Extract frames from the MJPEG file as it's being written
	go func() {
		ticker := time.NewTicker(time.Duration(FrameExtractionMS) * time.Millisecond)
		defer ticker.Stop()
		frameCount := int64(0)
		var lastFrameHash uint64 // Simple hash to detect duplicates

		for {
			select {
			case <-ticker.C:
				frameData := c.extractFrameFromMJPEG(filename)
				if len(frameData) > 0 && c.streamManager != nil {
					// Compute simple hash to detect duplicate frames
					// Using first/last bytes and length as a quick hash
					frameHash := uint64(len(frameData))
					if len(frameData) >= 16 {
						frameHash ^= uint64(frameData[0])<<56 | uint64(frameData[8])<<32 |
							uint64(frameData[len(frameData)-8])<<16 | uint64(frameData[len(frameData)-1])
					}

					// Only update if frame changed
					if frameHash != lastFrameHash {
						c.streamManager.UpdateFrame(frameData)
						lastFrameHash = frameHash
						frameCount++
					}
				}
			case <-time.After(time.Duration(c.config.SegmentLengthS) * time.Second):
				c.logger.Debugf("Segment complete: extracted %d unique frames", frameCount)
				return
			}
		}
	}()

	// Wait for recording to complete
	recordErr := recordCmd.Wait()

	c.cmdMu.Lock()
	c.recordCmd = nil
	c.cmdMu.Unlock()

	// Log ffmpeg errors if recording failed
	if recordErr != nil && stderrOutput.Len() > 0 {
		c.logger.Printf("FFmpeg error output: %s", stderrOutput.String())
	}

	return recordErr
}

// extractFrameFromMJPEG extracts a frame from an MJPEG file using mmap for efficiency
func (c *Camera) extractFrameFromMJPEG(filename string) []byte {
	// Use memory mapping for efficient random access (especially on Pi)
	r, err := mmap.Open(filename)
	if err != nil {
		// Fallback to traditional file method if mmap fails
		file, err := os.Open(filename)
		if err != nil {
			return nil
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return nil
		}
		return c.extractFrameFromMJPEGFallback(file, info.Size())
	}
	defer r.Close()

	// Get the entire mapped data as slice
	bufLen := r.Len()
	if bufLen < MinFileSize {
		return nil
	}

	// Create a temporary buffer to read the data safely
	buf := make([]byte, bufLen)
	n, err := r.ReadAt(buf, 0)
	if err != nil && err != io.EOF {
		return nil
	}
	if n == 0 {
		return nil
	}
	buf = buf[:n]

	// Find the LAST complete JPEG frame by searching backwards from end
	// Step 1: Find the most recent JPEG end marker (0xFF 0xD9)
	lastFrameEnd := -1
	searchStart := len(buf) - 1
	if searchStart < 1 {
		return nil
	}

	// Scan from end backwards for JPEG end marker
	for i := searchStart - 1; i >= 0; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD9 {
			lastFrameEnd = i + 2
			break
		}
	}

	if lastFrameEnd == -1 {
		return nil // No complete frame found
	}

	// Step 2: Search backwards from end marker to find matching start marker
	// Limit search to MaxFrameSizeKB to avoid finding old frames
	searchLimit := lastFrameEnd - (MaxFrameSizeKB * BytesPerKB)
	if searchLimit < 0 {
		searchLimit = 0
	}

	for i := lastFrameEnd - 2; i >= searchLimit; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD8 {
			// Found frame start - extract and return
			frameSize := lastFrameEnd - i
			frameData := make([]byte, frameSize)
			copy(frameData, buf[i:lastFrameEnd])
			return frameData
		}
	}

	return nil // No matching start marker found
}

// extractFrameFromMJPEGFallback uses traditional file reading as fallback
func (c *Camera) extractFrameFromMJPEGFallback(file *os.File, fileSize int64) []byte {
	// Read last portion of file for frame extraction
	readSize := int64(FrameBufferSizeKB * BytesPerKB)
	if fileSize < readSize {
		readSize = fileSize
	}

	_, err := file.Seek(-readSize, 2)
	if err != nil {
		return nil
	}

	buf := make([]byte, readSize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}

	if n == 0 {
		return nil
	}

	// Find the LAST complete JPEG frame by searching backwards from end
	lastFrameEnd := -1
	for i := n - 2; i >= 0; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD9 {
			lastFrameEnd = i + 2
			break
		}
	}

	if lastFrameEnd == -1 {
		return nil
	}

	searchLimit := lastFrameEnd - (MaxFrameSizeKB * BytesPerKB)
	if searchLimit < 0 {
		searchLimit = 0
	}

	for i := lastFrameEnd - 2; i >= searchLimit; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD8 {
			frameSize := lastFrameEnd - i
			frameData := make([]byte, frameSize)
			copy(frameData, buf[i:lastFrameEnd])
			return frameData
		}
	}

	return nil
}

// getCameraInput returns the format and device based on OS
func (c *Camera) getCameraInput() (string, string) {
	switch runtime.GOOS {
	case "darwin":
		return "avfoundation", "0"
	case "windows":
		return "dshow", "video=\"USB Video Device\""
	default: // linux
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

// StreamManager handles HTTP streaming of video to clients
type StreamManager struct {
	logger   *Logger
	config   *Config
	done     chan struct{}
	stopOnce sync.Once
	mu       sync.RWMutex
	frameJPG []byte
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
		sm.frameJPG = make([]byte, len(frameData))
		copy(sm.frameJPG, frameData)
	}
}

// ServeJPEG returns the latest frame as JPEG
func (sm *StreamManager) ServeJPEG(w http.ResponseWriter, r *http.Request) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.frameJPG) == 0 {
		http.Error(w, "No frame available", http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(sm.frameJPG)))
	w.Write(sm.frameJPG)
}

// GetLatestFrame returns the latest JPEG frame
func (sm *StreamManager) GetLatestFrame() []byte {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.frameJPG) == 0 {
		return nil
	}

	frame := make([]byte, len(sm.frameJPG))
	copy(frame, sm.frameJPG)
	return frame
}
