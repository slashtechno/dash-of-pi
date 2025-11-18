package camera

import (
	"fmt"
	"net/http"
	"sync"
)

// StreamManager handles HTTP streaming of video to clients
type StreamManager struct {
	logger      Logger
	done        chan struct{}
	stopOnce    sync.Once
	mu          sync.RWMutex
	latestFrame []byte
}

func NewStreamManager(logger Logger) *StreamManager {
	return &StreamManager{
		logger: logger,
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
