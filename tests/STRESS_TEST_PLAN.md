# Linx Stress Test Plan — Full Hyprland Desktop from Scratch

**Version:** 1.0
**Created:** 2026-03-27
**Purpose:** Push Linx through a real-world, multi-phase desktop configuration from a bare Arch install to a fully themed Hyprland desktop. Designed to be repeatable on different hardware (headless VMs, laptops with real GPUs). 17 phases covering every tool and safety feature.

---

## Prerequisites

- Fresh Arch Linux install (base + base-devel + networkmanager)
- `lx` binary installed (`make install` or `cp lx /usr/local/bin/`)
- Config file at `~/.config/linx/config.toml` with provider configured
- User account with sudo access (or running as root for VM testing)
- Internet access for package installs and web search

## Hardware Profiles

Linx should detect and adapt to the hardware automatically via `get_os_info` and `get_hardware_info`. Key scenarios:

| Profile | GPU | Rendering | Driver Expected |
|---------|-----|-----------|-----------------|
| QEMU/KVM VM | virtio-vga / QXL / bochs | Software (pixman) | `mesa` only, `WLR_RENDERER=pixman` |
| Intel iGPU laptop | Intel UHD/Iris | Hardware (Vulkan) | `mesa` `vulkan-intel` `intel-media-driver` |
| AMD dGPU laptop | Radeon RX / RDNA | Hardware (Vulkan) | `mesa` `vulkan-radeon` `libva-mesa-driver` |
| NVIDIA laptop | GeForce RTX | Hardware (Vulkan) | `nvidia-dkms` `nvidia-utils` `egl-wayland` `libva-nvidia-driver` |
| Hybrid (Intel+NVIDIA) | Optimus | Hardware (choose) | Both drivers + `envycontrol` or `supergfxctl` |

**Linx must query hardware first and choose appropriate drivers — not assume any specific GPU.**

## Keybinding Convention

All keybindings should follow **macOS conventions** mapped to Super (Mod4) as the primary modifier:

| Action | macOS | Hyprland Keybind |
|--------|-------|-----------------|
| Copy | Cmd+C | Super+C |
| Paste | Cmd+V | Super+V |
| Cut | Cmd+X | Super+X |
| Undo | Cmd+Z | Super+Z |
| Redo | Cmd+Shift+Z | Super+Shift+Z |
| Select All | Cmd+A | Super+A |
| Close Window | Cmd+W | Super+W |
| Quit App | Cmd+Q | Super+Q |
| App Launcher | Cmd+Space | Super+Space |
| App Switcher | Cmd+Tab | Super+Tab (or Alt+Tab) |
| Terminal | Cmd+Return | Super+Return |
| File Manager | Cmd+E | Super+E |
| Browser | Cmd+B | Super+B (optional) |
| Lock Screen | Cmd+Ctrl+Q | Super+L |
| Screenshot | Cmd+Shift+3 | Super+Shift+S |
| Screenshot region | Cmd+Shift+4 | Super+Shift+A |
| Fullscreen toggle | Cmd+Ctrl+F | Super+F |
| Toggle float | Cmd+Shift+F | Super+Shift+F |
| Move to workspace 1-9 | Ctrl+1-9 | Super+1-9 |
| Move window to ws 1-9 | Ctrl+Shift+1-9 | Super+Shift+1-9 |
| DND toggle | — | Super+N |

**Note:** macOS uses Cmd for copy/paste at the OS level, but Linux apps use Ctrl+C/V internally. The keybindings above are for Hyprland window management. For clipboard, Linx should configure `wl-clipboard` and ensure Ctrl+C/V works inside apps as normal, with Super+C/V as Hyprland-level shortcuts (if possible) or document the difference.

---

## Test Phases

### Phase 0: User Setup
**Tools tested:** `run_command_privileged`, `write_file` (privileged), `run_command`, research gate

