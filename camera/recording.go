package camera

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

const (
	// FFmpeg stderr capture
	FFmpegStderrBufferKB = 4 // 4KB buffer for FFmpeg error messages
)

// recordAndStreamSegment records video to MJPEG (Motion JPEG) format
// MJPEG supports real-time streaming and safe recovery from interrupted recordings
// Each frame is a complete JPEG, so the file is always readable even while recording
func (c *Camera) recordAndStreamSegment(filename string) error {
	inputFormat, inputDevice := c.getCameraInput()

	args := []string{
		"-y",
		"-loglevel", "warning",
		"-f", inputFormat,
	}

	if inputFormat == "video4linux2" || inputFormat == "v4l2" {
		args = append(args,
			"-input_format", "mjpeg",
			"-video_size", fmt.Sprintf("%dx%d", c.camConfig.ResWidth, c.camConfig.ResHeight),
		)
	}

	args = append(args,
		"-framerate", fmt.Sprintf("%d", c.camConfig.FPS),
		"-rtbufsize", "5M",
		"-thread_queue_size", "16",
		"-i", inputDevice,
	)

	// Build video filters
	var videoFilters []string
	
	// Apply rotation if specified
	if c.camConfig.Rotation != 0 {
		switch c.camConfig.Rotation {
		case 90:
			videoFilters = append(videoFilters, "transpose=1")
		case 180:
			videoFilters = append(videoFilters, "transpose=1,transpose=1")
		case 270:
			videoFilters = append(videoFilters, "transpose=2")
		}
	}
	
	if inputFormat != "video4linux2" && inputFormat != "v4l2" {
		videoFilters = append(videoFilters, fmt.Sprintf("scale=%d:%d", c.camConfig.ResWidth, c.camConfig.ResHeight))
	}
	if c.camConfig.EmbedTimestamp {
		timestampFilter := "drawtext=text='%{gmtime\\:%Y-%m-%d %H\\\\\\:%M\\\\\\:%S} \\\\(UTC\\\\)':fontcolor=white:fontsize=24:box=1:boxcolor=black@0.5:boxborderw=5:x=10:y=10"
		videoFilters = append(videoFilters, timestampFilter)
	}
	if len(videoFilters) > 0 {
		args = append(args, "-vf", strings.Join(videoFilters, ","))
	}

	// Encode to MJPEG (Motion JPEG) for real-time streaming and robust recovery
	args = append(args,
		"-c:v", "mjpeg",
		"-q:v", fmt.Sprintf("%d", c.camConfig.MJPEGQuality),
		"-r", fmt.Sprintf("%d", c.camConfig.FPS),
		"-t", fmt.Sprintf("%d", c.segmentLength),
		"-f", "mjpeg",
		filename,
	)

	recordCmd := exec.Command("ffmpeg", args...)

	stderr, err := recordCmd.StderrPipe()
	if err != nil {
		return err
	}

	c.cmdMu.Lock()
	c.recordCmd = recordCmd
	c.cmdMu.Unlock()

	if err := recordCmd.Start(); err != nil {
		c.cmdMu.Lock()
		c.recordCmd = nil
		c.cmdMu.Unlock()
		return err
	}

	// Capture stderr for debugging
	var stderrOutput strings.Builder
	go func() {
		buf := make([]byte, FFmpegStderrBufferKB*BytesPerKB)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				stderrOutput.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
	}()

	// Wait for recording to complete
	recordErr := recordCmd.Wait()

	c.cmdMu.Lock()
	c.recordCmd = nil
	c.cmdMu.Unlock()

	if recordErr != nil && stderrOutput.Len() > 0 {
		c.logger.Printf("FFmpeg error output: %s", stderrOutput.String())
	}

	return recordErr
}

// getCameraInput returns the format and device based on OS
func (c *Camera) getCameraInput() (string, string) {
	switch runtime.GOOS {
	case "darwin":
		return "avfoundation", "0"
	case "windows":
		return "dshow", "video=\"USB Video Device\""
	default:
		device := c.camConfig.Device
		if device == "" {
			device = "/dev/video0"
		}
		return "v4l2", device
	}
}

// Stop halts the recording
func (c *Camera) Stop() {
	close(c.done)
	c.cmdMu.Lock()
	defer c.cmdMu.Unlock()
	if c.recordCmd != nil && c.recordCmd.Process != nil {
		c.recordCmd.Process.Kill()
	}
}
