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
	// Clean up stale temp dirs from any previously crashed export
	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d stale temp export director%s", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	exportPath := filepath.Join(s.config.VideoDir, ".export", ExportFilename)
	infoPath := filepath.Join(s.config.VideoDir, ".export", "export_info.json")

	info, err := os.Stat(exportPath)
	if err != nil {
		return
	}

	infoData, err := os.ReadFile(infoPath)
	if err != nil {
		return
	}

	var exportInfo ExportInfo
	if err := json.Unmarshal(infoData, &exportInfo); err != nil {
		return
	}

	if exportInfo.InProgress {
		// Crashed mid-export -  clean up
		s.logger.Printf("Found interrupted export, removing...")
		os.Remove(exportPath)
		os.Remove(infoPath)
		return
	}

	exportInfo.Size = info.Size()
	exportInfo.Available = true
	s.exportMutex.Lock()
	s.exportInfo = &exportInfo
	s.exportMutex.Unlock()
	s.logger.Printf("Found existing export: %.2f MB (%s to %s)",
		float64(info.Size())/BytesPerMB,
		exportInfo.StartTime.Format(time.RFC3339),
		exportInfo.EndTime.Format(time.RFC3339))
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

	go s.generateExportAsync(startTime, endTime)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "started",
		"message": "Export generation started",
	})
}

func (s *APIServer) generateExportAsync(startTime, endTime time.Time) {
	s.logger.Printf("Starting export from %s to %s", startTime.Format(time.RFC3339), endTime.Format(time.RFC3339))

	if cleaned := s.storage.CleanupTempExportDirs(); cleaned > 0 {
		s.logger.Printf("Cleaned up %d stale temp export director%s before starting", cleaned, map[bool]string{true: "y", false: "ies"}[cleaned == 1])
	}

	setProgress := func(msg string) {
		s.exportMutex.Lock()
		s.exportInfo.Progress = msg
		s.exportMutex.Unlock()
	}

	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{
		InProgress: true,
		Progress:   "Scanning for video files...",
		StartTime:  startTime,
		EndTime:    endTime,
	}
	s.exportMutex.Unlock()

	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("Export panicked: %v", r)
			s.exportMutex.Lock()
			s.exportInfo = &ExportInfo{Progress: "Error: export failed unexpectedly"}
			s.exportMutex.Unlock()
		}
	}()

	// Collect MJPEG files in the date range
	mjpegFiles, err := walkCameraVideos(s.config.VideoDir, func(_, _ string, info os.FileInfo) bool {
		t := info.ModTime()
		return (t.After(startTime) || t.Equal(startTime)) && !t.After(endTime)
	})
	if err != nil {
		s.logger.Printf("Failed to scan video directory: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{Progress: "Error: failed to scan video directory"}
		s.exportMutex.Unlock()
		return
	}

	if len(mjpegFiles) == 0 {
		s.logger.Printf("No videos found in date range")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{Progress: "No videos found in the specified date range"}
		s.exportMutex.Unlock()
		return
	}

	// Sort by modification time; precompute to avoid repeated os.Stat calls
	type fileEntry struct {
		path    string
		modTime time.Time
	}
	entries := make([]fileEntry, 0, len(mjpegFiles))
	for _, p := range mjpegFiles {
		if info, err := os.Stat(p); err == nil {
			entries = append(entries, fileEntry{p, info.ModTime()})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})

	s.exportMutex.Lock()
	s.exportInfo.TotalSegments = len(entries)
	s.exportMutex.Unlock()

	// Temp dir holds only the concat list (a tiny text file).
	// No file copying -  ffmpeg reads the original paths directly.
	tempDir := filepath.Join(s.config.VideoDir, fmt.Sprintf(".temp_export_%d", time.Now().Unix()))
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		s.logger.Printf("Failed to create temp directory: %v", err)
		return
	}
	defer os.RemoveAll(tempDir)

	var concatContent strings.Builder
	for _, e := range entries {
		fmt.Fprintf(&concatContent, "file '%s'\n", e.path)
	}
	concatFile := filepath.Join(tempDir, "concat_list.txt")
	if err := os.WriteFile(concatFile, []byte(concatContent.String()), 0644); err != nil {
		s.logger.Printf("Failed to write concat file: %v", err)
		return
	}

	exportDir := filepath.Join(s.config.VideoDir, ".export")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		s.logger.Printf("Failed to create export directory: %v", err)
		return
	}
	outputFile := filepath.Join(exportDir, ExportFilename)
	os.Remove(outputFile)
	os.Remove(filepath.Join(exportDir, "export_info.json"))

	setProgress(fmt.Sprintf("Remuxing %d segments...", len(entries)))
	s.logger.Printf("Remuxing %d MJPEG segments to MP4 (copy codec)...", len(entries))

	// Run ffmpeg at low CPU priority so SSH and other services remain responsive.
	// -c:v copy remuxes MJPEG frames directly into the MP4 container -  no decoding or
	// re-encoding, so the Pi's single core isn't saturated.
	cmd := exec.Command(
		"nice", "-n", "19",
		"ffmpeg",
		"-y",
		"-loglevel", "error",
		"-fflags", "+discardcorrupt",
		"-err_detect", "ignore_err",
		"-f", "concat",
		"-safe", "0",
		"-i", concatFile,
		"-c:v", "copy",
		"-movflags", "+faststart",
		"-f", "mp4",
		outputFile,
	)

	var stderrBuf strings.Builder
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		s.logger.Printf("Failed to start ffmpeg: %v", err)
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{Progress: "Error: failed to start FFmpeg"}
		s.exportMutex.Unlock()
		return
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	lastSize := int64(0)

	for {
		select {
		case err := <-done:
			if err != nil {
				s.logger.Printf("FFmpeg error: %s", stderrBuf.String())
				s.exportMutex.Lock()
				s.exportInfo = &ExportInfo{Progress: "Error: FFmpeg failed -  " + stderrBuf.String()}
				s.exportMutex.Unlock()
				return
			}
			goto encodingDone
		case <-ticker.C:
			if info, err := os.Stat(outputFile); err == nil {
				sizeMB := float64(info.Size()) / BytesPerMB
				speedMBps := float64(info.Size()-lastSize) / BytesPerMB / 3.0
				lastSize = info.Size()
				setProgress(fmt.Sprintf("Writing... %.1f MB (%.1f MB/s)", sizeMB, speedMBps))
				s.exportMutex.Lock()
				s.exportInfo.CurrentSizeMB = sizeMB
				s.exportMutex.Unlock()
			}
		}
	}