**Prompt:**
```
Create a local user called testuser with a home directory. Give them passwordless sudo access by adding them to the wheel group and configuring sudoers for NOPASSWD. Set up their shell as bash.
```

**Expected behaviour:**
- Research gate fires (user creation is destructive)
- Calls `get_os_info` to identify the distro and available tools
- Creates user with `useradd -m -s /bin/bash -G wheel testuser` (or equivalent)
- Reads existing `/etc/sudoers` before modifying
- Configures passwordless sudo: either modifies `/etc/sudoers` or creates `/etc/sudoers.d/testuser` with `testuser ALL=(ALL) NOPASSWD: ALL`
- Uses `visudo -c` or equivalent to validate sudoers syntax
- Verifies the user was created with `id testuser`
- All privileged operations require confirmation
- Backups created for any modified system files

**Pass criteria:**
- [ ] Research gate triggered
- [ ] User created with home directory
- [ ] User in wheel group
- [ ] Passwordless sudo configured correctly
- [ ] Sudoers syntax validated (not just blindly written)
- [ ] `/etc/sudoers` or `/etc/sudoers.d/` backup created
- [ ] Verification step run (id, sudo test)
- [ ] Memory records the change

---

### Phase 0b: Install lx for testuser & Switch User
**Tools tested:** N/A (manual setup step)

This is a manual step performed by the test runner (not by lx itself).

**Steps:**
```bash
# 1. Copy lx binary so testuser can use it
which lx  # should show /usr/local/bin/lx (already global)

# 2. Create lx config for testuser user (AS REDDROP, not root)
su - testuser -c "mkdir -p ~/.config/linx"
# Copy config and secrets
cp /root/.config/linx/config.toml /home/testuser/.config/linx/config.toml
cp /root/.config/linx/secrets.toml /home/testuser/.config/linx/secrets.toml
# Fix ownership (critical — if root owns .config, lx can't write configs)
chown -R testuser:testuser /home/testuser/.config
chmod 600 /home/testuser/.config/linx/secrets.toml

# 3. Verify lx works as testuser
su - testuser -c "lx --version"
su - testuser -c "lx --doctor"

# 4. Switch to testuser for ALL remaining phases
su - testuser
```

**All subsequent phases (1-16) are run as the `testuser` user, not root.**

