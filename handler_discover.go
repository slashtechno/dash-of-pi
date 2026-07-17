package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// DiscoveredFormat is one supported capture format+size+framerate for a V4L2 device.
type DiscoveredFormat struct {
	Format string `json:"format"` // FourCC, e.g. "MJPG", "YUYV", "H264"
	Width  int    `json:"width"`
	Height int    `json:"height"`
	FPS    int    `json:"fps"`
}

// DiscoveredDevice is one physical camera discovered on the system.
type DiscoveredDevice struct {
	Device  string             `json:"device"`  // /dev/videoN to open for capture
	Name    string             `json:"name"`    // V4L2 card name
	Driver  string             `json:"driver"`  // kernel driver, e.g. uvcvideo, bcm2835-unicam
	Type    string             `json:"type"`    // "usb" | "csi" | "v4l2"
	Formats []DiscoveredFormat `json:"formats"` // supported capture formats (empty for CSI)
}

// handleDiscoverCameras enumerates cameras available on the system so the UI can
// populate device + resolution/FPS dropdowns instead of making the user guess.
// USB/UVC cameras are enumerated via v4l2-ctl; CSI cameras via rpicam-still.
func (s *APIServer) handleDiscoverCameras(w http.ResponseWriter, r *http.Request) {
	resp := map[string]interface{}{
		"devices":        []DiscoveredDevice{},
		"csi_available":  false,
		"v4l2_available": v4l2ctlAvailable(),
	}

	if runtime.GOOS != "linux" {
		// ponytail: discovery is Linux-only; dev machines get an empty list and
		// the UI falls back to a free-text device field.
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
		return
	}

	devices := discoverV4L2Cameras()
	csi := csiAvailable()
	resp["devices"] = devices
	resp["csi_available"] = csi

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func v4l2ctlAvailable() bool {
	_, err := exec.LookPath("v4l2-ctl")
	return err == nil
}

// csiAvailable reports whether libcamera enumerates a usable CSI camera.
// Mirrors camera.IsCSICamera's "Available cameras" header check without depending
// on the camera package (which needs runtime config).
func csiAvailable() bool {
	if _, err := exec.LookPath("rpicam-still"); err != nil {
		return false
	}
	out, err := exec.Command("rpicam-still", "--list-cameras").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "Available cameras")
}

// discoverV4L2Cameras walks /sys/class/video4linux, groups multi-node cameras
// (a UVC cam often exposes separate capture + metadata nodes) by their underlying
// device symlink, and returns one entry per physical camera with supported formats.
func discoverV4L2Cameras() []DiscoveredDevice {
	entries, err := os.ReadDir("/sys/class/video4linux")
	if err != nil {
		return nil
	}

	type node struct {
		videoN     string
		devPath    string // resolved physical device symlink target (for grouping)
		driver     string
		name       string
		hasCapture bool
		formats    []DiscoveredFormat
	}

	byDev := map[string]*node{} // keyed by devPath (or videoN if no symlink)
	var order []string

	for _, e := range entries {
		videoN := e.Name()
		if !strings.HasPrefix(videoN, "video") {
			continue
		}
		base := filepath.Join("/sys/class/video4linux", videoN)

		// name
		nameBytes, _ := os.ReadFile(filepath.Join(base, "name"))
		name := strings.TrimSpace(string(nameBytes))

		// driver
		driverLink, err := os.Readlink(filepath.Join(base, "device/driver"))
		driver := ""
		if err == nil {
			driver = filepath.Base(driverLink)
		}

		// physical device symlink (groups capture + metadata nodes of one UVC cam)
		devLink, err := os.Readlink(filepath.Join(base, "device"))
		devPath := ""
		if err == nil {
			if abs, err2 := filepath.Abs(filepath.Join(base, devLink)); err2 == nil {
				devPath = abs
			} else {
				devPath = devLink
			}
		}
		key := devPath
		if key == "" {
			key = videoN
		}

		n, exists := byDev[key]
		if !exists {
			n = &node{videoN: videoN, devPath: devPath, driver: driver, name: name}
			byDev[key] = n
			order = append(order, key)
		}
		// keep the lowest videoN as the canonical capture device path
		if videoN < n.videoN {
			n.videoN = videoN
		}
		if name != "" && n.name == "" {
			n.name = name
		}

		// only capture-capable nodes yield formats; a node with formats marks the
		// physical camera as having a usable capture interface.
		formats := listV4L2Formats("/dev/" + videoN)
		if len(formats) > 0 {
			n.hasCapture = true
			// Merge formats (capture node may be the higher-numbered one)
			n.formats = append(n.formats, formats...)
		}
	}

	var devices []DiscoveredDevice
	for _, key := range order {
		n := byDev[key]
		if !n.hasCapture {
			continue // metadata-only node, not a usable capture camera
		}
		// Deduplicate formats (capture + metadata nodes may overlap)
		n.formats = dedupFormats(n.formats)
		sortFormats(n.formats)

		typ := "v4l2"
		if n.driver == "uvcvideo" {
			typ = "usb"
		} else if strings.Contains(n.driver, "unicam") {
			typ = "csi"
		}
		devices = append(devices, DiscoveredDevice{
			Device:  "/dev/" + n.videoN,
			Name:    n.name,
			Driver:  n.driver,
			Type:    typ,
			Formats: n.formats,
		})
	}
	return devices
}

