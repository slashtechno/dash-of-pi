Recovering from a disabled NetworkManager / lost SSH (Generic guide)

If AutoHotspot disabled NetworkManager during install (or you otherwise lost SSH access), you can recover the device by editing the SD card on another computer. This guide gives several safe options.

Before you begin
- You need physical access to the Pi's SD card and another computer with an SD card reader.
- Identify the SD card's boot partition (FAT) and root partition (ext4). On many systems the boot partition is small and labeled "boot".

Quick summary
1. Mount the SD card on another computer
2. Restore or re-enable the original wpa_supplicant configuration
3. Create a state file so uninstall will re-enable NetworkManager
4. Re-enable NetworkManager for next boot (symlink or chroot)
5. Reinsert SD card and boot the Pi

Quick recovery (copy-paste)

Run these commands on your host machine (replace $USER with your username if needed):

```bash
# Find your mount path
lsblk | grep rootfs

# Set ROOT variable (adjust path as needed)
ROOT="/run/media/$USER/rootfs"

# 1) Restore wpa_supplicant if present
sudo bash -c "
if [ -f \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf.disabled\" ]; then
  mv \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf.disabled\" \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf\"
  echo \"restored from .disabled\"
elif [ -f \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf.backup\" ]; then
  mv \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf.backup\" \"\$ROOT/etc/wpa_supplicant/wpa_supplicant.conf\"
  echo \"restored from .backup\"
else
  echo \"no backup found\"
fi
"

# 2) Create state file so uninstall will re-enable NetworkManager
sudo mkdir -p "$ROOT/var/lib/autohotspot"
echo "networkmanager_disabled=true" | sudo tee "$ROOT/var/lib/autohotspot/state"

# 3) Enable NetworkManager for next boot (symlink method)
sudo bash -c "
if [ -f \"\$ROOT/lib/systemd/system/NetworkManager.service\" ]; then
  mkdir -p \"\$ROOT/etc/systemd/system/multi-user.target.wants\"
  ln -sf /lib/systemd/system/NetworkManager.service \"\$ROOT/etc/systemd/system/multi-user.target.wants/NetworkManager.service\"
  echo \"NetworkManager enabled for next boot\"
else
  echo \"NetworkManager unit not present; skip\"
fi
"

# 4) Sync and unmount (cd out of mount point first if needed)
cd ~
sync
sudo umount "$ROOT"
```

After running these commands, reinsert the SD card into your Pi and boot it. SSH should work again if your wpa_supplicant contains valid credentials.

---

## Detailed diagnostic and recovery steps

If the quick recovery doesn't work or you want to understand what's happening, follow these detailed steps:

### Step 1: Mount and locate your Pi's root filesystem

```bash
# Insert SD card and find the mount point
lsblk | grep rootfs

# Set the ROOT variable (adjust path to match your system)
ROOT="/run/media/$USER/rootfs"

# Verify it's mounted
ls -la "$ROOT/etc" || echo "Mount point not found - adjust ROOT path"
```

### Step 2: Check AutoHotspot configuration

```bash
# Check if config file exists and has valid content
echo "=== AutoHotspot Config ==="
cat "$ROOT/boot/autohotspot.json"
echo ""

# Verify it contains your actual WiFi SSID, not placeholder values
# If it shows "YourHomeSSID", edit it with your real credentials
```

### Step 3: Check network configuration files

```bash
# Check wpa_supplicant status
echo "=== WPA Supplicant Files ==="
ls -la "$ROOT/etc/wpa_supplicant/" | grep -E "wpa_supplicant\.conf"
echo ""

# Check which files exist:
# - wpa_supplicant.conf (active) - NEEDED for WiFi to work
# - wpa_supplicant.conf.disabled - needs to be restored
# - wpa_supplicant.conf.backup - can be used as fallback
```

### Step 4: Check NetworkManager status

```bash
# Check if NetworkManager is enabled
echo "=== NetworkManager Status ==="
ls -la "$ROOT/etc/systemd/system/multi-user.target.wants/NetworkManager.service" 2>&1 || echo "NetworkManager NOT enabled"
echo ""
```

### Step 5: Check AutoHotspot service and logs

```bash
# Check if service is installed
echo "=== AutoHotspot Service ==="
ls -la "$ROOT/etc/systemd/system/wifi_mode.service" 2>&1 || echo "Service not found"
ls -la "$ROOT/etc/systemd/system/multi-user.target.wants/wifi_mode.service" 2>&1 || echo "Service not enabled"
echo ""

# View recent logs
echo "=== Recent Logs ==="
tail -15 "$ROOT/var/log/wifi_mode_switch.log" 2>&1 || echo "No logs found"
echo ""

# Check hotspot configuration
echo "=== Hotspot Config ==="
cat "$ROOT/etc/hostapd/hostapd.conf" 2>&1 || echo "No hostapd config"
```

### Step 6: Apply fixes based on diagnosis

**If wpa_supplicant.conf is missing or disabled:**
```bash
# Restore from disabled or backup
cd "$ROOT/etc/wpa_supplicant"
if [ -f wpa_supplicant.conf.disabled ]; then
  sudo mv wpa_supplicant.conf.disabled wpa_supplicant.conf
  echo "✓ Restored from .disabled"
elif [ -f wpa_supplicant.conf.backup ]; then
  sudo mv wpa_supplicant.conf.backup wpa_supplicant.conf
  echo "✓ Restored from .backup"
fi
```

**If NetworkManager is disabled:**
```bash
# Re-enable NetworkManager
sudo mkdir -p "$ROOT/etc/systemd/system/multi-user.target.wants"
sudo ln -sf /lib/systemd/system/NetworkManager.service \
  "$ROOT/etc/systemd/system/multi-user.target.wants/NetworkManager.service"
echo "✓ NetworkManager enabled"
```

**Create state file for proper uninstall:**
```bash
sudo mkdir -p "$ROOT/var/lib/autohotspot"
echo "networkmanager_disabled=true" | sudo tee "$ROOT/var/lib/autohotspot/state"
echo "✓ State file created"
```

### Step 7: Verify and unmount

```bash
# Verify fixes were applied
echo "=== Verification ==="
ls -la "$ROOT/etc/wpa_supplicant/wpa_supplicant.conf" 2>&1 && echo "✓ wpa_supplicant.conf exists" || echo "✗ Still missing"
ls -la "$ROOT/etc/systemd/system/multi-user.target.wants/NetworkManager.service" 2>&1 && echo "✓ NetworkManager enabled" || echo "✗ Not enabled"
echo ""

# Unmount safely (make sure you're not in the mount directory)
cd ~
sync
sudo umount "$ROOT"
echo "✓ Safe to remove SD card"
```

### Step 8: Boot and test

1. Remove SD card from your computer
2. Insert into Raspberry Pi
3. Power on the Pi
4. Wait 30-60 seconds for boot
5. Try to SSH: `ssh user@pizero.local`
6. If SSH works, check the AutoHotspot status:
   ```bash
   sudo systemctl status wifi_mode.service
   tail -20 /var/log/wifi_mode_switch.log
   ```

Alternative recovery options (if you can't access another computer)
- Connect a keyboard and monitor to the Pi and use the local console to undo the changes or run the uninstall script.
- If the hotspot is active, connect a laptop/phone to the hotspot SSID and use the web UI (if available) or check logs at http://192.168.4.1. The hotspot route may let you access the Pi.

Notes and safety
- These steps are non-destructive if you only move files or create the state file. Still make backups if you are unsure.
- If you had custom network settings, restoring backups will return the system to previous state.
- The state file is used to avoid re-enabling services that weren't disabled by AutoHotspot.