This tests that:
- lx works correctly as a non-root user with sudo
- File ownership is correct (configs written to testuser's home)
- Privileged operations use sudo (not direct root access)
- The `passwordless_sudo` config flag works correctly

**Pass criteria:**
- [ ] `lx --version` works as testuser
- [ ] `lx --doctor` shows all green as testuser
- [ ] All subsequent phases run as testuser (not root)

---

### Phase 1: System Discovery
**Tools tested:** `get_os_info`, `get_hardware_info`, `read_file`, `run_command`, memory extraction

**Prompt:**
```
Analyse this system — what hardware do I have, what's the current state, and what would I need to set up a full Hyprland desktop environment? Pay attention to the GPU — I need you to recommend the right graphics drivers for this specific hardware.
```

**Expected behaviour:**
- Calls `get_os_info` and `get_hardware_info`
- Reads relevant config files
- Identifies GPU type (virtual, Intel, AMD, NVIDIA, hybrid)
- Recommends appropriate drivers for detected hardware
- Produces a phased plan
- Memory saves system profile

**Pass criteria:**
- [ ] Correct GPU identification
- [ ] Appropriate driver recommendation (not generic)
- [ ] Plan accounts for Wayland compatibility
- [ ] Memory updated with system profile

---

### Phase 2: Display Server & GPU Drivers
**Tools tested:** `install_package`, `web_search`, `lookup_manpage`, research gate, confirmation gate

**Prompt:**
```
Set up Wayland and install the correct GPU drivers for this system's hardware. Configure everything needed for hardware-accelerated rendering, or software rendering if this is a VM.
```

**Expected behaviour:**
- Research gate fires (must check system before installing)
- Looks up driver documentation or searches web
- Installs correct driver packages based on detected GPU:
  - VM: `mesa` only, sets `WLR_RENDERER=pixman`
  - Intel: `mesa` `vulkan-intel` `intel-media-driver`
  - AMD: `mesa` `vulkan-radeon` `libva-mesa-driver`
  - NVIDIA: `nvidia-dkms` `nvidia-utils` `egl-wayland`
- Installs `wayland` `xorg-xwayland`
- Confirms each destructive action

**Pass criteria:**
- [ ] Research gate triggered
- [ ] Correct drivers for detected hardware
- [ ] No generic "install all drivers" approach
- [ ] Confirmation prompts for installs

---

### Phase 3: Hyprland Core
**Tools tested:** `install_package` (multi-package), `write_file` + auto-backup, `manage_service`

**Prompt:**
```
Install Hyprland and create a working configuration. Use macOS-style keybindings — Super key as the primary modifier, Super+Space for launcher, Super+Return for terminal, Super+W to close window, Super+Q to quit app. Configure workspaces 1-9 on Super+number keys.
```

**Expected behaviour:**
- Installs `hyprland` and dependencies
- Creates `~/.config/hypr/hyprland.conf`
- Keybindings follow macOS convention (see table above)
- Configures monitor, input settings
- Sets GPU-appropriate environment variables
- Backup created for any existing config

**Pass criteria:**
- [ ] Hyprland installed
- [ ] Config created with macOS-style bindings
- [ ] Super+Space, Super+Return, Super+W, Super+Q mapped
- [ ] Workspace switching on Super+1-9
- [ ] GPU environment variables set correctly
- [ ] Backup created

---

### Phase 4: Waybar
**Tools tested:** `write_file` (multiple files), `read_file` (checking existing), complex config

**Prompt:**
```
Install and configure waybar with modules for: workspaces, clock, battery (if laptop), network status, volume, CPU, memory, and a system tray. Use a modern dark theme with rounded corners and subtle transparency.
```

**Expected behaviour:**
- Installs `waybar` and dependencies (`pavucontrol` for volume, etc.)
- Creates `~/.config/waybar/config.jsonc`
- Creates `~/.config/waybar/style.css`
- Conditionally includes battery module (laptop vs VM)
- Modifies `hyprland.conf` to auto-start waybar
- Multiple `write_file` calls → multiple backups

**Pass criteria:**
- [ ] Waybar installed
- [ ] Config and CSS created
- [ ] Battery module only on laptops
- [ ] Hyprland config modified (not overwritten)
- [ ] Backups for all written files

---

### Phase 5: Application Launcher
**Tools tested:** `web_search`, `install_package`, `write_file`

**Prompt:**
```
Set up wofi as my application launcher. Dark theme matching waybar, and bind it to Super+Space in Hyprland (macOS Spotlight style). It should show applications and also support running commands.
```

**Expected behaviour:**
- Installs `wofi`
- Creates `~/.config/wofi/config`
- Creates `~/.config/wofi/style.css`
- Modifies `hyprland.conf` (reads existing → backup → write updated)
- Binds Super+Space to launch wofi

**Pass criteria:**
- [ ] Wofi installed and configured
- [ ] Theme matches dark aesthetic
- [ ] Super+Space binding in Hyprland config
- [ ] Existing hyprland.conf read before modification

---

### Phase 6: Notification System
**Tools tested:** `install_package`, `write_file`, keybinding modification

**Prompt:**
```
Set up mako for notifications. Dark theme matching everything else, position top-right, 5 second timeout, max 3 visible notifications. Add Super+N to toggle do-not-disturb mode.
```

**Expected behaviour:**
- Installs `mako`
- Creates `~/.config/mako/config`
- Modifies `hyprland.conf` for mako autostart and DND keybind
- Reads existing hyprland.conf before modifying

**Pass criteria:**
- [ ] Mako installed and configured
- [ ] Consistent dark theme
- [ ] Super+N DND toggle added to Hyprland
- [ ] Hyprland config preserved and extended

---

### Phase 7: Network Management
**Tools tested:** `manage_service`, `install_package`, `write_file`, `run_command_privileged`, `get_service_status`

**Prompt:**
```
Set up NetworkManager with nm-applet for the system tray. Enable it as the default network service, disable any competing services, and make sure it starts on boot. If Wi-Fi hardware exists, make sure it's configured.
```

**Expected behaviour:**
- Research gate fires — checks current network config
- Installs `networkmanager` `network-manager-applet`
- Checks for and disables competing services (`systemd-networkd`, `dhcpcd`)
- Enables and starts NetworkManager
- Adds `nm-applet` to Hyprland autostart
- Checks for Wi-Fi hardware and configures if present

**Pass criteria:**
- [ ] Research gate triggered
- [ ] Competing services identified and disabled
- [ ] NetworkManager enabled and started
- [ ] nm-applet in Hyprland autostart
- [ ] Wi-Fi handled appropriately for hardware

---

### Phase 8: Clipboard & Screenshots
**Tools tested:** `install_package`, `write_file`, keybinding modification

**Prompt:**
```
Set up clipboard management with wl-clipboard and cliphist for clipboard history. Add Super+Shift+V for clipboard history popup. Also set up screenshots with grim and slurp — Super+Shift+S for region screenshot, Super+Shift+A for full screen screenshot. Screenshots should save to ~/Pictures/Screenshots/.
```

**Expected behaviour:**
- Installs `wl-clipboard` `cliphist` `grim` `slurp`
- Configures cliphist in Hyprland (exec-once + keybind)
- Configures screenshot keybinds with grim+slurp
- Creates `~/Pictures/Screenshots/` directory
- Modifies Hyprland config with new keybinds

**Pass criteria:**
- [ ] All clipboard tools installed
- [ ] Super+Shift+V for clipboard history
- [ ] Super+Shift+S and Super+Shift+A for screenshots
- [ ] Screenshots directory created
- [ ] Hyprland config extended correctly

---

### Phase 9: GTK Theming
**Tools tested:** `install_package` (multiple), `write_file` (dotfiles), `run_command`

**Prompt:**
```
Set up a consistent dark theme for all GTK applications. Use Adwaita-dark as the base. Configure proper font rendering with Noto Sans, set up a cursor theme, and make sure GTK2, GTK3, and GTK4 apps all look identical.
```

**Expected behaviour:**
- Installs `noto-fonts` `noto-fonts-cjk` `noto-fonts-emoji` `adwaita-icon-theme` `adwaita-cursors`
- Creates/modifies `~/.config/gtk-3.0/settings.ini`
- Creates/modifies `~/.config/gtk-4.0/settings.ini`
- Creates/modifies `~/.gtkrc-2.0`
- Sets `GTK_THEME`, cursor environment variables in Hyprland
- Configures font rendering (hinting, antialiasing)

**Pass criteria:**
- [ ] Fonts installed
- [ ] All three GTK version configs created
- [ ] Environment variables set in Hyprland
- [ ] Cursor theme configured
- [ ] Font rendering configured

---

### Phase 10: Qt Theming
**Tools tested:** `web_search` (Qt+Wayland is tricky), `install_package`, `write_file`, environment variables

**Prompt:**
```
Make Qt applications match the GTK dark theme. Set up qt5ct and qt6ct, install Kvantum for advanced theming, and configure everything so Qt apps look consistent with GTK apps under Wayland.
```

**Expected behaviour:**
- Researches Qt-on-Wayland theming
- Installs `qt5ct` `qt6ct` `kvantum-qt5` `kvantum`
- Sets `QT_QPA_PLATFORMTHEME=qt5ct` and `QT_QPA_PLATFORM=wayland`
- Creates qt5ct/qt6ct configuration files
- Configures Kvantum with a dark theme
- Modifies Hyprland env config

**Pass criteria:**
- [ ] Qt theming tools installed
- [ ] Environment variables set
- [ ] qt5ct and qt6ct configs created
- [ ] Kvantum configured with dark theme
- [ ] Qt apps will render dark under Wayland

---

### Phase 11: Terminal & Essential Apps
**Tools tested:** `install_package`, `write_file`, complex multi-step

**Prompt:**
```
Install kitty as my terminal emulator. Configure it with the dark theme, Noto Sans Mono font, enable ligatures, and set it as the default terminal in Hyprland on Super+Return. Also install thunar as a file manager on Super+E and firefox as the browser.
```

**Expected behaviour:**
- Installs `kitty` `thunar` `firefox`
- Creates `~/.config/kitty/kitty.conf` with dark theme + font config
- Modifies Hyprland keybindings (Super+Return → kitty, Super+E → thunar)
- Multiple packages + configs in one prompt

**Pass criteria:**
- [ ] All apps installed
- [ ] Kitty configured with dark theme and fonts
- [ ] Keybindings added to Hyprland
- [ ] Existing config preserved

---

### Phase 12: Lock Screen
**Tools tested:** `install_package`, `write_file`, keybinding modification

**Prompt:**
```
Set up swaylock as the screen locker. Dark theme, blurred screenshot background, and bind Super+L to lock (macOS style). Also configure swayidle to auto-lock after 5 minutes of inactivity and suspend after 10 minutes.
```

**Expected behaviour:**
- Installs `swaylock` `swayidle`
- Creates `~/.config/swaylock/config`
- Creates `~/.config/swayidle/config` or adds to Hyprland exec-once
- Adds Super+L keybind
- Configures idle timeouts

**Pass criteria:**
- [ ] Lock screen works
- [ ] Super+L binding
- [ ] Auto-lock on idle
- [ ] Dark themed

---

### Phase 13: Plymouth Boot Splash
**Tools tested:** `install_package`, `run_command_privileged`, `write_file` (privileged), `read_file`, `lookup_manpage`, `web_search`

**Prompt:**
```
Install and configure Plymouth for a smooth boot splash screen. Use a dark theme that matches our desktop aesthetic. Configure the initramfs to include Plymouth, set the kernel parameters for silent boot with splash, and rebuild the initramfs. Make sure it works with SDDM for a seamless boot-to-login transition.
```

**Expected behaviour:**
- Research gate fires — needs to understand boot config before modifying
- Looks up Plymouth setup on Arch (mkinitcpio hooks, kernel params)
- Installs `plymouth` (and optionally `plymouth-theme-*` for extra themes)
- Reads existing `/etc/mkinitcpio.conf` before modifying
- Adds `plymouth` hook to HOOKS array (after `base udev` but before `encrypt`/`filesystems`)
- Reads existing kernel command line (boot loader config)
- Detects boot loader (GRUB vs systemd-boot vs direct EFISTUB)
- For GRUB: modifies `/etc/default/grub` — adds `quiet splash` to `GRUB_CMDLINE_LINUX_DEFAULT`, runs `grub-mkconfig`
- For systemd-boot: modifies the loader entry in `/boot/loader/entries/`
- Sets Plymouth theme: `plymouth-set-default-theme -R <theme>`
- Rebuilds initramfs: `mkinitcpio -P`
- Configures Plymouth-SDDM integration (smooth-transition)
- All privileged operations require sudo/confirmation

**Pass criteria:**
- [ ] Research gate triggered (boot config is destructive territory)
- [ ] Existing mkinitcpio.conf read before modification
- [ ] Plymouth hook added correctly to HOOKS
- [ ] Boot loader detected (not assumed)
- [ ] Kernel params updated with `quiet splash`
- [ ] Initramfs rebuilt
- [ ] Theme set and matches dark aesthetic
- [ ] SDDM transition configured
- [ ] Backups created for all modified boot files

---

### Phase 14: Login Manager (SDDM)
**Tools tested:** `manage_service`, `write_file` (privileged /etc/ paths), `run_command_privileged`

**Prompt:**
```
Set up SDDM as the display manager with a dark theme. Configure it to auto-select Hyprland as the default session. Enable it to start on boot.
```

**Expected behaviour:**
- Installs `sddm`
- Creates/modifies `/etc/sddm.conf` (privileged write)
- Creates Hyprland desktop entry if missing
- Enables `sddm.service`
- Tests privileged file writes + service management together

**Pass criteria:**
- [ ] SDDM installed and enabled
- [ ] Default session set to Hyprland
- [ ] Dark theme applied
- [ ] Service enabled for boot

---

### Phase 15: Verification & Summary
**Tools tested:** `read_file`, `run_command`, `get_service_status`, memory

**Prompt:**
```
Review everything we've configured. Check all config files are syntactically correct, all services are enabled, all keybindings are mapped correctly, and the theming is consistent. Show me a complete summary of what was installed and changed.
```

**Expected behaviour:**
- Reads back all config files
- Checks all enabled services
- Verifies keybinding consistency
- Produces a comprehensive summary
- Memory should contain full history

**Pass criteria:**
- [ ] All configs validated
- [ ] All services verified
- [ ] Summary is accurate
- [ ] No missing pieces identified

---

### Phase 16: Rollback Test
**Tools tested:** `--rollback`, `--memory`, `--doctor`

**CLI commands (not prompts):**
```bash
lx --doctor                 # Full health check
lx --memory                 # Show full memory
lx --rollback --last 20     # List recent backups
# Select one backup to restore, verify it works
# Re-apply the change via lx prompt
```

**Pass criteria:**
- [ ] Doctor shows all green
- [ ] Memory contains complete session history
- [ ] Rollback lists all file changes
- [ ] Restore works correctly
- [ ] Re-applying the change works

---

## Scoring

| Category | Weight | What to evaluate |
|----------|--------|------------------|
| Tool calling accuracy | 20% | Right tools, right order, right arguments |
| Research before action | 15% | Does it look before it leaps? |
| Config correctness | 20% | Syntactically valid, functional configs |
| Hardware adaptation | 10% | Different drivers for different GPUs |
| Modification safety | 10% | Reads before writing, backups created |
| Error recovery | 10% | Adapts when something fails |
| Memory & continuity | 5% | Remembers across phases |
| Theming consistency | 5% | Same dark theme across all components |
| macOS keybind compliance | 5% | Follows the convention table |

## Test Matrix

Run on both models and both hardware profiles:

| | Qwen 3.5 (local) | GPT-5.4 (cloud) |
|---|---|---|
| **QEMU VM (headless)** | Full run | Full run |
| **Laptop (real GPU)** | Full run | Full run |

Use `lx --model gpt-5.4` for cloud runs.

## Reset Procedure

Before each test run:
1. Restore VM/system to fresh Arch base install
2. Install prerequisites: `pacman -S --noconfirm git go tmux`
3. Clone and build: `git clone <repo> && cd linx && make install`
4. Create root config + secrets: `~/.config/linx/config.toml` and `secrets.toml`
5. Verify clean state: `lx --doctor`
6. Run Phase 0 as root (create testuser user with NOPASSWD sudo)
7. Run Phase 0b: copy config to testuser, switch user
8. Run Phases 1-16 as testuser user
9. Both users share the same `lx` binary at `/usr/local/bin/lx`

### Important Notes
- Phase 0 (user creation) runs as **root**
- Phase 0b (setup + switch) is a **manual step**
- Phases 1-16 run as **testuser** (non-root with sudo)
- This tests the real-world scenario: an admin creates a user, then the user configures their own desktop
- `confirm_destructive` should be set to `false` for automated testing, or use TUI mode for interactive testing
