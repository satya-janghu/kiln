# Debian 13 "trixie" notes and gotchas

These facts were checked on **2026-11-07** against primary sources. Package versions are trixie point-release versions on that date, so treat them as "at least", not as pins. There are no tests (see [review-process.md](review-process.md)), so primary documentation is the only form of verification. Statuses:

- **V**: verified against a primary source, which is linked. Code may rely on it.
- **U**: not yet verified. Code must not rely on it. Verify it from primary sources (packages.debian.org, manpages.debian.org, sources.debian.org, release notes, upstream docs) and flip it to V, or redesign so it isn't needed.
- **R**: deliberately not assumed. The code detects it at run time and handles both outcomes, typically hardware behavior that no document can promise. These rows record what we expect, not what the code depends on.

Re-check V facts older than 30 days during `/audit`.

## Install-time state

| Fact | Status | Source |
|---|---|---|
| With a root password set at install, d-i doesn't add the first user to `sudo`. sudo gets installed and the user added only when the root password is left empty. | V | Installation Guide §6.3.2.1; user-setup source |
| Plain `su` keeps the caller's PATH (no `/usr/sbin`). `su -` sets `ENV_SUPATH` (`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`). | V | Debian `login.defs`, su(1) |
| Minimal installs often have no curl, but wget is usually present. | R | install.sh uses whichever of the two exists |
| `/tmp` is a tmpfs (up to 50% of RAM) on new installs. `/tmp` and `/var/tmp` are cleaned periodically by systemd-tmpfiles. | V | [release notes §5.1.6, §5.2.1](https://www.debian.org/releases/trixie/release-notes/issues.en.html) |
| Offline installs leave a `deb cdrom:` entry that breaks `apt update` online. | R | apt-sources disables any cdrom entry it finds |
| isc-dhcp-client is deprecated. ifupdown uses `dhcpcd-base` on new installs. | V | release notes, issues page |

## APT 3.0

| Fact | Status | Source |
|---|---|---|
| apt 3.0.x. `apt modernize-sources` converts `.list` files to `.sources`, renames the originals to `*.list.bak`, moves `sources.list` entries to `<origin>.sources` or `moved-from-main.sources`, keeps only `signed-by`/`trusted`, and treats an "N" answer as a dry run. | V | salsa apt-team/apt `apt-private/private-sources.cc` (3.0.3); [upgrading notes](https://www.debian.org/releases/trixie/release-notes/upgrading.en.html) |
| `apt-key` is removed. Use `Signed-By:` keyrings (`/etc/apt/keyrings/` for third parties). | V | apt changelog, packages.debian.org file list |
| One-line `sources.list` is deprecated but still read ("not before 2029"). | V | [sources.list(5)](https://manpages.debian.org/trixie/apt/sources.list.5.en.html) |
| The Debian archive keyring path is `/usr/share/keyrings/debian-archive-keyring.pgp`. The `.gpg` symlinks are going away. | V | release notes |
| `mirror+file:/path` URIs are allowed in deb822 `URIs:`. Mirrorlist format: one URI per line, `#` comments, TAB, then `key:value` tags (`priority:N` with the lowest tried first, plus `arch: codename: component: lang: suite: type:`). Untagged lines are tried last, in random order. | V | [apt-transport-mirror(1)](https://manpages.debian.org/trixie/apt/apt-transport-mirror.1.en.html) |
| `Acquire::Retries` (+ `::Delay`, `::Delay::Maximum`) and `Acquire::Queue-Mode` (`host`/`access`) are in apt.conf(5). `Acquire::http::Pipeline-Depth` is in apt-transport-http(1) (default 10), not apt.conf(5). | V | apt.conf(5), apt-transport-http(1), configure-index |
| `-o DPkg::Lock::Timeout=<sec>` makes apt wait for the dpkg lock. | U | apt-get(8) / apt.conf(5) on manpages.debian.org |
| The mirror masterlist at `https://mirror-master.debian.org/status/Mirrors.masterlist` is live, with about 370 RFC822 stanzas (`Site, Country, Type, Archive-architecture, Archive-http, ...`). The salsa repo holds `Mirrors.masterlist.in`, not the generated file. | V | fetched; salsa mirror-team/masterlist |
| Trace file `<archive>/project/trace/master`: a date line, then `Archive serial:`, `Date:` and `Architectures:`. | V | fetched from deb.debian.org |

## Packages (non-obvious ones)

All are in **main** unless noted. Source: `https://packages.debian.org/trixie/<pkg>` (V).

- **non-free-firmware:** `amd64-microcode`, `intel-microcode`, `firmware-amd-graphics`, `firmware-misc-nonfree`, `firmware-realtek`, `firmware-iwlwifi`, `firmware-atheros`, `firmware-mediatek`, `firmware-brcm80211` (firmware-nonfree 20250410).
- **non-free:** `nvidia-driver` (550.163), `intel-media-va-driver-non-free`. **contrib:** `libdvd-pkg`.
- **Renamed or transitional:** `pkg-config` is a transitional package for **`pkgconf`**, so declare `pkgconf`.
- **Not packaged:** **`uv`**. The CLI isn't in trixie under any name (forky/sid only builds `python3-uv-build`). Install it with `pipx install uv` instead.
- **Versions that matter:**
  - `cockpit`: 337 in trixie, **371** in trixie-backports.
  - `linux-image-amd64`: 6.12.x in trixie, **7.2.x** in trixie-backports.
  - `golang-go`: 1.24 in trixie, **1.27** in trixie-backports.
  - `gh`: 2.46 (old).
  - `docker.io`: 26.1.5 (too old; see Docker).
  - `firewalld`: 2.3.1. `fail2ban`: 1.1.0. `network-manager`: 1.52.1. `power-profiles-daemon`: 0.30. `tuned`/`tuned-ppd`: 2.25.1. `systemd`: 257. `openssh`: 10.0p1. GNOME: 48 (`gnome-remote-desktop` 48.1, `gdm3` 48.0). `upower`: 1.90.9.
- Also verified in main: `task-gnome-desktop`, `libavcodec-extra`, `gstreamer1.0-plugins-{good,bad,ugly}`, `gstreamer1.0-libav`, `mesa-va-drivers`, `vainfo`, `extrepo`, `flatpak`, `gnome-software-plugin-flatpak`, `git-lfs`, `neovim`, `btop`, `ripgrep`, `fd-find`, `fzf`, `bat`, `shellcheck`, `bind9-dnsutils`, `mtr-tiny`, `netcat-openbsd`, `ninja-build`, `systemd-zram-generator`, `zram-tools`, `thinkfan`, `lm-sensors`, `python3-systemd`, `unattended-upgrades`, `apt-listchanges`, `needrestart`, `pipx`, `python3-venv`, `fwupd`, `systemd-timesyncd`, `chrony`.
- **Not yet checked:** the rest of the devtools list (`build-essential`, `tmux`, `htop`, `tree`, `strace`, `traceroute`, `cmake`, `libssl-dev`, `bash-completion`, `man-db`, `libxml2-dev`, `libxslt1-dev`, `libffi-dev`, `python3-dev`). These are almost certainly fine, but `/check-packages` must confirm each one on packages.debian.org. (U)

## Networking

| Fact | Status | Source |
|---|---|---|
| NetworkManager.conf ships `plugins=ifupdown,keyfile` + `[ifupdown] managed=false`. Interfaces in `/etc/network/interfaces` stay unmanaged, and NM leaves them alone. | V | [sources: network-manager 1.52.1 debian/NetworkManager.conf](https://sources.debian.org/src/network-manager/1.52.1-1/debian/NetworkManager.conf/), [wiki](https://wiki.debian.org/NetworkManager) |
| `cockpit-networkmanager` depends on network-manager and is "incompatible with systemd-networkd/ifupdown defined networks". | V | [packages.debian.org](https://packages.debian.org/trixie/cockpit-networkmanager) |
| NM has a native checkpoint/rollback D-Bus API (`CheckpointCreate` with a rollback timeout), but nmcli has no command for it, and it can't undo ifupdown state. | V/U | libnm docs |
| NM's internal DHCP client may send a different client-id than dhcpcd/dhclient did, so the IP can change after migration. | R | kiln copies the old client ID; if the IP still changes, it shows the new one |

## SSH

| Fact | Status | Source |
|---|---|---|
| Service is `ssh.service` (enabled). `ssh.socket` is shipped `--no-enable`. | V | openssh 1:10.0p1 `debian/rules`, README.Debian |
| `sshd_config` has `Include /etc/ssh/sshd_config.d/*.conf` near the top, and the first value wins, so low-numbered drop-ins take precedence. | V | Debian sshd_config, sshd_config(5) |
| DSA support is removed. Default KEX dropped the finite-field DH groups. `~/.pam_environment` isn't read. | V | release notes §5.1.7–5.1.8, openssh debian/NEWS |
| No SELinux (AppArmor), so a port change needs no `semanage`. | V | Debian default |

## Firewall and fail2ban

| Fact | Status | Source |
|---|---|---|
| No firewall is active by default. nftables is the framework. | U | trixie release notes / Debian wiki nftables page; kiln checks `nft list ruleset` at run time anyway |
| firewalld 2.3.1 supports `StrictForwardPorts` (added in 2.3.0). | V | [firewalld blog](https://firewalld.org/2024/11/strict-forward-ports), firewalld.conf(5) |
| Docker with firewalld creates a `docker` zone (target ACCEPT) and a `docker-forwarding` policy. Published ports bypass zone rules unless `StrictForwardPorts=yes`. | V | [Docker packet filtering docs](https://docs.docker.com/engine/network/packet-filtering-firewalls/), [firewalld blog](https://firewalld.org/2024/04/strictly-filtering-docker-containers) |
| Docker 28.3.3 fixed loopback-bound published ports becoming LAN-reachable after a firewalld reload. Docker 29 added an *experimental* nftables backend. | V | [Docker 28 release notes](https://docs.docker.com/engine/release-notes/28/), [nftables docs](https://docs.docker.com/engine/network/firewall-nftables/) |
| fail2ban 1.1.0-8 depends on `python3-systemd`. `jail.d/defaults-debian.conf` sets `banaction = nftables`, `banaction_allports = nftables[type=allports]`, and `[sshd] backend = systemd`, `journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd`. | V | [sources.debian.org fail2ban 1.1.0-8](https://sources.debian.org/src/fail2ban/1.1.0-8/debian/debian-files/) |
| There's no `/var/log/auth.log` (rsyslog isn't installed by default). | U | Debian rsyslog / journald docs for trixie; fail2ban uses the journal regardless |

## GitHub CLI repo

| Fact | Status | Source |
|---|---|---|
| Official repo: keyring `https://cli.github.com/packages/githubcli-archive-keyring.gpg` → `/etc/apt/keyrings/githubcli-archive-keyring.gpg`; `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main`; package `gh`. | U | github.com/cli/cli docs/install_linux.md; confirm before implementing |

## Docker repo

| Fact | Status | Source |
|---|---|---|
| `download.docker.com/linux/debian/dists/trixie` exists (stable, amd64). | V | fetched |
| Key at `/etc/apt/keyrings/docker.asc`. deb822 `docker.sources` with `Types/URIs/Suites/Components: stable/Architectures/Signed-By`. | V | [Docker Debian install docs](https://docs.docker.com/engine/install/debian/) |
| Remove: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`. | V | same |

## Power, thermal, battery

| Fact | Status | Source |
|---|---|---|
| power-profiles-daemon `Conflicts: laptop-mode-tools, tlp`. `tuned-ppd` `Conflicts/Provides: power-profiles-daemon`. | V | sources.debian.org control files (ppd 0.30-1.1, tuned 2.25.1-1) |
| ppd writes `/sys/firmware/acpi/platform_profile` itself, adopts external changes, and (amd-pstate active) sets EPP/governor/boost per profile. | V | [ppd 0.30 source](https://sources.debian.org/src/power-profiles-daemon/0.30-1.1/src/) |
| timesyncd is the default. timesyncd and chrony both `Provides/Conflicts/Replaces: time-daemon`. | V | systemd 257 and chrony 4.6.1 control files |
| `systemd-zram-generator` 1.2.1 ships `/usr/lib/systemd/zram-generator.conf` enabling zram0 at `min(ram / 2, 4096)`. `/etc/systemd/zram-generator.conf` overrides it. | V | [package file list](https://packages.debian.org/trixie/amd64/systemd-zram-generator/filelist), [zram-generator.conf(5)](https://github.com/systemd/zram-generator/blob/main/man/zram-generator.conf.md) |
| Kernel ABI: `charge_control_{start,end}_threshold` (0–100, may round), `charge_behaviour` (`auto`, `inhibit-charge`, `inhibit-charge-awake`, `force-discharge`), `charge_types` (`Custom`, `Long_Life`, ...). | V | [sysfs-class-power](https://www.kernel.org/doc/Documentation/ABI/testing/sysfs-class-power) |
| Vendor matrix (ThinkPad, ASUS, Dell, Huawei, IdeaPad, LG, Samsung, Sony, Toshiba, MSI, System76, Framework). HP isn't listed. | V | [TLP battery care vendors](https://linrunner.de/tlp/settings/bc-vendors.html) |
| hp-wmi exposes only `charge_behaviour`, DMI-gated to two Spectre x360 boards, and only in kernels newer than 6.12. | V | mainline `drivers/platform/x86/hp/hp-wmi.c` |
| thinkpad_acpi rejects start > current stop and stop < current start. `fan_control=1` enables writes to `/proc/acpi/ibm/fan`. | V | thinkpad_acpi.c, [kernel docs](https://docs.kernel.org/admin-guide/laptops/thinkpad-acpi.html) |
| E14 Gen 5 AMD exposes thresholds, `/proc/acpi/ibm/fan`, and DYTC platform_profile. | R | the battery ladder and thermal step probe at run time |
| Envy x360 13 (4500U) hp-wmi platform_profile works. | R | the thermal step probes `platform_profile_choices` at run time |
| UPower 1.90.9 / GNOME 48 "Preserve Battery Health": enabled only when `charge_control_start_threshold` exists. Limits come from hwdb `CHARGE_LIMIT` (default `75,80`). The flag is stored in a `charging-threshold-status` file in UPower's state dir. Disabling writes 0/100. | V | [upower 1.90.9 source](https://sources.debian.org/src/upower/1.90.9-1/) |
| `/sys/class/platform-profile/` (multi-handler class) needs kernel ≥ 6.14, so it's not in stable 6.12 but is in the backports kernel. | V | sysfs-class-platform-profile ABI |

## GNOME / GDM / remote desktop

| Fact | Status | Source |
|---|---|---|
| Debian ships no `/etc/dconf/profile/user` (only debian-edu-config does). | V | packages.debian.org contents search |
| `/etc/gdm3/greeter.dconf-defaults` is ucf-managed (three-way merged from `/usr/share/gdm/greeter.dconf-defaults`) and symlinked as `/usr/share/gdm/dconf/90-debian-settings`. | V | gdm3 48.0-2 postinst, README.Debian |
| `grdctl --system ...` exists in 48.1. `--system` must come first, and it re-runs via `pkexec --user gnome-remote-desktop`. `rdp set-credentials` with no args prompts on stdin. `rdp enable` in system mode also enables the unit. TLS files go in `~gnome-remote-desktop/.local/share/gnome-remote-desktop/`. The unit is disabled by default. | V | [grd-ctl.c 48.1-4](https://sources.debian.org/src/gnome-remote-desktop/48.1-4/src/grd-ctl.c/), README.md. (The grdctl(1) manpage is outdated.) |
| Running `grdctl --system` as root without a polkit agent works non-interactively. | U | grd-ctl.c + polkit rules in gnome-remote-desktop 48.1 source; design: kiln uses `runuser` so no polkit prompt is needed |

## Auto-updates

| Fact | Status | Source |
|---|---|---|
| needrestart 3.11 reads `/etc/needrestart/conf.d/*.conf` (sorted). The default restart mode `'i'` becomes list-only when non-interactive. | V | needrestart.conf |

## Still to verify (tracked)

- debian-installer netcfg: when `network-manager` is installed during the install (GNOME task), does netcfg leave only `lo` in `/etc/network/interfaces` and hand the interface to NM? (netcfg source on salsa).
- tasksel in trixie: exact task package names (`task-gnome-desktop`, `task-ssh-server`) and how Debian blends show up as installed packages.

- `DPkg::Lock::Timeout` behavior in apt 3.0.
- `libdvd-pkg` debconf question names, and the libdvdcss install path.
- DKMS MOK key path and the Secure Boot signing flow for `nvidia-driver` 550 on trixie.
- extrepo: the `google_chrome` name and which policy (`non-free`) it needs.
- `fwupdmgr` exit codes and the `--no-reboot-check` flag.
- `thinkfan` config-test flag.
- Whether `tuned`'s `throughput-performance` sets platform_profile.
- Whether `systemctl reload systemd-logind` applies `Handle*` changes without killing sessions.
- Whether `playwright install-deps --dry-run` prints the apt package list.
- Whether the backports kernel needs backported firmware packages.
