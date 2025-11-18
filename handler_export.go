package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *APIServer) checkExistingExport() {
	// First, clean up any leftover temporary export directories
	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d temporary export director%s", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", ExportFilename)
	infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")

	if info, err := os.Stat(exportPath); err == nil {
		if infoData, err := os.ReadFile(infoPath); err == nil {
			var exportInfo ExportInfo
			if err := json.Unmarshal(infoData, &exportInfo); err == nil {
				// Only mark as available if it was completed (not in progress)
				if !exportInfo.InProgress {
					exportInfo.Size = info.Size()
					exportInfo.Available = true
					s.exportMutex.Lock()
					s.exportInfo = &exportInfo
					s.exportMutex.Unlock()
					s.logger.Printf("Found existing export: %.2f MB from %s to %s",
						float64(info.Size())/BytesPerMB,
						exportInfo.StartTime.Format(time.RFC3339),
						exportInfo.EndTime.Format(time.RFC3339))
				} else {
					// Export was interrupted, clean it up
					s.logger.Printf("Found interrupted export, cleaning up...")
					os.Remove(exportPath)
					os.Remove(infoPath)
					s.exportMutex.Lock()
					s.exportInfo = &ExportInfo{
						Available:  false,
						InProgress: false,
						Progress:   "Previous export was interrupted",
					}
					s.exportMutex.Unlock()
				}
			}
		}
	}
}

func (s *APIServer) handleGenerateExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	startStr := r.URL.Query().Get("start")
	endStr := r.URL.Query().Get("end")

	if startStr == "" || endStr == "" {
		http.Error(w, "Missing start or end parameter", http.StatusBadRequest)
		return
	}

	// Parse timestamps
	startTime, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		http.Error(w, "Invalid start time format", http.StatusBadRequest)
		return
	}

	endTime, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		http.Error(w, "Invalid end time format", http.StatusBadRequest)
		return
	}

	// Start generation in background
	go s.generateExportAsync(startTime, endTime)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Export generation started",
	})
}

