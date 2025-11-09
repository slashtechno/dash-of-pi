package main

import "time"

// =============================================================================
// Performance and Timing Constants
// =============================================================================

const (
	// Frame extraction and streaming rates
	// Calculation: 1000ms / 25fps = 40ms per frame
	TargetStreamFPS       = 20  // Minimum acceptable FPS for live stream quality
	FrameExtractionMS     = 40  // Extract frames every 40ms = 25 FPS (1000ms ÷ 25 = 40ms)
	MJPEGStreamIntervalMS = 40  // Send MJPEG frames every 40ms = 25 FPS stream
	
	// Timeouts and intervals
	// Calculation: 50 intervals × 40ms = 2000ms = 2 seconds
	MJPEGNoFrameTimeout   = 50  // Disconnect after 50 missed frames (50 × 40ms = 2 seconds)
	StatusUpdateIntervalS = 5   // Frontend polls server status every 5 seconds
	
	// Retry and reconnect
	StreamRetryAttempts   = 3     // Attempt to reconnect 3 times before giving up
	StreamStallCheckMS    = 10000 // Check if stream stalled every 10 seconds
)

// =============================================================================
// Buffer Sizes and Memory Limits
// =============================================================================

const (
	// Frame extraction buffers
	// Why: MJPEG file is being written continuously. We read from the END of the file
	//      to get the most recent frame. 256KB ensures we capture 2-3 frames worth of
	//      data, allowing us to find the most recent complete frame.
	FrameBufferSizeKB     = 256  // Read last 256KB from MJPEG file (typical frame: 80-150KB)
	MaxFrameSizeKB        = 200  // Max size to search backwards for frame start (prevents old frames)
	MinFileSize           = 100  // Skip extraction if file too small (not enough data yet)
	
	// FFmpeg stderr capture
	// Why: Capture FFmpeg error messages for debugging. 4KB is enough for typical errors
	FFmpegStderrBufferKB = 4    // 4KB buffer for FFmpeg error messages
	
	// HTTP and network
	// Why: Prevents malicious clients from sending huge headers that consume memory.
	//      1MB is more than enough for legitimate HTTP headers (typical: <10KB)
	HTTPMaxHeaderBytes   = 1 << 20  // 1MB = maximum HTTP header size (security limit)
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
	DefaultPort            = 8080
	DefaultStorageCapGB    = 10
	DefaultVideoBitrate    = 1024 // kbps
	DefaultVideoFPS        = 24
	DefaultVideoWidth      = 1280
	DefaultVideoHeight     = 720
	DefaultSegmentLengthS  = 60   // seconds
	DefaultMJPEGQuality    = 5    // 2-31 scale, lower is better
	
	// Device defaults
	DefaultCameraDevice    = "/dev/video0"
)

// =============================================================================
// FFmpeg Quality Settings
// =============================================================================

const (
	// MPEG-4 quality for exports (q:v scale)
	ExportVideoQuality     = 2    // 1-31 scale, lower=better quality (2=very high)
	
	// MJPEG quality range
	MJPEGQualityMin        = 2    // Best quality (largest files)
	MJPEGQualityMax        = 31   // Worst quality (smallest files)
)

// =============================================================================
// Logging and Debug
// =============================================================================

const (
	// Log intervals for periodic updates
	FrameLogInterval       = 30   // Log frame stats every 30 frames
	StreamLogInterval      = 100  // Log stream stats every 100 frames
	
	// Error throttling
	ErrorLogThrottleS      = 5    // Don't log same error more than once per 5 seconds
)

// =============================================================================
// File Extensions and Formats
// =============================================================================

const (
	ExtensionMJPEG = ".mjpeg"
	ExtensionMP4   = ".mp4"
	ExtensionWebM  = ".webm"
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

// IsMJPEGFile checks if file is an MJPEG recording
func IsMJPEGFile(filename string) bool {
	return HasExtension(filename, ExtensionMJPEG)
}
