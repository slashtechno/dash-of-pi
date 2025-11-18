#!/bin/bash
set -e

# Temporary swap provided to keep Go builds alive on memory-constrained Pis
BUILD_SWAP_FILE="/var/swap-dash-of-pi-build"
BUILD_SWAP_SIZE_MB=1024
SWAP_CREATED=false

cleanup_build_swap() {
    if [ "$SWAP_CREATED" = true ] && [ -f "$BUILD_SWAP_FILE" ]; then
        swapoff "$BUILD_SWAP_FILE" >/dev/null 2>&1 || true
        rm -f "$BUILD_SWAP_FILE"
        SWAP_CREATED=false
        echo "Disabled temporary build swap"
    fi
}

trap cleanup_build_swap EXIT

maybe_enable_build_swap() {
    local mem_total_kb
    mem_total_kb=$(awk '/MemTotal/ {print $2}' /proc/meminfo)
    local swap_threshold_kb=920000
    if [ "$mem_total_kb" -lt "$swap_threshold_kb" ]; then
        if [ -f "$BUILD_SWAP_FILE" ]; then
            rm -f "$BUILD_SWAP_FILE"
        fi
        echo "Low-memory system detected (${mem_total_kb} kB); enabling temporary swap for Go build"
        if command -v fallocate >/dev/null 2>&1; then
            fallocate -l "${BUILD_SWAP_SIZE_MB}M" "$BUILD_SWAP_FILE"
        else
            dd if=/dev/zero of="$BUILD_SWAP_FILE" bs=1M count="$BUILD_SWAP_SIZE_MB" status=none
        fi
        chmod 600 "$BUILD_SWAP_FILE"
        mkswap "$BUILD_SWAP_FILE" >/dev/null
        swapon "$BUILD_SWAP_FILE"
        SWAP_CREATED=true
        echo "Temporary ${BUILD_SWAP_SIZE_MB}MB swap enabled"
    fi
}

echo "=== Dash of Pi Installation ==="
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "ERROR: This script must be run as root"
    echo "Use: sudo ./scripts/install.sh"
    exit 1
fi

PI_MODEL=$(tr -d '\0' </proc/device-tree/model 2>/dev/null || true)
IS_PI=false
if echo "$PI_MODEL" | grep -qi "raspberry pi"; then
    IS_PI=true
    echo "Detected Raspberry Pi model: $PI_MODEL"
else
    echo "WARNING: This doesn't appear to be a Raspberry Pi"
    read -p "Continue anyway? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        exit 1
    fi
fi

CAMERA_MODE="${DASH_OF_PI_CAMERA_SENSOR:-auto}"
PIN_IMX219=true
case "$CAMERA_MODE" in
    auto|AUTO|Auto)
        CAMERA_MODE="auto"
        PIN_IMX219=false
        ;;
    imx219|IMX219|"")
        CAMERA_MODE="imx219"
        PIN_IMX219=true
        ;;
    *)
        echo "WARNING: Unknown DASH_OF_PI_CAMERA_SENSOR value '$CAMERA_MODE'. Falling back to auto-detect."
        CAMERA_MODE="auto"
        PIN_IMX219=false
        ;;
esac

CAMERA_OVERLAY_LINE="dtoverlay=imx219"
CAMERA_OVERLAY_KEY="imx219"

CONFIG_FILE=""
if [ "$IS_PI" = true ]; then
    if [ -f /boot/firmware/config.txt ]; then
        CONFIG_FILE=/boot/firmware/config.txt
    elif [ -f /boot/config.txt ]; then
        CONFIG_FILE=/boot/config.txt
    fi
fi

set_config_value() {
    local key="$1"
    local value="$2"

    if grep -Eq "^\s*${key}\s*=" "$CONFIG_FILE"; then
        sed -i "s|^\s*${key}\s*=.*|${key}=${value}|" "$CONFIG_FILE"
    else
        printf "\n%s=%s\n" "$key" "$value" >> "$CONFIG_FILE"
    fi
}

ensure_camera_overlay() {
    local overlay_key="$1"
    local overlay_line="$2"

    if grep -Eq "^\s*dtoverlay=${overlay_key}" "$CONFIG_FILE"; then
        sed -i "s|^\s*dtoverlay=${overlay_key}.*|${overlay_line}|" "$CONFIG_FILE"
    else
        printf "\n# Added by dash-of-pi installer\n%s\n" "$overlay_line" >> "$CONFIG_FILE"
    fi
}

echo "[1/9] Installing dependencies..."
apt-get update
apt-get install -y \
    build-essential \
    git \
    ffmpeg \
    wget

if [ "$IS_PI" = true ]; then
    echo "Installing Raspberry Pi camera utilities..."
    if ! apt-get install -y rpicam-apps v4l-utils; then
        echo "rpicam-apps not available, falling back to libcamera-apps"
        apt-get install -y libcamera-apps v4l-utils
    fi
fi

echo "[2/9] Configuring Raspberry Pi camera support..."
if [ "$IS_PI" = true ] && [ -n "$CONFIG_FILE" ]; then
    if [ "$PIN_IMX219" = true ]; then
        set_config_value camera_auto_detect 0
        ensure_camera_overlay "$CAMERA_OVERLAY_KEY" "$CAMERA_OVERLAY_LINE"
        echo "Pinned $CAMERA_MODE overlay in $CONFIG_FILE (${CAMERA_OVERLAY_LINE})."
        echo "A reboot is required for camera changes to take effect."
    else
        echo "Auto-detect enabled (DASH_OF_PI_CAMERA_SENSOR=auto), leaving config.txt untouched."
    fi
