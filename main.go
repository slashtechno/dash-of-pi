package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/adrg/xdg"
	"github.com/joho/godotenv"
)

func main() {
	// Load .env file if it exists
	godotenv.Load()

	// Parse command-line flags
	var (
		configPath = flag.String("config", "", "Path to config file (default: XDG config directory)")
	)
	flag.Parse()

	// Initialize logger
	logger := NewLogger(false)

	// Use XDG config directory if not specified
	if *configPath == "" {
		var err error
		*configPath, err = xdg.ConfigFile("dash-of-pi/config.json")
		if err != nil {
			// Fallback to legacy location
			*configPath = filepath.Join(os.ExpandEnv("$HOME"), ".config/dash-of-pi/config.json")
		}
	}

	// Create directories if they don't exist
	if err := os.MkdirAll(filepath.Dir(*configPath), 0755); err != nil {
		log.Fatalf("Failed to create config directory: %v", err)
	}

	// Load or create config
	config, err := LoadOrCreateConfig(*configPath)
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	logger.Printf("Starting Pi Dashboard Cam...")
	logger.Printf("Listening on port %d", config.Port)
	logger.Printf("Auth token: %s", config.AuthToken)
	logger.Printf("Video directory: %s", config.VideoDir)
	logger.Printf("Storage cap: %dGB", config.StorageCapGB)

	// Create storage manager
	sm, err := NewStorageManager(config.VideoDir, config.StorageCapGB)
	if err != nil {
		logger.Fatalf("Failed to initialize storage manager: %v", err)
	}

	// Create camera
	camera, err := NewCamera(config, logger)
	if err != nil {
		logger.Fatalf("Failed to initialize camera: %v", err)
	}

	// Create API server
	server := NewAPIServer(config, camera, sm, logger)

	// Start recording in background
	recordingDone := make(chan error, 1)
	go func() {
		recordingDone <- camera.Start(config.VideoDir)
	}()

	// Start HTTP server in background
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Start()
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-recordingDone:
		logger.Printf("Recording stopped: %v", err)
	case err := <-serverDone:
		logger.Printf("Server stopped: %v", err)
	case sig := <-sigChan:
		fmt.Printf("\nReceived signal: %v\n", sig)
	}

	// Cleanup
	logger.Printf("Shutting down...")
	camera.Stop()
	server.Stop()
}
