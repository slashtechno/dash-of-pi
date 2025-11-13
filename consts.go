package main

import "time"

// =============================================================================
// Performance and Timing Constants
// =============================================================================

const (
	// Frame extraction and streaming rates
	TargetStreamFPS       = 24 // Target FPS from camera
	MJPEGStreamIntervalMS = 33 // Send frames every 33ms = 30 FPS stream

	// Timeouts and intervals
	MJPEGNoFrameTimeout   = 50 // Disconnect after 50 missed frames
	StatusUpdateIntervalS = 5  // Frontend polls server status every 5 seconds

	// Retry and reconnect
	StreamRetryAttempts = 3     // Attempt to reconnect 3 times before giving up
	StreamStallCheckMS  = 10000 // Check if stream stalled every 10 seconds
)

// =============================================================================
// Buffer Sizes and Memory Limits
// =============================================================================

const (
	// Frame extraction buffers
	FrameBufferSizeKB = 256 // Read last 256KB from MJPEG file (typical frame: 80-150KB)
	MaxFrameSizeKB    = 200 // Max size to search backwards for frame start (prevents old frames)
	MinFileSize       = 100 // Skip extraction if file too small (not enough data yet)

	// FFmpeg stderr capture
	FFmpegStderrBufferKB = 4 // 4KB buffer for FFmpeg error messages

	// HTTP and network
	HTTPMaxHeaderBytes = 1 << 20 // 1MB = maximum HTTP header size (security limit)
)

// =============================================================================
// Server Timeouts
// =============================================================================

const (
	// Why: Protects against slow-read attacks and hung connections
	ServerReadTimeout       = 30 * time.Second  // 30s max to read entire request body
	ServerIdleTimeout       = 120 * time.Second // 2min max idle before closing connection
	ServerReadHeaderTimeout = 10 * time.Second  // 10s max to read HTTP headers
	ServerWriteTimeout      = 0                 // 0 = no timeout (needed for long video streams)
)

// =============================================================================
// Storage and Data Conversions
// =============================================================================

const (
	// Byte conversions
	BytesPerKB = 1024
	BytesPerMB = 1024 * 1024
	BytesPerGB = 1024 * 1024 * 1024

	// Bitrate to storage estimation multiplier
	BitrateToStorageMultiplier = 128 // bytes / (bitrate * 128) = seconds
)

// =============================================================================
// Default Configuration Values
// =============================================================================

const (
	// Video defaults
	DefaultPort           = 8080
	DefaultStorageCapGB   = 10
	DefaultVideoBitrate   = 1024 // kbps
	DefaultVideoFPS       = 24
	DefaultVideoWidth     = 1280
	DefaultVideoHeight    = 720
	DefaultSegmentLengthS = 60   // seconds
	DefaultMJPEGQuality   = 8    // 2-31 scale, lower is better, 8=good balance
	DefaultEmbedTimestamp = true // Embed timestamp by default

	// Device defaults
	DefaultCameraDevice = "/dev/video0"
)

// =============================================================================
// FFmpeg Quality Settings
// =============================================================================

const (
	// MPEG-4 quality for exports (q:v scale)
	ExportVideoQuality = 2 // 1-31 scale, lower=better quality (2=very high)
)

// =============================================================================
// Logging and Debug
// =============================================================================

const (
	// Log intervals for periodic updates
	FrameLogInterval  = 30  // Log frame stats every 30 frames
	StreamLogInterval = 100 // Log stream stats every 100 frames

	// Error throttling
	ErrorLogThrottleS = 5 // Don't log same error more than once per 5 seconds
)

// =============================================================================
// File Extensions and Formats
// =============================================================================

const (
	ExtensionMJPEG = ".mjpeg"
	ExtensionMP4   = ".mp4"
	ExtensionWebM  = ".webm"

	// Export filename
	ExportFilename = "current_export.mp4"
)

// =============================================================================
// Helper Functions
// =============================================================================

// HasExtension checks if filename has the given extension
func HasExtension(filename, ext string) bool {
	if len(filename) < len(ext) {
		return false
	}
	return filename[len(filename)-len(ext):] == ext
}

// IsPlayableVideo checks if file is a playable video format (MP4/WebM)
func IsPlayableVideo(filename string) bool {
	return HasExtension(filename, ExtensionMP4) || HasExtension(filename, ExtensionWebM)
}

// IsMJPEGFile checks if file is a video recording (MJPEG or MP4)
func IsMJPEGFile(filename string) bool {
	return HasExtension(filename, ExtensionMJPEG) || HasExtension(filename, ExtensionMP4)
}
