package camera

import (
	"fmt"
	"path/filepath"
	"sync"
)

// CameraManager manages multiple camera instances
type CameraManager struct {
	cameras        map[string]*Camera        // ID -> Camera
	streamManagers map[string]*StreamManager // ID -> StreamManager
	logger         Logger
	videoDir       string
	segmentLength  int
	mu             sync.RWMutex
	cameraWg       sync.WaitGroup // Wait group for camera goroutines
	stopCh         chan struct{}
	stopOnce       sync.Once
}

// NewCameraManager creates a new camera manager
func NewCameraManager(configs []CameraConfig, segmentLength int, videoDir string, logger Logger) (*CameraManager, error) {
	cm := &CameraManager{
		cameras:        make(map[string]*Camera),
		streamManagers: make(map[string]*StreamManager),
		logger:         logger,
		videoDir:       videoDir,
		segmentLength:  segmentLength,
		stopCh:         make(chan struct{}),
	}

	if err := cm.initializeCameras(configs, segmentLength); err != nil {
		return nil, err
	}

	return cm, nil
}

// initializeCameras creates camera instances from configs
func (cm *CameraManager) initializeCameras(configs []CameraConfig, segmentLength int) error {
	for _, config := range configs {
		if !config.Enabled {
			cm.logger.Printf("Camera '%s' (%s) is disabled, skipping", config.Name, config.ID)
			continue
		}

		camera, err := NewCamera(config, segmentLength, cm.logger)
		if err != nil {
			return fmt.Errorf("failed to create camera '%s': %w", config.Name, err)
		}

		streamMgr := NewStreamManager(cm.logger)
		camera.SetStreamManager(streamMgr)

		cm.cameras[config.ID] = camera
		cm.streamManagers[config.ID] = streamMgr

		cm.logger.Printf("Initialized camera: %s (%s) - Device: %s", config.Name, config.ID, config.Device)
	}

	if len(cm.cameras) == 0 {
		return fmt.Errorf("no enabled cameras configured")
	}

	return nil
}

// Start begins recording on all cameras
func (cm *CameraManager) Start() error {
	cm.startAllCameras()

	<-cm.stopCh
	cm.cameraWg.Wait()
	return nil
}

// Stop stops all cameras
func (cm *CameraManager) Stop() {
	cm.stopOnce.Do(func() { close(cm.stopCh) })

	cm.mu.RLock()
	defer cm.mu.RUnlock()

	for id, camera := range cm.cameras {
		cm.logger.Printf("Stopping camera: %s", id)
		camera.Stop()
	}

	for _, streamMgr := range cm.streamManagers {
		streamMgr.Stop()
	}
}

// RestartWithConfigs stops all cameras and starts them again with the provided configs
func (cm *CameraManager) RestartWithConfigs(configs []CameraConfig, segmentLength int, videoDir string) error {
	// Stop all existing cameras
	cm.mu.RLock()
	oldCameras := make([]*Camera, 0, len(cm.cameras))
	for _, camera := range cm.cameras {
		oldCameras = append(oldCameras, camera)
	}
	oldStreamManagers := make([]*StreamManager, 0, len(cm.streamManagers))
	for _, sm := range cm.streamManagers {
		oldStreamManagers = append(oldStreamManagers, sm)
	}
	cm.mu.RUnlock()

	// Stop cameras (but don't lock mu during this)
	for _, camera := range oldCameras {
		camera.Stop()
	}
	for _, sm := range oldStreamManagers {
		sm.Stop()
	}

	// Clear old cameras and create new ones
	cm.mu.Lock()
	cm.cameras = make(map[string]*Camera)
	cm.streamManagers = make(map[string]*StreamManager)
	cm.videoDir = videoDir
	cm.segmentLength = segmentLength
	cm.mu.Unlock()

	// Initialize new cameras
	if err := cm.initializeCameras(configs, segmentLength); err != nil {
		return err
	}

	cm.startAllCameras()

	cm.logger.Printf("Camera restart complete")
	return nil
}

// startAllCameras launches all configured cameras in their own goroutines.
func (cm *CameraManager) startAllCameras() {
	cm.mu.RLock()
	cameras := make([]*Camera, 0, len(cm.cameras))
	for _, camera := range cm.cameras {
		cameras = append(cameras, camera)
	}
	cm.mu.RUnlock()

	for _, camera := range cameras {
		cm.startCamera(camera)
	}
}

func (cm *CameraManager) startCamera(cam *Camera) {
	cm.cameraWg.Add(1)
	go func(cam *Camera) {
		defer cm.cameraWg.Done()
		config := cam.GetConfig()
		cameraVideoDir := filepath.Join(cm.videoDir, config.ID)
		if err := cam.Start(cameraVideoDir); err != nil {
			cm.logger.Printf("Camera '%s' stopped: %v", config.Name, err)
		}
	}(cam)
}

// GetCamera returns a camera by ID
func (cm *CameraManager) GetCamera(id string) (*Camera, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	camera, ok := cm.cameras[id]
	return camera, ok
}

// GetStreamManager returns a stream manager by camera ID
func (cm *CameraManager) GetStreamManager(id string) (*StreamManager, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	sm, ok := cm.streamManagers[id]
	return sm, ok
}

// ListCameras returns all camera configurations
func (cm *CameraManager) ListCameras() []CameraConfig {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	configs := make([]CameraConfig, 0, len(cm.cameras))
	for _, camera := range cm.cameras {
		configs = append(configs, camera.GetConfig())
	}
	return configs
}

// GetDefaultCameraID returns the ID of the first camera (for backward compatibility)
func (cm *CameraManager) GetDefaultCameraID() string {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return first camera ID (deterministic via map iteration in Go 1.12+)
	for id := range cm.cameras {
		return id
	}
	return ""
}