encodingDone:

	info, err := os.Stat(outputFile)
	if err != nil || info.Size() == 0 {
		s.logger.Printf("Export output file missing or empty")
		s.exportMutex.Lock()
		s.exportInfo = &ExportInfo{Progress: "Error: output file missing or empty"}
		s.exportMutex.Unlock()
		return
	}

	s.logger.Printf("Export complete: %.2f MB from %d segments", float64(info.Size())/BytesPerMB, len(entries))

	exportInfo := ExportInfo{
		Filename:      ExportFilename,
		StartTime:     startTime,
		EndTime:       endTime,
		Size:          info.Size(),
		Available:     true,
		Progress:      "Complete",
		CurrentSizeMB: float64(info.Size()) / BytesPerMB,
		TotalSegments: len(entries),
	}

	if data, err := json.Marshal(exportInfo); err == nil {
		os.WriteFile(filepath.Join(exportDir, "export_info.json"), data, 0644)
	}

	s.exportMutex.Lock()
	s.exportInfo = &exportInfo
	s.exportMutex.Unlock()
}

func (s *APIServer) handleExportStatus(w http.ResponseWriter, r *http.Request) {
	s.exportMutex.RLock()
	defer s.exportMutex.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.exportInfo)
}

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

func (s *APIServer) handleDeleteExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	os.Remove(filepath.Join(s.config.VideoDir, ".export", ExportFilename))
	os.Remove(filepath.Join(s.config.VideoDir, ".export", "export_info.json"))

	s.exportMutex.Lock()
	s.exportInfo = &ExportInfo{Available: false}
	s.exportMutex.Unlock()

	s.logger.Printf("Export deleted")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "deleted"})
}