func dedupFormats(in []DiscoveredFormat) []DiscoveredFormat {
	seen := map[string]bool{}
	out := in[:0]
	for _, f := range in {
		k := f.Format + ":" + strconv.Itoa(f.Width) + "x" + strconv.Itoa(f.Height) + "@" + strconv.Itoa(f.fpsKey())
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, f)
	}
	return out
}

func (f DiscoveredFormat) fpsKey() int {
	if f.FPS > 0 {
		return f.FPS
	}
	return 0
}

// sortFormats: MJPG first (preferred for dashcam), then by resolution desc, then fps desc.
func sortFormats(f []DiscoveredFormat) {
	sort.SliceStable(f, func(i, j int) bool {
		if formatPriority(f[i].Format) != formatPriority(f[j].Format) {
			return formatPriority(f[i].Format) < formatPriority(f[j].Format)
		}
		if f[i].Width != f[j].Width {
			return f[i].Width > f[j].Width
		}
		if f[i].Height != f[j].Height {
			return f[i].Height > f[j].Height
		}
		return f[i].FPS > f[j].FPS
	})
}

func formatPriority(fourcc string) int {
	switch fourcc {
	case "MJPG", "MJPEG":
		return 0
	case "H264":
		return 1
	case "YUYV":
		return 2
	default:
		return 3
	}
}

var (
	reFormat  = regexp.MustCompile(`^\s*\[\d+\]:\s*'([^']+)'.*$`)
	reSize    = regexp.MustCompile(`Size:\s+Discrete\s+(\d+)x(\d+)`)
	reFps     = regexp.MustCompile(`Interval:\s+Discrete\s+[\d.]+s\s+\(([\d.]+)\s*fps\)`)
)

// listV4L2Formats runs `v4l2-ctl --device=D --list-formats-ext` and parses the
// capture formats, sizes, and framerates. Returns nil if the device isn't a
// capture device or v4l2-ctl isn't installed.
func listV4L2Formats(device string) []DiscoveredFormat {
	if !v4l2ctlAvailable() {
		return nil
	}
	out, err := exec.Command("v4l2-ctl", "--device="+device, "--list-formats-ext").CombinedOutput()
	if err != nil {
		return nil
	}
	return parseV4L2Formats(out)
}

func parseV4L2Formats(out []byte) []DiscoveredFormat {
	var formats []DiscoveredFormat
	var curFourCC string
	var curW, curH int

	flush := func(fps float64) {
		if curFourCC == "" || curW == 0 || curH == 0 {
			return
		}
		formats = append(formats, DiscoveredFormat{
			Format: curFourCC,
			Width:  curW,
			Height: curH,
			FPS:    int(fps + 0.5),
		})
	}

	for _, line := range strings.Split(string(out), "\n") {
		if m := reFormat.FindStringSubmatch(line); m != nil {
			curFourCC = m[1]
			curW, curH = 0, 0
			continue
		}
		if m := reSize.FindStringSubmatch(line); m != nil {
			// previous size block done; reset for new size
			curW, _ = strconv.Atoi(m[1])
			curH, _ = strconv.Atoi(m[2])
			continue
		}
		if m := reFps.FindStringSubmatch(line); m != nil {
			fps, _ := strconv.ParseFloat(m[1], 64)
			flush(fps)
			continue
		}
	}
	return formats
}