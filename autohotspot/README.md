# AutoHotspot for Raspberry Pi Zero 2 W

This project automatically switches your Raspberry Pi between your home Wi‑Fi and hotspot mode so you can reach the device when you're away.

**Files**

- `autohotspot.json.example` — example configuration file (copy to `autohotspot.json` and edit)
- `autohotspot.json` — your actual configuration file (git-ignored)
- `wifi_mode_switch.sh` — script to switch between client and hotspot mode
- `wifi_mode.service` — systemd unit to run the script on boot
- `install.sh` — automated installation script
- `uninstall.sh` — automated uninstallation script
- `README.md` — this file
- `RECOVER_FROM_DISABLED_NETWORK.md` — recovery guide if you lose SSH access

**Prerequisites**

Before installation, ensure you have:
- Raspberry Pi Zero 2 W with Raspberry Pi OS installed
- SSH access to the Pi
- `hostapd`, `dnsmasq`, `jq`, and `iw` packages (installed automatically by `install.sh`)

**Important Notes**

- If you used Raspberry Pi Imager to set up WiFi on your SD card, the installation script will automatically back up and disable that configuration to prevent conflicts. Your original WiFi settings will be restored if you uninstall AutoHotspot.
- Make sure to edit `autohotspot.json` with your actual home WiFi credentials before installation.

**Installation**

Follow these steps to install AutoHotspot on your Raspberry Pi.

1. **Create and edit the configuration file** on your development machine:

   ```bash
   # Copy the example config
   cp autohotspot.json.example autohotspot.json
   
   # Edit with your WiFi credentials
   nano autohotspot.json  # or use your preferred editor
   ```

   Update it with your actual WiFi credentials:

   ```json
   {
     "home_wifi": {
       "ssid": "YourActualHomeSSID",
       "psk": "YourActualPassword"
     },
     "hotspot": {
       "ssid": "PiHotspot",
       "psk": "HotspotPassword"
     }
   }
   ```
   
   **Note:** `autohotspot.json` is git-ignored to protect your credentials.

2. **Copy the autohotspot directory to the Pi:**

   From your development machine:

   ```sh
   scp -r autohotspot user@pizero.local:~/
   ```

3. **Run the installation script on the Pi:**

   SSH into the Pi and execute the installer:

   ```sh
   ssh user@pizero.local
   cd ~/autohotspot
   chmod +x install.sh
   sudo ./install.sh
   ```

   The installer will automatically:
   - Install required packages (hostapd, dnsmasq, jq, iw, dhcpcd5)
   - Unmask and configure hostapd
   - Create dnsmasq DHCP configuration
   - Back up existing WiFi configuration
   - Install the AutoHotspot service

4. **Reboot the Pi to activate AutoHotspot:**

   ```sh
   sudo reboot
   ```

**Reinstallation**

If you need to update the configuration or reinstall AutoHotspot:

1. **Update the configuration file** (`autohotspot.json`) on your development machine if needed.

2. **Copy the updated autohotspot directory to the Pi:**

   ```sh
   scp -r autohotspot user@pizero.local:~/
   ```

3. **Run the installation script again** (it will automatically uninstall the previous version first):

   ```sh
   ssh user@pizero.local
   cd ~/autohotspot
   chmod +x install.sh
   sudo ./install.sh
   sudo reboot
   ```

**Usage**

- **At home:** The Pi scans for your home Wi‑Fi on boot. If found, it connects as a client and obtains an IP address via DHCP. Your Go web server is available on the Pi's home network IP.
  
- **Away from home:** If the home Wi‑Fi is not found, the Pi starts an access point (hotspot) using the configured SSID and password. Connect a phone or laptop to the hotspot and access the web server at:

  ```
  http://192.168.4.1:<port>
  ```

  Replace `<port>` with the port your web server uses.

**Uninstallation**

To remove AutoHotspot and its configuration:

1. **SSH into the Pi:**

   ```sh
   ssh user@pizero.local
   ```

2. **Run the uninstallation script:**

   ```sh
   cd ~/autohotspot
   chmod +x uninstall.sh
   sudo ./uninstall.sh
   ```

3. **Optionally remove the dnsmasq configuration:**

   ```sh
   sudo rm /etc/dnsmasq.d/hotspot.conf
   ```

4. **Reboot the Pi:**

   ```sh
   sudo reboot
   ```

**Troubleshooting**

- **Check logs:** View the AutoHotspot log file for debugging:
  ```sh
  sudo tail -f /var/log/wifi_mode_switch.log
  ```

- **Manual service control:**
  ```sh
  sudo systemctl status wifi_mode.service
  sudo systemctl restart wifi_mode.service
  ```

- **Test the script manually:**
  ```sh
  sudo /usr/local/bin/wifi_mode_switch.sh
  ```

**Notes**

- This setup is intended for small, local deployments (development or personal use).
- The script manages its own dedicated configuration files and does not interfere with system-wide network settings.
- Review `wifi_mode_switch.sh` and service unit before enabling on production devices.
- The hotspot mode uses a static IP (192.168.4.1) and runs `hostapd` and `dnsmasq` services.
