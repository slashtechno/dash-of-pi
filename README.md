# Dash of Pi

Headless dash cam for Raspberry Pi Zero 2W. Continuous video recording with web dashboard for downloads and live streaming.

## Quick Start

**Local Testing:**
```bash
go build && ./pi-dashcam -v
# Open http://localhost:8080
```

**Pi Setup (SSH):**
```bash
ssh pi@raspberrypi.local
git clone <repo> && cd pi-dashcam
docker-compose up -d
# Open http://raspberrypi.local:8080
```

## Features

- Records continuous video in segments with rolling storage
- Web dashboard for live streaming and downloading videos
- Authentication
- Docker and Systemd options

## Recording Format & Conversion

**Recording Process:**
- Video is recorded as **MJPEG** files (.mjpeg) with frame-level atomicity, ensuring data integrity even if power fails mid-recording
- MJPEG is not easily playable in most players (suitable for streaming/processing)
- After each recording segment completes, a **background conversion** process re-muxes the MJPEG to AVI (.avi) format with proper metadata
- Conversion uses FFmpeg's `-c copy` flag (no re-encoding) for instant 1:1 fidelity with minimal CPU overhead

**Storage Accounting:**
- Both MJPEG and AVI files count toward the storage cap
- When the cap is exceeded, oldest files are deleted first (regardless of type)
- Temporary duplication during conversion is expected and will be cleaned up

**Power Loss & Recovery:**
- **If power is cut during AVI conversion:** The MJPEG source file remains intact and playable. The incomplete/missing AVI file will not be created on restart
- Click "🔄 Regenerate Missing AVIs" button on dashboard to recover orphaned MJPEG files
- The dashboard displays only AVI files by default (with a toggle to show all formats)
- Completed AVI files are always playable; partially-converted files are discarded on the next system start

## API Endpoints

All endpoints except `/health` require `Authorization: Bearer <token>` header.

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

Config stored at `~/.config/dash-of-pi/config.json`:
```json
{
  "port": 8080,
  "storage_cap_gb": 10,
  "video_bitrate": 1024,
  "video_fps": 24,
  "video_res_width": 1280,
  "video_res_height": 720,
  "segment_length_s": 60,
  "camera_device": "/dev/video0"
}
```

Key settings:
- `camera_device`: Video input device (e.g., `/dev/video0`, `/dev/video1`)
- `storage_cap_gb`: Max disk usage before deleting oldest videos
- `segment_length_s`: Recording segment duration in seconds

Restart service to apply changes.

## Dashboard

- **AVI Filter Toggle:** Shows only AVI files by default
- **Regenerate Button:** Re-encodes MJPEG files to AVI (use if power cut during conversion)
- **Merge Script:** `./scripts/merge-mjpeg.sh [dir] [output]` combines all MJPEG files

## Troubleshooting

**Service won't start:**
```bash
# Docker
docker compose logs pi-dashcam

# Systemd
journalctl -u dash-of-pi -f
```

**No video being recorded:**
```bash
ls ~/.local/state/dash-of-pi/videos/
# Should have .mjpeg files (converted to .avi in background)
```

**Dashboard shows "Failed to connect":**
```bash
curl http://localhost:8080/health
cat ~/.config/dash-of-pi/config.json | grep auth_token
```

**MJPEG files without AVI (power cut during conversion):**
- Click "🔄 Regenerate Missing AVIs" button on dashboard
- Or manually: `./scripts/merge-mjpeg.sh /path/to/videos output.mp4`

## Hardware Requirements

- Raspberry Pi Zero 2W (or compatible Pi)
- Pi Camera v2/v3
- microSD 16GB+
- 5V/2.5A power

## Deployment

See docker-compose.yml or scripts/install.sh for systemd setup.
