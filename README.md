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

- Records continuous video in segments with rolling storage
- Web dashboard for live streaming and downloading videos
- Timestamps embedded on video (optional)
- On-demand video export with persistent storage
- Authentication
- Docker and Systemd options
- All times in UTC (noted in footer)

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
  "mjpeg_quality": 8,
  "embed_timestamp": false
}
```

Key settings:
- `camera_device`: Video input device (e.g., `/dev/video0`, `/dev/video1`)
- `storage_cap_gb`: Max disk usage before deleting oldest videos
- `segment_length_s`: Recording segment duration in seconds
- `mjpeg_quality`: MJPEG recording quality (2-31, default 8; lower = higher quality, higher storage)
  - Recommended: 8 (balanced), 3-4 (high quality), 10+ (low quality for long-term storage)
- `video_fps`: Recording framerate (default 24, can be increased to 30)
- `video_bitrate`: Not used for MP4 export (uses quality setting instead)
- `embed_timestamp`: Overlay timestamp on video (format: YYYY-MM-DD HH:MM:SS (UTC), positioned at top-left)

Restart service to apply changes.

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

- Raspberry Pi Zero 2W (or compatible Pi)
- Pi Camera v2/v3
- microSD 16GB+
- 5V/2.5A power

## Deployment

See docker-compose.yml or scripts/install.sh for systemd setup.
