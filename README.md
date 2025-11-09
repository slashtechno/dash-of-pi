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

- ✅ Continuous recording with configurable quality & segments
- ✅ Rolling storage (auto-delete old videos)
- ✅ Web dashboard with live stream & downloads
- ✅ REST API with 8 endpoints
- ✅ Token authentication
- ✅ Docker or systemd deployment
- ✅ XDG Base Directory compliance (config in `~/.config`, videos in `~/.local/state`)
- ✅ Minimal resource usage (30-50MB RAM, 15-25% CPU)

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
  "segment_length_s": 60
}
```

Restart service to apply changes.

## Troubleshooting

**Service won't start:**
```bash
# Docker
docker-compose logs pi-dashcam

# Systemd
journalctl -u dash-of-pi -f
```

**No video being recorded:**
```bash
ls ~/.local/state/dash-of-pi/videos/
# Should have .mp4 files
```

**Dashboard shows "Failed to connect":**
```bash
curl http://localhost:8080/health
cat ~/.config/dash-of-pi/config.json | grep auth_token
```

## Hardware Requirements

- Raspberry Pi Zero 2W (or compatible Pi)
- Pi Camera v2/v3
- microSD 16GB+
- 5V/2.5A power

## Deployment

See docker-compose.yml or scripts/install.sh for systemd setup.
