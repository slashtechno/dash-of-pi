package camera

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

// LibcameraCapture manages rpicam-vid process for CSI cameras
type LibcameraCapture struct {
	cmd           *exec.Cmd
	cmdMu         sync.Mutex
	done          chan struct{}
	frameChan     chan []byte
	logger        Logger
	camConfig     CameraConfig
	segmentLength int
}

// NewLibcameraCapture creates a libcamera capture instance
func NewLibcameraCapture(config CameraConfig, segmentLength int, logger Logger) (*LibcameraCapture, error) {
	if !isLibcameraAvailable(logger) {
		return nil, fmt.Errorf("libcamera not available")
	}

	return &LibcameraCapture{
		camConfig:     config,
		logger:        logger,
		done:          make(chan struct{}),
		frameChan:     make(chan []byte, 1),
		segmentLength: segmentLength,
	}, nil
}

// isLibcameraAvailable checks if rpicam-vid is installed
func isLibcameraAvailable(logger Logger) bool {
	_, err := exec.LookPath("rpicam-vid")
	if err != nil {
		logger.Debugf("rpicam-vid not found: %v", err)
		return false
	}
	return true
}

// IsCSICamera detects if a device is a CSI camera (libcamera) or USB (V4L2)
func IsCSICamera(logger Logger) bool {
	// If rpicam-vid is available and /dev/video0 exists but doesn't respond to V4L2 properly,
	// assume it's a CSI camera with libcamera
	if !isLibcameraAvailable(logger) {
		return false
	}

	// Check if rpicam-vid can enumerate cameras
	cmd := exec.Command("rpicam-still", "--list-cameras")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Debugf("rpicam-still enumeration failed: %v", err)
		return false
	}

	// If we got output with "camera" in it, libcamera is available
	return strings.Contains(strings.ToLower(string(output)), "camera")
}

// recordAndStreamSegmentLibcamera records video using rpicam-vid (libcamera)
func (c *Camera) recordAndStreamSegmentLibcamera(filename string) error {
	// Check if we need post-processing (FFmpeg)
	// - EmbedTimestamp: requires drawtext filter
	// - Rotation 90/270: requires transpose filter (rpicam-vid only supports 0/180)
	needsPostProcessing := c.camConfig.EmbedTimestamp ||
		c.camConfig.Rotation == 90 ||
		c.camConfig.Rotation == 270

	if needsPostProcessing {
		return c.recordAndStreamSegmentLibcameraPipeline(filename)
	}

	// Build rpicam-vid command for MJPEG output
	args := []string{
		"-t", fmt.Sprintf("%d", c.segmentLength*1000), // timeout in milliseconds
		"--width", fmt.Sprintf("%d", c.camConfig.ResWidth),
		"--height", fmt.Sprintf("%d", c.camConfig.ResHeight),
		"--framerate", fmt.Sprintf("%d", c.camConfig.FPS),
		"--inline",         // include headers in stream
		"--codec", "mjpeg", // output MJPEG
		"-o", filename, // output file
	}

	if c.camConfig.Rotation == 180 {
		args = append(args, "--rotation", "180")
	}

	recordCmd := exec.Command("rpicam-vid", args...)

	c.cmdMu.Lock()
	c.recordCmd = recordCmd
	c.cmdMu.Unlock()

	// Capture stderr for debugging
	stderr, err := recordCmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := recordCmd.Start(); err != nil {
		c.cmdMu.Lock()
		c.recordCmd = nil
		c.cmdMu.Unlock()
		return err
	}

	var stderrBuf bytes.Buffer

	// Log stderr (rpicam-vid debugging)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				// Keep last 4KB of logs for error reporting
				if stderrBuf.Len()+n > 4096 {
					stderrBuf.Reset()
				}
				stderrBuf.Write(chunk)
				c.logger.Debugf("rpicam-vid: %s", string(chunk))
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

	if recordErr != nil {
		return fmt.Errorf("%w: %s", recordErr, stderrBuf.String())
	}

	return nil
}

