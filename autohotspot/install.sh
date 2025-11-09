#!/bin/bash
set -e

echo "--- Starting AutoHotspot Installation ---"

# Configuration
CONFIG_SRC="autohotspot.json"
CONFIG_DEST="/boot/autohotspot.json"
SWITCH_SCRIPT_SRC="wifi_mode_switch.sh"
SWITCH_SCRIPT_DEST="/usr/local/bin/wifi_mode_switch.sh"
SERVICE_FILE_SRC="wifi_mode.service"
SERVICE_FILE_DEST="/etc/systemd/system/wifi_mode.service"
HOSTAPD_CONF_DEST="/etc/hostapd/hostapd.conf"
WPA_CONF_DEST="/etc/wpa_supplicant/wpa_supplicant-wlan0.conf"
LOG_FILE="/var/log/wifi_mode_switch.log"
STATE_FILE="/var/lib/autohotspot/state"

# Check for root
if [ "$(id -u)" -ne 0 ]; then
  echo "This script must be run as root. Please use sudo." >&2
  exit 1
fi

# --- Uninstall previous versions if they exist ---
echo "Removing any previous AutoHotspot installation..."
# NOTE: Do NOT stop the service here - it manages your active WiFi connection!
# Disable the old service (it will keep running until reboot).
# After we copy the new service file and enable it below, the new version will start on next boot.
systemctl disable wifi_mode.service >/dev/null 2>&1 || true
rm -f "$SERVICE_FILE_DEST"
rm -f "$SWITCH_SCRIPT_DEST"
rm -f "$CONFIG_DEST"
rm -f "$HOSTAPD_CONF_DEST"
rm -f "$WPA_CONF_DEST"
# Don't remove log file, it might be useful for debugging

echo "Reloading systemd daemon..."
systemctl daemon-reload

# --- Installation ---
echo "Installing required packages..."
apt update
apt install -y hostapd dnsmasq jq iw dhcpcd5

echo "Configuring services..."
# Unmask hostapd if it's masked (some distros mask it by default)
systemctl unmask hostapd 2>/dev/null || true
# Enable hostapd and dnsmasq so they CAN be started, but don't start them now
# The wifi_mode_switch.sh script will start/stop them as needed
systemctl enable hostapd 2>/dev/null || true
systemctl enable dnsmasq 2>/dev/null || true
systemctl stop hostapd 2>/dev/null || true
systemctl stop dnsmasq 2>/dev/null || true

echo "Creating dnsmasq configuration for hotspot..."
mkdir -p /etc/dnsmasq.d
cat > /etc/dnsmasq.d/hotspot.conf <<EOF
interface=wlan0
dhcp-range=192.168.4.2,192.168.4.20,255.255.255.0,24h
EOF
echo "dnsmasq configuration created"

# Note: NetworkManager will be disabled after service setup to avoid disconnecting SSH

echo "Backing up and disabling existing WiFi configurations..."
# Backup and disable any existing wpa_supplicant configuration (from Raspberry Pi Imager)
if [ -f "/etc/wpa_supplicant/wpa_supplicant.conf" ]; then
  cp "/etc/wpa_supplicant/wpa_supplicant.conf" "/etc/wpa_supplicant/wpa_supplicant.conf.backup"
  mv "/etc/wpa_supplicant/wpa_supplicant.conf" "/etc/wpa_supplicant/wpa_supplicant.conf.disabled"
  echo "Backed up existing wpa_supplicant.conf to .backup and disabled it"
fi

# Stop any running wpa_supplicant processes
# NOTE: do not kill wpa_supplicant here to avoid disconnecting SSH sessions.
# Cleanup (killing processes and restarting services) is handled by the boot-time script `wifi_mode_switch.sh`.
# pkill -f "wpa_supplicant" || true
# sleep 2

echo "Copying configuration and script files..."
# Move config to /boot
cp "$CONFIG_SRC" "$CONFIG_DEST"

# Move script and make executable
cp "$SWITCH_SCRIPT_SRC" "$SWITCH_SCRIPT_DEST"
chmod +x "$SWITCH_SCRIPT_DEST"

# Create placeholder config files to be managed by the switch script
echo "Creating placeholder configuration files..."
touch "$HOSTAPD_CONF_DEST"
touch "$WPA_CONF_DEST"

# Create log file and set permissions
echo "Creating log file..."
touch "$LOG_FILE"
chmod 644 "$LOG_FILE"

# Create state file to track installation state
echo "Creating state file..."
mkdir -p "/var/lib/autohotspot"
touch "$STATE_FILE"
chmod 644 "$STATE_FILE"

echo "Installing and enabling systemd service..."
cp "$SERVICE_FILE_SRC" "$SERVICE_FILE_DEST"
systemctl daemon-reload
systemctl enable wifi_mode.service
# NOTE: Do not start the service now - it will start automatically on next boot
# systemctl start wifi_mode.service  # Removed to avoid running during install

echo "Checking NetworkManager status..."
# Only disable NetworkManager if it's currently active
# NOTE: NetworkManager will be disabled (not stopped) during install to avoid dropping SSH.
# It will be fully stopped on the next boot by the wifi_mode_switch.sh service.
if systemctl is-active --quiet NetworkManager; then
  echo "NetworkManager is active, disabling it for next boot..."
  systemctl disable NetworkManager
  # Record that we disabled NetworkManager
  mkdir -p "$(dirname "$STATE_FILE")"
  echo "networkmanager_disabled=true" >> "$STATE_FILE"
  echo "NOTE: NetworkManager will remain active until reboot to avoid disconnecting SSH."
else
  echo "NetworkManager not active, no action needed"
fi

echo "--- AutoHotspot Installation Complete ---"
echo ""
echo "✅ Continuous monitoring is enabled! The service will:"
echo "   • Monitor WiFi connectivity every 30 seconds"
echo "   • Automatically switch to hotspot if home network is lost"
echo "   • Automatically reconnect to home network when available"
echo ""
echo "Please reboot the Raspberry Pi to apply all changes."
echo "Command: sudo reboot"
echo ""
echo "After reboot, monitor the service with:"
echo "   sudo tail -f /var/log/wifi_mode_switch.log"
echo ""
echo "For more details, see MONITORING.md"
