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

## Recording Format & On-Demand Conversion

**Recording Process:**
- Video is recorded as **MJPEG** files (.mjpeg) with frame-level atomicity, ensuring data integrity even if power fails mid-recording
- MJPEG is a sequence of JPEG-compressed frames with configurable quality (see `mjpeg_quality` config)

**On-Demand AVI Generation:**
- Use the dashboard "Generate Video" section to create downloadable AVI files on-demand
- Choose either **Lifetime** (all MJPEG files) or **Custom Date Range**
- AVI files are re-encoded using MPEG-4 codec at configurable bitrate (see `avi_bitrate` config)
- Generated AVI files are streamed directly to the user and not stored on disk

**Storage Accounting:**
- Only MJPEG files count toward the storage cap
- When the cap is exceeded, oldest MJPEG files are deleted automatically

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
  "camera_device": "/dev/video0",
  "mjpeg_quality": 5,
  "avi_bitrate": 1024
}
```

Key settings:
- `camera_device`: Video input device (e.g., `/dev/video0`, `/dev/video1`)
- `storage_cap_gb`: Max disk usage before deleting oldest videos
- `segment_length_s`: Recording segment duration in seconds
- `mjpeg_quality`: MJPEG recording quality (1-10, default 5; lower = higher quality, higher storage)
  - Recommended: 5 (balanced), 3-4 (high quality for important footage), 7-8 (low quality for long-term storage)
- `avi_bitrate`: Bitrate for on-demand AVI generation in kbps (default 1024)
  - Recommended: 1024 (good balance), 512 (smaller files for slow connections), 2000-4000 (high quality)

Restart service to apply changes.

## Dashboard

- **Generate Video Section:**
  - **Lifetime:** Creates AVI from all stored MJPEG files
  - **Custom Date Range:** Creates AVI from MJPEG files within specified dates
  - AVI file is re-encoded during generation and streamed directly (not stored)

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
# Should have .mjpeg files
```

**Dashboard shows "Failed to connect":**
```bash
curl http://localhost:8080/health
cat ~/.config/dash-of-pi/config.json | grep auth_token
```

**AVI generation is slow:**
- Reduce `avi_bitrate` in config (lower bitrate = faster re-encoding)
- Example: Set `"avi_bitrate": 512` for 2x faster generation with smaller files

## Hardware Requirements

- Raspberry Pi Zero 2W (or compatible Pi)
- Pi Camera v2/v3
- microSD 16GB+
- 5V/2.5A power

## Deployment

See docker-compose.yml or scripts/install.sh for systemd setup.
