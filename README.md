# Dash of Pi

Headless dash cam for Raspberry Pi Zero 2W (but usable on other machines). Continuous video recording with web dashboard for downloads and live streaming. Most importantly, **footage is recoverable even if power loss occurs mid-recording**.

## Quick Start

**Raspberry Pi Setup (Recommended for Pi Zero 2W and other Pi models):**
```bash
# SSH into your Pi (replace 'pi' with your username and hostname/IP)
# ssh pi@raspberrypi.local
git clone https://github.com/slashtechno/dash-of-pi && cd dash-of-pi
sudo ./scripts/install.sh
# Follow the installer prompts and reboot when prompted.
# The installer prints your auth token and a token-prefilled dashboard URL at the end.
# Access at http://<your-pi-hostname-or-ip>:8080
```

**Local Testing (macOS, Linux, or other machines):**
```bash
git clone https://github.com/slashtechno/dash-of-pi && cd dash-of-pi
go run .
# Open http://localhost:8080 and use the auth token printed in the terminal
```


## Features

- **Multi-camera support** - Record from multiple cameras simultaneously
- **Camera auto-discovery** - The dashboard scans `/dev/video*`, detects USB (UVC) vs CSI cameras, and lists each camera's *supported* resolutions and framerates, so you pick from a dropdown instead of guessing (and can't pick an unsupported combo)
- **Per-camera configuration** - Independent settings for resolution, rotation, quality, timestamp overlay
- **Settings page** - Edit cameras, storage cap, segment length, port, and the auth token from the browser (most changes apply live, no restart)
- Records continuous video in segments with rolling storage
- Web dashboard for live streaming and downloading videos
- Timestamps embedded on video (optional, per-camera; USB cameras only)
- On-demand video export with persistent storage
- Authentication (token reveal/copy/regenerate from the UI)
- Systemd service for reliable background operation
- All times in UTC (noted in footer)
- Organized video storage by camera (videos/front/, videos/rear/, etc.)

## Recording Format & On-Demand Conversion

**Recording Process:**
- Video is recorded as **MJPEG** files (.mjpeg) with frame-level atomicity, ensuring data integrity even if power fails mid-recording
- MJPEG is a sequence of JPEG-compressed frames with configurable quality (see `mjpeg_quality` config)

**On-Demand MP4 Generation:**
- Use the dashboard "Generate Video" section to create downloadable MP4 files on-demand
- Select either "Lifetime" (all footage) or custom date range
- MP4 files are re-encoded using MPEG-4 codec at high quality (q=2)
- Generated exports are saved to disk (max 1 export stored at a time)
- Previous export is automatically replaced when generating a new one
- Export can be downloaded multiple times or deleted manually

**Storage Accounting:**
- Only MJPEG files count toward the storage cap
- Exports stored in `.export` directory don't count toward storage cap
- When the cap is exceeded, oldest MJPEG files are deleted automatically

## API Endpoints

All endpoints except `/health` require `Authorization: Bearer <token>` header (or `?token=<token>` query param for stream/download URLs that are opened in a browser).

```bash
GET  /health                       # No auth required
GET  /api/status                   # System status + storage + video list
GET  /api/videos                   # List recorded segments
GET  /api/video/download           # Download a segment (?camera=&file=)
POST /api/video/remux               # Remux a segment to MP4 (?camera=&file=)
GET  /api/video/remux/status       # Remux progress
GET  /api/video/remux/download     # Download the remuxed MP4
GET  /api/video/latest             # Latest video info
POST /api/videos/generate-export   # Generate an MP4 export (?start=&end= ISO-8601)
GET  /api/videos/export-status     # Export progress
GET  /api/videos/download-export   # Download the current export
DELETE /api/videos/delete-export   # Delete the current export
GET  /api/stream/frame             # Latest frame as JPEG (?camera=)
GET  /api/stream/mjpeg             # MJPEG stream (?camera=)
GET  /api/config                    # Current configuration
POST /api/config/update            # Update global settings (storage/segment/port; cameras)
GET  /api/cameras                   # Configured cameras
GET  /api/cameras/discover          # Scan for cameras + supported formats (USB/UVC + CSI)
POST /api/cameras/add               # Add a camera
PUT  /api/cameras/update            # Update a camera (?id=)
DELETE /api/cameras/delete          # Delete a camera (?id=)
GET  /api/auth/token                # Current auth token
POST /api/auth/regenerate-token     # Mint a new token (applies immediately)
```

