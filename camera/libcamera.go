package camera

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// IsCSICamera detects if the given device is a CSI (libcamera) camera or a USB (V4L2) camera.
// A UVC USB webcam (V4L2 driver "uvcvideo") is always handled by the V4L2/ffmpeg path:
// libcamera's uvcvideo pipeline handler lists these cameras but cannot produce a usable
// stream, so rpicam-vid fails with "no cameras available". Only genuine CSI sensors
// (e.g. bcm2835-unicam) are routed to libcamera, and only when it actually enumerates
// a usable camera. rpicam-still prints "No cameras available!" when none are usable —
// that string contains "camera", so we match the real listing header instead.
func IsCSICamera(logger Logger, device string) bool {
	if !isLibcameraAvailable(logger) {
		return false
	}

	if driver := v4l2Driver(device); driver == "uvcvideo" {
		logger.Debugf("Camera device %s uses V4L2 driver %s -> USB webcam, using V4L2 path", device, driver)
		return false
	}

	cmd := exec.Command("rpicam-still", "--list-cameras")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Debugf("rpicam-still enumeration failed: %v", err)
		return false
	}
	return strings.Contains(string(output), "Available cameras")
}

// v4l2Driver returns the kernel V4L2 driver name for a /dev/videoN device
// (e.g. "uvcvideo", "bcm2835-unicam"), or "" if it cannot be determined.
func v4l2Driver(device string) string {
	if device == "" {
		device = "/dev/video0"
	}
	link, err := os.Readlink(filepath.Join("/sys/class/video4linux", filepath.Base(device), "device/driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(link)
}

// recordAndStreamSegmentLibcamera records video using rpicam-vid (libcamera)
func (c *Camera) recordAndStreamSegmentLibcamera(filename string) error {
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

	if c.camConfig.EmbedTimestamp {
		c.logger.Printf("[WARN] Camera '%s': Timestamp embedding is not supported for CSI cameras (rpicam-vid). Ignoring.", c.camConfig.Name)
	}

	if c.camConfig.Rotation != 0 {
		// rpicam-vid MJPEG encoder does not support 90/270 degree rotation (transpose)
		// See: https://github.com/raspberrypi/rpicam-apps/issues/505
		if c.camConfig.Rotation == 90 || c.camConfig.Rotation == 270 {
			c.logger.Printf("[WARN] Camera '%s': Rotation %d is not supported by rpicam-vid MJPEG encoder. Ignoring rotation to prevent crash.", c.camConfig.Name, c.camConfig.Rotation)
		} else {
			args = append(args, "--rotation", fmt.Sprintf("%d", c.camConfig.Rotation))
		}
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
