#!/bin/bash
set -e

echo "=== Dash of Pi Installation ==="
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
    ffmpeg \
    wget

# Install Go 1.24.9 using the golang installation script (unless there's a go version 1.20+ already) 
if ! command -v go &> /dev/null || ! go version | grep -q "go1\.[2-9][0-9]\|go[2-9]\."; then
    echo "Installing Go 1.24.9..."
    wget -q -O - https://raw.githubusercontent.com/canha/golang-tools-install-script/master/goinstall.sh | bash -s -- --version 1.24.9
    export PATH=$HOME/.go/bin:$PATH
    export GOROOT=$HOME/.go
else
    echo "Go 1.20+ already installed"
fi

# Verify Go installation
go version

echo "[2/7] Creating dash-of-pi user..."
if ! id "dash-of-pi" &>/dev/null; then
    useradd -r -s /bin/false dash-of-pi
    echo "Created user: dash-of-pi"
else
    echo "User dash-of-pi already exists"
fi

echo "[3/7] Building application..."
cd "$(dirname "$0")/.."
go mod download
go build -o dash-of-pi .

echo "[4/7] Installing files..."
mkdir -p /var/lib/dash-of-pi/videos
mkdir -p /etc/dash-of-pi

cp dash-of-pi /usr/local/bin/
chmod 755 /usr/local/bin/dash-of-pi
cp scripts/dash-of-pi.service /etc/systemd/system/

# Set permissions
chown -R dash-of-pi:dash-of-pi /var/lib/dash-of-pi
chown dash-of-pi:dash-of-pi /etc/dash-of-pi

chmod 750 /var/lib/dash-of-pi
chmod 750 /etc/dash-of-pi

echo "[5/7] Creating initial config..."
if [ ! -f /etc/dash-of-pi/config.json ]; then
    sudo -u dash-of-pi /usr/local/bin/dash-of-pi -config /etc/dash-of-pi/config.json -v &
    sleep 2
    pkill -f "dash-of-pi" || true
    echo "Config created at /etc/dash-of-pi/config.json"
else
    echo "Config already exists at /etc/dash-of-pi/config.json"
fi

echo "[6/7] Enabling systemd service..."
systemctl daemon-reload
systemctl enable dash-of-pi

echo "[7/7] Starting service..."
systemctl start dash-of-pi

echo
echo "=== Installation Complete ==="
echo
echo "Status: $(systemctl is-active dash-of-pi)"
echo "View logs: journalctl -u dash-of-pi -f"
echo "Config file: /etc/dash-of-pi/config.json"
echo "Videos directory: /var/lib/dash-of-pi/videos"
echo
echo "Access the dashboard at: http://$(hostname -I | awk '{print $1}'):8080"
echo
