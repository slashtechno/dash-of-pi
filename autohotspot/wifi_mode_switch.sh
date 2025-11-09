#!/bin/bash

CONFIG="/boot/autohotspot.json"
DEVICE="wlan0"
LOG_FILE="/var/log/wifi_mode_switch.log"
HOSTAPD_CONF="/etc/hostapd/hostapd.conf"
WPA_CONF="/etc/wpa_supplicant/wpa_supplicant-wlan0.conf"

# Monitoring settings
CHECK_INTERVAL=30  # Check connection every 30 seconds
PING_HOST="8.8.8.8"  # Google DNS for connectivity check
PING_TIMEOUT=5
FAILED_CHECKS_THRESHOLD=3  # Switch to hotspot after 3 failed checks
CURRENT_MODE=""  # Track current mode: "client" or "hotspot"

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

# Function to check if we have internet connectivity in client mode
check_connectivity() {
  # First check if we have an IP address
  if ! ip addr show "$DEVICE" | grep -q "inet "; then
    return 1
  fi
  
  # Then check if we can ping
  if ping -c 1 -W "$PING_TIMEOUT" "$PING_HOST" >/dev/null 2>&1; then
    return 0
  else
    return 1
  fi
}

# Function to check if home network is available
home_network_available() {
  iw dev "$DEVICE" scan ap-force 2>/dev/null | grep -q "SSID: $HOME_SSID"
}

# Main mode switching logic
perform_mode_switch() {
  log_message "Performing mode detection..."
  
  if home_network_available; then
    if [ "$CURRENT_MODE" != "client" ]; then
      switch_to_client
      CURRENT_MODE="client"
    fi
  else
    if [ "$CURRENT_MODE" != "hotspot" ]; then
      switch_to_hotspot
      CURRENT_MODE="hotspot"
    fi
  fi
}

# Monitoring loop
monitor_connection() {
  log_message "Starting continuous connection monitoring (checking every ${CHECK_INTERVAL}s)"
  log_message "HOME_SSID=$HOME_SSID, HOTSPOT_SSID=$HS_SSID"
  
  local failed_checks=0
  
  # Initial mode switch
  perform_mode_switch
  
  while true; do
    sleep "$CHECK_INTERVAL"
    
    if [ "$CURRENT_MODE" = "client" ]; then
      # In client mode - check connectivity
      if check_connectivity; then
        failed_checks=0
        log_message "Client mode: Connection OK"
      else
        failed_checks=$((failed_checks + 1))
        log_message "Client mode: Connection check failed ($failed_checks/$FAILED_CHECKS_THRESHOLD)"
        
        if [ $failed_checks -ge $FAILED_CHECKS_THRESHOLD ]; then
          log_message "Client mode: Connection lost after $failed_checks failed checks"
          # Check if home network is still visible
          if ! home_network_available; then
            log_message "Home network no longer visible, switching to hotspot"
            switch_to_hotspot
            CURRENT_MODE="hotspot"
            failed_checks=0
          else
            log_message "Home network still visible, attempting to reconnect"
            switch_to_client
            failed_checks=0
          fi
        fi
      fi
    else
      # In hotspot mode - periodically check if home network becomes available
      log_message "Hotspot mode: Checking for home network"
      if home_network_available; then
        log_message "Home network detected, switching to client mode"
        switch_to_client
        CURRENT_MODE="client"
        failed_checks=0
      fi
    fi
  done
}

# Main logic - always run in monitoring mode
log_message "Starting WiFi mode detection..."
log_message "HOME_SSID=$HOME_SSID, HOTSPOT_SSID=$HS_SSID"
monitor_connection
