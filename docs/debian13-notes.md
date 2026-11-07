# Debian 13 "trixie" notes and gotchas

These facts were checked on **2026-11-07** against primary sources. Package versions are trixie point-release versions on that date, so treat them as "at least", not as pins. There are no tests (see [review-process.md](review-process.md)), so primary documentation is the only form of verification. Rows are grouped by step in catalog order; [steps-catalog.md](steps-catalog.md) also cites sources under each step's "Facts used". Statuses:

- **V**: verified against a primary source, which is linked. Code may rely on it.
- **U**: not yet verified. Code must not rely on it. Verify it from primary sources (packages.debian.org, manpages.debian.org, sources.debian.org, release notes, upstream docs) and flip it to V, or redesign so it isn't needed.
- **R**: deliberately not assumed. The code detects it at run time and handles both outcomes, typically hardware behavior that no document can promise. These rows record what we expect, not what the code depends on.

Re-check V facts older than 30 days during `/audit`.

## Shared: apt helper and process model

| Fact | Status | Source |
|---|---|---|
| `/tmp` is a tmpfs (up to 50% of RAM) on new installs. `/tmp` and `/var/tmp` are cleaned periodically by systemd-tmpfiles. | V | [release notes §5.1.6, §5.2.1](https://www.debian.org/releases/trixie/release-notes/issues.en.html) |
| `-o DPkg::Lock::Timeout=<sec>` exists (configure-index, `debsystem.cc`) but isn't in apt-get(8) or apt.conf(5). It waits only on the dpkg `lock-frontend`/`lock`; `/var/lib/apt/lists/lock` and `/var/cache/apt/archives/lock` fail immediately. | V | apt 3.0.3 source (`apt-pkg/deb/debsystem.cc`, `acquire.cc`, `private-install.cc`) |
| `dpkg --audit` always exits 0, so check its output. All-digit entries in `/var/lib/dpkg/updates/` trigger apt's "dpkg was interrupted". Recovery is `dpkg --configure -a`. | V | dpkg 1.22.22 dpkg(1), `src/main/enquiry.c`; apt 3.0.3 `debsystem.cc` |
| needrestart 3.11: a non-empty `NEEDRESTART_SUSPEND` makes the apt hook skip needrestart and keep `/run/needrestart/unpacked` for the next run. Debian's `override_rc` never restarts dbus, display managers (gdm…), systemd-logind, NetworkManager, wpa_supplicant, docker, getty@, user@, apt-daily* or unattended-upgrades. `-b` (batch) never restarts anything; `-r a` restarts. | V | needrestart 3.11-1 `ex/apt/apt-pinvoke`, `ex/needrestart.conf`, `needrestart`, needrestart(1) |
| `systemctl mask --runtime` writes to `/run/systemd/system/` (gone at reboot) and doesn't stop a running unit (use `--now` or `stop`). | V | systemctl(1), systemd 257.13 |
| `packagekit.service` is D-Bus activated. GNOME Software 48.3 checks hourly, refreshes at most daily and downloads updates in the background (`download-updates` defaults to true). PackageKit's apt backend gives up on the dpkg lock after 10 s. | V | packagekit 1.3.1, gnome-software 48.3 sources |
| `systemd-run --scope` as root lands in `system.slice`. Debian builds with `default-kill-user-processes=false`. | V | systemd 257.13 `src/core/unit.c`, `debian/rules`, systemd-run(1), logind.conf(5) |
| `setsid(2)`: "the new session has no controlling terminal". That terminal signals then don't reach the child is derived from tty job control. | V / derived | setsid(2), manpages 6.9.1 |
| A unit with `ExecStart=` and no `Type=` is `Type=simple`, so `systemctl start`/`restart` reports success right after fork, not when the program finishes. Seen on the HP test machine: `systemctl restart ifup@wlo1.service` returned in 52 ms and `ifup` failed 120 ms later. | V | manpages.debian.org/trixie systemd.service(5) `Type=`; field test 2026-11-07 |

## Packages (non-obvious ones)

All are in **main** unless noted. Source: `https://packages.debian.org/trixie/<pkg>` (V).

- **non-free-firmware:** `amd64-microcode`, `intel-microcode`, `firmware-amd-graphics`, `firmware-misc-nonfree`, `firmware-realtek`, `firmware-iwlwifi`, `firmware-atheros`, `firmware-mediatek`, `firmware-brcm80211` (firmware-nonfree 20250410).
- **non-free:** `nvidia-driver` (550.163), `intel-media-va-driver-non-free`. **contrib:** `libdvd-pkg`.
- **Renamed or transitional:** `pkg-config` is a transitional package for **`pkgconf`**, so declare `pkgconf`.
- **Not packaged:** **`uv`**. The CLI isn't in trixie under any name (forky/sid only builds `python3-uv-build`). Install it with `pipx install uv` instead.
- **Versions that matter:** `cockpit` 337 (trixie-backports **371**); `linux-image-amd64` 6.12.x (backports **7.2.x**); `golang-go` 1.24 (backports **1.27**); `gh` 2.46 (old); `docker.io` 26.1.5 (too old; see docker); `fail2ban` 1.1.0; `power-profiles-daemon` 0.30; `tuned`/`tuned-ppd` 2.25.1; `systemd` 257; `openssh` 10.0p1; GNOME 48 (`gdm3` 48.0); `upower` 1.90.9.
- Also verified in main: `task-gnome-desktop`, `libavcodec-extra`, `gstreamer1.0-plugins-{good,bad,ugly}`, `gstreamer1.0-libav`, `mesa-va-drivers`, `vainfo`, `extrepo`, `flatpak`, `gnome-software-plugin-flatpak`, `git-lfs`, `neovim`, `btop`, `ripgrep`, `fd-find`, `fzf`, `bat`, `shellcheck`, `bind9-dnsutils`, `mtr-tiny`, `netcat-openbsd`, `ninja-build`, `systemd-zram-generator`, `zram-tools`, `thinkfan`, `lm-sensors`, `python3-systemd`, `unattended-upgrades`, `apt-listchanges`, `needrestart`, `pipx`, `python3-venv`, `fwupd`, `systemd-timesyncd`, `chrony`.
- The whole devtools list (34 packages, including `build-essential`, `tmux`, `htop`, `tree`, `strace`, `traceroute`, `cmake`, `libssl-dev`, `bash-completion`, `man-db`) and `libxml2-dev`, `libxslt1-dev`, `libffi-dev`, `python3-dev` are in trixie main. (V, qa.debian.org madison s=trixie, 2026-11-07)
- Binary names: `fd-find` 10.2.0 ships `/usr/bin/fdfind`, `bat` 0.25.0 ships `/usr/bin/batcat`. (V, packages.debian.org file lists)

## 0. preflight

| Fact | Status | Source |
|---|---|---|
| Plain `su` keeps the caller's PATH (no `/usr/sbin`). `su -` sets `ENV_SUPATH` (`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`). | V | Debian `login.defs`, su(1) |
| Minimal installs often have no curl, but wget is usually present. | R | install.sh uses whichever of the two exists |
| tasksel 3.81: `task-gnome-desktop` → `gnome-core` (gdm3, gnome-shell); `task-ssh-server` → `openssh-server`; "standard" is Priority standard, not a package. Blends are `task-*-blend` (blendsel 0.10); Debian Edu isn't offered. | V | tasksel 3.81, blendsel 0.10 debian/control |
| `mokutil` is present only via shim-signed. The Secure Boot state can be read from efivar `SecureBoot-8be4df61-…` byte 5. | V | mokutil 0.7.2-1 |
| PCI class `0280` is "Network controller" (Wi-Fi cards); vendor IDs Intel `8086`, MediaTek `14c3`, Realtek `10ec`, Qualcomm Atheros `168c`, Qualcomm Technologies `17cb`, Broadcom `14e4`. rtw88 drivers register as `rtw_<chip>` (the HP's `rtw_8822ce`). | V | pci-ids.ucw.cz pci.ids; field check 2026-11-07 |
| NetworkManager.conf ships `plugins=ifupdown,keyfile` + `[ifupdown] managed=false`, so interfaces in `/etc/network/interfaces` stay unmanaged and NM leaves them alone. | V | [sources: network-manager 1.52.1 debian/NetworkManager.conf](https://sources.debian.org/src/network-manager/1.52.1-1/debian/NetworkManager.conf/), [wiki](https://wiki.debian.org/NetworkManager) |
| `nmcli -t -f DEVICE,STATE device` (facts: which interfaces NetworkManager manages) prints one `<device>:<state>` line per device with no header; `-t -g` prints the same without field names (`wlo1:wifi:connected:kiln-wlo1`). | V | nmcli(1) 1.52.1 (terse mode); field test 2026-11-07 (HP test machine) |
| `systemd-inhibit --what=sleep:idle:handle-lid-switch` is valid, and needed because `LidSwitchIgnoreInhibited=yes` is the default; a non-root caller isn't allowed `handle-lid-switch`. A `block` shutdown inhibitor refuses every logind reboot/poweroff caller, root included (`systemctl reboot` and `reboot` go through logind first); `-i`/`--check-inhibitors=no` and `--force` bypass it. | V | systemd 257.13 systemd-inhibit(1), logind.conf(5), login1 polkit policy, `src/login/logind-dbus.c`, `systemctl-compat-halt.c`, systemctl(1) |

## 1. sudo

| Fact | Status | Source |
|---|---|---|
| With a root password set at install, d-i doesn't add the first user to `sudo`. sudo is installed and the user added only when the root password is left empty. | V | Installation Guide §6.3.2.1; user-setup source |
| sudoers.d skips names ending in `~` or containing `.`. visudo(8): checking a single include file isn't sufficient, so run `visudo -c`. | V | sudo 1.9.16p2 sudoers(5), visudo(8) |

## 2. apt-sources

| Fact | Status | Source |
|---|---|---|
| d-i (apt-setup 0.198) writes a **one-line** `sources.list` with active `deb-src` lines and `main non-free-firmware`. cdrom lines are commented out unless the medium was a real CD. | V | apt-setup 1:0.198 generators/50mirror, 91security, templates, changelog |
| Offline installs leave a `deb cdrom:` entry that breaks `apt update` online. | R | apt-sources disables any cdrom entry it finds |
| apt 3.0.x `apt modernize-sources` honours `-y` and treats an "N" answer as a dry run. It converts `X.list` to `.sources` and renames it `X.list.bak` (`sources.list` → `sources.list.bak`), appends `sources.list` entries to `<origin>.sources` (`debian.sources` for Debian) or `moved-from-main.sources`, keeps only `signed-by`/`trusted`, and can leave `Signed-By` empty with a warning. | V | salsa apt-team/apt `apt-private/private-sources.cc`, `private-cmndline.cc` (3.0.3); [upgrading notes](https://www.debian.org/releases/trixie/release-notes/upgrading.en.html) |
| `apt-key` is removed. Use `Signed-By:` keyrings (`/etc/apt/keyrings/` for third parties). | V | apt changelog, packages.debian.org file list |
| One-line `sources.list` is deprecated but still read ("not before 2029"). | V | [sources.list(5)](https://manpages.debian.org/trixie/apt/sources.list.5.en.html) |
| The Debian archive keyring is `/usr/share/keyrings/debian-archive-keyring.pgp`. The `.gpg` symlinks are going away. | V | release notes |

## 3. mirrors

| Fact | Status | Source |
|---|---|---|
| `mirror+file:/path` URIs are allowed in deb822 `URIs:`. Mirrorlist: one URI per line, `#` comments, TAB, then `key:value` tags (`priority:N`, lowest tried first, plus `arch: codename: component: lang: suite: type:`). The method sorts by priority and randomises only among **equal** priorities, per file; untagged lines are tried last, in random order. | V | [apt-transport-mirror(1)](https://manpages.debian.org/trixie/apt/apt-transport-mirror.1.en.html); apt 3.0.3 `methods/mirror.cc` |
| `Acquire::Retries` (default 3) is in apt.conf(5). | V | apt.conf(5) |
| `https://mirror-master.debian.org/status/Mirrors.masterlist` is live, about 370 RFC822 stanzas (`Site, Country, Type, Archive-architecture, Archive-http, ...`), HTTP only. `Archive-architecture:` is a space-separated list (`amd64 arm64 …`); 360 of 368 sites list amd64. The salsa repo holds `Mirrors.masterlist.in`, not the generated file. | V | fetched 2026-11-07; salsa mirror-team/masterlist |
| `dists/trixie/main/Contents-amd64.gz` is about 12.7 MB; `Contents-all.gz` about 38 MB. | V | HEAD on deb.debian.org |

## 4–5b. upgrade, firmware-packages, kernel

| Fact | Status | Source |
|---|---|---|
| `/run/reboot-required` is written only by unattended-upgrades' kernel postinst hook. | V | unattended-upgrades 2.12 `kernel/postinst.d/unattended-upgrades` |
| firmware-nonfree 20250410 (backports 20260916) splits out `firmware-intel-graphics`, `firmware-intel-misc`, `firmware-nvidia-graphics` and `firmware-mediatek`; `firmware-misc-nonfree` only Recommends them. | V | firmware-nonfree 20250410-2 debian/, packages.debian.org |
| `isenkram-autoinstall-firmware -l` echoes package names on stdout and every `info:` line on stderr; it exits 1 only when no loaded module requests firmware, else 0 (including "No new firmware package"). | V | isenkram 0.69+deb13u1 isenkram-autoinstall-firmware; field test 2026-11-07 (HP test machine) |
| Kernels: trixie 6.12.x, backports 7.2.x, both from `linux-signed-amd64`. The backports image needs `linux-base ≥ 4.17~` (backports only). Image names are `linux-image-<ver>+deb13[.N]-amd64`; there's no `linux-image-6.12-amd64` metapackage. unattended-upgrades installs new names only as dependencies of an upgrade. | V | qa.debian.org madison; unattended-upgrades 2.12 source |

## 7. multimedia (including NVIDIA)

| Fact | Status | Source |
|---|---|---|
| `libavcodec-extra61` Conflicts/Provides `libavcodec61`. `intel-media-va-driver-non-free` Conflicts `intel-media-va-driver`. | V | ffmpeg 7:7.1.5 and intel-media-driver debian/control |
| libdvd-pkg 1.4.3: templates `build` (boolean), `post-invoke_hook-install` (boolean), `first-install` (note). `dpkg-reconfigure -f noninteractive` builds `libdvdcss2` into `/usr/lib/x86_64-linux-gnu/libdvdcss.so.2`. | V | libdvd-pkg 1.4.3-1-1.1 debian/templates, config, postinst |
| `nvidia-driver` 550.163 (non-free, DKMS) pulls `firmware-nvidia-gsp`; its newest backports build documents fixes only up to Linux 6.17. dkms 3.2.2: `/var/lib/dkms/mok.{key,pub}`, `dkms generate_mok`. mokutil: `--import … --hash-file`, `--generate-hash=`, `--test-key`, `--list-new`. | V | nvidia-graphics-drivers changelog; dkms 3.2.2-1~deb13u1 dkms.in; mokutil(1) |
| `mokutil --test-key` exits 0 with "… is not enrolled", 1 with "… is already enrolled / in db / in the enrollment request" or "… is blocked …", 255 on errors. `--generate-hash` reads the password twice with `getline` and turns off echo only when stdin is a TTY. | V | mokutil 0.7.2-1 src/mokutil.c `test_key`, `print_skip_message`, `get_password`; src/util.c `read_hidden_line` |
| Debian's `nvidia-driver` 550 "supports Maxwell, Pascal, Volta, Turing, Ampere and Ada/Hopper GPUs, it does not support Blackwell", and "is no longer maintained and contains several security issues". `nvidia-detect` is described as outdated. | V | [wiki.debian.org/NvidiaGraphicsDrivers](https://wiki.debian.org/NvidiaGraphicsDrivers) |
| NVIDIA's repository `https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/` is flat (one-line form `… x86_64/ /`). Release: `Origin: NVIDIA`, `Label: NVIDIA CUDA`. Branches with pinning packages: 590, 595, 610, 615, 620 (newest 620.70.06); **no 580**. | V | repository index, `Packages.gz`, `InRelease` |
| Setup: `cuda-keyring_1.1-1_all.deb` (sha256 `d0d4ef98…f5f`) ships `/usr/share/keyrings/cuda-archive-keyring.gpg` (fingerprint `02182E60104FCDC26EAE1B8597A5D4CB8793F200`, uid "Kitmaker (Debian 13 Trixie)") and a one-line `/etc/apt/sources.list.d/cuda-debian13-x86_64.list`. No standalone keyring at the repository URL (404). | V | NVIDIA driver installation guide (Debian); package contents inspected |
| Optional pinning: `apt install nvidia-driver-pinning-<branch>` before the driver. From 590 onward the default is to follow the latest drivers. | V | NVIDIA driver installation guide, "Recent updates" |
| Only open kernel modules ship ("The proprietary kernel modules have been removed"). In 620, `nvidia-kernel-dkms` and `cuda-drivers` are transitional, pointing to `nvidia-kernel-open-dkms` and `nvidia-open`. | V | NVIDIA guide; repository Packages metadata |
| Install sets: full `nvidia-open`; desktop-only `nvidia-driver nvidia-kernel-open-dkms`; compute-only `nvidia-driver-cuda nvidia-kernel-open-dkms`. `nvidia-kernel-open-dkms` 620 depends on `dkms (>= 3.1.8)`, `firmware-nvidia-gsp (= 620…)` and `g++`. | V | NVIDIA guide; Packages metadata |
| The open kernel modules "can be used on any Turing or later GPU" and need Linux ≥ 4.15. | V | NVIDIA/open-gpu-kernel-modules README (620.70.06) |
| Open-module device-ID table: README.md at 620.70.06 (sha256 adb1d8f1…f170) lists 299 IDs, identical to `nvidia_ids.go`. All IDs up to 0x28F8 are Ada or older, all from 0x2901 are Blackwell (B200, GB200, RTX 50); the 550.163.01 README has none at or above 0x2900. RTX 2070 Super (1e84) is listed, GTX 970 (13c2) isn't. | V | github.com/NVIDIA/open-gpu-kernel-modules README.md at 620.70.06 and 550.163.01, re-derived 2026-11-07 |
| deb822 flat repositories: `Suites:` is a path ending in `/`, and `Components:` must then be omitted. | V | sources.list(5), trixie |
| `dpkg --compare-versions A gt B` exits 0 when true, 1 when false, and orders kernel release strings as kiln needs (`6.12.57+deb13-amd64` > `6.12.48+deb13-amd64`, `7.2.0-0.bpo13-amd64` > `6.12.57+deb13-amd64`) without syntax warnings. | V | dpkg(1) trixie; observed on the HP test machine 2026-11-07 |

## 8. repos

| Fact | Status | Source |
|---|---|---|
| extrepo 0.14: `/etc/extrepo/config.yaml` key `enabled_policies:` (default `main` only). `google_chrome` has policy `non-free`. `extrepo enable` writes `/etc/apt/sources.list.d/extrepo_<name>.sources` with `Signed-By: /var/lib/extrepo/keys/<name>.asc`; a re-run only sets `Enabled: yes`. Chrome's post-enable command writes `/etc/default/google-chrome` (`repo_add_once="false"`) to stop a duplicate source. | V | [extrepo 0.14 source](https://sources.debian.org/src/extrepo/0.14/), extrepo-data live index |
| `extrepo search <re>` prints `Found <name>:` then a YAML dump of the entry (top-level keys unindented), and exits 1 with "No matches found". Entries carry `policy: <p>` or a `policies:` map of component → policy (289 and 44 entries in the trixie index). | V | extrepo 0.14 Commands/Search.pm, Commands/Enable.pm; extrepo-data debian/trixie/index.yaml, fetched 2026-11-07 |
| `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo` (flatpak 1.16.6). `gnome-software-plugin-flatpak` is 48.3. | V | [flatpak-remote-add(1) trixie](https://manpages.debian.org/trixie/flatpak/flatpak-remote-add.1.en.html), flathub.org/setup/Debian |

## 9. devtools

| Fact | Status | Source |
|---|---|---|
| GitHub CLI repo: keyring `https://cli.github.com/packages/githubcli-archive-keyring.gpg` (sha256 `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b`) → `/etc/apt/keyrings/`. `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main` (Release Codename `stable`). Package `gh`. The doc uses a one-line source; kiln writes the deb822 equivalent. | V | [install_linux.md](https://github.com/cli/cli/blob/trunk/docs/install_linux.md), repo Release file (2026-11-07) |

## 10. system

| Fact | Status | Source |
|---|---|---|
| timesyncd is the default. timesyncd and chrony both `Provides/Conflicts/Replaces: time-daemon`. | V | systemd 257 and chrony 4.6.1 control files |
| `systemd-zram-generator` 1.2.1 ships `/usr/lib/systemd/zram-generator.conf` enabling zram0 at `min(ram / 2, 4096)`; `/etc/systemd/zram-generator.conf` overrides it, and `zram-generator.conf.d/*.conf` drop-ins are supported. Restarting `systemd-zram-setup@zram0` also restarts `dev-zram0.swap` (swapoff). | V | [package file list](https://packages.debian.org/trixie/amd64/systemd-zram-generator/filelist), [zram-generator.conf(5)](https://github.com/systemd/zram-generator/blob/main/man/zram-generator.conf.md), rust-zram-generator 1.2.1-2; systemd.unit(5) |
| 6.12 inotify `max_user_watches` scales with RAM (clamped 8192…1048576), and no Debian sysctl.d touches it. | V | linux v6.12 inotify_user.c |
| journald.conf(5) recommends drop-in prefixes 60–90. | V | systemd 257.13 journald.conf(5) |

## 11. power

| Fact | Status | Source |
|---|---|---|
| power-profiles-daemon `Conflicts: laptop-mode-tools, tlp`. `tuned-ppd` `Conflicts/Provides: power-profiles-daemon`. | V | sources.debian.org control files (ppd 0.30-1.1, tuned 2.25.1-1) |
| ppd writes `/sys/firmware/acpi/platform_profile` itself, adopts external changes, and (amd-pstate active) sets EPP/governor/boost per profile. | V | [ppd 0.30 source](https://sources.debian.org/src/power-profiles-daemon/0.30-1.1/src/) |
| ppd 0.30 `balanced`: platform profile `balanced`; amd-pstate EPP `balance_performance` on AC, `balance_power` on battery; `balanced` is always offered. The choice persists in `/var/lib/power-profiles-daemon/state.ini`. | V | sources.debian.org power-profiles-daemon 0.30-1.1 src/ppd-driver-amd-pstate.c `profile_to_epp_pref`, src/ppd-driver-platform-profile.c, src/ |
| tuned 2.25.1 `balanced`: `[cpu] governor=schedutil\|ondemand\|powersave`, `energy_perf_bias=normal`, `energy_performance_preference=balance_performance`, `boost=1`; `[acpi] platform_profile=balanced`; no `vm.swappiness`. `tuned-adm active` prints `Current active profile: <name>` while tuned runs ("Preset profile:" when it doesn't). With it active the HP's cores idled at ~1.4 GHz on `schedutil`. | V | /usr/lib/tuned/profiles/balanced/tuned.conf on the test machine (tuned 2.25.1-1); field test 2026-11-07 |
| logind is `Type=notify-reload`, and SIGHUP re-reads the config. `HandleSuspendKeyLongPress` defaults to hibernate. | V | systemd 257.13 units/systemd-logind.service.in, src/login/ |
| Debian ships no `/etc/dconf/profile/user` (only debian-edu-config does). | V | packages.debian.org contents search |
| `/etc/gdm3/greeter.dconf-defaults` is ucf-managed (three-way merged from `/usr/share/gdm/greeter.dconf-defaults`) and symlinked as `/usr/share/gdm/dconf/90-debian-settings`. | V | gdm3 48.0-2 postinst, README.Debian |
| GDM 48: greeter power keys are commented out in `greeter.dconf-defaults`. `systemctl reload gdm3` runs `/usr/share/gdm/generate-config` and SIGHUP, which keeps sessions. gsd 48.1 `power-button-action` defaults to `'suspend'`. | V | gdm3 48.0-2 debian/generate-config, data/gdm.service.in, daemon/main.c; gsd 48.1 gschema |

## 12–13. thermal, battery

| Fact | Status | Source |
|---|---|---|
| Kernel ABI: `charge_control_{start,end}_threshold` (0–100; drivers may round). `charge_behaviour` is `auto`/`inhibit-charge`/`force-discharge` in 6.12; `inhibit-charge-awake` only in later kernels (present in 7.2). `charge_types` (`Custom`, `Long_Life`, …) is documented from 6.14, but dell-laptop exposes it in 6.12. `scope` exists in `power_supply_sysfs.c` but is undocumented, and the ACPI battery driver doesn't expose it. Threshold writes emit no uevent. | V | sysfs-class-power at v6.12 and v7.2; `drivers/power/supply/` |
| `/sys/class/platform-profile/` (multi-handler class) needs kernel ≥ 6.14: not in stable 6.12, present in the backports kernel. | V | sysfs-class-platform-profile ABI |
| Vendor matrix (ThinkPad, ASUS, Dell, Huawei, IdeaPad, LG, Samsung, Sony, Toshiba, MSI, System76, Framework). HP isn't listed. | V | [TLP battery care vendors](https://linrunner.de/tlp/settings/bc-vendors.html) |
| hp-wmi exposes only `charge_behaviour`, DMI-gated to two Spectre x360 boards, and only in kernels newer than 6.12. | V | mainline `drivers/platform/x86/hp/hp-wmi.c` |
| thinkpad_acpi rejects start > current stop and stop < current start, and allows start == stop. `fan_control=1` enables writes to `/proc/acpi/ibm/fan`. dell-laptop clamps thresholds silently; asus-wmi's read-back is a cached value. | V | thinkpad_acpi.c, [kernel docs](https://docs.kernel.org/admin-guide/laptops/thinkpad-acpi.html); linux v6.12 drivers/platform/x86/ |
| `tee <sysfs attr>` writes the value; an invalid value fails with "Invalid argument" and exit 1, and the attribute keeps its old value. | V | coreutils 9.7 tee(1); observed on `/sys/block/sda/queue/read_ahead_kb`, field test 2026-11-07 (HP test machine) |
| systemd.device(5): device `Wants=` (SYSTEMD_WANTS) act only when a device first becomes active, never on change events. | V | systemd 257 systemd.device(5) |
| UPower 1.90.9 / GNOME 48 "Preserve Battery Health": enabled only when `charge_control_start_threshold` exists. Limits come from hwdb `CHARGE_LIMIT` (default `75,80`). The flag is stored in a `charging-threshold-status` file in UPower's state dir. Disabling writes 0/100. | V | [upower 1.90.9 source](https://sources.debian.org/src/upower/1.90.9-1/) |
| thinkfan 1.3.1 has **no** config-test flag (`-n` foreground, `-c` config, `-D` dangerous). It reads `/etc/thinkfan.yaml`, then `.conf`. The unit ships disabled. | V | thinkfan.1, thinkfan 1.3.1-6 debian/rules |
| `sensors-detect --auto` writes `/etc/modprobe.d/lm_sensors.conf` when needed and never `/etc/modules`. On these laptops the hwmon drivers autoload anyway. | V | lm-sensors 3.6.2 prog/detect/sensors-detect |
| E14 Gen 5 AMD exposes thresholds, `/proc/acpi/ibm/fan` and DYTC platform_profile. | R | the battery ladder and thermal step probe at run time |
| Envy x360 13 (4500U) hp-wmi platform_profile works. | R | the thermal step probes `platform_profile_choices` at run time |

## 15. ssh

| Fact | Status | Source |
|---|---|---|
| Service is `ssh.service` (enabled). `ssh.socket` is shipped `--no-enable`. | V | openssh 1:10.0p1 `debian/rules`, README.Debian |
| `sshd_config` has `Include /etc/ssh/sshd_config.d/*.conf` near the top, and the first value wins, so low-numbered drop-ins take precedence. | V | Debian sshd_config, sshd_config(5) |
| openssh 10.0p1: Debian's sshd_config already sets `KbdInteractiveAuthentication no`. `Port` lines accumulate. `sshd -T` applies Match only with `-C`, and prints one `port N` line per configured port (and `listenaddress` lines). `ssh.service` ExecReload runs `sshd -t` then HUP. | V | openssh 1:10.0p1-7+deb13u4 debian/, sshd(8), sshd_config(5); `sshd -T` with `Port 22` + `Port 2222`, field test 2026-11-07 (HP test machine) |
| `systemctl reload ssh` (signal-based ExecReload) is asynchronous. sshd re-execs on SIGHUP after closing its listening sockets, so there's a short window with no listener. | V | systemd.service(5), openssh 10.0p1 `sshd.c`, sshd(8) |
| `ss -H` suppresses the header; `ss -H -ltn` lines are `LISTEN q q addr:port peer`, and `-p` adds `users:(("sshd",pid=…,fd=…))`. `ss -H -l -t -n -p sport = :N` works with the filter passed as separate argv words. | V (filter form observed) | ss(8) iproute2 6.15.0; field check 2026-11-07 (HP test machine) |
| `chpasswd -e` reads `user:<encrypted password>` lines on stdin ("Supplied passwords are in encrypted form"). | V | chpasswd(8) trixie |
| DSA support is removed. Default KEX dropped the finite-field DH groups. `~/.pam_environment` isn't read. | V | release notes §5.1.7–5.1.8, openssh debian/NEWS |
| No SELinux (AppArmor), so a port change needs no `semanage`. | V | Debian default |

## 16. fail2ban

| Fact | Status | Source |
|---|---|---|
| fail2ban 1.1.0-8 depends on `python3-systemd`. `jail.d/defaults-debian.conf` sets `banaction = nftables`, `banaction_allports = nftables[type=allports]`, and `[sshd] backend = systemd`, `journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd`. | V | [sources.debian.org fail2ban 1.1.0-8](https://sources.debian.org/src/fail2ban/1.1.0-8/debian/debian-files/) |
| A jail-level `journalmatch` overrides the filter's, so `_COMM=sshd-session` must be listed. | V | fail2ban 1.1.0-8 configreader/jailreader, filter.d/sshd.conf |
| There's no `/var/log/auth.log`: rsyslog isn't installed by default, and fail2ban's Debian config uses the journal for sshd. | V | fail2ban 1.1.0-8 `paths-debian.conf` / `defaults-debian.conf`; rsyslog is Priority optional |

## 17. docker

| Fact | Status | Source |
|---|---|---|
| `download.docker.com/linux/debian/dists/trixie` exists (stable, amd64). | V | fetched |
| Key at `/etc/apt/keyrings/docker.asc`; deb822 `docker.sources` with `Types/URIs/Suites/Components: stable/Architectures/Signed-By`. Remove: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`. | V | [Docker Debian install docs](https://docs.docker.com/engine/install/debian/) |
| docker-ce 29.9.3 Depends `iptables, nftables`. log-opts values must be strings and apply to new containers only. | V | download.docker.com Packages; docs.docker.com json-file driver |

## 18. cockpit

| Fact | Status | Source |
|---|---|---|
| cockpit 337/371 Recommends `cockpit-storaged`, `cockpit-networkmanager` and `cockpit-packagekit` (not machines/podman). The socket is enabled on install. postinst writes `root` to `disallowed-users`. | V | cockpit 337-1+deb13u2 / 371-1~bpo13+1 debian/control, postinst |
| `cockpit-networkmanager` depends on network-manager and is "incompatible with systemd-networkd/ifupdown defined networks". | V | [packages.debian.org](https://packages.debian.org/trixie/cockpit-networkmanager) |

## 20. autoupdates

| Fact | Status | Source |
|---|---|---|
| Debian's `50unattended-upgrades` allows `label=Debian` (point releases) as well as security. `Origins-Pattern` accumulates across files, and `#clear` resets it. `20auto-upgrades` is ucf-managed from debconf `enable_auto_updates`. Upgrades run from `apt-daily-upgrade.timer`. | V | unattended-upgrades 2.12 data/, postinst, templates; apt.conf(5) |
| unattended-upgrades matches `Origins-Pattern` values with `fnmatch`, so `origin=*` matches any origin. | V | unattended-upgrades 2.12 `unattended-upgrade` (`match_whitelist_string`) |
| Release `Origin`/`Label`: Docker `Docker`/`Docker CE`; GitHub CLI `gh`/`gh`; Chrome `Google LLC`/`Google`; NVIDIA `NVIDIA`/`NVIDIA CUDA`. | V | each repository's InRelease |
| `apt-daily.timer`: `OnCalendar=*-*-* 6,18:00`, `RandomizedDelaySec=12h`, runs `apt.systemd.daily update`. `apt-daily-upgrade.timer`: `OnCalendar=*-*-* 6:00`, `RandomizedDelaySec=60m`, runs `apt.systemd.daily install` (unattended-upgrade). Both services have `ConditionACPower=true`. | V | apt 3.0.3 `debian/apt-daily*.timer`, `apt-daily*.service` |
| `APT::Periodic::*` intervals are in days, accept `s`/`m`/`h`/`d` suffixes, or `always` ("which can be used with systemd OnCalendar overrides to define custom schedules"). | V | apt 3.0.3 `debian/apt.systemd.daily` |
| `Persistent=true` timers fire immediately on (re)start if a run was missed; on the very first start (no stamp) they don't. | V | systemd.timer(5), `src/core/timer.c` |
| `systemctl show -p TimersCalendar` prints `TimersCalendar={ OnCalendar=*-*-* 06,18:00:00 ; next_elapse=… }` (normalized with seconds). | V (observed) | field check 2026-11-07 |
| `apt-config dump` prints list values as `Key:: "v";` after a header line `Key "";`. | V | observed with apt 3.0.4: `Unattended-Upgrade::Origins-Pattern "";` then `Unattended-Upgrade::Origins-Pattern:: "origin=*";`, field test 2026-11-07 (HP test machine) |
| needrestart 3.11 reads `/etc/needrestart/conf.d/*.conf` (sorted). The default restart mode `'i'` becomes list-only when non-interactive. | V | needrestart.conf |

## 21–23. node, python, go

| Fact | Status | Source |
|---|---|---|
| nvm v0.41.0 (2026-11-04) is the latest release. install.sh honours `PROFILE=/dev/null`, `METHOD=script` and `NVM_DIR` (exits 1 if an explicit NVM_DIR doesn't exist). Node 26 is LTS "Lithium" since v26.13.0 (2026-10-28). | V | [nvm v0.41.0 install.sh](https://github.com/nvm-sh/nvm/blob/v0.41.0/install.sh), nodejs.org/dist/index.json |
| pipx 1.7.1 installs to `~/.local/bin`; a re-run exits 0. uv 0.13.7: `uv venv` errors on a non-empty path, `uv pip install --python <venv-dir>` works, and `uv tool install` takes one package per call. | V | pipx paths.py 1.7.1, docs.astral.sh/uv |
| uv's documented pinned installer is `https://astral.sh/uv/<version>/install.sh` ("Request a specific version by including it in the URL"); 0.13.7 redirects to releases.astral.sh and sets `APP_VERSION="0.13.7"`. | V | docs.astral.sh/uv/getting-started/installation; the script itself, fetched 2026-11-07 |
| Playwright 1.64 supports Debian 13. `install-deps --dry-run chromium` prints `Missing system dependencies (N):` plus one indented package per line and exits 1, or prints "All system dependencies are installed." and exits 0. | V | playwright v1.64.0 `registry/dependencies.ts`, playwright.dev |
| `go install` accepts several packages only from one module, so tools need separate runs. Debian's golang keeps `GOTOOLCHAIN=auto`. go.dev `/dl/?mode=json&include=all` returns every release (369 on 2026-11-07, go1.27.3 included) as `{version, stable, files[{filename, os, arch, version, sha256, size, kind}]}`. | V | go.dev/ref/mod, go.dev/dl JSON (fetched 2026-11-07), go1.27.3 |

## 24. firmware-updates (fwupd)

| Fact | Status | Source |
|---|---|---|
| fwupd 2.0.20: exit 2 = nothing to do, 101 = no network. `-y`, `--no-reboot-check`, `--no-device-prompt`, `--json` and `check-reboot-needed` exist. LVFS is on by default. The UEFI capsule plugin needs an ESRT, an ESP and `fwupd-amd64-signed` under Secure Boot. `--no-reboot-check` returns before the "Restart now?" prompt, which is the only reboot path; `-y` doesn't answer it; without a TTY `no_reboot_check` is forced on. | V | fwupd 2.0.20-1~deb13u1 `src/fu-util.c`, `fu-util-common.c`, plugins/uefi-capsule/README.md |

## Still to verify (tracked)

- R (run time): whether `dockerd --validate --config-file` exists. kiln runs it only when `dockerd --help` lists `--validate`; otherwise it relies on its own JSON parse plus a restart-and-check (step 17).
- Whether UPower 1.90.9 re-reads hwdb `CHARGE_LIMIT` on a udev change event, or only at startup (step 13 does `try-restart upower` either way).
- R (run time): whether the backports kernel works better with backported firmware (no packaging relation exists); the amd-pstate default mode on each laptop; whether `/var/log/journal` exists.

## Field observations (test machine 10.10.10.150, 2026-11-07)

Fresh Debian 13 server install (no desktop, Wi-Fi). Evidence, not primary documentation. All V (observed) unless noted.

- Root password set at install → `sudo` isn't installed ("sudo: command not found"), so the first kiln run is as root (`su -`). Matches user-setup 1.107.
- HP Envy x360 13-ay0xxx (board 876E): BAT1 has no `charge_control_*` or `charge_behaviour`; `hp-wmi` loads but offers no `platform_profile`; `acpi-cpufreq` (no amd-pstate) on the Ryzen 5 4500U; Wi-Fi is a Realtek RTL8822CE (`rtw_8822ce`, PCI vendor 10ec); the installer had already put in firmware-realtek, firmware-amd-graphics and amd64-microcode.
- `/etc/network/interfaces` on a Wi-Fi server install: `allow-hotplug wlo1` / `iface wlo1 inet dhcp` with `wpa-ssid`/`wpa-psk`, plus `source /etc/network/interfaces.d/*`.
- SSH banner `SSH-2.0-OpenSSH_10.0p2 Debian-7+deb13u4` (says 10.0p2; the package is 1:10.0p1-7+deb13u4). kiln never parses it (`nc 10.10.10.150 22`).
- Wi-Fi power saving was on by default: 19–572 ms latency and 6.7% loss on the LAN vs 2 ms to the router; after `iw … set power_save off`, 3–7 ms and no loss (ping from the LAN).
- SSH timeouts from one macOS app while another app on the same Mac worked: caused on the Mac (an AdGuard per-app network extension is the main suspect), not Debian or OpenWRT. (R, diagnosis)
- No suspend and no Wi-Fi disconnect during the boot with the timeouts (journal 09:53–15:16, `journalctl -b -1`), which rules out "lid/suspend or Wi-Fi drop"; power saving explains only the latency. The platform supports s2idle ("Low-power S0 idle"), not S3.
- Boot errors (`journalctl -b -p err`): `ACPI BIOS Error … CreateField of length zero` in `\_SB.WMID.WQBC`/`WQBE` and `AE_ALREADY_EXISTS` for `\_SB.PCI0.GPP0.DEV0` (firmware table bugs, every boot, not kiln). No failed units.
- Second run on the configured machine (field test run 5): 1 done (upgrade, by design), 21 unchanged, 0 failed, 4 skipped, 15 s. node, npm, Python + Playwright (headless Chromium), uv, Go, gh, `docker run hello-world` as satya, Cockpit HTTPS 200 and the kiln services all worked.

## No longer used (kept for reference)

These served steps and options removed on 2026-11-07 (firewall, remote-desktop, network-migration, SSH key options, performance profiles; D50–D53). No current code relies on them.

- isc-dhcp-client is deprecated; ifupdown uses `dhcpcd-base` on new installs. (V; release notes)
- NM's checkpoint/rollback D-Bus API (`CheckpointCreate`) has no nmcli command and can't undo ifupdown state. (libnm docs; never fully verified, and no longer needed)
- NM's DHCP client may send a different client-id than dhcpcd/dhclient, so the IP can change after migration. (R)
- netcfg 1.197: with NM in the target, connections go to NM and `interfaces` gets only `lo`. (V; netcfg 1.197 55netcfg-copy-config, nm-conf.c)
- ifupdown 0.8.44 tries dhclient, udhcpc, dhcpcd in that order (`execable()`); plain `dhcpcd <iface>` (no `-4`) also does IPv6 unless `ipv4only`/`noipv6`; dhcpcd 10.1.0 uses a DUID client-id (`/var/lib/dhcpcd/duid`), IAID = last 4 MAC bytes, trixie's `dhcpcd.conf` has `slaac private`. (V; ifupdown inet.defn, main.c; dhcpcd 1:10.1.0-11+deb13u4 src/, dhcpcd.conf(5))
- ifupdown units: `ifup@.service` (`ifup --allow=hotplug %I`, `RemainAfterExit=true`, `BindsTo=sys-subsystem-net-devices-%i.device`, `TimeoutStartSec=5min`) started by udev's `ifupdown-hotplug`; `networking.service` runs `ifup -a --read-environment`, and its `ExecStopPost` touches `/run/network/restart-hotplug`. `ifup` of a configured and `ifdown` of an unconfigured interface warn and exit 0. (V; ifupdown 0.8.44+deb13u1 debian/, main.c; field test)
- dhclient writes `default-duid` as `\NNN`-escaped quoted string, or colon hex with `lease-id-format hex`; dhcpcd.conf `clientid`/`duid`/`iaid` are global or per `interface`. (V; isc-dhcp 4.4.3-P1-8 dhclient.c, print.c; dhcpcd.conf(5))
- NM: hex `ipv4.dhcp-client-id`, `ipv6.addr-gen-mode` (eui64/stable-privacy), `ipv6.dhcp-duid`, `ipv6.dhcp-iaid`; keyfile keys use the same names; keyfiles must be root-only; `duid` mode hashes machine-id. (V; nm-settings-nmcli(5), nm-settings-keyfile(5) 1.52.1; field test)
- NM 1.52.1: an automatic "Wired connection" only for a managed device with no compatible profile; `connection up` on an activating profile restarts it; `nmcli --wait` defaults to 90 s. (V; NetworkManager.conf(5), nm-settings.c, nm-manager.c, nmcli(1))
- `pgrep -a` prints `pid cmdline`, exits 1 on no match; names over 15 characters never match. (V; pgrep(1) procps 2:4.0.4-9)
- No firewall by default (nftables framework); installing `firewalld` starts it at once, zone `public` (ssh + dhcpv6-client). (V; firewalld 2.3.1-1+deb13u1 debian/rules, public.xml)
- firewalld 2.3.1: `StrictForwardPorts` (since 2.3.0); `--new-zone` permanent-only; reload replaces only `table inet firewalld`; ships `cockpit`, `rdp`, `ssh` services; Debian's `/etc/nftables.conf` starts with `flush ruleset`. (V; firewalld blog, firewalld.conf(5), firewall-cmd(1), nftables.py; nftables 1.1.3-1)
- `firewall-cmd --query-*` exits 0/1, 112 for INVALID_ZONE; `--get-active-zones` prints zone names at line start. (V; firewall-cmd(1) 2.3.1; field test)
- Docker under firewalld: `docker` zone + `docker-forwarding` policy, published ports bypass zones unless `StrictForwardPorts=yes`; 28.3.3 fixed loopback ports exposed after reload; 29 added an experimental nftables backend. (V; Docker packet-filtering docs, release notes 28, firewalld blog)
- `grdctl --system` (48.1) re-runs via `pkexec --user gnome-remote-desktop` and exits 0 even if that fails; run as that user, grdctl enters system mode itself. `rdp set-credentials <user>` reads only the password on stdin; `rdp enable` needs `polkitd`; the unit ships disabled; `grdctl status` prints `RDP:` then `Status:`, `TLS certificate:`, `TLS key:`, `Username:`/`Password:`. (V; grd-ctl.c 48.1-4, debian/rules, postinst; polkit 126-2)
- `openssl req -x509` defaults to 30 days and prompts unless `-subj`; `-nodes` deprecated for `-noenc`. (V; openssl-req(1) trixie)
- `ssh-keygen -l -f` exits 0 with a valid key, 255 otherwise. (V; ssh-keygen.c `do_fingerprint` 1:10.0p1-7+deb13u4; field test)
- ppd 0.30 offers `performance` only with amd-pstate EPP or a platform_profile with balanced + performance; tuned `throughput-performance` sets `platform_profile=performance`, EPP performance, `vm.swappiness=10`. Superseded by the balanced rows. (V; ppd 0.30-1.1 src/; tuned 2.25.1-1 profiles/throughput-performance)
- `Acquire::Queue-Mode` (`host`/`access`), `::Delay`/`::Delay::Maximum` (configure-index only), `Acquire::http::Pipeline-Depth` (default 10). (V; apt.conf(5), apt-transport-http(1), configure-index 3.0.3)
- Trace file `project/trace/master`: date line, `Archive serial:`, `Date:`, `Architectures:`. (V; deb.debian.org)
- Versions: `firewalld` 2.3.1, `network-manager` 1.52.1, `gnome-remote-desktop` 48.1. (V; packages.debian.org)
- Field, 2026-11-07 (V observed): dhcpcd runs in manager mode with a DUID-LLT and `<iface>-<ssid>.lease` leases; NM needed `systemctl restart NetworkManager` to manage a released interface, then a wait for `GENERAL.STATE` ≥ 30 and a Wi-Fi scan before `nmcli --wait 90 connection up` (7 s migration, SSH kept); copying client-id/DUID/IAID kept the same IPv4 and DHCPv6 address; a plain `ifup` puts wpa_supplicant/dhcpcd in the caller's cgroup; NM's and ifupdown's wpa_supplicant share `/run/wpa_supplicant` ("ctrl_iface exists and seems to be in use"); a forced revert restored all addresses in about 12 s; firewalld logged `NAME_CONFLICT … 'docker-forwarding'` when Docker re-created its policy.
