package camera

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Frame extraction buffers
	FrameBufferSizeKB = 256 // Read last 256KB from MJPEG file (typical frame: 80-150KB)
	MaxFrameSizeKB    = 200 // Max size to search backwards for frame start (prevents old frames)
	MinFileSize       = 100 // Skip extraction if file too small (not enough data yet)
	BytesPerKB        = 1024
)

// ExtractFrameFromLatestSegment extracts a JPEG frame from the most recent MJPEG segment
// MJPEG is just concatenated JPEGs, so we read the last JPEG directly from the file
// This is near-instantaneous (no FFmpeg overhead) and works even while recording
func ExtractFrameFromLatestSegment(videoDir string, logger Logger) []byte {
	// Find the latest MJPEG file
	entries, err := os.ReadDir(videoDir)
	if err != nil {
		logger.Printf("[WARN] Failed to read video directory '%s': %v", videoDir, err)
		return nil
	}

	var latestFile string
	var latestTime time.Time

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".mjpeg") {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			logger.Debugf("Failed to get info for file '%s': %v", name, err)
			continue
		}

		if info.ModTime().After(latestTime) {
			latestTime = info.ModTime()
			latestFile = filepath.Join(videoDir, name)
		}
	}

	if latestFile == "" {
		logger.Debugf("No video segments found in '%s' - recording may be initializing", videoDir)
		return nil
	}

	// Extract the last JPEG frame directly from the MJPEG file
	// MJPEG = concatenated JPEGs with markers: FFD8 (start) ... FFD9 (end)
	frameData := extractLastJPEGFromMJPEG(latestFile)
	if len(frameData) == 0 {
		logger.Debugf("Could not extract JPEG frame from '%s'", filepath.Base(latestFile))
		return nil
	}

	return frameData
}

// extractLastJPEGFromMJPEG reads the last complete JPEG frame from an MJPEG file
// by scanning backwards for JPEG markers. This is near-instantaneous (no FFmpeg).
func extractLastJPEGFromMJPEG(filepath string) []byte {
	file, err := os.Open(filepath)
	if err != nil {
		return nil
	}
	defer file.Close()

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		return nil
	}
	fileSize := fileInfo.Size()

	if fileSize < 4 {
		return nil // Too small to be a valid JPEG
	}

	// Read the last 1MB of the file (should contain at least one complete JPEG frame)
	readSize := int64(1024 * 1024) // 1MB
	if readSize > fileSize {
		readSize = fileSize
	}

	startPos := fileSize - readSize
	_, err = file.Seek(startPos, 0)
	if err != nil {
		return nil
	}

	buf := make([]byte, readSize)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return nil
	}
	buf = buf[:n]

	// Scan backwards for JPEG end marker (FFD9) followed by next start marker (FFD8) or EOF
	// Work backwards from the end to find the last complete JPEG frame
	var jpegEnd int64 = -1
	var jpegStart int64 = -1

	// Find the last FFD9 (JPEG end marker)
	for i := len(buf) - 1; i > 0; i-- {
		if buf[i] == 0xD9 && buf[i-1] == 0xFF {
			jpegEnd = int64(i) + 1
			break
		}
	}

	if jpegEnd == -1 {
		return nil // No JPEG end marker found
	}

	// Find the FFD8 (JPEG start marker) before the end
	// Limit search to MaxFrameSizeKB to avoid finding very old frames
	searchLimit := int(jpegEnd) - (MaxFrameSizeKB * BytesPerKB)
	if searchLimit < 0 {
		searchLimit = 0
	}

	for i := int(jpegEnd) - 2; i >= searchLimit; i-- {
		if buf[i] == 0xD8 && buf[i-1] == 0xFF {
			jpegStart = int64(i) - 1
			break
		}
	}

	if jpegStart == -1 {
		return nil // No JPEG start marker found
	}

	// Return the JPEG frame
	return buf[jpegStart:jpegEnd]
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
