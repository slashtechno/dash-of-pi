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

# Read hotspot config
HS_SSID=$(jq -r '.hotspot.ssid' "$CONFIG" 2>/dev/null) || true
HS_PSK=$(jq -r '.hotspot.psk' "$CONFIG" 2>/dev/null) || true

# Validate hotspot config was loaded
if [ -z "$HS_SSID" ] || [ -z "$HS_PSK" ]; then
  echo "$(date '+%Y-%m-%d %H:%M:%S'): ERROR: Failed to read hotspot config from $CONFIG" >> "$LOG_FILE"
  exit 1
fi

# Get number of configured networks
NETWORK_COUNT=$(jq -r '.networks | length' "$CONFIG" 2>/dev/null) || NETWORK_COUNT=0

if [ "$NETWORK_COUNT" -eq 0 ]; then
  echo "$(date '+%Y-%m-%d %H:%M:%S'): ERROR: No networks configured in $CONFIG" >> "$LOG_FILE"
  exit 1
fi

# Ensure device is up
ip link set "$DEVICE" up 2>/dev/null || true
sleep 2

# Log function
log_message() {
  echo "$(date '+%Y-%m-%d %H:%M:%S'): $1" >> "$LOG_FILE"
}

# Function to generate wpa_supplicant network config for a network
generate_network_config() {
  local index=$1
  local ssid=$(jq -r ".networks[$index].ssid" "$CONFIG")
  local type=$(jq -r ".networks[$index].type" "$CONFIG")
  # Priority based on array order: first network = highest priority
  local priority=$((100 - index * 10))
  
  echo "network={"
  echo "    ssid=\"$ssid\""
  echo "    priority=$priority"
  
  if [ "$type" = "wpa_psk" ]; then
    local psk=$(jq -r ".networks[$index].psk" "$CONFIG")
    echo "    psk=\"$psk\""
    echo "    key_mgmt=WPA-PSK"
    
  elif [ "$type" = "wpa_eap" ]; then
    local identity=$(jq -r ".networks[$index].identity" "$CONFIG")
    local password=$(jq -r ".networks[$index].password" "$CONFIG")
    local ca_cert_validation=$(jq -r ".networks[$index].ca_cert_validation // \"enabled\"" "$CONFIG")
    
    echo "    key_mgmt=WPA-EAP"
    echo "    eap=PEAP"
    echo "    identity=\"$identity\""
    echo "    password=\"$password\""
    echo "    phase2=\"auth=MSCHAPV2\""
    
    # Disable certificate validation if specified (common for institutional WiFi)
    if [ "$ca_cert_validation" = "disabled" ]; then
      echo "    # Certificate validation disabled - accept any certificate"
      echo "    ca_cert=\"/etc/ssl/certs/ca-certificates.crt\""
      echo "    phase1=\"peaplabel=0\""
      echo "    # Don't validate server certificate"
      echo "    # This is equivalent to accepting the certificate on iOS"
    fi
  fi
  
  echo "}"
}

# Function to switch to client mode - tries all configured networks
switch_to_client() {
  log_message "Switching to client mode - will try all configured networks in priority order"
  
  # Stop hotspot services
  systemctl stop hostapd 2>/dev/null || true
  systemctl stop dnsmasq 2>/dev/null || true
  
  # Flush IP addresses
  ip addr flush dev "$DEVICE"
  
  # Kill any existing wpa_supplicant processes
  pkill -f "wpa_supplicant" || true
  sleep 2
  
  # Create wpa_supplicant configuration with ALL networks
  cat > "$WPA_CONF" <<EOF
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=US

EOF
  
  # Add all configured networks to wpa_supplicant config
  for ((i=0; i<NETWORK_COUNT; i++)); do
    generate_network_config $i >> "$WPA_CONF"
    echo "" >> "$WPA_CONF"
  done
  
  log_message "Generated wpa_supplicant config with $NETWORK_COUNT networks"
  
  # Start wpa_supplicant with the dedicated config
  wpa_supplicant -B -i "$DEVICE" -c "$WPA_CONF" -D nl80211,wext
  sleep 5
  
  # Request DHCP address
  dhclient -r "$DEVICE" 2>/dev/null || true
  sleep 1
  dhclient "$DEVICE" 2>/dev/null || true
  
  log_message "Client mode activated - wpa_supplicant will auto-select best available network"
}

# Function to switch to hotspot mode
switch_to_hotspot() {
  log_message "No configured networks available, switching to hotspot mode"
  
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

# Function to check if any configured network is available
any_network_available() {
  local scan_results=$(iw dev "$DEVICE" scan ap-force 2>/dev/null)
  
  # Check each configured network
  for ((i=0; i<NETWORK_COUNT; i++)); do
    local ssid=$(jq -r ".networks[$i].ssid" "$CONFIG")
    if echo "$scan_results" | grep -q "SSID: $ssid"; then
      log_message "Found available network: $ssid"
      return 0
    fi
  done
  
  return 1
}

# Main mode switching logic
perform_mode_switch() {
  log_message "Performing mode detection..."
  
  if any_network_available; then
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
  log_message "Configured networks: $NETWORK_COUNT, HOTSPOT_SSID=$HS_SSID"
  
  # Log all configured networks (in priority order)
  for ((i=0; i<NETWORK_COUNT; i++)); do
    local ssid=$(jq -r ".networks[$i].ssid" "$CONFIG")
    local type=$(jq -r ".networks[$i].type" "$CONFIG")
    log_message "  Network $((i+1)): $ssid (type=$type)"
  done
  
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
          # Check if any configured network is still visible
          if ! any_network_available; then
            log_message "No configured networks visible, switching to hotspot"
            switch_to_hotspot
            CURRENT_MODE="hotspot"
            failed_checks=0
          else
            log_message "Networks still visible, attempting to reconnect"
            switch_to_client
            failed_checks=0
          fi
        fi
      fi
    else
      # In hotspot mode - periodically check if any configured network becomes available
      log_message "Hotspot mode: Checking for configured networks"
      if any_network_available; then
        log_message "Configured network detected, switching to client mode"
        switch_to_client
        CURRENT_MODE="client"
        failed_checks=0
      fi
    fi
  done
}

# Main logic - always run in monitoring mode
log_message "Starting WiFi mode detection..."
log_message "Loaded $NETWORK_COUNT network(s) and hotspot config from $CONFIG"
monitor_connection