// recordAndStreamSegmentLibcameraPipeline records video using rpicam-vid piped to ffmpeg
// This allows for features not supported by rpicam-vid directly (timestamps, 90/270 rotation)
func (c *Camera) recordAndStreamSegmentLibcameraPipeline(filename string) error {
	// 1. Start rpicam-vid outputting YUV420 to stdout
	rpiArgs := []string{
		"-t", fmt.Sprintf("%d", c.segmentLength*1000), // timeout in milliseconds
		"--width", fmt.Sprintf("%d", c.camConfig.ResWidth),
		"--height", fmt.Sprintf("%d", c.camConfig.ResHeight),
		"--framerate", fmt.Sprintf("%d", c.camConfig.FPS),
		"--inline",         // include headers in stream
		"--codec", "yuv420", // output raw YUV420
		"-o", "-",          // output to stdout
		"--nopreview",
	}

	// Handle 180 rotation in hardware (rpicam-vid)
	if c.camConfig.Rotation == 180 {
		rpiArgs = append(rpiArgs, "--rotation", "180")
	}
	// 90/270 handled by ffmpeg

	rpiCmd := exec.Command("rpicam-vid", rpiArgs...)

	// 2. Start ffmpeg reading from stdin
	ffmpegArgs := []string{
		"-y",
		"-f", "rawvideo",
		"-pix_fmt", "yuv420p",
		"-s", fmt.Sprintf("%dx%d", c.camConfig.ResWidth, c.camConfig.ResHeight),
		"-r", fmt.Sprintf("%d", c.camConfig.FPS),
		"-i", "-", // Read from stdin
	}

	// Filters
	var filters []string

	// Rotation (90/270)
	if c.camConfig.Rotation == 90 {
		filters = append(filters, "transpose=1")
	} else if c.camConfig.Rotation == 270 {
		filters = append(filters, "transpose=2")
	}

	// Timestamp
	if c.camConfig.EmbedTimestamp {
		filters = append(filters, getTimestampFilter())
	}

	if len(filters) > 0 {
		ffmpegArgs = append(ffmpegArgs, "-vf", strings.Join(filters, ","))
	}

	// Encoding
	ffmpegArgs = append(ffmpegArgs,
		"-c:v", "mjpeg",
		"-q:v", fmt.Sprintf("%d", c.camConfig.MJPEGQuality),
		"-f", "mjpeg",
		filename,
	)

	ffmpegCmd := exec.Command("ffmpeg", ffmpegArgs...)

	// Pipe rpicam-vid stdout to ffmpeg stdin
	rpiStdout, err := rpiCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create rpicam-vid stdout pipe: %w", err)
	}
	ffmpegCmd.Stdin = rpiStdout

	// Capture stderr for debugging (ffmpeg only, or both?)
	// We'll capture ffmpeg stderr as it's the main process we care about for encoding errors
	ffmpegStderr, err := ffmpegCmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create ffmpeg stderr pipe: %w", err)
	}

	// Start rpicam-vid
	if err := rpiCmd.Start(); err != nil {
		return fmt.Errorf("failed to start rpicam-vid: %w", err)
	}

	c.cmdMu.Lock()
	c.recordCmd = ffmpegCmd // We track ffmpeg as the main process
	c.cmdMu.Unlock()

	// Start ffmpeg
	if err := ffmpegCmd.Start(); err != nil {
		rpiCmd.Process.Kill()
		c.cmdMu.Lock()
		c.recordCmd = nil
		c.cmdMu.Unlock()
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	var stderrBuf bytes.Buffer

	// Log stderr
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ffmpegStderr.Read(buf)
			if n > 0 {
				chunk := buf[:n]
				if stderrBuf.Len()+n > 4096 {
					stderrBuf.Reset()
				}
				stderrBuf.Write(chunk)
				// c.logger.Debugf("ffmpeg: %s", string(chunk)) // Optional: log all ffmpeg output
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for ffmpeg to complete
	err = ffmpegCmd.Wait()
	
	// rpicam-vid should exit when pipe closes or timeout reached
	rpiCmd.Wait()

	c.cmdMu.Lock()
	c.recordCmd = nil
	c.cmdMu.Unlock()

	if err != nil {
		return fmt.Errorf("%w: %s", err, stderrBuf.String())
	}

	return nil
}
