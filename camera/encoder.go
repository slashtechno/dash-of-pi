package camera

import (
	"os/exec"
	"strings"
)

// detectVideoEncoder checks available encoders and returns the best one
// Priority: h264_v4l2m2m (Pi hardware) > h264_vaapi (generic hardware) > libopenh264 (open) > libx264 (fallback)
func detectVideoEncoder(logger Logger) string {
	cmd := exec.Command("ffmpeg", "-encoders")
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Debugf("Failed to query FFmpeg encoders: %v", err)
		return "libopenh264"
	}

	encodersOutput := string(output)

	// Priority list of encoders to check
	preferredEncoders := []string{
		"h264_v4l2m2m", // Raspberry Pi hardware encoder
		"h264_vaapi",   // Generic hardware encoder
		"libopenh264",  // Open-source H.264
		"libx264",      // Software fallback
	}

	for _, encoder := range preferredEncoders {
		if strings.Contains(encodersOutput, encoder) {
			// Test if encoder actually works with a quick validation
			if isEncoderUsable(encoder, logger) {
				return encoder
			}
		}
	}

	// Ultimate fallback
	logger.Printf("[WARN] No suitable H.264 encoders found, defaulting to libopenh264")
	return "libopenh264"
}

// isEncoderUsable tests if an encoder can actually be used
func isEncoderUsable(encoder string, logger Logger) bool {
	// Skip hardware encoders that require specific hardware
	if encoder == "h264_v4l2m2m" || encoder == "h264_vaapi" {
		// Try a quick test to see if the encoder works
		testCmd := exec.Command("ffmpeg",
			"-f", "lavfi",
			"-i", "color=c=black:s=640x480:d=0.1",
			"-c:v", encoder,
			"-f", "null",
			"-",
		)

		if err := testCmd.Run(); err != nil {
			logger.Debugf("Encoder %s not usable: %v", encoder, err)
			return false
		}
	}

	return true
}
