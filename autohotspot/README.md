# AutoHotspot for Raspberry Pi Zero 2 W

Automatically switches your Pi between home WiFi and hotspot mode with **continuous connection monitoring**. Always accessible at home or on the road.

## How It Works

```
At Home:  Connects to your WiFi → Monitors connection every 30s
          Connection lost? → Switches to hotspot automatically
          
Away:     Runs as hotspot → Scans for home WiFi every 30s
          Home WiFi found? → Reconnects automatically
```

**Key Features:**
- ✅ Auto-connects to home WiFi on boot
- ✅ Auto-switches to hotspot if WiFi is lost (after 3 failed checks)
- ✅ Auto-reconnects to home WiFi when it becomes available
- ✅ Handles temporary network hiccups without switching
- ✅ <1% CPU usage, minimal battery impact

## Prerequisites

- Raspberry Pi Zero 2 W with Raspberry Pi OS
- SSH access to the Pi
- Required packages (auto-installed): `hostapd`, `dnsmasq`, `jq`, `iw`

## Installation

1. **Create your config file** (on your dev machine):
   ```bash
   cp autohotspot.json.example autohotspot.json
   nano autohotspot.json  # Add your WiFi credentials
   ```

2. **Copy to Pi and install:**
   ```bash
   scp -r autohotspot user@pizero.local:~/
   ssh user@pizero.local
   cd ~/autohotspot
   chmod +x install.sh
   sudo ./install.sh
   sudo reboot
   ```
   
   ⚠️ **Important**: After running `install.sh`, you MUST reboot. The service manages your active WiFi connection and won't fully update until reboot.

3. **Verify it's working:**
   ```bash
   sudo tail -f /var/log/wifi_mode_switch.log
   ```
   
   You should see:
   ```
   Starting continuous connection monitoring (checking every 30s)
   Client mode: Connection OK
   ```

## Usage

**At Home:** Pi connects to your WiFi and monitors connection every 30s. Access via home network IP.

**Away:** Pi runs as hotspot. Connect to the hotspot SSID and access at:
```
http://192.168.4.1:<port>
```

## Configuration

Customize monitoring by editing `/usr/local/bin/wifi_mode_switch.sh`:

```bash
CHECK_INTERVAL=30              # Seconds between checks
PING_HOST="8.8.8.8"            # Connectivity test host
PING_TIMEOUT=5                 # Ping timeout
FAILED_CHECKS_THRESHOLD=3      # Failures before switching (~90s)
```

After changes: `sudo systemctl restart wifi_mode.service`

## Monitoring & Troubleshooting

**View logs:**
```bash
sudo tail -f /var/log/wifi_mode_switch.log
```

**Service control:**
```bash
sudo systemctl status wifi_mode.service
sudo systemctl restart wifi_mode.service
```

**Run script manually (for testing):**
```bash
sudo /usr/local/bin/wifi_mode_switch.sh  # Runs monitoring loop
```

## Quick Test

1. Turn off your WiFi router
2. Wait ~2 minutes
3. Pi should switch to hotspot mode
4. Turn router back on
5. Pi should reconnect to WiFi in ~1 minute

## Uninstallation

```bash
cd ~/autohotspot
chmod +x uninstall.sh
sudo ./uninstall.sh
sudo rm /etc/dnsmasq.d/hotspot.conf  # Optional
sudo reboot
```

## Notes

- Hotspot uses static IP: `192.168.4.1`
- Monitoring uses <1% CPU
- Check interval: 30 seconds
- Switch threshold: 3 failed checks (~90 seconds)
- Recovery: See `RECOVER_FROM_DISABLED_NETWORK.md` if you lose SSH access
