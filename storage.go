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
	lastUsed     int64 // Cache last calculated storage usage
	lastChecked  time.Time
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
	// Get all video files from camera subdirectories
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
	var totalSize int64

	// Scan camera subdirectories for video files
	for _, entry := range entries {
		if !entry.IsDir() {
			// Skip non-directories (shouldn't have loose files here)
			continue
		}

		// Skip special directories
		if entry.Name()[0] == '.' {
			continue
		}

		cameraDir := filepath.Join(sm.videoDir, entry.Name())
		cameraEntries, err := os.ReadDir(cameraDir)
		if err != nil {
			continue
		}

		for _, videoEntry := range cameraEntries {
			if videoEntry.IsDir() {
				continue
			}
			if !isVideoFile(videoEntry.Name()) {
				continue
			}

			info, err := videoEntry.Info()
			if err != nil {
				continue
			}

			fileSize := info.Size()
			files = append(files, fileInfo{
				path:    filepath.Join(cameraDir, videoEntry.Name()),
				modTime: info.ModTime(),
				size:    fileSize,
			})
			totalSize += fileSize
		}
	}

	// Update cached usage
	sm.lastUsed = totalSize
	sm.lastChecked = time.Now()

	capBytes := int64(sm.storageCapGB) * BytesPerGB

	// If over cap, delete oldest files
	if totalSize > capBytes {
		// Sort by modification time (oldest first)
		sort.Slice(files, func(i, j int) bool {
			return files[i].modTime.Before(files[j].modTime)
		})

		deletedCount := 0
		for _, f := range files {
			if totalSize <= capBytes {
				break
			}

			if err := os.Remove(f.path); err == nil {
				deletedCount++
				totalSize -= f.size
				sm.lastUsed = totalSize // Update cache after deletion
				fmt.Printf("Deleted old video: %s (modified: %s, size: %.2f MB)\n",
					filepath.Base(f.path),
					f.modTime.Format("2006-01-02 15:04:05"),
					float64(f.size)/BytesPerMB)
			}
		}

		if deletedCount > 0 {
			fmt.Printf("Storage cleanup complete: deleted %d video(s), now using %.2f GB / %d GB\n",
				deletedCount,
				float64(totalSize)/BytesPerGB,
				sm.storageCapGB)
		}
	}

	return nil
}

func (sm *StorageManager) GetStorageStats() (used int64, cap int64, err error) {
	// Use cached value if recent (within 5 seconds)
	if time.Since(sm.lastChecked) < 5*time.Second && sm.lastUsed > 0 {
		cap = int64(sm.storageCapGB) * BytesPerGB
		return sm.lastUsed, cap, nil
	}

	// Otherwise, recalculate from camera subdirectories
	entries, err := os.ReadDir(sm.videoDir)
	if err != nil {
		return 0, 0, err
	}

	used = 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Skip special directories like .export and .temp_export_*
		if entry.Name()[0] == '.' {
			continue
		}

		cameraDir := filepath.Join(sm.videoDir, entry.Name())
		cameraEntries, err := os.ReadDir(cameraDir)
		if err != nil {
			continue
		}

		for _, videoEntry := range cameraEntries {
			if videoEntry.IsDir() {
				continue
			}
			if !isVideoFile(videoEntry.Name()) {
				continue
			}

			info, err := videoEntry.Info()
			if err != nil {
				continue
			}
			used += info.Size()
		}
	}

	// Update cache
	sm.lastUsed = used
	sm.lastChecked = time.Now()

	cap = int64(sm.storageCapGB) * BytesPerGB
	return used, cap, nil
}

func (sm *StorageManager) Stop() {
	sm.ticker.Stop()
	close(sm.done)
}

// CleanupTempExportDirs removes any leftover temporary export directories
// These can be left behind if the process crashes during export generation
func (sm *StorageManager) CleanupTempExportDirs() int {
	entries, err := os.ReadDir(sm.videoDir)
	if err != nil {
		fmt.Printf("Failed to read video directory for cleanup: %v\n", err)
		return 0
	}

	var cleaned int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Check if it's a temporary export directory
		if len(name) > 13 && name[:13] == ".temp_export_" {
			dirPath := filepath.Join(sm.videoDir, name)
			if err := os.RemoveAll(dirPath); err != nil {
				fmt.Printf("Failed to remove temp export dir %s: %v\n", name, err)
			} else {
				fmt.Printf("Cleaned up leftover temp export directory: %s\n", name)
				cleaned++
			}
		}
	}

	return cleaned
}

func isVideoFile(name string) bool {
	return IsMJPEGFile(name)
}