## Configuration

Config stored at `~/.config/dash-of-pi/config.json` (or `/etc/dash-of-pi/config.json` under the systemd service). See `config.json.example` for a complete multi-camera example.

**Prefer the dashboard.** The **Settings** page edits cameras, storage cap, segment length, port, and the auth token in the browser and persists to this file. Storage cap, segment length, and camera changes apply live; changing the HTTP port requires a service restart.

> Using `sudo ./scripts/install.sh`? The generated systemd service keeps its config at `/etc/dash-of-pi/config.json` and writes recordings to `/var/lib/dash-of-pi/videos`.

### Multi-Camera Configuration

```json
{
  "port": 8080,
  "storage_cap_gb": 20,
  "segment_length_s": 60,
  "cameras": [
    {
      "id": "front",
      "name": "Front Camera",
      "device": "/dev/video0",
      "rotation": 0,
      "res_width": 1920,
      "res_height": 1080,
      "bitrate": 2048,
      "fps": 30,
      "mjpeg_quality": 5,
      "embed_timestamp": true,
      "enabled": true
    },
    {
      "id": "rear",
      "name": "Rear Camera",
      "device": "/dev/video1",
      "rotation": 180,
      "res_width": 1280,
      "res_height": 720,
      "bitrate": 1024,
      "fps": 24,
      "mjpeg_quality": 8,
      "embed_timestamp": true,
      "enabled": true
    }
  ]
}
```

### Configuration Options

**Global Settings:**
- `port`: HTTP server port (default: 8080)
- `storage_cap_gb`: Max disk usage before deleting oldest videos
- `segment_length_s`: Recording segment duration in seconds

**Per-Camera Settings:**
- `id`: Unique camera identifier (used in URLs and directory structure)
- `name`: User-friendly camera name
- `device`: Video input device (e.g., `/dev/video0`, `/dev/video1`)
- `rotation`: Camera rotation in degrees (0, 90, 180, 270)
 - Note: 90 deg and 270 deg are only supported on USB cameras (not Pi CSI cameras)
- `res_width` / `res_height`: Video resolution
- `bitrate`: Video bitrate in kbps (unused by MJPEG capture; kept for config compatibility)
- `fps`: Recording framerate
- `mjpeg_quality`: MJPEG quality (2-31, lower = better quality)
 - Recommended: 5-8 (balanced), 2-4 (high quality), 10+ (low quality/storage)
- `embed_timestamp`: Overlay timestamp (format: YYYY-MM-DD HH:MM:SS UTC)
 - Note: Only supported on USB cameras (not Pi CSI cameras)
- `enabled`: Whether this camera is active

### Legacy Configuration

Old single-camera configs are automatically migrated to the new format on startup.

### Accessing Camera Streams

- Live frame: `/api/stream/frame?camera=front`
- MJPEG stream: `/api/stream/mjpeg?camera=rear`
- If no camera parameter is provided, the first camera is used

Camera, storage-cap, and segment-length changes apply live from the Settings page. Only a port change requires a service restart.

## Dashboard

The UI has two tabs:

**Dashboard** — live stream, storage/segments/uptime stats, the export tool, and the recorded-segments list (download MJPEG or remux to MP4 per segment; toggle local/UTC times).

**Settings**
- **Cameras** — add/remove/edit cameras. The add/edit form runs **camera auto-discovery**: it lists detected cameras (USB/UVC or CSI), auto-fills the device path, and populates the resolution/FPS dropdowns from the camera's *actual supported formats* — so you can't select an unsupported combo. CSI cameras hide the options they don't support (90°/270° rotation, timestamp overlay) and use libcamera defaults.
- **General** — storage cap (GB), segment length (s), HTTP port. Storage cap and segment length apply live; a port change requires a service restart (shown in the UI).
- **Auth Token** — reveal, copy, and regenerate the bearer token. Regeneration takes effect immediately.

