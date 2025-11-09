#!/bin/bash
set -e

echo "=== Pi DashCam Installation ==="
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "ERROR: This script must be run as root"
    echo "Use: sudo ./scripts/install.sh"
    exit 1
fi

# Detect Pi Zero 2W
if ! grep -q "Pi Zero 2" /proc/device-tree/model 2>/dev/null; then
    echo "WARNING: This doesn't appear to be a Raspberry Pi Zero 2W"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

echo "[1/7] Installing dependencies..."
apt-get update
apt-get install -y \
    build-essential \
    git \
    libcamera0 \
    libcamera-tools \
    libraspberrypi-bin \
    libraspberrypi0 \
    libraspberrypi-dev \
    golang-1.21

# Add Go to PATH if not already there
export PATH=/usr/lib/go-1.21/bin:$PATH

echo "[2/7] Creating pi-dashcam user..."
if ! id "pi-dashcam" &>/dev/null; then
    useradd -r -s /bin/false pi-dashcam
    echo "Created user: pi-dashcam"
else
    echo "User pi-dashcam already exists"
fi

echo "[3/7] Building application..."
cd "$(dirname "$0")/.."
/usr/lib/go-1.21/bin/go mod download
/usr/lib/go-1.21/bin/go build -o pi-dashcam .

echo "[4/7] Installing files..."
mkdir -p /opt/pi-dashcam/bin
mkdir -p /var/lib/pi-dashcam/videos
mkdir -p /etc/pi-dashcam

cp pi-dashcam /opt/pi-dashcam/bin/
chmod 755 /opt/pi-dashcam/bin/pi-dashcam
cp scripts/pi-dashcam.service /etc/systemd/system/

# Set permissions
chown -R pi-dashcam:pi-dashcam /var/lib/pi-dashcam
chown pi-dashcam:pi-dashcam /etc/pi-dashcam
chown pi-dashcam:pi-dashcam /opt/pi-dashcam

chmod 750 /var/lib/pi-dashcam
chmod 750 /etc/pi-dashcam

echo "[5/7] Creating initial config..."
if [ ! -f /etc/pi-dashcam/config.json ]; then
    sudo -u pi-dashcam /opt/pi-dashcam/bin/pi-dashcam -config /etc/pi-dashcam/config.json -v &
    sleep 2
    pkill -f "pi-dashcam" || true
    echo "Config created at /etc/pi-dashcam/config.json"
else
    echo "Config already exists at /etc/pi-dashcam/config.json"
fi

echo "[6/7] Enabling systemd service..."
systemctl daemon-reload
systemctl enable pi-dashcam

echo "[7/7] Starting service..."
systemctl start pi-dashcam

echo
echo "=== Installation Complete ==="
echo
echo "Status: $(systemctl is-active pi-dashcam)"
echo "View logs: journalctl -u pi-dashcam -f"
echo "Config file: /etc/pi-dashcam/config.json"
echo "Videos directory: /var/lib/pi-dashcam/videos"
echo
echo "Access the dashboard at: http://$(hostname -I | awk '{print $1}'):8080"
echo
