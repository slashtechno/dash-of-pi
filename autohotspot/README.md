# AutoHotspot for Raspberry Pi

Automatically switches between multiple WiFi networks and hotspot mode. Supports both standard WiFi (WPA-PSK) and enterprise WiFi (WPA-EAP) like universities and corporations.

## Features

- ✅ Multiple WiFi networks (tries in order)
- ✅ Standard WiFi with password
- ✅ Enterprise WiFi with username/password (PEAP/MSCHAPv2)
- ✅ Auto-fallback to hotspot when no network available
- ✅ Continuous connection monitoring
- ✅ Auto-reconnect when networks become available

## Quick Start

1. **Configure:**
   ```bash
   cp autohotspot.json.example autohotspot.json
   nano autohotspot.json
   ```

2. **Install:**
   ```bash
   scp -r autohotspot pi@pizero.local:~/
   ssh pi@pizero.local
   cd ~/autohotspot
   chmod +x install.sh
   sudo ./install.sh
   sudo reboot
   ```

3. **Verify:**
   ```bash
   sudo journalctl -u wifi_mode -f
   ```

## Configuration Examples

### Home WiFi Only
```json
{
  "networks": [
    {
      "ssid": "MyHomeWiFi",
      "type": "wpa_psk",
      "psk": "wifi_password"
    }
  ],
  "hotspot": {
    "ssid": "PiHotspot",
    "psk": "hotspot_password"
  }
}
```

### Home + Enterprise (University/Corporate)
```json
{
  "networks": [
    {
      "ssid": "HomeWiFi",
      "type": "wpa_psk",
      "psk": "home_password"
    },
    {
      "ssid": "UniversityWiFi",
      "type": "wpa_eap",
      "identity": "student@university.edu",
      "password": "campus_password",
      "ca_cert_validation": "disabled"
    }
  ],
  "hotspot": {
    "ssid": "PiHotspot",
    "psk": "hotspot_password"
  }
}
```

### Multiple Networks
```json
{
  "networks": [
    {
      "ssid": "HomeWiFi",
      "type": "wpa_psk",
      "psk": "home_password"
    },
    {
      "ssid": "WorkWiFi",
      "type": "wpa_psk",
      "psk": "work_password"
    },
    {
      "ssid": "CoffeeShopWiFi",
      "type": "wpa_psk",
      "psk": "coffee_password"
    }
  ],
  "hotspot": {
    "ssid": "PiHotspot",
    "psk": "hotspot_password"
  }
}
```

**Priority**: Networks are tried in order - first network in list has highest priority.

## Configuration Reference

### Network Types

#### WPA-PSK (Standard WiFi)
```json
{
  "ssid": "NetworkName",
  "type": "wpa_psk",
  "psk": "password"
}
```
Used by home networks, coffee shops, hotels, etc.

#### WPA-EAP (Enterprise WiFi)
```json
{
  "ssid": "NetworkName",
  "type": "wpa_eap",
  "identity": "username@domain.edu",
  "password": "password",
  "ca_cert_validation": "disabled"
}
```
Used by universities, corporations, government facilities.

**Enterprise WiFi Notes:**
- \`identity\`: Your username (format varies - try \`username@domain.edu\`, \`username\`, or \`DOMAIN\\username\`)
- \`ca_cert_validation\`: Set to \`"disabled"\` if iOS prompts you to accept certificate (safe for trusted networks)
- This is for networks that require username + password and make you accept a certificate

### Hotspot
```json
{
  "ssid": "HotspotName",
  "psk": "hotspot_password"
}
```
Created when no configured networks are available.

## How It Works

```
1. Scan for all configured networks
2. Try to connect (first network = highest priority)
3. Monitor connection every 30 seconds
4. If connection fails → try next network
5. If no networks available → create hotspot
6. In hotspot mode → scan for networks every 30s
```

## Usage

```bash
# Check status
sudo systemctl status wifi_mode

# View logs
sudo journalctl -u wifi_mode -f

# Restart (re-reads config)
sudo systemctl restart wifi_mode

# Update config
sudo nano /boot/autohotspot.json
sudo systemctl restart wifi_mode
```

## Troubleshooting

### Enterprise WiFi not connecting

1. Check username format (\`username@domain.edu\` vs \`username\`)
2. Verify password is correct
3. Set \`"ca_cert_validation": "disabled"\`
4. Check logs: \`sudo journalctl -u wifi_mode -f\`

### Pi creates hotspot instead of connecting

1. Check SSID spelling (case-sensitive!)
2. Verify password is correct
3. Check network is in range: \`sudo iw dev wlan0 scan | grep SSID\`
4. Validate JSON: \`jq . /boot/autohotspot.json\`

### Connection keeps dropping

1. Check signal strength
2. Verify router is stable
3. Try increasing \`FAILED_CHECKS_THRESHOLD\` in script (default: 3)

## Migration from Old Config

If you have an old config with \`home_wifi\` field:

**Old format:**
```json
{
  "home_wifi": {
    "ssid": "MyWiFi",
    "psk": "password"
  },
  "hotspot": { ... }
}
```

**New format:**
```json
{
  "networks": [
    {
      "ssid": "MyWiFi",
      "type": "wpa_psk",
      "psk": "password"
    }
  ],
  "hotspot": { ... }
}
```

Steps:
1. Backup: \`sudo cp /boot/autohotspot.json /boot/autohotspot.json.backup\`
2. Update config to new format
3. Validate: \`jq . /boot/autohotspot.json\`
4. Restart: \`sudo systemctl restart wifi_mode\`

## Uninstall

```bash
cd ~/autohotspot
chmod +x uninstall.sh
sudo ./uninstall.sh
sudo reboot
```

## Technical Details

- **Authentication**: WPA-PSK (password) and WPA-EAP (PEAP/MSCHAPv2)
- **Monitoring**: Connection checked every 30 seconds
- **Failover**: 3 failed checks → reconnect
- **Hotspot IP**: 192.168.4.1/24
- **Dependencies**: hostapd, dnsmasq, jq, iw

## Common Use Cases

**University Student**: Home WiFi + campus WiFi (enterprise) + hotspot fallback

**Mobile Professional**: Home + office (enterprise) + coffee shops + hotspot

**Multi-Location**: Multiple home/work locations, auto-connect wherever you are

---

**Part of dash-of-pi dashcam project**