- **Export Video:**
 - **Lifetime:** creates an MP4 from all stored MJPEG files
 - **Custom Date Range:** creates an MP4 from MJPEG files within the given dates (input times are in UTC)
 - MP4 is re-encoded during generation and saved for download (only one export at a time; a new export replaces the previous)
 - All times displayed in UTC (noted in footer)

## Troubleshooting

**Service won't start:**
```bash
journalctl -u dash-of-pi -f
```

**No video being recorded:**
```bash
# Should have .mjpeg files:
ls ~/.local/state/dash-of-pi/videos/
# USB webcams are captured with ffmpeg via V4L2; Pi CSI cameras use rpicam-vid (libcamera).
# The app auto-detects the type from the V4L2 driver (uvcvideo = USB).
# Use the Settings → Add Camera discovery to see detected cameras and their supported
# resolutions/FPS, or check manually:
v4l2-ctl --device=/dev/video0 --list-formats-ext   # USB/UVC camera
rpicam-still --list-cameras                       # Pi CSI camera (libcamera)
# Still doesn't work? Plug out the camera, reboot, plug it back in, and reboot again. Why does it work? No idea.
```

**Go build killed on low-RAM Pis:**
`./scripts/install.sh` automatically creates a temporary 1 GB swap file at `/var/swap-dash-of-pi-build` whenever the system reports less than ~900 MB of RAM so the Go compiler can finish. The swap file is removed after the build completes.

If you're running `go build` manually on a constrained device, enable swap before building and clean it up afterwards:
```bash
sudo fallocate -l 1G /var/swap-dash-of-pi-build
sudo chmod 600 /var/swap-dash-of-pi-build
sudo mkswap /var/swap-dash-of-pi-build
sudo swapon /var/swap-dash-of-pi-build
go build ./...
sudo swapoff /var/swap-dash-of-pi-build
sudo rm /var/swap-dash-of-pi-build
```

**Dashboard shows "Failed to connect":**
```bash
curl http://localhost:8080/health
cat /etc/dash-of-pi/config.json | grep auth_token   # or ~/.config/dash-of-pi/config.json
# The auth token is also shown in the Settings → Auth Token tab (reveal) or the server logs.
```

**MP4 generation is slow:**
- MP4 generation uses high quality (q=2) and may take several minutes for large date ranges
- Check server logs for encoding progress (shows MB/s and frames processed)
- Encoding speed depends on CPU - typically processes at 5-7x realtime speed

## Hardware Requirements

- Raspberry Pi Zero 2W, Pi 4/5, or Compute Module 4/5
- Pi CSI camera (Sony IMX219 / Pi Camera Module 2 or Arducam clone) **or** any UVC USB webcam
- microSD 16GB+
- 5V/2.5A power


## Raspberry Pi Camera Setup (IMX219)

Dash of Pi automatically detects CSI cameras and uses the native libcamera stack (rpicam-vid) for capture - no configuration needed.

**Setup:**

`scripts/install.sh` handles camera detection and overlay configuration. If you have a CSI camera:

1. **Plug the camera in while the Pi is off.** Follow the [official guide](https://www.raspberrypi.com/documentation/accessories/camera.html) for the ribbon cable and, on Pi 5/CM5, the 22-pin adapter.
2. **Run the installer:** `sudo ./scripts/install.sh`
3. **Reboot when prompted.**
4. **Verify** with rpicam before launching Dash of Pi:
   ```bash
   rpicam-still --list-cameras
   rpicam-hello -t 2000
   ```
5. **Start Dash of Pi.** It will auto-detect the camera and begin recording.

> Different sensor? Follow the [official docs](https://www.raspberrypi.com/documentation/accessories/camera.html) and open a PR with what worked - we'll gladly add it.
## Deployment

Run `sudo ./scripts/install.sh` for a full automated setup on Raspberry Pi (or any Linux system). This installs dependencies, compiles the binary, creates a systemd service, and configures camera support. The installer handles everything needed for production deployment.
