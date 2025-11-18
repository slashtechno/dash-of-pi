package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func (s *APIServer) handleListVideos(w http.ResponseWriter, r *http.Request) {
	videos, err := s.listVideoFiles()
	if err != nil {
		http.Error(w, "Failed to list videos", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"videos": videos,
	})
}

func (s *APIServer) handleDownloadVideo(w http.ResponseWriter, r *http.Request) {
	cameraID := r.URL.Query().Get("camera")
	filename := r.URL.Query().Get("file")

	if filename == "" {
		http.Error(w, "Missing file parameter", http.StatusBadRequest)
		return
	}

	if cameraID == "" {
		http.Error(w, "Missing camera parameter", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	if filepath.Dir(filename) != "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	videoPath := filepath.Join(s.config.VideoDir, cameraID, filename)

	// Verify file exists and is in video directory
	if _, err := os.Stat(videoPath); err != nil {
		http.Error(w, "File not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	file, err := os.Open(videoPath)
	if err != nil {
		http.Error(w, "Failed to open file", http.StatusInternalServerError)
		return
	}
	defer file.Close()

	io.Copy(w, file)
}

func (s *APIServer) handleLatestVideo(w http.ResponseWriter, r *http.Request) {
	// List all video files in directory
	entries, err := os.ReadDir(s.config.VideoDir)
	if err != nil {
		http.Error(w, "Failed to list videos", http.StatusInternalServerError)
		return
	}

	// Collect all video files sorted by modification time
	type fileInfo struct {
		name    string
		modTime time.Time
	}
	var files []fileInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !IsPlayableVideo(name) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		files = append(files, fileInfo{name: name, modTime: info.ModTime()})
	}

	if len(files) == 0 {
		http.Error(w, "No videos available", http.StatusNotFound)
		return
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Serve the second-newest file (previous segment) to avoid serving incomplete current recording
	fileToServe := files[0].name
	if len(files) > 1 {
		fileToServe = files[1].name
	}

	videoPath := filepath.Join(s.config.VideoDir, fileToServe)

	// Set content type based on file extension
	contentType := "video/mp4"
	if HasExtension(videoPath, ExtensionWebM) {
		contentType = "video/webm"
	} else if strings.HasSuffix(strings.ToLower(videoPath), ".mjpeg") {
		contentType = "video/x-motion-jpeg"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Cache-Control", "no-cache")

	http.ServeFile(w, r, videoPath)
}

func (s *APIServer) handleServeSegment(w http.ResponseWriter, r *http.Request) {
	// Extract filename from path /api/videos/filename
	filename := filepath.Base(r.URL.Path)
	if filename == "" || filename == "videos" {
		http.Error(w, "Missing filename", http.StatusBadRequest)
		return
	}

	// Prevent directory traversal
	if filepath.Dir(filename) != "." {
		http.Error(w, "Invalid filename", http.StatusBadRequest)
		return
	}

	videoPath := filepath.Join(s.config.VideoDir, filename)

	// Verify file exists
	if _, err := os.Stat(videoPath); err != nil {
		http.Error(w, "Video not found", http.StatusNotFound)
		return
	}

	// Determine content type based on file extension
	contentType := "video/mp4"
	if strings.HasSuffix(strings.ToLower(filename), ".mjpeg") {
		contentType = "video/x-motion-jpeg"
	} else if HasExtension(videoPath, ExtensionWebM) {
		contentType = "video/webm"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Accept-Ranges", "bytes")
	http.ServeFile(w, r, videoPath)
}

func (s *APIServer) listVideoFiles() ([]VideoInfo, error) {
	var videos []VideoInfo

	// List camera directories
	cameras := s.cameraManager.ListCameras()
	for _, cam := range cameras {
		cameraDir := filepath.Join(s.config.VideoDir, cam.ID)

		// Skip if camera directory doesn't exist
		if _, err := os.Stat(cameraDir); os.IsNotExist(err) {
			continue
		}

		entries, err := os.ReadDir(cameraDir)
		if err != nil {
			s.logger.Debugf("Failed to read camera directory %s: %v", cameraDir, err)
			continue
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

			// Rough estimate: bytes / (bitrate * multiplier) = seconds
			duration := int(info.Size() / int64(cam.Bitrate*BitrateToStorageMultiplier))

			videos = append(videos, VideoInfo{
				Name:     entry.Name(),
				Path:     fmt.Sprintf("/api/video/download?camera=%s&file=%s&token=%s", cam.ID, entry.Name(), s.config.AuthToken),
				Size:     info.Size(),
				ModTime:  info.ModTime(),
				Duration: duration,
				CameraID: cam.ID,
			})
		}
	}

	// Sort by modification time (newest first)
	sort.Slice(videos, func(i, j int) bool {
		return videos[i].ModTime.After(videos[j].ModTime)
	})

	return videos, nil
}
