package camera

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// CameraConfig represents the configuration for a single camera
type CameraConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Device         string `json:"device"`
	Rotation       int    `json:"rotation"`
	ResWidth       int    `json:"res_width"`
	ResHeight      int    `json:"res_height"`
	Bitrate        int    `json:"bitrate"`
	FPS            int    `json:"fps"`
	MJPEGQuality   int    `json:"mjpeg_quality"`
	EmbedTimestamp bool   `json:"embed_timestamp"`
	Enabled        bool   `json:"enabled"`
}

// Camera handles video capture and recording for a single camera
type Camera struct {
	camConfig     CameraConfig
	logger        Logger
	done          chan struct{}
	streamManager *StreamManager
	lastErrorTime time.Time
	recordCmd     *exec.Cmd
	cmdMu         sync.Mutex
	videoEncoder  string
	segmentLength int
}

// NewCamera creates a new camera instance
func NewCamera(config CameraConfig, segmentLength int, logger Logger) (*Camera, error) {
	camera := &Camera{
		camConfig:     config,
		logger:        logger,
		done:          make(chan struct{}),
		segmentLength: segmentLength,
	}

	// Detect available encoder on startup
	camera.videoEncoder = detectVideoEncoder(logger)
	logger.Printf("Camera '%s' (%s): Using video encoder: %s", config.Name, config.ID, camera.videoEncoder)

	return camera, nil
}

// SetStreamManager connects the camera to a stream manager
func (c *Camera) SetStreamManager(sm *StreamManager) {
	c.streamManager = sm
}

// GetConfig returns the camera configuration
func (c *Camera) GetConfig() CameraConfig {
	return c.camConfig
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
		filename := filepath.Join(videoDir, fmt.Sprintf("dashcam_%s_%s.mjpeg", c.camConfig.ID, timestamp))

		c.logger.Debugf("Camera '%s': Starting recording segment: %s", c.camConfig.Name, filepath.Base(filename))

		if err := c.recordAndStreamSegment(filename); err != nil {
			if time.Since(c.lastErrorTime) > 5*time.Second {
				c.logger.Printf("Camera '%s': Recording error: %v", c.camConfig.Name, err)
				c.lastErrorTime = time.Now()
			}
		}

		select {
		case <-c.done:
			return nil
		default:
			c.logger.Debugf("Camera '%s': Segment completed, starting next recording...", c.camConfig.Name)
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
			frameData := ExtractFrameFromLatestSegment(videoDir, c.logger)
			if len(frameData) > 0 && c.streamManager != nil {
				c.streamManager.UpdateFrame(frameData)
			}
		}
	}
}
