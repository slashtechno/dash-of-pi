#!/bin/bash
set -e

echo "=== Pi DashCam Uninstallation ==="
echo

# Check if running as root
if [ "$EUID" -ne 0 ]; then 
    echo "ERROR: This script must be run as root"
    echo "Use: sudo ./scripts/uninstall.sh"
    exit 1
fi

echo "WARNING: This will stop the service and remove installed files."
read -p "Continue? (yes/no) " -r
if [[ ! $REPLY == "yes" ]]; then
    echo "Aborted"
    exit 0
fi

echo "[1/4] Stopping service..."
systemctl stop pi-dashcam 2>/dev/null || true
systemctl disable pi-dashcam 2>/dev/null || true

echo "[2/4] Removing service file..."
rm -f /etc/systemd/system/pi-dashcam.service
systemctl daemon-reload

echo "[3/4] Removing installed files..."
rm -rf /opt/pi-dashcam

echo "[4/4] Removing user..."
userdel pi-dashcam 2>/dev/null || true

echo
echo "=== Uninstallation Complete ==="
echo
echo "Config and video data preserved at:"
echo "  /etc/pi-dashcam/config.json"
echo "  /var/lib/pi-dashcam/videos/"
echo
echo "To remove those as well, run:"
echo "  sudo rm -rf /etc/pi-dashcam /var/lib/pi-dashcam"
echo
