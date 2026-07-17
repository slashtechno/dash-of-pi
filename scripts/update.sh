#!/bin/bash
# Update an existing dash-of-pi install: pull, build, redeploy, restart.
#
# Usage (from the repo root):
#   sudo ./scripts/update.sh             # full update: pull + build + redeploy
#   sudo ./scripts/update.sh --web-only  # UI-only change: pull + copy web + restart (skips the build)
#
# One-time setup (apt deps, dash-of-pi user, systemd unit) is done by install.sh.
# This script assumes Go is already installed (install.sh puts it at /usr/local/go).

set -e

BUILD_SWAP_FILE="/var/swap-dop-build"
BUILD_SWAP_SIZE_MB=2048   # 1 GB OOM-killed the compiler on a 512 MB Pi; 2 GB is safe.
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

# Create a temporary swap file on memory-constrained Pis so the Go build finishes.
maybe_enable_build_swap() {
    local mem_total_kb
    mem_total_kb=$(awk '/MemTotal/ {print $2}' /proc/meminfo)
    local swap_threshold_kb=920000
    if [ "$mem_total_kb" -lt "$swap_threshold_kb" ]; then
        if [ -f "$BUILD_SWAP_FILE" ]; then rm -f "$BUILD_SWAP_FILE"; fi
        echo "Low-memory system (${mem_total_kb} kB); enabling temporary swap for Go build"
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

WEB_ONLY=false
if [ "${1:-}" = "--web-only" ]; then WEB_ONLY=true; fi

echo "=== Dash of Pi Update ==="

if [ "$EUID" -ne 0 ]; then
    echo "ERROR: run with sudo: sudo ./scripts/update.sh"
    exit 1
fi

# When run via sudo, perform unprivileged steps (git pull, go build) as the
# invoking user so the repo doesn't fill up with root-owned files (which would
# break a later `git pull` as the normal user).
AS_USER="${SUDO_USER:-}"

# Repo root is the parent of this script's directory.
cd "$(dirname "$0")/.."

echo "[1/5] Pulling latest..."
if [ -n "$AS_USER" ]; then sudo -u "$AS_USER" git pull --ff-only; else git pull --ff-only; fi

if [ "$WEB_ONLY" = true ]; then
    echo "[2/5] Skipping build (--web-only)"
    echo "[3/5] Stopping service..."
    systemctl stop dash-of-pi || true
    echo "[4/5] Installing web files..."
    rm -rf /var/lib/dash-of-pi/web && mkdir -p /var/lib/dash-of-pi/web
    cp -r web/* /var/lib/dash-of-pi/web/
    chown -R dash-of-pi:dash-of-pi /var/lib/dash-of-pi
else
    echo "[2/5] Building..."
    maybe_enable_build_swap
    # Resolve the Go binary. install.sh puts Go at /usr/local/go; fall back to
    # whatever's on PATH (e.g. a dev box) instead of mutating PATH.
    GO_BIN=/usr/local/go/bin/go
    [ -x "$GO_BIN" ] || GO_BIN=go
    # GOMAXPROCS=1 + GOGC=25 keep peak memory low on the Pi Zero 2W. Build as the
    # invoking user (not root) so the output binary isn't root-owned in the repo.
    if [ -n "$AS_USER" ]; then
        sudo -u "$AS_USER" env GOMAXPROCS=1 GOGC=25 "$GO_BIN" build -p 1 -o dash-of-pi .
    else
        GOMAXPROCS=1 GOGC=25 "$GO_BIN" build -p 1 -o dash-of-pi .
    fi
    cleanup_build_swap

    echo "[3/5] Stopping service..."
    systemctl stop dash-of-pi || true

    echo "[4/5] Installing binary + web..."
    cp dash-of-pi /usr/local/bin/dash-of-pi
    chmod 755 /usr/local/bin/dash-of-pi
    rm -rf /var/lib/dash-of-pi/web && mkdir -p /var/lib/dash-of-pi/web
    cp -r web/* /var/lib/dash-of-pi/web/
    chown -R dash-of-pi:dash-of-pi /var/lib/dash-of-pi
fi

# Reload the systemd unit only if it changed in this update (avoids an unneeded daemon-reload).
NEW_SERVICE=scripts/dash-of-pi.service
INSTALLED_SERVICE=/etc/systemd/system/dash-of-pi.service
if [ -f "$NEW_SERVICE" ] && [ -f "$INSTALLED_SERVICE" ] && ! cmp -s "$NEW_SERVICE" "$INSTALLED_SERVICE"; then
    cp "$NEW_SERVICE" "$INSTALLED_SERVICE"
    systemctl daemon-reload
    echo "  systemd unit updated"
fi

echo "[5/5] Starting service..."
systemctl start dash-of-pi
sleep 3

echo
echo "=== Update Complete ==="
echo "Status: $(systemctl is-active dash-of-pi)"
echo "View logs: journalctl -u dash-of-pi -f"
if [ -f /etc/dash-of-pi/config.json ]; then
    TOKEN=$(grep -o '"auth_token"[[:space:]]*:[[:space:]]*"[^"]*"' /etc/dash-of-pi/config.json | sed -E 's/.*"auth_token"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/')
    IP=$(hostname -I | awk '{print $1}')
    if [ -n "$TOKEN" ]; then
        echo "Dashboard: http://${IP}:8080/?token=${TOKEN}"
    fi
fi
echo