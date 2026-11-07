# Steps catalog

**This file is the source of truth for behavior.** Code follows it. When behavior changes, update this file in the same change (rule 11).

Steps run in the order listed. Every step and sub-option defaults to **on** in the wizard (options are in [options-reference.md](options-reference.md)). "(verify)" marks a fact not yet confirmed against a current primary source. Code may not rely on it until [debian13-notes.md](debian13-notes.md) marks it **V** (see [review-process.md](review-process.md)).

**The Debian installer owns the desktop (GNOME or none), the SSH server and blends.** kiln never installs or removes them. It detects them in `preflight`, and steps that depend on them are "not applicable" when they're absent. "GNOME detected" means `gnome-shell` and `gdm3` are installed. "SSH server detected" means `openssh-server` is installed.

Two steps aren't in the original brief's list: `kernel` (5b), split out so it can be toggled on its own (D13), and `network-migration` (6, D4). The old `desktop` step was removed (D29).

**Risky** marks a step that uses the verify-then-revert pattern from [architecture.md](architecture.md#risky-changes-verify-then-revert-on-failure).

Each entry has: **ID · Category · Depends on**, then **Purpose**, **Applies**, **Options**, **Actions**, **Check (already done when)**, **Gotchas**.

All apt operations use the `apt` helper: non-interactive, confold, lock wait (see architecture). "Install X" means `AptInstall`, which is a no-op when X is already installed.

---

## 0. preflight

**ID** `preflight` · base · always runs, can't be skipped

**Purpose:** make sure it's safe and possible to continue, and gather Facts.

**Actions (read-only, except the inhibitor):**
- Root check. If not root, stop with "Run with sudo, or as root via `su -`". If started via plain `su` (PATH lacks `/usr/sbin`), prepend `/usr/local/sbin:/usr/sbin:/sbin` to kiln's own PATH and note it.
- OS check: `/etc/os-release` `ID=debian` and `VERSION_ID="13"`, and `dpkg --print-architecture` = `amd64`. Anything else stops the run.
- Virtualization: `systemd-detect-virt`. A VM gets a warning only. A container stops the run ("kiln sets up real machines, not containers").
- Interrupted change: if `state.json` has a `pending` entry, the wizard's revert prompt runs before anything else (see architecture).
- Internet: HTTPS HEAD to `deb.debian.org` and `github.com` (5 s timeout each), plus a DNS check. Failure stops the run, with what to check (cable, Wi-Fi, DNS).
- **Installer choices:**
  - GNOME detected (`gnome-shell` + `gdm3` installed; also report `task-gnome-desktop`);
  - SSH server detected (`openssh-server` installed and `ssh.service` enabled);
  - installed `task-*` packages and any Debian blend (`education-*`, `med-*`, etc.), reported for information only;
  - the default systemd target.
- **Hardware facts:**
  - DMI vendor, product, family and chassis type;
  - CPU vendor, model and cores, and RAM;
  - GPUs (lspci classes 0300/0302/0380, vendor IDs 1002/10de/8086);
  - Wi-Fi chip (PCI/USB IDs and driver);
  - Secure Boot (`mokutil --sb-state`, or the EFI variable);
  - batteries (`/sys/class/power_supply/*` with type=Battery and scope≠Device) and the AC adapter;
  - `platform_profile` and its choices, amd-pstate status, and whether thinkpad_acpi or hp-wmi is present.
- **Network facts:**
  - NICs, physical vs virtual;
  - IPv4/IPv6 addresses, LAN subnets and the default route;
  - whether each NIC is managed by ifupdown, NetworkManager, systemd-networkd or nothing.
- **Session:** the target user, and whether kiln runs inside an SSH session (`SSH_CONNECTION`). An SSH session gets a warning only.
- Start the sleep inhibitor for the whole run.

**Check:** n/a (always runs). **Failure** stops the run (the only step that does).

---

## 1. sudo

**ID** `sudo` · base · depends: preflight

**Purpose:** give the target user passwordless sudo.

**Options:** `sudo.nopasswd`.

**Actions:**
1. Install `sudo` if missing. Verified: debian-installer installs sudo and adds the first user to `sudo` only when the root password is left empty. With a root password, the user isn't in the group, and sudo is usually absent.
2. `UserAddGroup(user, sudo)` (via `adduser <user> sudo` or `usermod -aG sudo`).
3. File name: `/etc/sudoers.d/90-<sanitized>-nopasswd`. sudoers.d skips names that contain `.` or end in `~`. Sanitize to `[a-z0-9_-]` and replace other characters with `_`.
4. Content `<user> ALL=(ALL:ALL) NOPASSWD: ALL`. Write it to a temp file, validate with `visudo -cf <tmp>`, then install it 0440 root:root through the managed writer. Never install a file that fails validation.

**Check:** sudo installed, user in group `sudo`, and the file exists with identical content and mode 0440.

**Gotchas:** group membership only takes effect at the next login, so note "log out and back in". Keep this file even when SSH goes key-only. This plus password SSH on the internet is exactly the risk the ssh step warns about.

---

## 2. apt-sources

**ID** `apt-sources` · base · depends: preflight

**Purpose:** deb822 sources with the right components, with no cdrom entries, and with backports.

**Options:** `apt.components`, `apt.backports`.

**Actions:**
1. If `/etc/apt/sources.list` or `sources.list.d/*.list` has active lines: run `apt modernize-sources` (APT 3.0.x, verified from source). It converts them to `.sources` (main-file entries go to `sources.list.d/<origin>.sources` or `moved-from-main.sources`) and renames the originals to `*.list.bak`. It needs a "yes" answer, so feed `y` on stdin (answering N makes it a dry run). It keeps only the `signed-by`/`trusted` options. If it fails, convert in Go.
2. Disable any `cdrom:` entry (`deb cdrom:` in `.list` files, `URIs: cdrom:` in `.sources`) by commenting out the line or setting `Enabled: no`.
3. Write `/etc/apt/sources.list.d/debian.sources` with three stanzas:
   - `Types: deb`, `URIs: http://deb.debian.org/debian`, `Suites: trixie trixie-updates`, `Components: main contrib non-free non-free-firmware`, `Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp` (the release notes say `.pgp`; the `.gpg` symlink will go away).
   - Security: `URIs: http://security.debian.org/debian-security`, `Suites: trixie-security`, same components and keyring.
   - Backports (if enabled): `URIs: http://deb.debian.org/debian`, `Suites: trixie-backports`, same components. Backports have a low default pin, so nothing comes from them unless a step asks.
   
   The `mirrors` step later changes only the first and third `URIs:` lines.
4. Disable duplicates in other `.sources` files that cover the same suites, such as `moved-from-main.sources` or `debian.sources` from modernize-sources (set `Enabled: no` or remove it after a backup), so apt doesn't warn about duplicate targets. kiln's `debian.sources` is the one owner of Debian archive entries.
5. `AptUpdate`.

**Check:** `debian.sources` parses to the wanted stanzas (URIs may be the mirror+file form), there are no active cdrom entries, there are no duplicate trixie entries, and `sources.list` has no active lines.

**Gotchas:** deb-src isn't needed. The `.kiln-bak` backup of the original `sources.list` is kept. `apt update` failing after this step → restore the backups and fail with the apt error text.

---

## 3. mirrors

**ID** `mirrors` · base · depends: apt-sources

**Purpose:** fastest download **throughput** (not latency), with automatic failover.

**Options:** `mirrors.*`. Subcommands: `kiln mirrors bench [--apply]`, `kiln mirrors reset`.

**Actions:**
1. Candidates: parse the Debian mirror masterlist (`https://mirror-master.debian.org/status/Mirrors.masterlist`, verified: about 370 RFC822-style stanzas separated by blank lines, with `Site:`, `Country:` (e.g. `DE Germany`), `Type:`, `Archive-architecture:`, `Archive-http:` (path such as `/debian/`) and more). Keep mirrors that carry amd64 over HTTP/HTTPS. Pick mirrors in the configured country (plus neighbors), add `worldwide_sample` random others, and add `deb.debian.org`. `country: auto` maps the configured timezone (`system.timezone`, else `/etc/timezone` / `timedatectl`) to a country code via the zone1970.tab table compiled into kiln (Asia/Kolkata → IN). Neighbors come from a small built-in region table. kiln never calls a geo-IP service (decision D21). Unknown timezone → worldwide sample + deb.debian.org only.
2. Freshness: fetch each candidate's `dists/trixie/InRelease` and parse `Date:`. Drop any mirror older than deb.debian.org's. Secondary signal: `<archive>/project/trace/master`, whose first line is a date followed by `Archive serial:` and `Date:`.
3. Throughput: test **sequentially**. `GET dists/trixie/main/Contents-amd64.gz` with `Range: bytes=0-<test_bytes>` and a `test_timeout` cap. Score is bytes/s, and the first 0.5 s is ignored as TCP warm-up. A mirror with an error or a short read gets score 0.
4. Write `/etc/apt/mirrors/debian.list` with the top `mirrors.top` mirrors, one URI per line, each followed by a **TAB** and `priority:N` (1 = best; apt tries lower numbers first). Add `http://deb.debian.org/debian<TAB>priority:99` last. Lines without priority are tried last in random order, so always set one. `#` comments are allowed (header goes there). Format verified in trixie's `apt-transport-mirror(1)`.
5. Change `debian.sources` URIs for main + backports to `mirror+file:/etc/apt/mirrors/debian.list`. Leave security alone.
6. Drop-in `/etc/apt/apt.conf.d/80kiln-acquire`: `Acquire::Retries "5";` (exists in apt.conf(5), with `::Delay` sub-options). Leave `Acquire::Queue-Mode` at its default (`host`, one queue per host, which is what spreads load across mirrors). Leave `Acquire::http::Pipeline-Depth` (documented in apt-transport-http(1), default 10) alone.
7. Verify: `apt update`. On failure, restore the previous `debian.sources` and fail with "Mirrors couldn't be used, kept deb.debian.org".

**Check:** the mirrorlist exists, is younger than 30 days, all its mirrors are fresh, and the sources point at it. (Re-benchmarking is not part of Check; use `kiln mirrors bench --apply` manually.)

**Gotchas:** apt spreads downloads across the listed mirrors, so listing several gives parallel bandwidth. Rejected: netselect-apt (ranks by latency), apt-fast/aria2 (extra deps, bypasses apt's verification flow, and the mirror method already parallelizes across hosts).

---

## 4. upgrade

**ID** `upgrade` · base · depends: apt-sources

**Actions:** `AptUpdate`, then `apt-get full-upgrade -y` (with the apt helper options). Before and after, compare `/boot/vmlinuz-*` and the running kernel (`uname -r`). If a newer kernel is installed or `/run/reboot-required` exists → `RebootNeeded("new kernel <ver>")`.

**Check:** `apt-get -s full-upgrade` shows 0 upgraded, 0 newly installed and 0 to remove (after an update within this run).

---

## 5. firmware-packages

**ID** `firmware-packages` · hardware · depends: apt-sources

**Applies:** always (containers are refused in preflight).

**Actions:** install
- `amd64-microcode` if the CPU vendor is AMD, `intel-microcode` if Intel;
- `firmware-amd-graphics` if there's an AMD GPU; `firmware-misc-nonfree` always (covers many devices, including NVIDIA GSP and Intel bits);
- `firmware-realtek` if any Realtek NIC/Wi-Fi/Bluetooth;
- Wi-Fi by driver/vendor: Intel `iwlwifi` → `firmware-iwlwifi`; Qualcomm Atheros `ath*` → `firmware-atheros`; MediaTek `mt7*` → `firmware-mediatek`; Broadcom `brcmfmac` → `firmware-brcm80211`. All of these are in non-free-firmware.

Reboot note if microcode changed.

**Check:** all the selected packages are installed.

---

## 5b. kernel

**ID** `kernel` · hardware · depends: apt-sources, firmware-packages · option `kernel.backports`

**Applies:** backports enabled. Otherwise "not applicable: backports disabled".

**Purpose:** a newer kernel for newer laptops, with the stable 6.12 LTS kernel kept as a boot fallback.

**Actions:** `AptInstall{linux-image-amd64, Suite: trixie-backports}` (7.2.x in backports today, versus 6.12.x in trixie), plus `linux-headers-amd64` from backports when an NVIDIA GPU is present (DKMS). Also install the matching `firmware-*` packages from backports if the kernel needs newer firmware (verify whether that's needed). The versioned stable `linux-image-6.12.*` package stays installed, so GRUB keeps it as an entry. Mark it manually installed (`apt-mark manual`) so `autoremove` never removes it. Reboot note: "new kernel <ver>".

**Stable fallback tracking** (`kernel.track_stable_fallback`, default on, decision D17): the newest `linux-image-6.12.*-amd64` (and its headers when NVIDIA is present) available from `trixie`/`trixie-security` is installed and marked manual. Older stable 6.12 images are left for `apt autoremove` once they're no longer the newest two. unattended-upgrades won't install new **package names** (an ABI bump is a new name), so kiln installs `kiln-kernel-fallback.timer` (weekly, after `apt-daily-upgrade.service`), which runs `kiln kernel fallback` and does the same. Reboot note only when the backports kernel changes, not when the fallback does.

**Check:** a backports `linux-image-*` is installed, the newest stable-suite `linux-image-6.12.*-amd64` is installed and marked manual, and the timer is enabled.

**Gotchas:** once the `linux-image-amd64` metapackage comes from backports, it tracks backports. New stable-suite kernel ABI versions then stop arriving automatically, so the fallback kernel slowly ages. Backports kernels do get security fixes, but with less guarantee. kiln handles this with the stable fallback tracking above.

---

## 6. network-migration

**ID** `network-migration` · network · depends: preflight · **risky**

**Purpose:** make NetworkManager the only network owner, so Cockpit's networking page works and nothing fights over interfaces.

**Applies:** at least one physical interface is configured in `/etc/network/interfaces` (or `interfaces.d`). Otherwise "not applicable: NetworkManager already manages networking". On GNOME installs this is expected to be not applicable, because the installer hands networking to NetworkManager when the desktop task brings it in (verify the debian-installer netcfg behavior). In practice this step matters on machines installed without a desktop.

**Options:** move interfaces to NetworkManager (on), include Wi-Fi (on).

**Actions (one interface at a time):**
1. Install `network-manager` if it's missing.
2. Read the current config from `interfaces` plus the live `ip -j addr` / `ip -j route` and `/etc/resolv.conf`: DHCP or static (address, gateway, DNS), plus the MAC.
3. Create the NM profile `kiln-<iface>` with `nmcli connection add type ethernet ifname <iface> ...`, matching the live config exactly, with `autoconnect yes`, and not yet activated. For DHCP, read the client ID the current client used (trixie's ifupdown uses `dhcpcd-base` on new installs, older ones `isc-dhcp-client`, so read whichever lease file exists) and set `ipv4.dhcp-client-id` to match, so the router hands back the same address (which matters later for port forwarding). If it can't be determined, use `mac` and note that the IP may change.
4. Wi-Fi stanzas (`wpa-ssid`/`wpa-psk`, or `wpa-conf` pointing to a wpa_supplicant file), when "include Wi-Fi" is on: an NM `wifi` profile with the same SSID and PSK (`802-11-wireless-security.psk`, stored root-only by NM). If the PSK can't be read, that interface stays on ifupdown with a note.
5. Record `pending` in `state.json`, with the `interfaces` backup and the profile name.
6. Comment out that interface's stanzas in `interfaces` (keep `lo`), then `ifdown <iface>` → `nmcli device set <iface> managed yes` → `nmcli connection up kiln-<iface>`.
7. Check locally: the link is up, it has an address, the default route is present, DNS resolves, and HTTPS to `deb.debian.org` works (30 s budget).
   - If the check passes, clear `pending`. If the address changed, show the new IP on the run screen and in the summary.
   - If the check fails, revert: restore `interfaces`, delete `kiln-<iface>`, run `nmcli device set <iface> managed no` and `ifup <iface>`, clear `pending`, and record `failed`.
8. Disable `networking.service` only after every interface has been migrated.

**Check:** no physical interfaces in `interfaces`, every physical NIC is managed by NM, and `networking.service` is disabled.

**Gotchas:** later steps need the network, so a failed migration that couldn't be reverted stops the network-dependent steps (they're skipped as dependents), and the summary gives manual recovery instructions: restore `/etc/network/interfaces.kiln-bak` and run `systemctl restart networking`.

---

## 7. multimedia

**ID** `multimedia` · media · depends: apt-sources. The GStreamer parts apply only when GNOME is detected. The rest applies everywhere.

**Actions:**
- Codecs: `libavcodec-extra`, `gstreamer1.0-plugins-good`, `gstreamer1.0-plugins-bad`, `gstreamer1.0-plugins-ugly`, `gstreamer1.0-libav`.
- DVD: preseed `libdvd-pkg libdvd-pkg/first-install note` and `libdvd-pkg/post-invoke_hook-install boolean true` (verify the question names), install `libdvd-pkg`, then `dpkg-reconfigure -f noninteractive libdvd-pkg` to build libdvdcss.
- VA-API: AMD → `mesa-va-drivers`, `vainfo`. Intel graphics → `intel-media-va-driver-non-free` (non-free, Gen8+), falling back to `intel-media-va-driver` (main) if non-free is disabled.
- NVIDIA (`multimedia.nvidia`), only if an NVIDIA GPU is detected: install `linux-headers-amd64` (from the same suite as the target kernel) and `nvidia-driver` (550.x, **non-free**, DKMS). With Secure Boot **on**: DKMS signs with the MOK key in `/var/lib/dkms/mok.key` (verify). Run `mokutil --import` with a generated one-time password shown on screen, and add a summary note: "At next boot, choose *Enroll MOK* and enter <password>". Never disable Secure Boot. If enrollment can't be prepared, skip NVIDIA with a note instead of risking an unbootable GPU.

**Check:** all the selected packages are installed and libdvdcss is present (`/usr/lib/x86_64-linux-gnu/libdvdcss.so.2`, verify).

---

## 8. repos

**ID** `repos` · apps · depends: apt-sources

**Actions:**
1. Install `extrepo`. For each chosen extrepo repo (repos whose packages need a desktop, such as `google_chrome`, apply only when GNOME is detected), `extrepo enable <name>`. extrepo blocks non-free repos by default, so enable `contrib`/`non-free` policies in `/etc/extrepo/config.yaml` when a listed repo needs them (Chrome is non-free; verify). Then install the repo's package (e.g. `google-chrome-stable`).
2. Flatpak (only when GNOME is detected; otherwise "not applicable: no GNOME desktop"): install `flatpak` and `gnome-software-plugin-flatpak`. Then `flatpak remote-add --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo` (system-wide).

**Check:** the extrepo sources exist and are enabled, the packages are installed, and `flatpak remotes --system` lists flathub.

---

## 9. devtools

**ID** `devtools` · dev · depends: apt-sources

**Actions:** install `build-essential git git-lfs gh curl wget jq vim neovim tmux htop btop ripgrep fd-find fzf bat tree unzip zip rsync lsof strace bind9-dnsutils iproute2 traceroute mtr-tiny netcat-openbsd cmake ninja-build pkgconf libssl-dev shellcheck bash-completion man-db`. Use `pkgconf`: in trixie `pkg-config` is only a transitional package for it. All the others were verified in trixie main. `gh` is **not** taken from Debian (2.46 is old). With `devtools.gh_source: github` (default, decision D18), add the GitHub CLI repo first: keyring `https://cli.github.com/packages/githubcli-archive-keyring.gpg` → `/etc/apt/keyrings/githubcli-archive-keyring.gpg` (0644), and `/etc/apt/sources.list.d/github-cli.sources` with `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main`, `Architectures: amd64`, `Signed-By:` that keyring (verify). Then install `gh` from it. With `debian`, use trixie's package. A missing package is logged and noted, not a step failure. As the user, create `~/.local/bin/fd → /usr/bin/fdfind` and `~/.local/bin/bat → /usr/bin/batcat`, and run `git lfs install --skip-repo` as the user.

**Check:** all available packages are installed and the symlinks point correctly.

---

## 10. system

**ID** `system` · base · depends: preflight

**Actions:**
- Hostname (if set): `hostnamectl set-hostname`, and update the `127.0.1.1` line in `/etc/hosts`.
- Timezone (if set): `timedatectl set-timezone`.
- Time sync: `systemd-timesyncd` (Debian's default; see decisions), enabled + `timedatectl set-ntp true`. With `chrony`, install chrony. Both `Provides/Conflicts/Replaces: time-daemon`, so apt removes timesyncd automatically.
- journald: drop-in `/etc/systemd/journald.conf.d/50-kiln.conf` with `SystemMaxUse=500M`, then restart `systemd-journald`.
- inotify: read the current `fs.inotify.max_user_watches` / `max_user_instances`, write `/etc/sysctl.d/60-kiln-inotify.conf` with max(current, configured), and `sysctl --system`. Never lower a value.
- zram: `systemd-zram-generator` (see decisions). Debian's package already ships `/usr/lib/systemd/zram-generator.conf` enabling `[zram0]` at `min(ram / 2, 4096)`. kiln writes `/etc/systemd/zram-generator.conf` with `[zram0] zram-size = min(ram / 2, 4096)` and `compression-algorithm = zstd`, then runs `systemctl daemon-reload` + `systemctl restart systemd-zram-setup@zram0`. Done on all machines; the size scales with RAM, and it matters most on the 8 GB HP. Never install `zram-tools` alongside it, and remove it if present.

**Check:** each item matches the desired state.

---

## 11. power

**ID** `power` · power · depends: preflight · option `power.manager`

**Applies:** always.

**Actions:**
- `systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target suspend-then-hibernate.target`.
- logind drop-in `/etc/systemd/logind.conf.d/50-kiln-always-on.conf`: `HandleLidSwitch=ignore`, `HandleLidSwitchExternalPower=ignore`, `HandleLidSwitchDocked=ignore`, `HandleSuspendKey=ignore`, `HandleHibernateKey=ignore`, `IdleAction=ignore`. Then `systemctl reload systemd-logind` (restarting it kills desktop sessions; verify that reload is enough). The logind drop-in also applies on machines without a desktop, because the lid switch is handled by logind either way.
- GNOME (only when GNOME is detected):
  - User sessions: dconf system db. Debian ships **no** `/etc/dconf/profile/user` (verified), so create it with `user-db:user` and `system-db:local` (if one exists, add `system-db:local` only if it's missing). Write `/etc/dconf/db/local.d/50-kiln-power` with `[org/gnome/settings-daemon/plugins/power] sleep-inactive-ac-type='nothing'`, `sleep-inactive-battery-type='nothing'`, then `dconf update`. Screen blanking is left alone; only suspend matters. With `power.lock_gnome_settings` (default on, decision D23), also write `/etc/dconf/db/local.d/locks/50-kiln-power` listing `/org/gnome/settings-daemon/plugins/power/sleep-inactive-ac-type` and `/org/gnome/settings-daemon/plugins/power/sleep-inactive-battery-type`, so GNOME Settings can't turn automatic suspend back on.
  - Greeter: `/etc/gdm3/greeter.dconf-defaults` (verified: ucf-managed, copied from `/usr/share/gdm/greeter.dconf-defaults`, three-way merged on upgrade, and symlinked into GDM's dconf db). kiln edits **only** the same two keys under `[org/gnome/settings-daemon/plugins/power]` in place (key-level edit with backup, not a whole-file rewrite, so ucf merges stay clean). Then `dpkg-reconfigure gdm3` or the README-documented gdm3 reload at next boot (never restart gdm3 mid-run, because it kills the session).
- Power manager: with GNOME, `power-profiles-daemon` (pulled in by GNOME) set with `powerprofilesctl set performance`. Without GNOME: `tuned` with `tuned-adm profile throughput-performance`. Never both: if switching, stop, disable and remove the other. Never install TLP. If TLP is present, warn and stop it, since it conflicts.

**Check:** the targets are masked, the drop-in matches, the dconf keys are present, and the active profile is performance / throughput-performance.

**Gotchas:** power-profiles-daemon `Conflicts: tlp, laptop-mode-tools`, and `tuned-ppd` `Conflicts/Provides: power-profiles-daemon` (trixie control files). So "ppd" and "tuned + tuned-ppd" are mutually exclusive at the package level, but plain `tuned` + ppd aren't, and kiln must enforce the one-owner rule itself. ppd drives `platform_profile` and AMD EPP, so `thermal` defers to it. ppd's profile persists across reboots (state file under `/var/lib/power-profiles-daemon/`, verify).

---

## 12. thermal

**ID** `thermal` · power · depends: power · helper `kiln thermal apply`

**Applies:** each sub-part has its own applicability.

**Actions:**
- platform_profile: if `/sys/firmware/acpi/platform_profile_choices` contains `performance`, make sure it's set.
  - **With power-profiles-daemon:** ppd 0.30 itself writes `/sys/firmware/acpi/platform_profile` (and adopts external writes as its own state). So kiln **never writes the sysfs file**. It sets `powerprofilesctl set performance` (persisted by ppd) and only verifies. `kiln-thermal.service` isn't installed.
  - **With tuned (no GNOME):** check whether `throughput-performance` sets platform_profile (tuned's `[acpi] platform_profile` option, verify). If it does, kiln just verifies. If not, install `kiln-thermal.service` (oneshot, `After=tuned.service`, `ExecStart=/usr/local/bin/kiln thermal apply`), which writes `performance`.
  - Kernel ≥ 6.14 also has `/sys/class/platform-profile/*`. The legacy file sets every handler, so keep using it.
- AMD: ppd's amd-pstate driver (only when the status is `active`) sets EPP `performance`, governor `performance` and boost on under the performance profile. kiln reports the `amd_pstate/status` and per-CPU EPP, and notes it if they differ. Don't add kernel cmdline changes.
- ThinkPad (vendor LENOVO and thinkpad_acpi loaded): if `/proc/acpi/ibm/fan` exists, write `/etc/modprobe.d/kiln-thinkpad.conf` with `options thinkpad_acpi fan_control=1`, install `thinkfan`, and write `/etc/thinkfan.yaml` with an aggressive curve: never level 0, ramps from 45 °C, level 7 by ~65 °C, `level full-speed` (or "disengaged") from 70 °C. Sensors come from hwmon (`k10temp` Tctl, `amdgpu` edge, `thinkpad` hwmon). Enable `thinkfan`. If the fan file or usable sensors are missing → skip with "not applicable: this ThinkPad doesn't expose fan control".
- HP: there's no fan control from Linux. Rely on platform_profile, and note "check BIOS for *Fan Always On*".
- Install `lm-sensors`, then `sensors-detect --auto`.

**Check:** the profile is performance, the unit and modprobe file are present, and thinkfan is active (when applicable).

**Gotchas:** `fan_control=1` needs a module reload or reboot, so add a reboot note. Bad thinkfan configs make the service fail to start; validate with `thinkfan -n -c` (verify flag) before enabling.

---

## 13. battery

**ID** `battery` · power · depends: preflight · helpers `kiln battery status|apply|watch`

**Applies:** at least one system battery (`type=Battery` and `scope` not `Device`). Otherwise "not applicable: no battery".

**Goal:** keep the charge below 90%, preferring 80%. The ladder below stops at the first method that works. "Works" means the written value reads back as written.

### Method ladder

**Method 1: standard kernel thresholds** (`/sys/class/power_supply/BAT*/charge_control_end_threshold`, plus `charge_control_start_threshold` if present).
- Stop candidates in order: `80, 85, 75, 70, 60, 50` (anything ≥ `battery.max_stop`, i.e. 90+, is never tried). Start = stop − `start_gap` (75 for 80).
- Write order (thinkpad_acpi rejects start > current stop and stop < current start): when **lowering** (new stop < current start), write start first; when **raising**, write stop first. If a write fails with EINVAL, try the other order once before moving to the next candidate.
- `charge_types` present (Dell ≥ 6.12, IdeaPad ≥ 6.17): Dell needs `Custom` written before the thresholds take effect, and verify it stuck. Writing `Long_Life` is a fixed-mode fallback (method 2) where that's the only choice.
- Vendor ranges (TLP matrix, verified 2026-11 at linrunner.de/tlp/settings/bc-vendors.html): ThinkPad start 0–99 / stop 1–100. Dell start 50–95 / stop 55–100 with start ≤ stop−5 (a BIOS admin password blocks writes, so note it). ASUS is stop only, and some models accept only 40/60/80, which also reset at power-on (so the udev/boot re-apply matters). LG Gram accepts only 80/100. Huawei, System76 and Framework use full ranges. The kernel ABI says drivers may **round** to a supported value, so always compare the read-back to what was written and accept the rounded value only if it's still < 90.
- Log every tried value and its result. Record the accepted pair.

**Method 2: vendor fixed modes** (only if method 1 doesn't exist or doesn't work):
- Lenovo IdeaPad / ThinkBook: `/sys/bus/platform/drivers/ideapad_acpi/VPC2004:*/conservation_mode = 1` (caps around 60% or 80% depending on the model; the actual cap can't be read, so record "conservation mode"). On kernel ≥ 6.17, `charge_types=Long_Life` is the same thing.
- Samsung: `/sys/devices/platform/samsung/battery_life_extender = 1` (80%).
- LG (older): `/sys/devices/platform/lg-laptop/battery_care_limit = 80`.
- Huawei: `/sys/devices/platform/huawei-wmi/charge_control_thresholds = "75 80"`.
- Sony (50/80/100), Toshiba (80/100), MSI (`msi_ec`, specific models, start forced to stop−10): table entries with path, accepted values, read-back.
- The ladder is a data table (`internal/battery/vendors.go`): vendor match (DMI + driver), path glob, value candidates, write order, read-back rule. Adding a vendor is a table row plus its primary-source citation in debian13-notes.md.

**Method 3: software fallback.** If `charge_behaviour` exists and lists `inhibit-charge`: install `kiln-battery-watch.service` (or a 1-minute timer). It reads `capacity`, sets `inhibit-charge` at ≥ 80%, and sets `auto` at ≤ 75% (hysteresis). It's idempotent and logs only on transitions.

**Method 4: nothing works.** The summary says so clearly with the DMI vendor + product, lists what was probed, and suggests BIOS/UEFI options: "Battery Care", "Adaptive Battery Optimizer", "Battery Health Manager", "Primarily AC use", "Charge limit". It may mention that a smart plug driven by battery percentage is a possible idea (not built).

### Persistence

- `kiln-battery.service` (oneshot, `ExecStart=/usr/local/bin/kiln battery apply`) runs at boot.
- udev rule `/etc/udev/rules.d/90-kiln-battery.rules`: on `SUBSYSTEM=="power_supply"` change/add events (AC online changes, battery add), `TAG+="systemd", ENV{SYSTEMD_WANTS}+="kiln-battery.service"`.
- `kiln-battery.timer` (every 15 min) as a safety net. Something else writing the sysfs file (UPower, firmware) doesn't emit a uevent.
- `kiln battery apply` re-runs the ladder starting from the recorded method (stored in `/etc/kiln/state.json` under `battery`) and falls back to the full ladder if that method stops working.

### GNOME "Preserve Battery Health" (UPower)

Verified against trixie (GNOME 48, UPower 1.90.9). UPower's udev rule enables the feature only when `charge_control_start_threshold` exists. The limits come from hwdb `CHARGE_LIMIT` (default `75,80`). The on/off flag lives in a `charging-threshold-status` file in UPower's state dir, not in `UPower.conf`. **Enabling** writes start then end, which can fail on ThinkPads because of write order. **Disabling writes 0/100** and clobbers our limits.

**Owner: kiln.** So that the two can't disagree:
1. Write `/etc/udev/hwdb.d/61-kiln-battery.hwdb` setting `CHARGE_LIMIT=<accepted start>,<accepted stop>` for this machine's battery, then run `systemd-hwdb update` and `udevadm trigger -s power_supply`. If someone turns GNOME's toggle on, it applies the same values.
2. The summary note says: "Battery limit is managed by kiln. Leave GNOME's *Preserve Battery Health* switch alone; turning it **off** resets the limit to 100% until kiln re-applies it (within 15 minutes)."
3. `kiln battery status` reports UPower's `ChargeThresholdEnabled` (D-Bus) alongside the sysfs values.

### Commands

`kiln battery status` prints the detected batteries, available interfaces, current values, accepted values (from the last apply), the active method, and the owner. `kiln battery apply` runs the ladder and prints the result.

### Hardware expectations (fact status R: detected at run time, never hardcoded)

- ThinkPad E14 Gen 5 (AMD 7530U): thinkpad_acpi should expose start/end → method 1 with 75/80.
- HP Envy x360 13 (4500U): mainline hp-wmi exposes only `charge_behaviour`, and only for two Spectre x360 boards (DMI 8C15/8C17), in kernels newer than trixie's 6.12. The battery driver (ACPI `battery`) exposes no thresholds on consumer HPs. Expect methods 1–3 to fail and the summary to point at the BIOS ("Battery Health Manager" / "Adaptive Battery Optimizer"). This isn't assumed: the ladder probes at run time, and `kiln battery status` shows exactly what the hardware exposes (fact status **R**).

**Check:** the recorded method's values read back as desired, and the service and udev rule are installed.

---

## 14. firewall

**ID** `firewall` · security · depends: preflight · **risky** · option: backend (default firewalld)

**Actions (firewalld):**
1. Install `firewalld` (it uses the nftables backend).
2. If an SSH server is detected, the default zone (`public`) allows `ssh` (and the custom SSH port if set) **before** firewalld is enabled. That keeps later remote access working. Docker is installed after this step (catalog order), and Docker recreates its rules on a firewalld reload (≥ 28.1).
3. LAN subnets: auto-detect private IPv4 (RFC1918: 10/8, 172.16/12, 192.168/16) and IPv6 ULA (fc00::/7) subnets on physical NICs only. Global IPv6 prefixes are never treated as LAN (decision D20). Skip `lo docker* br-* veth* virbr* tun* tap* wg* tailscale*`.
4. Create zone `kiln-lan` with `sources` = the LAN subnets, and services `cockpit` + `rdp` (3389/tcp, only if GNOME is detected) + `ssh` (only if an SSH server is detected). Re-runs replace the zone's sources and services entirely (no stale rules).
5. No LAN detected → don't create `kiln-lan`, keep Cockpit/RDP closed, and note that Cockpit is then only reachable locally (`https://localhost:9090`) or through an SSH tunnel (`ssh -L 9090:localhost:9090 <user>@<host>`).
6. Verify, then revert on failure: record `pending`, apply to **runtime** only, then check that firewalld is running and the zones and services read back as expected (`firewall-cmd --get-active-zones`, `--zone=... --list-all`). If that passes, run `--runtime-to-permanent` and clear `pending`. If it fails, run `firewall-cmd --reload` (back to the previous permanent config), or stop and disable firewalld if this step enabled it, then record `failed`.

**Check:** firewalld is active, the default zone allows SSH (when an SSH server is detected), and `kiln-lan` has exactly the expected sources and services.

**Gotchas:**
- Docker-published ports (`-p`) bypass zone rules while firewalld's `StrictForwardPorts=no` (the default). Docker puts its bridges in a `docker` zone with a `docker-forwarding` policy. Note: "bind private services to 127.0.0.1 or a LAN IP".
- Option `firewall.strict_docker_ports` (default **off**, decision D16; firewalld ≥ 2.3.0, trixie has 2.3.1) sets `StrictForwardPorts=yes`, so published ports stay blocked until you add a firewalld forward-port. When it's off, the summary always carries the `-p` warning.
- Docker ≥ 28.3.3 is needed for a fix where a firewalld reload made loopback-bound published ports reachable from the LAN. Trixie's own `docker.io` is 26.1.5, which is another reason to use Docker's repo.
- The nftables alternative is a single managed `/etc/nftables.conf` with the same rules, the `nftables.service` enabled, and the same verify-then-revert. Coexisting with Docker's own rules is the hard part (Docker's nftables backend is experimental in 29.x).

---

## 15. ssh

**ID** `ssh` · security · depends: firewall (for a custom port) · **risky**

**Purpose:** harden the SSH server that the installer put on the machine, for later remote access. kiln itself is run at the console.

**Applies:** an SSH server was detected (you chose "SSH server" in the installer). Otherwise "not applicable: no SSH server installed (choose 'SSH server' in the Debian installer)". kiln never installs one.

**Actions:**
1. Keys: fetch `https://github.com/<GitHub user>.keys` (HTTPS, size cap, each line validated with `ssh-keygen -l -f`), and add the extra keys from the options. Merge them into `~user/.ssh/authorized_keys` (0600, directory 0700, owned by the user), adding missing keys only and never removing existing ones. Lines get a trailing comment ` kiln:github` or ` kiln:manual`.
2. Drop-in `/etc/ssh/sshd_config.d/10-kiln.conf`. Verified: Debian's `sshd_config` includes `sshd_config.d/*.conf` near the top in lexical order, and the first value wins, so a low number takes precedence. Contents:
   - `PermitRootLogin no`, `MaxAuthTries 4`, `X11Forwarding no`, `KbdInteractiveAuthentication no`;
   - `PasswordAuthentication no` **only if** the user has at least one valid key installed. Otherwise it stays `yes`, with a loud warning on the review screen and in the summary: "passwordless sudo + password SSH on the internet is risky".
3. Port (when it's not 22): `Port <n>` in the drop-in, plus the firewall service or port. Verified: trixie enables `ssh.service`, and `ssh.socket` is shipped but **not enabled**, so `Port` in the drop-in works. If `ssh.socket` was enabled by hand, the port goes in an `ssh.socket` drop-in (`ListenStream=`) instead, which needs `daemon-reload` and `restart ssh.socket`.
4. Verify, then revert on failure: record `pending` and validate with `sshd -t`. Then apply with `systemctl reload ssh`, and check that `ssh` is active, that `sshd -T` shows the expected values, and that something is listening on the port (`ss -ltn`). If any of that fails, restore the previous drop-in, reload, and record `failed`.

**Check:** the keys are present, the drop-in matches, `sshd -T` shows the effective values, and the port is listening.

**Gotchas:** the service is `ssh`, not `sshd`. OpenSSH 10.0 in trixie dropped DSA keys, so `ssh-dss` lines from GitHub are skipped with a note. Router port forwarding is manual, so it's only noted.

---

## 16. fail2ban

**ID** `fail2ban` · security · depends: ssh, firewall

**Applies:** an SSH server was detected, because the only jail is `sshd`. Otherwise "not applicable: no SSH server installed".

**Actions:** install `fail2ban python3-systemd`. Debian's `jail.d/defaults-debian.conf` (fail2ban 1.1.0) already sets `banaction = nftables` and `backend = systemd` for sshd. kiln keeps the nftables ban action under **both** firewall backends: it uses its own `f2b-table`, coexists with firewalld, and survives `firewall-cmd --reload`. Write `/etc/fail2ban/jail.d/50-kiln.local`:
```
[DEFAULT]
backend = systemd
banaction = nftables
banaction_allports = nftables[type=allports]
ignoreip = 127.0.0.1/8 ::1 <LAN subnets>
bantime = 1h
bantime.increment = true
maxretry = 5
[sshd]
enabled = true
port = <ssh ports>
```
Restart fail2ban, then `fail2ban-client ping` and `fail2ban-client status sshd`.

**Check:** the file matches, the service is active, and the jail is running.

**Gotchas:** trixie has no `/var/log/auth.log` by default (no rsyslog), so `backend = systemd` is required, and kiln sets it in `[DEFAULT]` so any future jail inherits it.

---

## 17. docker

**ID** `docker` · apps · depends: apt-sources, firewall

**Actions:**
1. Remove the conflicting packages if present: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc` (list from Docker's Debian install docs, verified).
2. Keyring: download `https://download.docker.com/linux/debian/gpg` to `/etc/apt/keyrings/docker.asc` (0644).
3. `/etc/apt/sources.list.d/docker.sources`: `Types: deb`, `URIs: https://download.docker.com/linux/debian`, `Suites: trixie`, `Components: stable`, `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/docker.asc` (verified: `dists/trixie` exists, amd64).
4. Install `docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`.
5. `/etc/docker/daemon.json` (JSON, no comment header): `{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}`, merged with existing keys. Restart docker.
6. Add the user to `docker`. Run `docker run --rm hello-world` (as root).

**Check:** the packages are installed, daemon.json contains the keys, the user is in the group, and docker is active.

**Notes:** "log out and back in for docker group"; "`-p` published ports bypass host firewall zones, so bind private services to 127.0.0.1". Membership in the docker group equals root.

---

## 18. cockpit

**ID** `cockpit` · apps · depends: apt-sources, firewall, network-migration (soft: works without it, but the networking page needs NM)

**Actions:** install `cockpit` from `trixie-backports` (`-t trixie-backports`, 371 today) if `apt.backports` is on, otherwise from main (337). `cockpit-networkmanager` depends on network-manager and doesn't work with ifupdown-defined interfaces, which is why `network-migration` exists. Never install `cockpit-machines` or `cockpit-podman`, and pass `--no-install-recommends` if recommends would pull them in (verify which recommends it has). Enable `cockpit.socket`.

**Check:** installed (from the expected suite) and `cockpit.socket` is enabled.

**Notes:** `https://<lan-ip>:9090`, who it's open to (the `kiln-lan` subnets, or "closed: use the SSH tunnel"), and that login uses the target user's password (root login is disallowed by Cockpit's default `/etc/cockpit/disallowed-users`).

---

## 19. remote-desktop

**ID** `remote-desktop` · apps · depends: firewall

**Applies:** GNOME detected. Otherwise "not applicable: no GNOME desktop installed".

**Actions:** install `gnome-remote-desktop` 48.x if missing (the GNOME task usually brings it). Use system mode (RDP at the login screen, no user logged in). Verified against the 48.1 source: `--system` must be the **first** argument, and grdctl re-executes itself via `pkexec --user gnome-remote-desktop`. As root, kiln runs the equivalent directly with `runuser -u gnome-remote-desktop -- grdctl ...` (verify against the grd-ctl.c source and polkit rules which of the two works without a prompt).
1. Generate a self-signed TLS key/cert (`openssl req -x509 -newkey rsa:4096 -nodes`) into `~gnome-remote-desktop/.local/share/gnome-remote-desktop/` (home is `/var/lib/gnome-remote-desktop`), owned by and readable only by that user.
2. `grdctl --system rdp set-tls-key <key>`, then `grdctl --system rdp set-tls-cert <crt>`.
3. `grdctl --system rdp set-credentials`, with **no username/password args**: it prompts on stdin, so kiln writes `<user>\n<password>\n` to its stdin. The password never appears in argv or logs.
4. `grdctl --system rdp enable` (in system mode this also enables the unit), then `systemctl enable --now gnome-remote-desktop.service`. The Debian postinst leaves it disabled by default.

The password comes from the wizard: masked, with a confirm field, or "generate one" (shown once, stored in `/etc/kiln/secrets/rdp`, and shown again by `kiln secrets show rdp`).

LAN-only via the firewall's `kiln-lan` zone (`rdp` service).

**Check:** `grdctl --system status` (without `--show-credentials`) shows RDP enabled, the TLS files are set, and the credentials are set. It can't compare the password, so it checks a hash marker kiln keeps next to the secret file.

---

## 20. autoupdates

**ID** `autoupdates` · security · depends: apt-sources

**Actions:** install `unattended-upgrades apt-listchanges needrestart` (all in trixie main). Write `/etc/apt/apt.conf.d/52kiln-unattended-upgrades`: Origins-Pattern for security only (or all, if `scope: all`: `origin=Debian,codename=${distro_codename}` and `-updates`), `Unattended-Upgrade::Automatic-Reboot "false"`. Write `/etc/apt/apt.conf.d/20auto-upgrades` with `APT::Periodic::Update-Package-Lists "1"` and `Unattended-Upgrade "1"`. Write `/etc/needrestart/conf.d/50-kiln.conf` with `$nrconf{restart} = 'a';`. Verified: needrestart 3.11 evaluates `conf.d/*.conf` in sorted order, and its default is `'i'`, which falls back to list-only when non-interactive. The `NEEDRESTART_MODE=a` env var covers kiln's own apt runs.

**Check:** the files match and `unattended-upgrades.service` is enabled.

---

## 21. node

**ID** `node` · dev · depends: devtools · runs as the user

**Actions:** through `fetch.PipedInstaller`: download `https://raw.githubusercontent.com/nvm-sh/nvm/v0.41.0/install.sh` (pinned; wget if curl is missing), run it with `bash` as the user, with `PROFILE=/dev/null` so it doesn't edit `.bashrc` itself (kiln's bashrc.d drop-in sources nvm instead). Then, as the user, run a single `bash` invocation without errexit: `. "$HOME/.nvm/nvm.sh"; nvm install 26; nvm alias default 26; node -v && npm -v`. That script is a fixed constant passed as a file, not interpolated. Write `~/.bashrc.d/50-kiln-nvm.sh`.

**Check:** `~/.nvm/nvm.sh` exists, the nvm default alias resolves to v26.x, and `node -v` reports v26.

**Gotchas:** never source nvm under `set -e`. nvm v0.41.0 and Node 26 are the owner's pinned choices. Don't "upgrade" them without being asked. nvm v0.41.0 is no longer the newest nvm, so the pin is deliberate.

---

## 22. python

**ID** `python` · dev · depends: devtools · user parts run as the user

**Actions:**
1. apt: `python3 python3-venv python3-dev pipx libxml2-dev libxslt1-dev libffi-dev`.
2. uv: **not packaged in trixie** (verified). Install it as the user with `pipx install uv` (pipx is a Debian package, so there's no piped script). The official installer through `fetch.PipedInstaller` (pinned, `UV_NO_MODIFY_PATH=1`) is the fallback option `python.uv_source: installer`.
3. Venv: `uv venv ~/.venvs/automation`, then `uv pip install --python ~/.venvs/automation requests "httpx[http2]" aiohttp tenacity beautifulsoup4 lxml selectolax parsel scrapy playwright selenium pandas pyyaml python-dotenv rich typer ipython`.
4. Playwright browsers: as the user, `~/.venvs/automation/bin/playwright install chromium`. System deps are installed **by kiln via apt**, using a list from `playwright install-deps --dry-run chromium` (verify it prints the apt command), rather than letting Playwright call apt itself.
5. `uv tool install ruff`, `pre-commit`, `httpie` (as the user).
6. Shell: add a one-time `~/.bashrc.d` loop to `~/.bashrc`, guarded by a marker comment:
   ```bash
   # kiln: load ~/.bashrc.d
   for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f
   ```
   Then write `~/.bashrc.d/50-kiln-python.sh`: PATH `~/.local/bin`, and `alias pyauto='. ~/.venvs/automation/bin/activate'`.

**Check:** the venv exists with all the packages (`uv pip list`), the chromium browser is installed, the tools are installed, and the drop-ins are present.

---

## 23. go

**ID** `go` · dev · depends: devtools

**Actions:**
1. Resolve the version: `go.version: latest` → `https://go.dev/dl/?mode=json`, the first entry with `stable: true`, file `kind=archive os=linux arch=amd64` (filename + sha256 from the JSON).
2. Download to `/var/tmp/kiln/`, verify sha256, remove the old `/usr/local/go`, and extract to `/usr/local/go` (owned by root).
3. `/etc/profile.d/kiln-go.sh`: `export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin`.
4. As the user: `go install golang.org/x/tools/gopls@latest github.com/go-delve/delve/cmd/dlv@latest honnef.co/go/tools/cmd/staticcheck@latest golang.org/x/vuln/cmd/govulncheck@latest`.
5. Fallback (`go.fallback_debian_package`): if go.dev can't be reached, install `golang-go` from **trixie-backports** (currently 1.27, versus 1.24 in trixie main), or from main if backports is off, and note the version.

**Check:** `/usr/local/go/bin/go version` matches the resolved version, and the tools exist in `~/go/bin`.

---

## 24. firmware-updates

**ID** `firmware-updates` · hardware · depends: apt-sources

**Applies:** always. UEFI is recommended for most updates.

**Actions:** install `fwupd`. Then `fwupdmgr refresh --force`, `fwupdmgr get-updates` (exit code 2 = nothing to do, verify), and `fwupdmgr update -y --no-reboot-check` (verify the flags). Add a note: "some firmware updates apply at the next reboot", and add a reboot reason if any were staged.

**Check:** fwupd is installed and `get-updates` reports none.

---

## 25. summary

**ID** `summary` · always runs

**Actions:** for each step, show done/unchanged/failed/skipped and why. Show the collected notes. Reboot needed: yes/no + reasons. The wizard offers a reboot and never reboots on its own. Write `/var/log/kiln-summary.txt` (0644, no secrets), the full log at `/var/log/kiln.log` (0600), and `/etc/kiln/state.json` (0600: this run's choices, battery result, power manager; never secrets). A generated secret was already shown once by the remote-desktop step. The summary names its file.
