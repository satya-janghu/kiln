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
| `Acquire::Retries` (default 3) and `Acquire::Queue-Mode` (`host`/`access`) are in apt.conf(5). `::Delay` / `::Delay::Maximum` exist only in configure-index and the source, not apt.conf(5). `Acquire::http::Pipeline-Depth` is in apt-transport-http(1) (default 10). | V | apt.conf(5), apt-transport-http(1), apt 3.0.3 `doc/examples/configure-index` |
| `-o DPkg::Lock::Timeout=<sec>` exists (configure-index, `debsystem.cc`) but isn't in apt-get(8) or apt.conf(5). It waits only on the dpkg `lock-frontend`/`lock`. `/var/lib/apt/lists/lock` and `/var/cache/apt/archives/lock` fail immediately. | V | apt 3.0.3 source (`apt-pkg/deb/debsystem.cc`, `acquire.cc`, `private-install.cc`) |
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
- The whole devtools list (34 packages, including `build-essential`, `tmux`, `htop`, `tree`, `strace`, `traceroute`, `cmake`, `libssl-dev`, `bash-completion`, `man-db`) and `libxml2-dev`, `libxslt1-dev`, `libffi-dev` and `python3-dev` are in trixie main. (V, qa.debian.org madison s=trixie, 2026-11-07)
- Binary names: `fd-find` 10.2.0 ships `/usr/bin/fdfind`, and `bat` 0.25.0 ships `/usr/bin/batcat`. (V, packages.debian.org file lists)

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
| No firewall is installed by default. nftables is the framework. Installing `firewalld` enables and starts it at once (plain `dh`), with default zone `public` (ssh + dhcpv6-client). | V | firewalld 2.3.1-1+deb13u1 `debian/rules`, `config/zones/public.xml` |
| firewalld 2.3.1 supports `StrictForwardPorts` (added in 2.3.0). | V | [firewalld blog](https://firewalld.org/2024/11/strict-forward-ports), firewalld.conf(5) |
| Docker with firewalld creates a `docker` zone (target ACCEPT) and a `docker-forwarding` policy. Published ports bypass zone rules unless `StrictForwardPorts=yes`. | V | [Docker packet filtering docs](https://docs.docker.com/engine/network/packet-filtering-firewalls/), [firewalld blog](https://firewalld.org/2024/04/strictly-filtering-docker-containers) |
| Docker 28.3.3 fixed loopback-bound published ports becoming LAN-reachable after a firewalld reload. Docker 29 added an *experimental* nftables backend. | V | [Docker 28 release notes](https://docs.docker.com/engine/release-notes/28/), [nftables docs](https://docs.docker.com/engine/network/firewall-nftables/) |
| fail2ban 1.1.0-8 depends on `python3-systemd`. `jail.d/defaults-debian.conf` sets `banaction = nftables`, `banaction_allports = nftables[type=allports]`, and `[sshd] backend = systemd`, `journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd`. | V | [sources.debian.org fail2ban 1.1.0-8](https://sources.debian.org/src/fail2ban/1.1.0-8/debian/debian-files/) |
| There's no `/var/log/auth.log`: rsyslog isn't installed by default, and fail2ban's Debian config uses the journal for sshd. | V | fail2ban 1.1.0-8 `paths-debian.conf` / `defaults-debian.conf`; rsyslog is Priority optional |

## GitHub CLI repo

| Fact | Status | Source |
|---|---|---|
| Official repo: keyring `https://cli.github.com/packages/githubcli-archive-keyring.gpg` (sha256 `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b`) → `/etc/apt/keyrings/`. `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main` (the Release file has Codename `stable`). Package `gh`. The doc uses a one-line source; kiln writes the deb822 equivalent. | V | [install_linux.md](https://github.com/cli/cli/blob/trunk/docs/install_linux.md), repo Release file (2026-11-07) |

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
| Kernel ABI: `charge_control_{start,end}_threshold` (0–100; drivers may round). `charge_behaviour` is `auto`/`inhibit-charge`/`force-discharge` in 6.12; `inhibit-charge-awake` exists only in later kernels (present in 7.2). `charge_types` (`Custom`, `Long_Life`, …) is documented from 6.14, but dell-laptop exposes it in 6.12. `scope` exists in `power_supply_sysfs.c` but is undocumented, and the ACPI battery driver doesn't expose it. Threshold writes emit no uevent. | V | sysfs-class-power at v6.12 and v7.2; `drivers/power/supply/` |
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
| `grdctl --system` always re-executes via `pkexec --user gnome-remote-desktop`. As root this works with no prompt (polkit 126 always authorizes uid 0), but the outer grdctl exits 0 even when the inner command fails, and `pkexec` is a separate package. grdctl enters system mode by itself when its euid is the `gnome-remote-desktop` user, so kiln runs it as that user via `runuser`, **without** `--system`. | V | [grd-ctl.c 48.1-4](https://sources.debian.org/src/gnome-remote-desktop/48.1-4/src/grd-ctl.c/), [polkit 126-2](https://sources.debian.org/src/policykit-1/126-2/src/polkitbackend/polkitbackendinteractiveauthority.c/) |
| `rdp set-credentials <user>` reads only the password from stdin. Sending user + password on stdin is unsafe: each prompt opens a new buffered reader on fd 0. `rdp enable` goes through `/usr/libexec/gnome-remote-desktop-enable-service` (polkit check → systemd StartUnit + EnableUnitFiles), so it needs `polkitd`. The unit ships disabled via `dh_installsystemd --no-enable`. The system user and its 0700 home come from sysusers/tmpfiles. | V | same source, debian/rules, postinst |
| `openssl req -x509` defaults to a 30-day certificate and prompts for a subject unless `-subj` is given. `-nodes` is deprecated in favor of `-noenc` (OpenSSL 3.5). | V | [openssl-req(1) trixie](https://manpages.debian.org/trixie/openssl/openssl-req.1ssl.en.html) |

## Apps and languages

| Fact | Status | Source |
|---|---|---|
| extrepo 0.14: `/etc/extrepo/config.yaml` key `enabled_policies:` (default `main` only). `google_chrome` has policy `non-free`. `extrepo enable` writes `/etc/apt/sources.list.d/extrepo_<name>.sources` with `Signed-By: /var/lib/extrepo/keys/<name>.asc`; a re-run only sets `Enabled: yes`. Chrome's post-enable command writes `/etc/default/google-chrome` (`repo_add_once="false"`) to stop a duplicate source. | V | [extrepo 0.14 source](https://sources.debian.org/src/extrepo/0.14/), extrepo-data live index |
| `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo` (flatpak 1.16.6). `gnome-software-plugin-flatpak` is 48.3. | V | [flatpak-remote-add(1) trixie](https://manpages.debian.org/trixie/flatpak/flatpak-remote-add.1.en.html), flathub.org/setup/Debian |
| nvm v0.41.0 (2026-11-04) is the latest release. install.sh honours `PROFILE=/dev/null`, `METHOD=script`, and `NVM_DIR` (exits 1 if an explicit NVM_DIR doesn't exist). Node 26 is LTS "Lithium" since v26.13.0 (2026-10-28). | V | [nvm v0.41.0 install.sh](https://github.com/nvm-sh/nvm/blob/v0.41.0/install.sh), nodejs.org/dist/index.json |
| pipx 1.7.1 installs to `~/.local/bin`; a re-run exits 0. uv 0.13.7: `uv venv` errors on a non-empty path, `uv pip install --python <venv-dir>` works, and `uv tool install` takes one package per call. | V | pipx paths.py 1.7.1, docs.astral.sh/uv |
| Playwright 1.64 supports Debian 13. `install-deps --dry-run chromium` prints `Missing system dependencies (N):` plus one indented package per line and exits 1, or prints "All system dependencies are installed." and exits 0. | V | playwright v1.64.0 `registry/dependencies.ts`, playwright.dev |
| `go install` accepts several packages only from one module, so tools need separate runs. go.dev JSON fields: `version`, `stable`, `files[].{filename,os,arch,version,sha256,size,kind}`. Debian's golang keeps `GOTOOLCHAIN=auto`. | V | go.dev/ref/mod, go.dev/dl/?mode=json, go1.27.3 |

## Auto-updates

| Fact | Status | Source |
|---|---|---|
| needrestart 3.11 reads `/etc/needrestart/conf.d/*.conf` (sorted). The default restart mode `'i'` becomes list-only when non-interactive. | V | needrestart.conf |

## Still to verify (tracked)

- `dockerd --validate --config-file` exists in Docker 29 (step 17). Until it's verified, kiln only does its own JSON parse plus a restart-and-check.
- Whether UPower 1.90.9 re-reads hwdb `CHARGE_LIMIT` on a udev change event, or only at startup (step 13 does `try-restart upower` either way).
- How Docker reacts to firewalld being (re)started after it. kiln restarts docker after firewall changes either way.
- R (run time): whether the backports kernel works better with backported firmware (no packaging relation exists); the amd-pstate default mode on each laptop; whether `/var/log/journal` exists.

## Verified 2026-11-07 (second pass: every catalog step re-checked against the exact trixie versions)

Sources are cited inline in [steps-catalog.md](steps-catalog.md) under each step's "Facts used". The headline facts are below.

| Fact | Status | Source |
|---|---|---|
| d-i (apt-setup 0.198) writes a **one-line** `sources.list` with active `deb-src` lines and `main non-free-firmware`. cdrom lines are commented out unless the medium was a real CD. | V | apt-setup 1:0.198 generators/50mirror, 91security, templates, changelog |
| `apt -y modernize-sources` honours `-y`. It renames `sources.list` to `sources.list.bak` and `X.list` to `X.list.bak`, and appends main-file entries to `<origin>.sources` (`debian.sources` for Debian). It can leave `Signed-By` empty with a warning. | V | apt 3.0.3 `apt-private/private-sources.cc`, `private-cmndline.cc` |
| The mirrorlist method sorts by priority and randomises only among **equal** priorities, per file. | V | apt 3.0.3 `methods/mirror.cc` |
| `dists/trixie/main/Contents-amd64.gz` is about 12.7 MB; `Contents-all.gz` is about 38 MB. The masterlist advertises HTTP only. | V | HEAD on deb.debian.org; Mirrors.masterlist |
| `/run/reboot-required` is written only by unattended-upgrades' kernel postinst hook. | V | unattended-upgrades 2.12 `kernel/postinst.d/unattended-upgrades` |
| Debian's `50unattended-upgrades` allows `label=Debian` (point releases) as well as security. `Origins-Pattern` accumulates across files, and `#clear` resets it. `20auto-upgrades` is ucf-managed from debconf `enable_auto_updates`. Upgrades run from `apt-daily-upgrade.timer`. | V | unattended-upgrades 2.12 data/, postinst, templates; apt.conf(5) |
| sudoers.d skips names ending in `~` or containing `.`. visudo(8): checking a single include file isn't sufficient, so run `visudo -c`. | V | sudo 1.9.16p2 sudoers(5), visudo(8) |
| tasksel 3.81: `task-gnome-desktop` → `gnome-core` (gdm3, gnome-shell); `task-ssh-server` → `openssh-server`; "standard" is Priority standard, not a package. Blends are `task-*-blend` (blendsel 0.10); Debian Edu isn't offered. | V | tasksel 3.81, blendsel 0.10 debian/control |
| `mokutil` is present only via shim-signed. The Secure Boot state can be read from efivar `SecureBoot-8be4df61-…` byte 5. | V | mokutil 0.7.2-1 |
| `systemd-inhibit --what=sleep:idle:handle-lid-switch` is valid, and needed because `LidSwitchIgnoreInhibited=yes` is the default. A non-root caller isn't allowed `handle-lid-switch`. | V | systemd 257.13 systemd-inhibit(1), logind.conf(5), login1 polkit policy |
| firmware-nonfree 20250410 (backports 20260916) splits out `firmware-intel-graphics`, `firmware-intel-misc`, `firmware-nvidia-graphics` and `firmware-mediatek`. `firmware-misc-nonfree` only Recommends them. | V | firmware-nonfree 20250410-2 debian/, packages.debian.org |
| Kernels: trixie 6.12.x, backports 7.2.x, both from `linux-signed-amd64`. The backports image needs `linux-base ≥ 4.17~` (backports only). Image names are `linux-image-<ver>+deb13[.N]-amd64`. There's no `linux-image-6.12-amd64` metapackage. unattended-upgrades installs new names only as dependencies of an upgrade. | V | qa.debian.org madison; unattended-upgrades 2.12 source |
| `nvidia-driver` 550.163 (non-free, DKMS) pulls `firmware-nvidia-gsp`. Its newest backports build documents fixes only up to Linux 6.17. dkms 3.2.2: `/var/lib/dkms/mok.{key,pub}`, `dkms generate_mok`. mokutil: `--import … --hash-file`, `--generate-hash=`, `--test-key`, `--list-new`. | V | nvidia-graphics-drivers changelog; dkms 3.2.2-1~deb13u1 dkms.in; mokutil(1) |
| libdvd-pkg 1.4.3: templates `build` (boolean), `post-invoke_hook-install` (boolean), and `first-install` (note). `dpkg-reconfigure -f noninteractive` builds `libdvdcss2` into `/usr/lib/x86_64-linux-gnu/libdvdcss.so.2`. | V | libdvd-pkg 1.4.3-1-1.1 debian/templates, config, postinst |
| `libavcodec-extra61` Conflicts/Provides `libavcodec61`. `intel-media-va-driver-non-free` Conflicts `intel-media-va-driver`. | V | ffmpeg 7:7.1.5 and intel-media-driver debian/control |
| fwupd 2.0.20: exit 2 = nothing to do, 101 = no network. `-y`, `--no-reboot-check`, `--no-device-prompt`, `--json` and `check-reboot-needed` exist. LVFS is on by default. The UEFI capsule plugin needs an ESRT, an ESP and `fwupd-amd64-signed` under Secure Boot. | V | fwupd 2.0.20-1~deb13u1 src/fu-util*.c, plugins/uefi-capsule/README.md |
| netcfg 1.197: with NM installed in the target, connections are copied to NM, and `interfaces` gets only `lo`. | V | netcfg 1.197 finish-install.d/55netcfg-copy-config, nm-conf.c |
| ifupdown 0.8.44 prefers dhclient over udhcpc over dhcpcd, and `ifdown` of an interface with no stanza runs nothing. dhcpcd 10.1.0 uses a DUID client-id (`/var/lib/dhcpcd/duid`). NM accepts a hex `ipv4.dhcp-client-id`; NM's `duid` mode hashes machine-id. NM ignores keyfiles that aren't root-only. | V | ifupdown inet.defn, main.c; dhcpcd 10.1.0 src/; nm-settings-nmcli(5), nm-settings-keyfile(5) |
| ifupdown 0.8.44+deb13u1 `inet dhcp` starts `dhclient -4 …`, `udhcpc …` or plain `dhcpcd <iface>` (no `-4`). So dhcpcd also does IPv6 (RA, SLAAC, DHCPv6) with no `inet6` stanza, unless `dhcpcd.conf` has `ipv4only` or `noipv6`. dhcpcd's IAID defaults to the last 4 bytes of the MAC and `ia_na` uses that IAID. `slaac private`/`stableprivate`/`stable` gives RFC 7217 addresses, any other value or no line gives hwaddr, and trixie's shipped `dhcpcd.conf` has `slaac private`. NM: `ipv6.addr-gen-mode` is eui64 or stable-privacy; `ipv6.dhcp-duid` takes a hex DUID; `ipv6.dhcp-iaid` takes a 32-bit number or colon-hex. On the HP test machine dhcpcd ran as `wlo1 [ip4] [ip6]`. | V | sources.debian.org ifupdown 0.8.44+deb13u1 inet.defn; dhcpcd 1:10.1.0-11+deb13u4 src/dhcpcd.conf, src/if-options.c; manpages.debian.org/trixie dhcpcd.conf(5), nm-settings-nmcli(5); field test 2026-11-07 |
| ifupdown 0.8.44+deb13u1 units: `ifup@.service` runs `ExecStart=/usr/sbin/ifup --allow=hotplug %I` and `ExecStop=/usr/sbin/ifdown %I`, with `RemainAfterExit=true`, `BindsTo=sys-subsystem-net-devices-%i.device` and `TimeoutStartSec=5min`. udev's `ifupdown-hotplug` starts it as `systemctl --no-block start $(systemd-escape --template ifup@.service <iface>)` for `allow-hotplug` interfaces. `networking.service` (oneshot, `RemainAfterExit`) runs `ifup -a --read-environment` and stops with `ifdown -a --read-environment --exclude=lo`; `ExecStopPost` touches `/run/network/restart-hotplug`, so a restart also brings hotplug interfaces up. `ifdown` of an interface that isn't configured warns and succeeds. | V | sources.debian.org ifupdown 0.8.44+deb13u1 debian/ifup@.service, debian/networking.service, debian/ifupdown-hotplug, main.c; systemd.unit(5) String Escaping |
| A unit with `ExecStart=` and no `Type=` is `Type=simple`, so `systemctl start`/`restart` reports success right after fork and doesn't wait for the program to finish. `ifup@.service` is such a unit: seen on the HP test machine, where `systemctl restart ifup@wlo1.service` returned in 52 ms and `ifup` failed 120 ms later. | V | manpages.debian.org/trixie systemd.service(5) `Type=`; field test 2026-11-07 |
| firewalld 2.3.1: `--new-zone` is permanent-only; `--check-config` checks permanent config; a reload replaces only `table inet firewalld`; services `cockpit`, `rdp` and `ssh` are shipped; there's no zone drifting. Debian's stock `/etc/nftables.conf` starts with `flush ruleset`. | V | firewall-cmd(1) trixie; firewalld 2.3.1 nftables.py, config/services; nftables 1.1.3-1 |
| openssh 10.0p1: Debian's sshd_config already sets `KbdInteractiveAuthentication no`. `Port` lines accumulate. `sshd -T` applies Match only with `-C`. `ssh.service` ExecReload runs `sshd -t` then HUP. | V | openssh 1:10.0p1-7+deb13u4 debian/, sshd(8), sshd_config(5) |
| fail2ban 1.1.0: a jail-level `journalmatch` overrides the filter's, so `_COMM=sshd-session` must be listed. | V | fail2ban 1.1.0-8 configreader/jailreader, filter.d/sshd.conf |
| cockpit 337/371 Recommends `cockpit-storaged`, `cockpit-networkmanager` and `cockpit-packagekit` (not machines/podman). The socket is enabled on install. postinst writes `root` to `disallowed-users`. | V | cockpit 337-1+deb13u2 / 371-1~bpo13+1 debian/control, postinst |
| docker-ce 29.9.3 Depends `iptables, nftables`. log-opts values must be strings and apply to new containers only. | V | download.docker.com Packages; docs.docker.com json-file driver |
| logind is `Type=notify-reload`, and SIGHUP re-reads the config. `HandleSuspendKeyLongPress` defaults to hibernate. journald.conf(5) recommends drop-in prefixes 60–90. | V | systemd 257.13 units/systemd-logind.service.in, src/login/, journald.conf(5) |
| GDM 48: greeter power keys are commented out in `greeter.dconf-defaults`. `systemctl reload gdm3` runs `/usr/share/gdm/generate-config` and SIGHUP, which keeps sessions. gsd 48.1 `power-button-action` defaults to `'suspend'`. | V | gdm3 48.0-2 debian/generate-config, data/gdm.service.in, daemon/main.c; gsd 48.1 gschema |
| ppd 0.30 persists in `/var/lib/power-profiles-daemon/state.ini`, and offers `performance` only with amd-pstate EPP or a platform_profile that has both balanced and performance. tuned 2.25.1 `throughput-performance` sets `platform_profile=performance`, EPP performance, and `vm.swappiness=10`. | V | ppd 0.30-1.1 src/; tuned 2.25.1-1 profiles/throughput-performance/tuned.conf |
| thinkfan 1.3.1 has **no** config-test flag (`-n` foreground, `-c` config, `-D` dangerous). It reads `/etc/thinkfan.yaml`, then `.conf`. The unit ships disabled. | V | thinkfan.1, thinkfan 1.3.1-6 debian/rules |
| `sensors-detect --auto` writes `/etc/modprobe.d/lm_sensors.conf` when needed and never `/etc/modules`. On these laptops the hwmon drivers autoload anyway. | V | lm-sensors 3.6.2 prog/detect/sensors-detect |
| systemd.device(5): device `Wants=` (SYSTEMD_WANTS) act only when a device first becomes active, never on change events. | V | systemd 257 systemd.device(5) |
| dell-laptop clamps thresholds silently. asus-wmi's read-back is a cached value. thinkpad_acpi allows start == stop. | V | linux v6.12 drivers/platform/x86/ |
| 6.12 inotify `max_user_watches` scales with RAM (clamped 8192…1048576), and no Debian sysctl.d touches it. zram-generator supports `zram-generator.conf.d/*.conf` drop-ins. Restarting `systemd-zram-setup@zram0` also restarts `dev-zram0.swap` (swapoff). | V | linux v6.12 inotify_user.c; rust-zram-generator 1.2.1-2; systemd.unit(5) |

## NVIDIA's Debian 13 repository (checked 2026-11-07)

| Fact | Status | Source |
|---|---|---|
| The repository is `https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/`, a flat repository (one-line form `… x86_64/ /`). Its Release has `Origin: NVIDIA`, `Label: NVIDIA CUDA`. Branches with pinning packages: 590, 595, 610, 615 and 620 (newest 620.70.06); **no 580**. | V | repository index, `Packages.gz`, `InRelease` |
| Setup per NVIDIA's guide: `cuda-keyring_1.1-1_all.deb` (sha256 `d0d4ef98…f5f`) ships `/usr/share/keyrings/cuda-archive-keyring.gpg` (fingerprint `02182E60104FCDC26EAE1B8597A5D4CB8793F200`, uid "Kitmaker (Debian 13 Trixie)") and a one-line `/etc/apt/sources.list.d/cuda-debian13-x86_64.list`. There's no standalone keyring file at the repository URL (404). | V | NVIDIA driver installation guide (Debian); package contents inspected |
| Optional pinning: `apt install nvidia-driver-pinning-<branch>` before the driver. From 590 onward the default is to follow the latest drivers. | V | NVIDIA driver installation guide, "Recent updates" |
| Only open kernel modules are shipped ("The proprietary kernel modules have been removed"). In 620, `nvidia-kernel-dkms` and `cuda-drivers` are transitional packages pointing to `nvidia-kernel-open-dkms` and `nvidia-open`. | V | NVIDIA guide; repository Packages metadata |
| Install sets: full `nvidia-open`; desktop-only `nvidia-driver nvidia-kernel-open-dkms`; compute-only `nvidia-driver-cuda nvidia-kernel-open-dkms`. `nvidia-kernel-open-dkms` 620 depends on `dkms (>= 3.1.8)`, `firmware-nvidia-gsp (= 620…)` and `g++`. | V | NVIDIA guide; Packages metadata |
| The open kernel modules "can be used on any Turing or later GPU" and need Linux ≥ 4.15. | V | NVIDIA/open-gpu-kernel-modules README (620.70.06) |
| Debian's `nvidia-driver` 550 "supports Maxwell, Pascal, Volta, Turing, Ampere and Ada/Hopper GPUs, it does not support Blackwell", and "is no longer maintained and contains several security issues". Debian's `nvidia-detect` is described as outdated. | V | [wiki.debian.org/NvidiaGraphicsDrivers](https://wiki.debian.org/NvidiaGraphicsDrivers) |
| deb822 flat repositories: `Suites:` is a path ending in `/`, and `Components:` must then be omitted. | V | sources.list(5), trixie |

## Automatic updates schedule (checked 2026-11-07)

| Fact | Status | Source |
|---|---|---|
| `apt-daily.timer`: `OnCalendar=*-*-* 6,18:00`, `RandomizedDelaySec=12h`, runs `apt.systemd.daily update`. `apt-daily-upgrade.timer`: `OnCalendar=*-*-* 6:00`, `RandomizedDelaySec=60m`, runs `apt.systemd.daily install` (unattended-upgrade). Both services have `ConditionACPower=true`. | V | apt 3.0.3 `debian/apt-daily*.timer`, `apt-daily*.service` |
| `APT::Periodic::*` intervals are in days, accept `s`/`m`/`h`/`d` suffixes, or `always` ("which can be used with systemd OnCalendar overrides to define custom schedules"). | V | apt 3.0.3 `debian/apt.systemd.daily` |
| unattended-upgrades matches `Origins-Pattern` values with `fnmatch`, so `origin=*` matches any origin. | V | unattended-upgrades 2.12 `unattended-upgrade` (`match_whitelist_string`) |
| Release `Origin`/`Label`: Docker `Docker`/`Docker CE`; GitHub CLI `gh`/`gh`; Chrome `Google LLC`/`Google`; NVIDIA `NVIDIA`/`NVIDIA CUDA`. | V | each repository's InRelease |

## Process and restart behaviour (checked 2026-11-07)

| Fact | Status | Source |
|---|---|---|
| needrestart 3.11: `NEEDRESTART_SUSPEND` (non-empty) makes the apt hook skip needrestart and keep `/run/needrestart/unpacked` for the next run. Debian's `needrestart.conf` `override_rc` never restarts dbus, display managers (gdm…), systemd-logind, NetworkManager, wpa_supplicant, docker, getty@, user@, apt-daily* or unattended-upgrades. `-b` (batch) never restarts anything; `-r a` restarts. | V | needrestart 3.11-1 `ex/apt/apt-pinvoke`, `ex/needrestart.conf`, `needrestart`, needrestart(1) |
| fwupd 2.0.20: `--no-reboot-check` returns before the "Restart now?" prompt; `-y` doesn't answer that prompt; without a TTY `no_reboot_check` is forced on; the only reboot path is that prompt. | V | fwupd 2.0.20-1~deb13u1 `src/fu-util.c`, `fu-util-common.c` |
| systemd 257: a `block` shutdown inhibitor refuses every logind reboot/poweroff caller, root included (`systemctl reboot` and `reboot` go through logind first). `-i`/`--check-inhibitors=no` and `--force` bypass it. | V | systemd 257.13 `src/login/logind-dbus.c`, `systemctl-compat-halt.c`, systemctl(1) |
| `systemctl mask --runtime` writes to `/run/systemd/system/` (gone at reboot) and doesn't stop a running unit (use `--now` or `stop`). | V | systemctl(1), systemd 257.13 |
| `Persistent=true` timers fire immediately on (re)start if a run was missed; on the very first start (no stamp) they don't. | V | systemd.timer(5), `src/core/timer.c` |
| `systemd-run --scope` as root lands in `system.slice`. Debian builds with `default-kill-user-processes=false`. | V | systemd 257.13 `src/core/unit.c`, `debian/rules`, systemd-run(1), logind.conf(5) |
| `systemctl reload ssh` (signal-based ExecReload) is asynchronous. sshd re-execs on SIGHUP after closing its listening sockets, so there's a short window with no listener. | V | systemd.service(5), openssh 10.0p1 `sshd.c`, sshd(8) |
| `dpkg --audit` always exits 0, so check its output. `/var/lib/dpkg/updates/` all-digit entries trigger apt's "dpkg was interrupted". Recovery is `dpkg --configure -a`. | V | dpkg 1.22.22 dpkg(1), `src/main/enquiry.c`; apt 3.0.3 `debsystem.cc` |
| NetworkManager 1.52.1: an automatic "Wired connection" is created only for a managed device with no compatible profile, checked once. `nmcli connection up` on an already-activating profile restarts the activation. `nmcli --wait` defaults to 90 s for `up`. | V | NetworkManager.conf(5), `nm-settings.c`, `nm-manager.c`, nmcli(1) |
| `packagekit.service` is D-Bus activated. GNOME Software 48.3 checks hourly, refreshes at most daily, and downloads updates in the background (`download-updates` defaults to true). PackageKit's apt backend gives up on the dpkg lock after 10 s. | V | packagekit 1.3.1, gnome-software 48.3 sources |
| `setsid(2)`: "the new session has no controlling terminal". That terminal signals then don't reach the child is derived from tty job control. | V / derived | setsid(2), manpages 6.9.1 |

## Facts the implementation relies on that aren't verified yet (U) — sign-off blockers

These came out of implementation on 2026-11-07. The code uses them, so each must be checked against a primary source for the trixie version (or replaced) before sign-off (review-process.md). `/audit` lists them.

| Fact the code relies on | Status | Where | Verify against |
|---|---|---|---|
| `apt-config dump` prints list values as `Key:: "v";`, after a header line `Key "";` | V | autoupdates | observed with apt 3.0.4: `Unattended-Upgrade::Origins-Pattern "";` then `Unattended-Upgrade::Origins-Pattern:: "origin=*";`, field test 2026-11-07 (HP test machine) |
| `systemctl show -p TimersCalendar` output format | V (observed) | autoupdates | field check 2026-11-07: `TimersCalendar={ OnCalendar=*-*-* 06,18:00:00 ; next_elapse=… }` |
| `firewall-cmd --query-*` returns 0 (`yes`) if true and 1 (`no`) otherwise, and 112 for INVALID_ZONE. `--get-active-zones` prints each zone name at the start of a line (the default zone gets a ` (default)` suffix), followed by indented `interfaces:`/`sources:` lines | V | firewall | firewall-cmd(1) firewalld 2.3.1 ("Returns 0 if true, 1 otherwise", EXIT CODES); field test 2026-11-07 (HP test machine) |
| `ss -H` suppresses the header; `ss -H -ltn` lines are `LISTEN q q addr:port peer`, with `-p` adding `users:(("sshd",pid=…,fd=…))`. `pgrep -a` prints `pid cmdline` and exits 1 when nothing matches; names longer than 15 characters never match | V | ssh, network-migration | ss(8) iproute2 6.15.0; pgrep(1) procps 2:4.0.4-9 EXIT STATUS; field test 2026-11-07 (HP test machine) |
| `nmcli -t -g A,B` prints the values only, colon-separated, one record per line (`wlo1:wifi:connected:kiln-wlo1`; `GENERAL.STATE` → `100 (connected)`) | V | network-migration | nmcli(1) 1.52.1; field test 2026-11-07 (HP test machine) |
| ifupdown looks for `dhclient`, `udhcpc` and `dhcpcd` in that order (`execable()`). dhcpcd.conf `clientid`/`duid`/`iaid` are scoped globally or per `interface` block. dhclient writes `default-duid` as a quoted string with `\NNN` octal escapes by default, or as unquoted colon hex with `lease-id-format hex`; kiln reads both | V | network-migration | ifupdown 0.8.44+deb13u1 inet.defn; dhcpcd.conf(5) 10.1.0; isc-dhcp 4.4.3-P1-8 client/dhclient.c `write_duid`, common/print.c `format_lease_id`/`quotify_buf`; dhclient.conf(5) `lease-id-format` |
| NM keyfile keys are the nm-settings-nmcli(5) property names in their setting's section, so `[ipv4] dhcp-client-id`, `[ipv6] addr-gen-mode`, `dhcp-duid` and `dhcp-iaid` are written as kiln does. NM honoured them on the test machine (same IPv4 and DHCPv6 address) | V | network-migration | nm-settings-keyfile(5) 1.52.1 FILE FORMAT; nm-settings-nmcli(5); field test 2026-11-07 (HP test machine) |
| Mirror masterlist `Archive-architecture:` is a space-separated list of architecture names (`amd64 arm64 …`); 360 of 368 sites list amd64 | V | mirrors | mirror-master.debian.org/status/Mirrors.masterlist, fetched 2026-11-07 |
| `ssh-keygen -l -f <file>` exits 0 for a file with at least one valid key and 255 (`fatal`: "… is not a public key file.") otherwise | V | ssh | openssh 1:10.0p1-7+deb13u4 ssh-keygen.c `do_fingerprint`; field test 2026-11-07 (HP test machine) |
| go.dev `/dl/?mode=json&include=all` returns every release (369 on 2026-11-07, go1.27.3 included) as `{version, stable, files[{filename, os, arch, version, sha256, size, kind}]}` | V | go | go.dev/dl JSON, fetched 2026-11-07 |
| uv's documented pinned installer is `https://astral.sh/uv/<version>/install.sh` ("Request a specific version by including it in the URL"); 0.13.7 redirects to releases.astral.sh and sets `APP_VERSION="0.13.7"` | V | python | docs.astral.sh/uv/getting-started/installation; the script itself, fetched 2026-11-07 |
| `grdctl status` prints `RDP:` then tab-indented `Status: enabled|disabled` (ANSI bold only when stdout is a TTY), `TLS certificate:`, `TLS key:`, `Username:`/`Password:` as `(hidden)` or `(empty)`. grdctl run as the `gnome-remote-desktop` user uses the system daemon's settings; `--system` re-runs it via pkexec and exits 0 even when the child failed | V | remote-desktop | gnome-remote-desktop 48.1-4 src/grd-ctl.c `print_rdp_status`, `status_to_string`, `main` |
| `extrepo search <re>` prints `Found <name>:` then a YAML dump of that entry (top-level keys unindented), and exits 1 with "No matches found". Entries carry `policy: <p>` or a `policies:` map of component → policy (289 and 44 entries in the trixie index) | V | repos | extrepo 0.14 Commands/Search.pm, Commands/Enable.pm; extrepo-data debian/trixie/index.yaml, fetched 2026-11-07 |
| `mokutil --test-key` exits 0 with "… is not enrolled", 1 with "… is already enrolled / in db / in the enrollment request" or "… is blocked …", and 255 on errors. `--generate-hash` reads the password twice with `getline` and turns off echo only when stdin is a TTY | V | multimedia | mokutil 0.7.2-1 src/mokutil.c `test_key`, `print_skip_message`, `get_password`; src/util.c `read_hidden_line` |
| NVIDIA open-module device-ID table: README.md at tag 620.70.06 (sha256 adb1d8f1…f170) lists 299 IDs, identical to `nvidia_ids.go`. All IDs up to 0x28F8 are Ada or older and all from 0x2901 are Blackwell (B200, GB200, RTX 50); the 550.163.01 README has none at or above 0x2900. RTX 2070 Super (1e84) is listed, GTX 970 (13c2) isn't | V | multimedia | github.com/NVIDIA/open-gpu-kernel-modules README.md at 620.70.06 and 550.163.01, re-derived 2026-11-07 |
| `isenkram-autoinstall-firmware -l` echoes package names on stdout and every `info:` line on stderr; it exits 1 only when no loaded module requests firmware, else 0 (including "No new firmware package") | V | firmware-packages | isenkram 0.69+deb13u1 isenkram-autoinstall-firmware; field test 2026-11-07 (HP test machine) |
| `tee <sysfs attr>` writes the value; an invalid value fails with "Invalid argument" and exit 1, and the attribute keeps its old value | V | battery, thermal | coreutils 9.7 tee(1); observed on `/sys/block/sda/queue/read_ahead_kb`, field test 2026-11-07 (HP test machine) |
| `ifup <iface>` on an already configured interface warns "already configured" and exits 0; `ifdown` of one that isn't configured warns and exits 0 | V | network-migration | ifupdown 0.8.44+deb13u1 main.c; field test 2026-11-07 (HP test machine) |
| `ss -H -l -t -n -p sport = :N` with the filter passed as separate argv words | V (observed) | ssh | field check 2026-11-07 |
| `sshd -T` prints one `port N` line per configured port (and `listenaddress` lines) | V | firewall, ssh | sshd(8) 10.0p1; observed with a test config holding `Port 22` and `Port 2222`, field test 2026-11-07 (HP test machine) |

## Field observations (test machine 10.10.10.150, 2026-11-07)

Observed directly on a fresh Debian 13 server install (no desktop, Wi-Fi). These are evidence, not primary documentation.

| Observation | Status | Source |
|---|---|---|
| A root password was set during install → `sudo` isn't installed ("sudo: command not found"), so kiln must be started as root (`su -`) the first time. | V (observed) | test machine; matches user-setup 1.107 |
| The SSH banner reads `SSH-2.0-OpenSSH_10.0p2 Debian-7+deb13u4`. The banner says 10.0p2, while the notes elsewhere name the package version 1:10.0p1-7+deb13u4; kiln never parses this banner. | V (observed) | `nc 10.10.10.150 22` |
| Wi-Fi power saving was on by default: 19–572 ms latency and 6.7% loss to the machine on the LAN, versus 2 ms to the router. After `iw … set power_save off`, 3–7 ms and no loss. | V (observed) | ping from the LAN |
| SSH connection timeouts from one macOS app while SSH from another app on the same Mac worked: caused on the Mac (an AdGuard network extension filtering per app is the main suspect), not by Debian or OpenWRT. | R | diagnosis, 2026-11-07 |
| The test machine never suspended and its Wi-Fi never disconnected during the boot when the timeouts happened (journal 09:53–15:16). That rules out the earlier "lid/suspend or Wi-Fi drop" guess for the timeouts; Wi-Fi power saving explains only the latency. The platform supports s2idle ("Low-power S0 idle"), not S3. | V (observed) | `journalctl -b -1` on the test machine |
| HP Envy x360 13-ay0xxx (board 876E): BAT1 exposes no `charge_control_*` and no `charge_behaviour`; `hp-wmi` loads but offers no `platform_profile`; the CPU uses `acpi-cpufreq` (no amd-pstate) on the Ryzen 5 4500U; Wi-Fi is a Realtek RTL8822CE (`rtw_8822ce`, PCI vendor 10ec); the installer had already put in firmware-realtek, firmware-amd-graphics and amd64-microcode. | V (observed) | field check, 2026-11-07 |
| A fresh trixie server install with Wi-Fi: `/etc/network/interfaces` has `allow-hotplug wlo1` / `iface wlo1 inet dhcp` with `wpa-ssid`/`wpa-psk`, plus `source /etc/network/interfaces.d/*`. `dhcpcd` runs in manager mode (process title without an interface), with a DUID-LLT in `/var/lib/dhcpcd/duid` and leases named `<iface>-<ssid>.lease`. | V (observed) | field check, 2026-11-07 |
| `systemctl show -p TimersCalendar` prints `TimersCalendar={ OnCalendar=*-*-* 06,18:00:00 ; next_elapse=… }` (normalized with seconds). `ss -H -l -t -n -p sport = :22` with the filter as separate argv words works and lists `users:(("sshd",…))`. | V (observed) | field check, 2026-11-07 |
| After `ifdown wlo1` and commenting out its stanza, `nmcli connection reload` plus `nmcli device set wlo1 managed yes` left wlo1 at `10 (unmanaged)` for 45 s. `systemctl restart NetworkManager` (the wiki.debian.org/NetworkManager procedure) made it managed. | V (observed) | field test run 2 and 3, 2026-11-07 |
| Straight after `managed yes`, `nmcli connection up` failed with "No suitable device found" 13 ms later. Waiting for `GENERAL.STATE` ≥ 30 plus a Wi-Fi scan with results, then `nmcli --wait 90 connection up <profile> ifname wlo1`, activated in about 1 s. The whole migration took 7 s and kept the SSH session. | V (observed) | field test run 3, 2026-11-07 |
| With `ipv4.dhcp-client-id=ff:<IAID>:<DUID>`, `ipv6.dhcp-duid=<DUID>` and `ipv6.dhcp-iaid=<IAID>` copied from dhcpcd, NM got the same IPv4 (10.10.10.150) and the same DHCPv6 address (`::150/128`) from OpenWrt. The `slaac private` address changed, as expected. NM installed the RA's /48 route-information route. | V (observed) | field test run 4, 2026-11-07 |
| At boot, ifupdown's `allow-hotplug` daemons (wpa_supplicant, dhcpcd) run in `ifup@wlo1.service`. A plain `ifup wlo1` puts them in the caller's cgroup: in kiln's `kiln-run-*.scope`, or a `systemd-run` unit, which kills them when it stops. That's what took the test machine offline during a manual revert. | V (observed) | field test 2026-11-07, `/proc/<pid>/cgroup` |
| NetworkManager's wpa_supplicant (`-u -O DIR=/run/wpa_supplicant`) and ifupdown's (`-i <iface> -C /run/wpa_supplicant`) share the control directory. After `nmcli device set wlo1 managed no`, NM's wpa_supplicant deinitialized wlo1 about 80 ms later. ifupdown's started in between and failed with "ctrl_iface exists and seems to be in use", so `ifup` failed and the machine stayed offline. | V (observed) | field test 2026-11-07, journal |
| A second run on the configured machine: 1 done (upgrade, by design), 21 unchanged, 0 failed, 4 skipped, 15 s. node, npm, Python + Playwright (headless Chromium), uv, Go, gh, `docker run hello-world` as satya, Cockpit HTTPS 200 and the kiln services all work. | V (observed) | field test run 5, 2026-11-07 |
| Forced failure of the post-migration check (a fake `/etc/hosts` entry for deb.debian.org, only during that step): the undo waited about 250 ms for NM's wpa_supplicant to release wlo1, `ifup@wlo1.service` reached `active/exited` after 9 s, and the network check passed 3 s later. IPv4, DHCPv6 and SLAAC addresses came back, the daemons ran in `ifup@wlo1.service`, `pending` was cleared and the SSH session survived. A normal run then migrated again; NM's stable-privacy address was identical to the first migration's, because `/var/lib/NetworkManager/secret_key` persists. | V (observed) | field test 2026-11-07 |
| Boot errors on the HP Envy x360: `ACPI BIOS Error … CreateField of length zero` in `\_SB.WMID.WQBC`/`WQBE` and `AE_ALREADY_EXISTS` for `\_SB.PCI0.GPP0.DEV0` (firmware table bugs, logged every boot, not caused by kiln); firewalld `NAME_CONFLICT: new_policy_object(): 'docker-forwarding'` when Docker re-creates its policy at start. No failed units. | V (observed) | `journalctl -b -p err`, 2026-11-07 |
