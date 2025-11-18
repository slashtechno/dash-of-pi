# Dash of Pi

Headless dash cam for Raspberry Pi Zero 2W (but usable on other machines). Continuous video recording with web dashboard for downloads and live streaming. Most importantly, **footage is recoverable even if power loss occurs mid-recording**.

## Quick Start
**Pi Setup:**
```bash
# ssh pi@raspberrypi.local
git clone https://github.com/slashtechno/dash-of-pi && cd dash-of-pi
docker-compose up -d
# Open http://raspberrypi.local:8080 and use the auth token printed in the terminal
```

**Local Testing:**
```bash
git clone https://github.com/slashtechno/dash-of-pi && cd dash-of-pi
go run .
# Open http://localhost:8080  and use the auth token printed in the terminal
```


## Features

- **Multi-camera support** - Record from multiple cameras simultaneously
- **Per-camera configuration** - Independent settings for resolution, rotation, bitrate, quality
- Records continuous video in segments with rolling storage
- Web dashboard for live streaming and downloading videos
- Timestamps embedded on video (optional, per-camera)
- On-demand video export with persistent storage
- Authentication
- Docker and Systemd options
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

All endpoints except `/health` require `Authorization: Bearer <token>` header.
<!-- TODO: outdated -->
```bash
GET /health              # No auth required
GET /api/status          # System status
GET /api/videos          # List videos
GET /api/video/download  # Download video
GET /api/video/stream    # Stream video
GET /api/config          # Configuration
GET /api/auth/token      # Auth token
```

## Configuration

Config stored at `~/.config/dash-of-pi/config.json`. See `config.json.example` for a complete multi-camera example.

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
- `res_width` / `res_height`: Video resolution
- `bitrate`: Video bitrate in kbps
- `fps`: Recording framerate
- `mjpeg_quality`: MJPEG quality (2-31, lower = better quality)
  - Recommended: 5-8 (balanced), 2-4 (high quality), 10+ (low quality/storage)
- `embed_timestamp`: Overlay timestamp (format: YYYY-MM-DD HH:MM:SS UTC)
- `enabled`: Whether this camera is active

### Legacy Configuration

Old single-camera configs are automatically migrated to the new format on startup.

### Accessing Camera Streams

- Live frame: `/api/stream/frame?camera=front`
- MJPEG stream: `/api/stream/mjpeg?camera=rear`
- If no camera parameter is provided, the first camera is used

Restart service to apply config changes.

## Dashboard

- **Generate Video Section:**
  - **Lifetime:** Creates MP4 from all stored MJPEG files
  - **Custom Date Range:** Creates MP4 from MJPEG files within specified dates (input times are in UTC)
  - MP4 file is re-encoded during generation and saved for download
  - Only one export is kept at a time (replaces previous export)
  - Export can be downloaded multiple times until deleted or replaced
  - All times displayed in UTC (noted in footer)

## Troubleshooting

**Service won't start:**
```bash
# Docker
docker compose logs dash-of-pi

# Systemd
journalctl -u dash-of-pi -f
```

**No video being recorded:**
```bash
ls ~/.local/state/dash-of-pi/videos/
# Should have .mjpeg files
```

**Go build killed on low-RAM Pis:**
`./scripts/install.sh` automatically creates a temporary 1 GB swap file at `/var/swap-dash-of-pi-build` whenever the system reports less than ~900 MB of RAM so the Go compiler can finish. The swap file is removed after the build completes.

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
cat ~/.config/dash-of-pi/config.json | grep auth_token
```

**MP4 generation is slow:**
- MP4 generation uses high quality (q=2) and may take several minutes for large date ranges
- Check server logs for encoding progress (shows MB/s and frames processed)
- Encoding speed depends on CPU - typically processes at 5-7x realtime speed

## Hardware Requirements

- Raspberry Pi Zero 2W, Pi 4/5, or Compute Module 4/5
- Sony IMX219 camera (Pi Camera Module 2 or Arducam IMX219 clone)
- microSD 16GB+
- 5V/2.5A power

## Raspberry Pi Camera Setup (IMX219)

`scripts/install.sh` grabs the official `rpicam-apps` tools (the Bookworm rename of `libcamera-*`) and handles the camera overlay for you. Here is the short version:

1. **Plug the camera in while the Pi is off.** Follow the [official guide](https://www.raspberrypi.com/documentation/accessories/camera.html) for the ribbon cable and, on Pi 5/CM5, the 22-pin adapter.
2. **Run the installer.** `sudo ./scripts/install.sh` now defaults to Raspberry Pi’s auto-detect, so no extra overlay work is needed.
  Want to pin the IMX219 overlay? Run `DASH_OF_PI_CAMERA_SENSOR=imx219 sudo ./scripts/install.sh` (add `,cam0` yourself when using CAM0 on Pi 5/CM5).
3. **Reboot when prompted** so the overlay takes effect.
4. **Spot-check with rpicam** before launching Dash of Pi:
   ```bash
   rpicam-still --list-cameras
   rpicam-hello -t 2000
   ```
   You should see a Sony IMX219 entry and a preview window.

> Different sensor? Follow the [official docs](https://www.raspberrypi.com/documentation/accessories/camera.html) and open a PR with what worked—we’ll gladly add it.

## Deployment

See docker-compose.yml or scripts/install.sh for systemd setup.
