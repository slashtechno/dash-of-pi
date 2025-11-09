package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

type StorageManager struct {
	videoDir     string
	storageCapGB int
	ticker       *time.Ticker
	done         chan struct{}
}

func NewStorageManager(videoDir string, storageCapGB int) (*StorageManager, error) {
	if err := os.MkdirAll(videoDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create video directory: %w", err)
	}

	sm := &StorageManager{
		videoDir:     videoDir,
		storageCapGB: storageCapGB,
		ticker:       time.NewTicker(30 * time.Second), // Check every 30 seconds
		done:         make(chan struct{}),
	}

	// Start cleanup goroutine
	go sm.cleanupLoop()

	return sm, nil
}

func (sm *StorageManager) cleanupLoop() {
	for {
		select {
		case <-sm.done:
			return
		case <-sm.ticker.C:
			if err := sm.enforceStorageCap(); err != nil {
				// Just log, don't crash
				fmt.Printf("Storage cleanup error: %v\n", err)
			}
		}
	}
}

func (sm *StorageManager) enforceStorageCap() error {
	// Get all video files
	entries, err := os.ReadDir(sm.videoDir)
	if err != nil {
		return fmt.Errorf("failed to read video directory: %w", err)
	}

	type fileInfo struct {
		path    string
		modTime time.Time
		size    int64
	}

	var files []fileInfo

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

		files = append(files, fileInfo{
			path:    filepath.Join(sm.videoDir, entry.Name()),
			modTime: info.ModTime(),
			size:    info.Size(),
		})
	}

	// Calculate total size
	var totalSize int64
	for _, f := range files {
		totalSize += f.size
	}

	capBytes := int64(sm.storageCapGB) * BytesPerGB

	// If over cap, delete oldest files
	if totalSize > capBytes {
		// Sort by modification time (oldest first)
		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.Before(files[j].modTime)
		})

		for _, f := range files {
			if totalSize <= capBytes {
				break
			}

			if err := os.Remove(f.path); err == nil {
				totalSize -= f.size
				fmt.Printf("Deleted old video: %s\n", filepath.Base(f.path))
			}
		}
	}

	return nil
}

func (sm *StorageManager) GetStorageStats() (used int64, cap int64, err error) {
	entries, err := os.ReadDir(sm.videoDir)
	if err != nil {
		return 0, 0, err
	}

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
		used += info.Size()
	}

	cap = int64(sm.storageCapGB) * BytesPerGB
	return used, cap, nil
}

func (sm *StorageManager) Stop() {
	sm.ticker.Stop()
	close(sm.done)
}

func isVideoFile(name string) bool {
	ext := filepath.Ext(name)
	return ext == ".mjpeg"
}
