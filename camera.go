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
// Captures video to a temporary file, then re-encodes for streaming while recording
func (c *Camera) recordAndStreamSegment(filename string) error {
	// Get camera input based on OS
	inputFormat, inputDevice := c.getCameraInput()

	// Start recording to MJPEG file with proper framerate metadata
	recordCmd := exec.Command(
		"ffmpeg",
		"-loglevel", "warning",
		"-f", inputFormat,
		"-framerate", fmt.Sprintf("%d", c.config.VideoFPS),
		"-i", inputDevice,
		"-vf", fmt.Sprintf("scale=%d:%d", c.config.VideoResWidth, c.config.VideoResHeight),
		"-c:v", "mjpeg",
		"-q:v", fmt.Sprintf("%d", c.config.MJPEGQuality),
		"-r", fmt.Sprintf("%d", c.config.VideoFPS),
		"-t", fmt.Sprintf("%d", c.config.SegmentLengthS),
		"-f", "mjpeg",
		filename,
	)

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
		buf := make([]byte, 4096)
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
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		frameCount := int64(0)
		failCount := 0

		for {
			select {
			case <-ticker.C:
				frameData := c.extractFrameFromMJPEG(filename)
				if frameData != nil && len(frameData) > 0 && c.streamManager != nil {
					c.streamManager.UpdateFrame(frameData)
					frameCount++
					failCount = 0
					if frameCount%4 == 0 {
						c.logger.Debugf("✓ Frame %d (%d bytes)", frameCount, len(frameData))
					}
				} else {
					failCount++
					if failCount == 1 || failCount == 3 {
						info, _ := os.Stat(filename)
						fileSize := int64(0)
						if info != nil {
							fileSize = info.Size()
						}
						c.logger.Debugf("⏳ Waiting for frames... (file: %d bytes, attempt %d)", fileSize, failCount)
					}
				}
			case <-time.After(time.Duration(c.config.SegmentLengthS) * time.Second):
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

// extractFrameFromMJPEG extracts a frame from an MJPEG file that's being written
func (c *Camera) extractFrameFromMJPEG(filename string) []byte {
	file, err := os.Open(filename)
	if err != nil {
		return nil
	}
	defer file.Close()

	// Get file info
	info, err := file.Stat()
	if err != nil {
		return nil
	}

	fileSize := info.Size()
	if fileSize < 100 {
		return nil
	}

	// For large files, read from near the end to get the most recent frame
	readSize := int64(1024 * 1024) // 1MB
	if fileSize < readSize {
		readSize = fileSize
	}

	// Seek to near the end
	_, err = file.Seek(-readSize, 2)
	if err != nil {
		return nil
	}

	// Read the end portion
	buf := make([]byte, readSize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}

	if n == 0 {
		return nil
	}

	// Find the last complete JPEG frame in this buffer
	// Search backwards from the end
	lastFrameEnd := -1
	lastFrameStart := -1

	// Find the last 0xFF 0xD9 (JPEG end)
	for i := n - 2; i >= 0; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD9 {
			lastFrameEnd = i + 1
			break
		}
	}

	if lastFrameEnd == -1 {
		return nil
	}

	// Find the 0xFF 0xD8 (JPEG start) before that end
	for i := lastFrameEnd - 2; i >= 0; i-- {
		if buf[i] == 0xFF && buf[i+1] == 0xD8 {
			lastFrameStart = i
			break
		}
	}

	if lastFrameStart != -1 && lastFrameEnd > lastFrameStart {
		frameData := make([]byte, lastFrameEnd-lastFrameStart)
		copy(frameData, buf[lastFrameStart:lastFrameEnd])
		return frameData
	}

	return nil
}





// ConvertMJPEGToAVI converts MJPEG to AVI with proper framerate metadata (no re-encoding)
func (c *Camera) ConvertMJPEGToAVI(mjpegFile string) {
	// Create output filename by replacing .mjpeg with .avi
	aviFile := strings.TrimSuffix(mjpegFile, ".mjpeg") + ".avi"

	// Use ffmpeg to re-mux MJPEG to AVI with framerate info, no re-encoding
	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-loglevel", "warning",
		"-framerate", fmt.Sprintf("%d", c.config.VideoFPS),
		"-i", mjpegFile,
		"-c", "copy",
		"-f", "avi",
		aviFile,
	)

	// Suppress output but capture errors
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Log error but don't fail - MJPEG file is still good for recovery
		c.logger.Printf("Warning: failed to convert MJPEG to AVI: %v", err)
		if stderr.Len() > 0 {
			c.logger.Printf("ffmpeg stderr: %s", stderr.String())
		}
	}
}

// readJPEGFrame reads a single JPEG frame from a stream
func readJPEGFrame(r interface{ Read([]byte) (int, error) }, buf []byte) ([]byte, error) {
	var frameBytes []byte
	foundStart := false
	readCount := 0
	maxRead := len(buf) * 10 // Allow multiple buffer reads

	for readCount < maxRead {
		n, err := r.Read(buf)
		if err != nil {
			return nil, err
		}

		data := buf[:n]

		for i := 0; i < len(data)-1; i++ {
			if !foundStart && data[i] == 0xFF && data[i+1] == 0xD8 {
				// Found JPEG start
				foundStart = true
				frameBytes = append(frameBytes, data[i:]...)
				i++
				continue
			}

			if foundStart {
				frameBytes = append(frameBytes, data[i])

				if i < len(data)-1 && data[i] == 0xFF && data[i+1] == 0xD9 {
					// Found JPEG end
					frameBytes = append(frameBytes, data[i+1])
					return frameBytes, nil
				}
			}
		}

		readCount++
	}

	if foundStart {
		return frameBytes, nil
	}
	return nil, fmt.Errorf("no JPEG found")
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
	logger    *Logger
	config    *Config
	done      chan struct{}
	stopOnce  sync.Once
	mu        sync.RWMutex
	frameJPG  []byte
	videoDir  string
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

// UpdateFrameCount is a legacy method for compatibility
func (sm *StreamManager) UpdateFrameCount(count int64) {
	// Noop
}

// ServeHTTP serves video over HTTP
func (sm *StreamManager) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Cache-Control", "no-cache")
	http.Error(w, "Live streaming requires HLS setup", http.StatusNotImplemented)
}

// ServeStreamM3U8 serves HLS manifest
func (sm *StreamManager) ServeStreamM3U8(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	manifest := `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:1
#EXT-X-MEDIA-SEQUENCE:0
#EXTINF:1.0,
/api/stream/ts
#EXT-X-ENDLIST
`
	w.Write([]byte(manifest))
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

// AddClient is for compatibility
func (sm *StreamManager) AddClient(conn interface{}) {
	sm.logger.Printf("Stream client connected")
}

// RemoveClient is for compatibility
func (sm *StreamManager) RemoveClient(conn interface{}) {
	sm.logger.Printf("Stream client disconnected")
}