// generateExportAsync generates an export in the background
func (s *APIServer) generateExportAsync(startTime, endTime time.Time) {
	s.logger.Printf("Starting async export generation from %s to %s", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	// Clean up any leftover temporary export directories from previous runs
	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d temporary export director%s before starting export", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	// Set initial progress state
	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{
		Available:  false,
		InProgress: true,
		Progress:   "Scanning for video files...",
		StartTime:  startTime,
		EndTime:    endTime,
	}
	s.exportMutex.Unlock()

	// Ensure we clean up on panic or unexpected exit
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("Export generation panicked: %v", r)
			s.exportMutex.Lock()
			s.exportInfo = &ExportInfo{
				Available:  false,
				InProgress: false,
				Progress:   "Error: Export generation failed unexpectedly",
			}
			s.exportMutex.Unlock()
			// Clean up any partial export
			exportPath := filepath.Join(s.config.VideoDir, ".export", ExportFilename)
			infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")
			os.Remove(exportPath)
			os.Remove(infoPath)
		}
	}()

	// Get all MJPEG files in date range from camera subdirectories
	mjpegFiles, err := walkCameraVideos(s.config.VideoDir, func(cameraDir, fileName string, info os.FileInfo) bool {
		modTime := info.ModTime()
		// Include files within the time range (inclusive of boundaries)
		// Use After/Before for start, and not After for end to include files up to and including endTime
		return (modTime.After(startTime) || modTime.Equal(startTime)) && !modTime.After(endTime)
	})
	if err != nil {
		s.logger.Printf("Failed to read video directory: %v", err)
		return
	}

	if len(mjpegFiles) == 0 {
		s.logger.Printf("No videos found in the specified date range")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "No videos found in the specified date range",
		}
		s.exportMutex.Unlock()
		return
	}

	// Update progress with total segments found
	s.exportMutex.Lock()
	s.exportInfo.Progress = fmt.Sprintf("Found %d video segments, preparing to copy...", len(mjpegFiles))
	s.exportInfo.TotalSegments = len(mjpegFiles)
	s.exportMutex.Unlock()

	// Sort by modification time
	sort.Slice(mjpegFiles, func(i, j int) bool {
		iInfo, _ := os.Stat(mjpegFiles[i])
		jInfo, _ := os.Stat(mjpegFiles[j])
		return iInfo.ModTime().Before(jInfo.ModTime())
	})

	// Create temporary directory for working files
	tempDir := filepath.Join(s.config.VideoDir, fmt.Sprintf(".temp_export_%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.logger.Printf("Failed to create temp directory: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Failed to create temporary directory",
		}
		s.exportMutex.Unlock()
		return
	}
	defer os.RemoveAll(tempDir)

	// Copy MJPEG files to temp directory
	s.logger.Printf("Copying %d MJPEG files to temporary directory...", len(mjpegFiles))
	var tempFiles []string
	for i, srcPath := range mjpegFiles {
		// Update progress every 10 files
		if i%10 == 0 {
			s.exportMutex.Lock()
			s.exportInfo.Progress = fmt.Sprintf("Copying files... %d/%d", i, len(mjpegFiles))
			s.exportInfo.ProcessedFiles = i
			s.exportMutex.Unlock()
		}
		tempPath := filepath.Join(tempDir, fmt.Sprintf("segment_%03d.mjpeg", i))

		src, err := os.Open(srcPath)
		if err != nil {
			s.logger.Printf("Warning: Could not open %s: %v", filepath.Base(srcPath), err)
			continue
		}

		dst, err := os.Create(tempPath)
		if err != nil {
			src.Close()
			s.logger.Printf("Failed to create temp file: %v", err)
			return
		}

		_, copyErr := io.Copy(dst, src)
		src.Close()
		dst.Close()

		if copyErr != nil {
			s.logger.Printf("Failed to copy file: %v", copyErr)
			return
		}

		tempFiles = append(tempFiles, tempPath)
	}

	if len(tempFiles) == 0 {
		s.logger.Printf("No videos could be copied (may have been deleted)")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: No videos could be copied (files may have been deleted)",
		}
		s.exportMutex.Unlock()
		return
	}

	s.logger.Printf("Successfully copied %d/%d files", len(tempFiles), len(mjpegFiles))
	s.exportMutex.Lock()
	s.exportInfo.Progress = fmt.Sprintf("Copied %d files, preparing to encode...", len(tempFiles))
	s.exportInfo.ProcessedFiles = len(tempFiles)
	s.exportMutex.Unlock()

	// Create concat file
	concatFile := filepath.Join(tempDir, "concat_list.txt")
	var concatContent strings.Builder
	for _, file := range tempFiles {
		concatContent.WriteString(fmt.Sprintf("file '%s'\n", file))
	}

	if err := os.WriteFile(concatFile, []byte(concatContent.String()), 0644); err != nil {
		s.logger.Printf("Failed to create concat file: %v", err)
		return
	}

	// Create export directory
	exportDir := filepath.Join(s.config.VideoDir, ".export")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		s.logger.Printf("Failed to create export directory: %v", err)
		return
	}

	// Delete old export if exists
	oldExportPath := filepath.Join(exportDir, ExportFilename)
	os.Remove(oldExportPath)
	os.Remove(filepath.Join(exportDir, "export_info.json"))
	s.logger.Printf("Removed old export if it existed")

	// Generate MP4
	outputFile := filepath.Join(exportDir, ExportFilename)

	// Use first camera's settings for export, or defaults if no cameras
	resWidth, resHeight, fps := DefaultVideoWidth, DefaultVideoHeight, DefaultVideoFPS
	if len(s.config.Cameras) > 0 {
		resWidth = s.config.Cameras[0].ResWidth
		resHeight = s.config.Cameras[0].ResHeight
		fps = s.config.Cameras[0].FPS
	}
	s.logger.Printf("Generating video from %d MJPEG segments at %dx%d@%dfps",
		len(tempFiles), resWidth, resHeight, fps)

	s.exportMutex.Lock()
	s.exportInfo.Progress = "Encoding video with FFmpeg..."
	s.exportMutex.Unlock()

	cmd := exec.Command(
		"ffmpeg",
		"-y",
		"-loglevel", "error",
		"-fflags", "+discardcorrupt",
		"-err_detect", "ignore_err",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c:v", "mpeg4",
		"-q:v", fmt.Sprintf("%d", ExportVideoQuality),
		"-r", fmt.Sprintf("%d", fps),
		"-fps_mode", "cfr",
		"-movflags", "+faststart",
		"-f", "mp4",
		outputFile,
	)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	s.logger.Printf("Starting FFmpeg encoding of %d segments...", len(tempFiles))

	if err := cmd.Start(); err != nil {
		s.logger.Printf("Failed to start encoding: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Failed to start FFmpeg encoding",
		}
		s.exportMutex.Unlock()
		return
	}

	// Monitor progress
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	progressTicker := time.NewTicker(5 * time.Second)
	defer progressTicker.Stop()

	lastSize := int64(0)
	for {
		select {
		case err := <-done:
			if err != nil {
				s.logger.Printf("FFmpeg error: %s", stderrBuf.String())
				s.exportMutex.Lock()
				s.exportInfo = &ExportInfo{
					Available:  false,
					InProgress: false,
					Progress:   "Error: FFmpeg encoding failed",
				}
				s.exportMutex.Unlock()
				return
			}
			s.logger.Printf("FFmpeg encoding complete!")
			goto encodingDone
		case <-progressTicker.C:
			if info, err := os.Stat(outputFile); err == nil {
				sizeMB := float64(info.Size()) / BytesPerMB
				speedMBps := float64(info.Size()-lastSize) / BytesPerMB / 5.0
				s.logger.Printf("Encoding progress: %.1f MB (%.1f MB/s)", sizeMB, speedMBps)
				lastSize = info.Size()

				// Update progress for frontend
				s.exportMutex.Lock()
				s.exportInfo.Progress = fmt.Sprintf("Encoding... %.1f MB (%.1f MB/s)", sizeMB, speedMBps)
				s.exportInfo.CurrentSizeMB = sizeMB
				s.exportMutex.Unlock()
			}
		}
	}
