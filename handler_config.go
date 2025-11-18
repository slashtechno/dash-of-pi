package main

import (
	"dash-of-pi/camera"
	"encoding/json"
	"net/http"
)

func convertCameraConfigs(configs []CameraConfig) []camera.CameraConfig {
	result := make([]camera.CameraConfig, len(configs))
	for i, c := range configs {
		result[i] = camera.CameraConfig{
			ID:       c.ID,
			Name:     c.Name,
			Device:   c.Device,
			Rotation: c.Rotation,
			ResWidth:       c.ResWidth,
			ResHeight:      c.ResHeight,
			Bitrate:        c.Bitrate,
			FPS:            c.FPS,
			MJPEGQuality:   c.MJPEGQuality,
			EmbedTimestamp: c.EmbedTimestamp,
			Enabled:        c.Enabled,
		}
	}
	return result
}

func (s *APIServer) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"port":             s.config.Port,
		"storage_cap_gb":   s.config.StorageCapGB,
		"segment_length_s": s.config.SegmentLengthS,
		"cameras":          s.config.Cameras,
	})
}

func (s *APIServer) handleUpdateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newConfig struct {
		Port           int            `json:"port"`
		StorageCapGB   int            `json:"storage_cap_gb"`
		SegmentLengthS int            `json:"segment_length_s"`
		Cameras        []CameraConfig `json:"cameras"`
	}

	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Update config in memory
	if newConfig.Port > 0 {
		s.config.Port = newConfig.Port
	}
	if newConfig.StorageCapGB > 0 {
		s.config.StorageCapGB = newConfig.StorageCapGB
	}
	if newConfig.SegmentLengthS > 0 {
		s.config.SegmentLengthS = newConfig.SegmentLengthS
	}
	if len(newConfig.Cameras) > 0 {
		s.config.Cameras = newConfig.Cameras
	}

	// Save config to disk
	if err := SaveConfig(s.config, s.configPath); err != nil {
		s.logger.Printf("Failed to save config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Configuration updated. Restart required for changes to take effect.",
	})
}

func (s *APIServer) handleListCameras(w http.ResponseWriter, r *http.Request) {
	cameras := s.cameraManager.ListCameras()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"cameras": cameras,
	})
}

func (s *APIServer) handleUpdateCamera(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" && r.Method != "PUT" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cameraID := r.URL.Query().Get("id")
	if cameraID == "" {
		http.Error(w, "Missing camera ID", http.StatusBadRequest)
		return
	}

	var updatedCamera CameraConfig
	if err := json.NewDecoder(r.Body).Decode(&updatedCamera); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Find and update camera in config
	found := false
	for i := range s.config.Cameras {
		if s.config.Cameras[i].ID == cameraID {
			// Preserve ID
			updatedCamera.ID = cameraID
			s.config.Cameras[i] = updatedCamera
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	// Save config to disk
	if err := SaveConfig(s.config, s.configPath); err != nil {
		s.logger.Printf("Failed to save config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	// Reload config from disk
	cfg, err := LoadOrCreateConfig(s.configPath)
	if err != nil {
		s.logger.Printf("Failed to reload config: %v", err)
		http.Error(w, "Failed to reload configuration", http.StatusInternalServerError)
		return
	}
	s.config = cfg

	// Restart cameras with new config
	if err := s.cameraManager.RestartWithConfigs(convertCameraConfigs(s.config.Cameras), s.config.SegmentLengthS, s.config.VideoDir); err != nil {
		s.logger.Printf("Failed to restart cameras: %v", err)
		http.Error(w, "Failed to apply camera changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Camera configuration updated and applied.",
	})
}

func (s *APIServer) handleAddCamera(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var newCamera CameraConfig
	if err := json.NewDecoder(r.Body).Decode(&newCamera); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if newCamera.ID == "" || newCamera.Name == "" || newCamera.Device == "" {
		http.Error(w, "Missing required fields (id, name, device)", http.StatusBadRequest)
		return
	}

	// Check if camera ID already exists
	for _, cam := range s.config.Cameras {
		if cam.ID == newCamera.ID {
			http.Error(w, "Camera with this ID already exists", http.StatusConflict)
			return
		}
	}

	// Add camera to config
	s.config.Cameras = append(s.config.Cameras, newCamera)

	// Save config to disk
	if err := SaveConfig(s.config, s.configPath); err != nil {
		s.logger.Printf("Failed to save config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	// Reload config from disk
	cfg, err := LoadOrCreateConfig(s.configPath)
	if err != nil {
		s.logger.Printf("Failed to reload config: %v", err)
		http.Error(w, "Failed to reload configuration", http.StatusInternalServerError)
		return
	}
	s.config = cfg

	// Restart cameras with new config
	if err := s.cameraManager.RestartWithConfigs(convertCameraConfigs(s.config.Cameras), s.config.SegmentLengthS, s.config.VideoDir); err != nil {
		s.logger.Printf("Failed to restart cameras: %v", err)
		http.Error(w, "Failed to apply camera changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Camera added and applied.",
	})
}

func (s *APIServer) handleDeleteCamera(w http.ResponseWriter, r *http.Request) {
	if r.Method != "DELETE" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cameraID := r.URL.Query().Get("id")
	if cameraID == "" {
		http.Error(w, "Missing camera ID", http.StatusBadRequest)
		return
	}

	// Find and remove camera from config
	found := false
	for i, cam := range s.config.Cameras {
		if cam.ID == cameraID {
			s.config.Cameras = append(s.config.Cameras[:i], s.config.Cameras[i+1:]...)
			found = true
			break
		}
	}

	if !found {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	// Save config to disk
	if err := SaveConfig(s.config, s.configPath); err != nil {
		s.logger.Printf("Failed to save config: %v", err)
		http.Error(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	// Reload config from disk
	cfg, err := LoadOrCreateConfig(s.configPath)
	if err != nil {
		s.logger.Printf("Failed to reload config: %v", err)
		http.Error(w, "Failed to reload configuration", http.StatusInternalServerError)
		return
	}
	s.config = cfg

	// Restart cameras with new config
	if err := s.cameraManager.RestartWithConfigs(convertCameraConfigs(s.config.Cameras), s.config.SegmentLengthS, s.config.VideoDir); err != nil {
		s.logger.Printf("Failed to restart cameras: %v", err)
		http.Error(w, "Failed to apply camera changes: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Camera deleted and applied.",
	})
}