else
    echo "Skipping camera configuration (not running on Raspberry Pi or config.txt not found)."
fi

# Install Go 1.24.9 to /usr/local (unless there's a go version 1.20+ already) 
# Check if Go is installed and get its version
GO_INSTALLED=false
if command -v go &> /dev/null; then
    GO_VERSION_OUTPUT=$(go version 2>/dev/null || echo "")
    if echo "$GO_VERSION_OUTPUT" | grep -q "go1\.[2-9][0-9]\|go[2-9]\."; then
        GO_INSTALLED=true
        echo "Go 1.20+ already installed: $GO_VERSION_OUTPUT"
    fi
elif [ -x /usr/local/go/bin/go ]; then
    GO_VERSION_OUTPUT=$(/usr/local/go/bin/go version 2>/dev/null || echo "")
    if echo "$GO_VERSION_OUTPUT" | grep -q "go1\.[2-9][0-9]\|go[2-9]\."; then
        GO_INSTALLED=true
        export PATH=$PATH:/usr/local/go/bin
        echo "Go 1.20+ already installed: $GO_VERSION_OUTPUT"
    fi
fi

if [ "$GO_INSTALLED" = false ]; then
    echo "Installing Go 1.24.9..."
    GO_VERSION="1.24.9"
    GO_ARCH="linux-arm64"
    
    # Download Go tarball
    wget -q "https://go.dev/dl/go${GO_VERSION}.${GO_ARCH}.tar.gz"
    
    # Remove any previous Go installation and extract the archive into /usr/local
    # This is done as a single command as recommended by Go's installation docs
    rm -rf /usr/local/go && tar -C /usr/local -xzf "go${GO_VERSION}.${GO_ARCH}.tar.gz"
    
    # Clean up tarball
    rm "go${GO_VERSION}.${GO_ARCH}.tar.gz"
    
    # Add /usr/local/go/bin to PATH for this session
    export PATH=$PATH:/usr/local/go/bin
    
    # Add to /etc/profile for system-wide installation (persists across logins)
    if ! grep -q "/usr/local/go/bin" /etc/profile; then
        echo 'export PATH=$PATH:/usr/local/go/bin' >> /etc/profile
    fi
    
    echo "Go installed to /usr/local/go"
fi

# Verify Go installation
go version

echo "[3/9] Creating dash-of-pi user..."
if ! id "dash-of-pi" &>/dev/null; then
    # Create a system user (-r flag) with home directory at /var/lib/dash-of-pi
    # System users automatically get their home set to /var/lib/<username> by default
    useradd -r -s /bin/false dash-of-pi
    echo "Created user: dash-of-pi (home: /var/lib/dash-of-pi)"
else
    echo "User dash-of-pi already exists"
fi

# Add dash-of-pi user to video group for camera access
if ! groups dash-of-pi | grep -q '\bvideo\b'; then
    usermod -a -G video dash-of-pi
    echo "Added dash-of-pi to video group for camera access"
else
    echo "dash-of-pi already in video group"
fi

echo "[4/9] Building application..."
maybe_enable_build_swap
cd "$(dirname "$0")/.."
go mod download
go build -o dash-of-pi .
cleanup_build_swap

echo "[5/9] Stopping existing service (if running)..."
if systemctl is-active --quiet dash-of-pi; then
    systemctl stop dash-of-pi
    echo "Stopped running service"
fi

echo "[6/9] Installing files..."
mkdir -p /var/lib/dash-of-pi/videos
mkdir -p /etc/dash-of-pi
mkdir -p /var/lib/dash-of-pi/web

cp dash-of-pi /usr/local/bin/
chmod 755 /usr/local/bin/dash-of-pi
cp scripts/dash-of-pi.service /etc/systemd/system/

# Copy web directory to working directory
cp -r web/* /var/lib/dash-of-pi/web/

# Set permissions
chown -R dash-of-pi:dash-of-pi /var/lib/dash-of-pi
chown dash-of-pi:dash-of-pi /etc/dash-of-pi

chmod 750 /var/lib/dash-of-pi
chmod 750 /etc/dash-of-pi

echo "[7/9] Creating initial config..."
if [ ! -f /etc/dash-of-pi/config.json ]; then
    # Run as dash-of-pi user to generate initial config
    # HOME is set to /var/lib/dash-of-pi (matches the system user's home directory)
    # This ensures the app detects it's running as a system service and uses /var/lib/dash-of-pi/videos
    cd /var/lib/dash-of-pi
    sudo -u dash-of-pi HOME=/var/lib/dash-of-pi /usr/local/bin/dash-of-pi -config /etc/dash-of-pi/config.json &
    sleep 2
    pkill -f "dash-of-pi" || true
    
    # Update the generated config to use the correct video directory
    if [ -f /etc/dash-of-pi/config.json ]; then
        # Use sed to replace "./videos" with "/var/lib/dash-of-pi/videos"
        sed -i 's|"./videos"|"/var/lib/dash-of-pi/videos"|g' /etc/dash-of-pi/config.json
        chown dash-of-pi:dash-of-pi /etc/dash-of-pi/config.json
        chmod 640 /etc/dash-of-pi/config.json
    fi
    
    echo "Config created at /etc/dash-of-pi/config.json"
else
    echo "Config already exists at /etc/dash-of-pi/config.json"
fi

echo "[8/9] Enabling systemd service..."
systemctl daemon-reload
systemctl enable dash-of-pi

echo "[9/9] Starting service..."
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
