package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

type CameraConfig struct {
	ID            string `json:"id"`              // Unique identifier (auto-generated if empty)
	Name          string `json:"name"`            // User-friendly name (e.g., "Front", "Rear")
	Device        string `json:"device"`          // e.g., /dev/video0, /dev/video1
	Rotation      int    `json:"rotation"`        // 0, 90, 180, 270 degrees
	ResWidth      int    `json:"res_width"`       // Video width
	ResHeight     int    `json:"res_height"`      // Video height
	Bitrate       int    `json:"bitrate"`         // in kbps
	FPS           int    `json:"fps"`             // frames per second
	MJPEGQuality  int    `json:"mjpeg_quality"`   // 2-31, lower = higher quality
	EmbedTimestamp bool  `json:"embed_timestamp"` // Whether to overlay timestamp on video
	Enabled       bool   `json:"enabled"`         // Whether this camera is active
}

type Config struct {
	Port           int            `json:"port"`
	VideoDir       string         `json:"video_dir"`
	StorageCapGB   int            `json:"storage_cap_gb"`
	AuthToken      string         `json:"auth_token"`
	SegmentLengthS int            `json:"segment_length_s"` // seconds
	Cameras        []CameraConfig `json:"cameras"`          // Multiple camera configurations
}

func DefaultConfig() *Config {
	// Default to current directory for videos if no config is provided
	// This allows the app to run without a home directory
	videoDir := "./videos"
	
	// Try XDG state directory only if we have a valid home directory
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		// Check if home directory is /var/lib/dash-of-pi (system-wide installation)
		// In this case, store videos directly in /var/lib/dash-of-pi/videos
		if homeDir == "/var/lib/dash-of-pi" {
			videoDir = "/var/lib/dash-of-pi/videos"
		} else if stateDir, err := xdg.StateFile("dash-of-pi/videos"); err == nil {
			videoDir = stateDir
		} else {
			// XDG fallback
			videoDir = filepath.Join(homeDir, ".local/state/dash-of-pi/videos")
		}
	}

	return &Config{
		Port:           DefaultPort,
		VideoDir:       videoDir,
		StorageCapGB:   DefaultStorageCapGB,
		SegmentLengthS: DefaultSegmentLengthS,
		Cameras: []CameraConfig{
			{
				ID:       "default",
				Name:     "Default Camera",
				Device:   DefaultCameraDevice,
				Rotation: 0,
				ResWidth:       DefaultVideoWidth,
				ResHeight:      DefaultVideoHeight,
				Bitrate:        DefaultVideoBitrate,
				FPS:            DefaultVideoFPS,
				MJPEGQuality:   DefaultMJPEGQuality,
				EmbedTimestamp: DefaultEmbedTimestamp,
				Enabled:        true,
			},
		},
	}
}

func LoadOrCreateConfig(configPath string) (*Config, error) {
	// If config exists, load it
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config: %w", err)
		}

		config := &Config{}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}

		// Ensure camera configs have defaults
		for i := range config.Cameras {
			cam := &config.Cameras[i]
			if cam.ID == "" {
				cam.ID = fmt.Sprintf("camera_%d", i)
			}
			if cam.ResWidth == 0 {
				cam.ResWidth = DefaultVideoWidth
			}
			if cam.ResHeight == 0 {
				cam.ResHeight = DefaultVideoHeight
			}
			if cam.Bitrate == 0 {
				cam.Bitrate = DefaultVideoBitrate
			}
			if cam.FPS == 0 {
				cam.FPS = DefaultVideoFPS
			}
			if cam.MJPEGQuality == 0 {
				cam.MJPEGQuality = DefaultMJPEGQuality
			}
		}

		return config, nil
	}

	// Create default config
	config := DefaultConfig()

	// Generate auth token if not present
	if config.AuthToken == "" {
		config.AuthToken = generateToken()
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Ensure video directory exists
	if err := os.MkdirAll(config.VideoDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create video directory: %w", err)
	}

	// Write default config
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return nil, fmt.Errorf("failed to write config: %w", err)
	}

	fmt.Printf("Created default config at %s\n", configPath)
	fmt.Printf("Auth token: %s\n", config.AuthToken)

	return config, nil
}

// SaveConfig saves the configuration to disk
func SaveConfig(config *Config, configPath string) error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}
