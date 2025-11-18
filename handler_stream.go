package main

import (
	"fmt"
	"net/http"
	"time"
)

// handleStreamFrame serves the latest JPEG frame from the live stream
func (s *APIServer) handleStreamFrame(w http.ResponseWriter, r *http.Request) {
	// Get camera ID from query parameter (defaults to first camera)
	cameraID := r.URL.Query().Get("camera")
	if cameraID == "" {
		cameraID = s.cameraManager.GetDefaultCameraID()
	}

	// Get the stream manager for this camera
	streamMgr, ok := s.cameraManager.GetStreamManager(cameraID)
	if !ok {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	// Get latest frame from stream manager
	frameData := streamMgr.GetLatestFrame()
	if len(frameData) == 0 {
		s.logger.Printf("[WARN] /api/stream/frame: No frames available for camera %s - returning 503", cameraID)
		http.Error(w, "Recording is initializing - no frames available yet. Please try again in a few seconds.", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(frameData)))
	w.Write(frameData)
}

// handleStreamMJPEG serves continuous MJPEG stream (multipart)
func (s *APIServer) handleStreamMJPEG(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
	w.Header().Set("Connection", "close")

	boundary := "frame"
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	// Get camera ID from query parameter (defaults to first camera)
	cameraID := r.URL.Query().Get("camera")
	if cameraID == "" {
		cameraID = s.cameraManager.GetDefaultCameraID()
	}

	// Get the stream manager for this camera
	streamMgr, ok := s.cameraManager.GetStreamManager(cameraID)
	if !ok {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	s.logger.Printf("MJPEG stream client connected for camera %s", cameraID)
	defer s.logger.Printf("MJPEG stream client disconnected")

	// Stream frames continuously at target FPS
	ticker := time.NewTicker(time.Duration(MJPEGStreamIntervalMS) * time.Millisecond)
	defer ticker.Stop()

	frameCount := 0
	noFrameCount := 0
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			frameData := streamMgr.GetLatestFrame()
			if len(frameData) == 0 {
				noFrameCount++
				if noFrameCount > MJPEGNoFrameTimeout {
					s.logger.Printf("MJPEG stream: No frames timeout, closing connection")
					return
				}
				continue
			}
			noFrameCount = 0

			// Write frame to stream
			_, err := fmt.Fprintf(w, "--%s\r\n", boundary)
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "Content-Type: image/jpeg\r\n")
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(frameData))
			if err != nil {
				return
			}
			_, err = w.Write(frameData)
			if err != nil {
				return
			}
			_, err = fmt.Fprintf(w, "\r\n")
			if err != nil {
				return
			}

			flusher.Flush()
			frameCount++

			if frameCount%StreamLogInterval == 0 {
				s.logger.Debugf("MJPEG stream: sent %d frames", frameCount)
			}
		}
	}
}
