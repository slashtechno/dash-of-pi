package camera

import (
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
	// Build rpicam-vid command for MJPEG output
	args := []string{
		"-t", fmt.Sprintf("%d", c.segmentLength*1000), // timeout in milliseconds
		"--width", fmt.Sprintf("%d", c.camConfig.ResWidth),
		"--height", fmt.Sprintf("%d", c.camConfig.ResHeight),
		"--framerate", fmt.Sprintf("%d", c.camConfig.FPS),
		"--inline",           // include headers in stream
		"--codec", "mjpeg",   // output MJPEG
		"-o", filename,       // output file
	}

	if c.camConfig.Rotation != 0 {
		args = append(args, "--rotation", fmt.Sprintf("%d", c.camConfig.Rotation))
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

	// Log stderr (rpicam-vid debugging)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				c.logger.Debugf("rpicam-vid: %s", string(buf[:n]))
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

	return recordErr
}
