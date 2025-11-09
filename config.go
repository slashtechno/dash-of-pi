package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/adrg/xdg"
)

type Config struct {
	Port           int    `json:"port"`
	StreamPort     int    `json:"stream_port"`  // UDP port for live stream from FFmpeg
	VideoDir       string `json:"video_dir"`
	StorageCapGB   int    `json:"storage_cap_gb"`
	AuthToken      string `json:"auth_token"`
	VideoBitrate   int    `json:"video_bitrate"`   // in kbps
	VideoFPS       int    `json:"video_fps"`
	VideoResWidth  int    `json:"video_res_width"`
	VideoResHeight int    `json:"video_res_height"`
	SegmentLengthS int    `json:"segment_length_s"` // seconds
	CameraDevice   string `json:"camera_device"`    // e.g., /dev/video0, /dev/video1
}

func DefaultConfig() *Config {
	// Use XDG state directory for videos
	stateDir, err := xdg.StateFile("dash-of-pi/videos")
	if err != nil {
		// Fallback if XDG fails
		homeDir, _ := os.UserHomeDir()
		stateDir = filepath.Join(homeDir, ".local/state/dash-of-pi/videos")
	}
	// Remove the "videos" part from stateDir since StateFile adds it
	stateDir = filepath.Dir(stateDir)

	return &Config{
		Port:           8080,
		StreamPort:     5000, // UDP port for FFmpeg stream output
		VideoDir:       stateDir,
		StorageCapGB:   10,
		VideoBitrate:   1024,
		VideoFPS:       24,
		VideoResWidth:  1280,
		VideoResHeight: 720,
		SegmentLengthS: 60,
		CameraDevice:   "/dev/video0",
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

		// Set defaults for new fields if missing
		if config.StreamPort == 0 {
			config.StreamPort = 5000
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
