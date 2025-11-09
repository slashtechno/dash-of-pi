#!/bin/bash

CONFIG="/boot/autohotspot.json"
DEVICE="wlan0"
LOG_FILE="/var/log/wifi_mode_switch.log"
HOSTAPD_CONF="/etc/hostapd/hostapd.conf"
WPA_CONF="/etc/wpa_supplicant/wpa_supplicant-wlan0.conf"

# Read configuration
if [ ! -f "$CONFIG" ]; then
  echo "$(date '+%Y-%m-%d %H:%M:%S'): ERROR: Config file $CONFIG not found" >> "$LOG_FILE"
  exit 1
fi

HOME_SSID=$(jq -r '.home_wifi.ssid' "$CONFIG" 2>/dev/null) || true
HOME_PSK=$(jq -r '.home_wifi.psk' "$CONFIG" 2>/dev/null) || true
HS_SSID=$(jq -r '.hotspot.ssid' "$CONFIG" 2>/dev/null) || true
HS_PSK=$(jq -r '.hotspot.psk' "$CONFIG" 2>/dev/null) || true

# Validate config was loaded
if [ -z "$HOME_SSID" ] || [ -z "$HOME_PSK" ] || [ -z "$HS_SSID" ] || [ -z "$HS_PSK" ]; then
  echo "$(date '+%Y-%m-%d %H:%M:%S'): ERROR: Failed to read config values from $CONFIG" >> "$LOG_FILE"
  echo "HOME_SSID=$HOME_SSID HOME_PSK=$HOME_PSK HS_SSID=$HS_SSID HS_PSK=$HS_PSK" >> "$LOG_FILE"
  exit 1
fi

# Ensure device is up
ip link set "$DEVICE" up 2>/dev/null || true
sleep 2

# Log function
log_message() {
  echo "$(date '+%Y-%m-%d %H:%M:%S'): $1" >> "$LOG_FILE"
}

# Function to switch to client mode
switch_to_client() {
  log_message "Home SSID found, switching to client mode"
  
  # Stop hotspot services
  systemctl stop hostapd 2>/dev/null || true
  systemctl stop dnsmasq 2>/dev/null || true
  
  # Flush IP addresses
  ip addr flush dev "$DEVICE"
  
  # Kill any existing wpa_supplicant processes
  pkill -f "wpa_supplicant" || true
  sleep 2
  
  # Create wpa_supplicant configuration
  cat > "$WPA_CONF" <<EOF
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=US

network={
    ssid="$HOME_SSID"
    psk="$HOME_PSK"
    key_mgmt=WPA-PSK
    priority=10
}
EOF
  
  # Start wpa_supplicant with the dedicated config
  wpa_supplicant -B -i "$DEVICE" -c "$WPA_CONF" -D nl80211,wext
  sleep 5
  
  # Request DHCP address
  dhclient -r "$DEVICE" 2>/dev/null || true
  sleep 1
  dhclient "$DEVICE" 2>/dev/null || true
  
  log_message "Client mode activated"
}

# Function to switch to hotspot mode
switch_to_hotspot() {
  log_message "Home SSID not found, switching to hotspot mode"
  
  # Kill all wpa_supplicant processes
  pkill -f "wpa_supplicant" || true
  sleep 2
  
  # Release DHCP lease and stop dhclient
  dhclient -r "$DEVICE" 2>/dev/null || true
  pkill -f "dhclient.*$DEVICE" || true
  sleep 1
  
  # Bring interface down and back up
  ip link set "$DEVICE" down
  sleep 1
  ip link set "$DEVICE" up
  sleep 1
  
  # Set static IP for hotspot
  ip addr flush dev "$DEVICE"
  ip addr add 192.168.4.1/24 dev "$DEVICE"
  
  # Create hostapd configuration
  cat > "$HOSTAPD_CONF" <<EOF
interface=$DEVICE
driver=nl80211
ssid=$HS_SSID
hw_mode=g
channel=7
wmm_enabled=0
macaddr_acl=0
auth_algs=1
ignore_broadcast_ssid=0
wpa=2
wpa_passphrase=$HS_PSK
wpa_key_mgmt=WPA-PSK
wpa_pairwise=TKIP
rsn_pairwise=CCMP
EOF
  
  # Start hotspot services
  systemctl restart hostapd
  systemctl restart dnsmasq
  
  log_message "Hotspot mode activated"
}

# Main logic: scan for home network
log_message "Starting WiFi mode detection..."
log_message "HOME_SSID=$HOME_SSID, HOTSPOT_SSID=$HS_SSID"

if iw dev "$DEVICE" scan ap-force 2>/dev/null | grep -q "SSID: $HOME_SSID"; then
  switch_to_client
else
  switch_to_hotspot
fi

log_message "WiFi mode switch complete"