encodingDone:

	// Verify output file
	info, err := os.Stat(outputFile)
	if err != nil {
		s.logger.Printf("Output file not found: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Output file not found",
		}
		s.exportMutex.Unlock()
		return
	}

	if info.Size() == 0 {
		s.logger.Printf("Output file is empty")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{
			Available:  false,
			InProgress: false,
			Progress:   "Error: Output file is empty",
		}
		s.exportMutex.Unlock()
		return
	}

	s.logger.Printf("Generated export: %.2f MB at %dx%d@%dfps",
		float64(info.Size())/BytesPerMB,
		resWidth, resHeight, fps)

	// Save export info
	exportInfo := ExportInfo{
		Filename:      ExportFilename,
		StartTime:     startTime,
		EndTime:       endTime,
		Size:          info.Size(),
		Available:     true,
		InProgress:    false,
		Progress:      "Complete",
		CurrentSizeMB: float64(info.Size()) / BytesPerMB,
	}

	infoPath := filepath.Join(exportDir, "export_info.json")
	infoData, _ := json.Marshal(exportInfo)
	os.WriteFile(infoPath, infoData, 0644)

	s.exportMutex.Lock()
	s.exportInfo = &exportInfo
	s.exportMutex.Unlock()

	s.logger.Printf("Export ready for download")
}

// handleExportStatus returns the status of the current export
func (s *APIServer) handleExportStatus(w http.ResponseWriter, r *http.Request) {
	s.exportMutex.RLock()
	defer s.exportMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.exportInfo)
}

// handleDownloadExport serves the current export file
func (s *APIServer) handleDownloadExport(w http.ResponseWriter, r *http.Request) {
	s.exportMutex.RLock()
	available := s.exportInfo.Available
	s.exportMutex.RUnlock()

	if !available {
		http.Error(w, "No export available", http.StatusNotFound)
		return
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", ExportFilename)
	info, err := os.Stat(exportPath)
	if err != nil {
		http.Error(w, "Export file not found", http.StatusNotFound)
		return
	}

	file, err := os.Open(exportPath)
	if err != nil {
		http.Error(w, "Failed to open export file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=dashcam_export_%s.mp4", time.Now().Format("2006-01-02")))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	w.Header().Set("Cache-Control", "no-cache")

	io.Copy(w, file)
	s.logger.Printf("Export downloaded by client")
}

// handleDeleteExport deletes the current export
func (s *APIServer) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", ExportFilename)
	infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")

	os.Remove(exportPath)
	os.Remove(infoPath)

	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{Available: false}
	s.exportMutex.Unlock()

	s.logger.Printf("Export deleted")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "deleted",
	})
}
