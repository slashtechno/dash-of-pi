#!/bin/bash
set -e

echo "--- Starting AutoHotspot Uninstallation ---"

# Configuration
CONFIG_DEST="/boot/autohotspot.json"
SWITCH_SCRIPT_DEST="/usr/local/bin/wifi_mode_switch.sh"
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

echo "Stopping and disabling AutoHotspot service..."
systemctl disable --now wifi_mode.service 2>/dev/null || true

echo "Stopping hotspot services..."
systemctl stop hostapd 2>/dev/null || true
systemctl stop dnsmasq 2>/dev/null || true

echo "Removing AutoHotspot files..."
rm -f "$SERVICE_FILE_DEST"
rm -f "$SWITCH_SCRIPT_DEST"
rm -f "$CONFIG_DEST"
rm -f "$HOSTAPD_CONF_DEST"
rm -f "$WPA_CONF_DEST"

echo "Restoring original WiFi configuration..."
# Restore the original wpa_supplicant configuration if it was backed up
if [ -f "/etc/wpa_supplicant/wpa_supplicant.conf.backup" ]; then
  mv "/etc/wpa_supplicant/wpa_supplicant.conf.backup" "/etc/wpa_supplicant/wpa_supplicant.conf"
  rm -f "/etc/wpa_supplicant/wpa_supplicant.conf.disabled"
  echo "Restored original wpa_supplicant.conf"
fi

echo "Re-enabling NetworkManager if it was disabled..."
# Check state file to see what was disabled during install
if [ -f "$STATE_FILE" ] && grep -q "networkmanager_disabled=true" "$STATE_FILE"; then
  echo "Re-enabling NetworkManager (was disabled during install)..."
  systemctl enable NetworkManager 2>/dev/null || true
else
  echo "NetworkManager was not disabled by install script"
fi

echo "Removing state file..."
rm -f "$STATE_FILE"

echo "Reloading systemd daemon..."
systemctl daemon-reload

echo "--- AutoHotspot Uninstallation Complete ---"
echo ""
echo "Note: The log file has been preserved at: $LOG_FILE"
echo "Note: You may need to manually reconfigure your WiFi connection."
echo "Note: Consider rebooting the Raspberry Pi: sudo reboot"
