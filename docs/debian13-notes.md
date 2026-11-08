# Debian 13 "trixie" notes and gotchas

These facts were checked on **2026-11-07** against primary sources; rows added for the rewrite (D55, D56) were checked on **2026-11-08**. Package versions are trixie point-release versions on that date, so treat them as "at least", not as pins. There are no tests (see [review-process.md](review-process.md)), so primary documentation is the only form of verification. Rows are grouped by the D55 checklist: what is always done, then each checkbox in order. Statuses:

- **V**: verified against a primary source, which is linked. Code may rely on it.
- **U**: not yet verified. Code must not rely on it. Verify it from primary sources (packages.debian.org, manpages.debian.org, sources.debian.org, release notes, upstream docs) and flip it to V, or redesign so it isn't needed.
- **R**: deliberately not assumed. The code detects it at run time and handles both outcomes, typically hardware behavior that no document can promise. These rows record what we expect, not what the code depends on.

Re-check V facts older than 30 days during `/audit`.

## Packages (non-obvious ones)

All are in **main** unless noted. Source: `https://packages.debian.org/trixie/<pkg>` (V).

- **non-free-firmware:** `amd64-microcode`, `intel-microcode`, `firmware-amd-graphics`, `firmware-misc-nonfree`, `firmware-realtek`, `firmware-iwlwifi`, `firmware-atheros`, `firmware-mediatek`, `firmware-brcm80211` (firmware-nonfree 20250410).
- **non-free:** `nvidia-driver` (550.163), `intel-media-va-driver-non-free`.
- **Renamed or transitional:** `pkg-config` is a transitional package for **`pkgconf`**, so declare `pkgconf`.
- **Not packaged:** **`uv`**. The CLI isn't in trixie under any name (forky/sid only builds `python3-uv-build`). Install it with `pipx install uv` instead.
- **Not packaged:** `nvidia-container-toolkit` (no such package in trixie); it comes from NVIDIA's repo (see Docker).
- **Versions that matter:** `cockpit` 337 (trixie-backports **371**); `linux-image-amd64` 6.12.x (backports **7.2.x**); `golang-go` 1.24 (backports **1.27**); `gh` 2.46 (old); `docker.io` 26.1.5 (too old; see docker); `fail2ban` 1.1.0; `power-profiles-daemon` 0.30; `tuned`/`tuned-ppd` 2.25.1; `systemd` 257; `openssh` 10.0p1; GNOME 48 (`gdm3` 48.0); `upower` 1.90.9.
- **Added 2026-11-08 (V):** `ufw` 0.36.2-9 (admin; Depends `iptables`, `ucf`, `debconf`, `python3`), `iptables` 1.8.11-2, `autoconf` 2.72-3.1, `automake` 1:1.17-4, `libtool` 2.5.4-4, `gdb` 16.3-1, `acpi-call-dkms` 1.2.2-2.1 (kernel section, main; Depends only dkms), `systemd-zram-generator` 1.2.1-2.
- Also verified in main: `task-gnome-desktop`, `libavcodec-extra`, `gstreamer1.0-plugins-{good,bad,ugly}`, `gstreamer1.0-libav`, `mesa-va-drivers`, `vainfo`, `extrepo`, `flatpak`, `gnome-software-plugin-flatpak`, `git-lfs`, `neovim`, `btop`, `ripgrep`, `fd-find`, `fzf`, `bat`, `shellcheck`, `bind9-dnsutils`, `mtr-tiny`, `netcat-openbsd`, `ninja-build`, `systemd-zram-generator`, `zram-tools`, `python3-systemd`, `unattended-upgrades`, `apt-listchanges`, `needrestart`, `pipx`, `python3-venv`, `systemd-timesyncd`, `chrony`.
- The whole devtools list (34 packages, including `build-essential`, `tmux`, `htop`, `tree`, `strace`, `traceroute`, `cmake`, `libssl-dev`, `bash-completion`, `man-db`) and `libxml2-dev`, `libxslt1-dev`, `libffi-dev`, `python3-dev` are in trixie main. (V, qa.debian.org madison s=trixie, 2026-11-07)
- Binary names: `fd-find` 10.2.0 ships `/usr/bin/fdfind`, `bat` 0.25.0 ships `/usr/bin/batcat`. (V, packages.debian.org file lists)

## Always done: safety check and installer detection

| Fact | Status | Source |
|---|---|---|
| Plain `su` keeps the caller's PATH (no `/usr/sbin`). `su -` sets `ENV_SUPATH` (`/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin`). | V | Debian `login.defs`, su(1) |
| Minimal installs often have no curl, but wget is usually present. | R | install.sh uses whichever of the two exists |
| tasksel 3.81: `task-gnome-desktop` → `gnome-core` (gdm3, gnome-shell); `task-ssh-server` → `openssh-server`; "standard" is Priority standard, not a package. Blends are `task-*-blend` (blendsel 0.10); Debian Edu isn't offered. | V | tasksel 3.81, blendsel 0.10 debian/control |

## Always done: apt behaviour

| Fact | Status | Source |
|---|---|---|
| `/tmp` is a tmpfs (up to 50% of RAM) on new installs. `/tmp` and `/var/tmp` are cleaned periodically by systemd-tmpfiles. | V | [release notes §5.1.6, §5.2.1](https://www.debian.org/releases/trixie/release-notes/issues.en.html) |
| `-o DPkg::Lock::Timeout=<sec>` exists (configure-index, `debsystem.cc`) but isn't in apt-get(8) or apt.conf(5). It waits only on the dpkg `lock-frontend`/`lock`; `/var/lib/apt/lists/lock` and `/var/cache/apt/archives/lock` fail immediately. | V | apt 3.0.3 source (`apt-pkg/deb/debsystem.cc`, `acquire.cc`, `private-install.cc`) |
| `dpkg --audit` always exits 0, so check its output. All-digit entries in `/var/lib/dpkg/updates/` trigger apt's "dpkg was interrupted". Recovery is `dpkg --configure -a`. | V | dpkg 1.22.22 dpkg(1), `src/main/enquiry.c`; apt 3.0.3 `debsystem.cc` |
| needrestart 3.11: a non-empty `NEEDRESTART_SUSPEND` makes the apt hook skip needrestart and keep `/run/needrestart/unpacked` for the next run. Debian's `override_rc` never restarts dbus, display managers (gdm…), systemd-logind, NetworkManager, wpa_supplicant, docker, getty@, user@, apt-daily* or unattended-upgrades. `-b` (batch) never restarts anything; `-r a` restarts. | V | needrestart 3.11-1 `ex/apt/apt-pinvoke`, `ex/needrestart.conf`, `needrestart`, needrestart(1) |

## Always done: package sources

| Fact | Status | Source |
|---|---|---|
| d-i (apt-setup 0.198) writes a **one-line** `sources.list` with active `deb-src` lines and `main non-free-firmware`. cdrom lines are commented out unless the medium was a real CD. | V | apt-setup 1:0.198 generators/50mirror, 91security, templates, changelog |
| Offline installs leave a `deb cdrom:` entry that breaks `apt update` online. | R | apt-sources disables any cdrom entry it finds |
| apt 3.0.x `apt modernize-sources` honours `-y` and treats an "N" answer as a dry run. It converts `X.list` to `.sources` and renames it `X.list.bak` (`sources.list` → `sources.list.bak`), appends `sources.list` entries to `<origin>.sources` (`debian.sources` for Debian) or `moved-from-main.sources`, keeps only `signed-by`/`trusted`, and can leave `Signed-By` empty with a warning. | V | salsa apt-team/apt `apt-private/private-sources.cc`, `private-cmndline.cc` (3.0.3); [upgrading notes](https://www.debian.org/releases/trixie/release-notes/upgrading.en.html) |
| `apt-key` is removed. Use `Signed-By:` keyrings (`/etc/apt/keyrings/` for third parties). | V | apt changelog, packages.debian.org file list |
| One-line `sources.list` is deprecated but still read ("not before 2029"). | V | [sources.list(5)](https://manpages.debian.org/trixie/apt/sources.list.5.en.html) |
| The Debian archive keyring is `/usr/share/keyrings/debian-archive-keyring.pgp`. The `.gpg` symlinks are going away. | V | release notes |
| deb822 flat repositories: `Suites:` is a path ending in `/`, and `Components:` must then be omitted. | V | sources.list(5), trixie |

## Always done: full upgrade, firmware and microcode

| Fact | Status | Source |
|---|---|---|
| `/run/reboot-required` is written only by unattended-upgrades' kernel postinst hook. | V | unattended-upgrades 2.12 `kernel/postinst.d/unattended-upgrades` |
| firmware-nonfree 20250410 (backports 20260916) splits out `firmware-intel-graphics`, `firmware-intel-misc`, `firmware-nvidia-graphics` and `firmware-mediatek`; `firmware-misc-nonfree` only Recommends them. | V | firmware-nonfree 20250410-2 debian/, packages.debian.org |
| `isenkram-autoinstall-firmware -l` echoes package names on stdout and every `info:` line on stderr; it exits 1 only when no loaded module requests firmware, else 0 (including "No new firmware package"). | V | isenkram 0.69+deb13u1 isenkram-autoinstall-firmware; field test 2026-11-07 (HP test machine) |
| PCI class `0280` is "Network controller" (Wi-Fi cards); vendor IDs Intel `8086`, MediaTek `14c3`, Realtek `10ec`, Qualcomm Atheros `168c`, Qualcomm Technologies `17cb`, Broadcom `14e4`. rtw88 drivers register as `rtw_<chip>` (the HP's `rtw_8822ce`). | V | pci-ids.ucw.cz pci.ids; field check 2026-11-07 |

## 1. sudo

| Fact | Status | Source |
|---|---|---|
| With a root password set at install, d-i doesn't add the first user to `sudo`. sudo is installed and the user added only when the root password is left empty. | V | Installation Guide §6.3.2.1; user-setup source |
| sudoers.d skips names ending in `~` or containing `.`. visudo(8): checking a single include file isn't sufficient, so run `visudo -c`. | V | sudo 1.9.16p2 sudoers(5), visudo(8) |

## 2. System basics (zram, journal, inotify, time)

| Fact | Status | Source |
|---|---|---|
| timesyncd is the default. timesyncd and chrony both `Provides/Conflicts/Replaces: time-daemon`. | V | systemd 257 and chrony 4.6.1 control files |
| `systemd-zram-generator` 1.2.1 ships `/usr/lib/systemd/zram-generator.conf` enabling zram0 at `min(ram / 2, 4096)`; `/etc/systemd/zram-generator.conf` overrides it, and `zram-generator.conf.d/*.conf` drop-ins are supported. Restarting `systemd-zram-setup@zram0` also restarts `dev-zram0.swap` (swapoff). | V | [package file list](https://packages.debian.org/trixie/amd64/systemd-zram-generator/filelist), [zram-generator.conf(5)](https://github.com/systemd/zram-generator/blob/main/man/zram-generator.conf.md), rust-zram-generator 1.2.1-2; systemd.unit(5) |
| zram-generator 1.2.1: `zram-size=` is an expression of MemTotal "available as the `ram` variable" (MB), so `zram-size = ram` gives a device equal to RAM. `compression-algorithm=` takes an algorithm name (unset = kernel default). `swap-priority=` defaults to **100**. Drop-ins in `/etc/systemd/zram-generator.conf.d/` override the main file; latest filename wins. | V | [zram-generator.conf(5) trixie 1.2.1-2](https://manpages.debian.org/trixie/systemd-zram-generator/zram-generator.conf.5.en.html) |
| A swap device with no priority set (the installer's disk swap in `/etc/fstab`) gets a **negative** kernel default, so zram at 100 is used first. Swap ordering comes from priority, not from `vm.swappiness`. | V | [swapon(8) trixie](https://manpages.debian.org/trixie/mount/swapon.8.en.html) `--priority`; zram-generator.conf(5) |
| `vm.swappiness` is 0–200, default 60; it sets the relative IO cost of swap vs filesystem paging (100 = equal). "For in-memory swap, like zram or zswap … values beyond 100 can be considered"; the doc gives a formula (swap 2× faster than filesystem IO → 133), not a fixed value. | V | [Documentation/admin-guide/sysctl/vm.rst at v6.12](https://github.com/torvalds/linux/blob/v6.12/Documentation/admin-guide/sysctl/vm.rst#swappiness) |
| 6.12 inotify `max_user_watches` scales with RAM (clamped 8192…1048576), and no Debian sysctl.d touches it. | V | linux v6.12 inotify_user.c |
| journald.conf(5) recommends drop-in prefixes 60–90. | V | systemd 257.13 journald.conf(5) |
| Whether `/var/log/journal` exists. | R | checked at run time |

## 3. SSH

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

## 4. Firewall (UFW) and fail2ban

| Fact | Status | Source |
|---|---|---|
| ufw 0.36.2-9 is **not enabled on install**: `/etc/ufw/ufw.conf` ships `ENABLED=no`, debconf `ufw/enable` defaults to false, and README.Debian says "On installation, ufw is not automatically enabled". `ufw.service` (oneshot, `Before=network-pre.target`) is enabled by dh_installsystemd but `ufw-init` loads rules only when `ENABLED=yes`; `ufw enable` sets that. | V | [sources.debian.org ufw 0.36.2-9](https://sources.debian.org/src/ufw/0.36.2-9/): `conf/ufw.conf`, `debian/templates`, `debian/README.Debian`, `debian/ufw.service`, `debian/rules`, `src/ufw-init-functions` |
| `/etc/default/ufw` defaults: `DEFAULT_INPUT_POLICY="DROP"`, `DEFAULT_OUTPUT_POLICY="ACCEPT"`, `DEFAULT_FORWARD_POLICY="DROP"`, `IPV6=yes`. `ufw default deny incoming` / `ufw default allow outgoing` set these explicitly. | V | ufw 0.36.2-9 `conf/ufw.defaults`; [ufw(8) trixie](https://manpages.debian.org/trixie/ufw/ufw.8.en.html) `default allow\|deny\|reject DIRECTION` |
| `ufw allow 22/tcp` adds a v4+v6 rule. Rules can be added **before** `ufw enable`; enabling flushes ufw's chains but the port is open afterwards. `ufw enable` prompts under SSH; `ufw --force enable` doesn't. | V | ufw(8) trixie, USAGE and REMOTE MANAGEMENT |
| Backend: ufw drives `iptables`/`ip6tables`. trixie's iptables 1.8.11-2 registers `iptables-nft` at alternatives priority 20 and `iptables-legacy` at 10, so ufw's rules land in nftables via iptables-nft by default. | V | [iptables 1.8.11-2 debian/iptables.postinst](https://sources.debian.org/src/iptables/1.8.11-2/debian/iptables.postinst/) |
| fail2ban 1.1.0-8 depends on `python3-systemd`. `jail.d/defaults-debian.conf` sets `banaction = nftables`, `banaction_allports = nftables[type=allports]`, and `[sshd] backend = systemd`, `journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd`. | V | [sources.debian.org fail2ban 1.1.0-8](https://sources.debian.org/src/fail2ban/1.1.0-8/debian/debian-files/) |
| fail2ban 1.1.0-8 ships `action.d/ufw.conf`; use `banaction = ufw` (and `banaction_allports = ufw`). It bans with `ufw prepend reject from <ip> to any` (all ports, `blocktype = reject` by default) and unbans with `ufw delete`; it needs `ufw enable` first. Optional `ufw[kill-mode=ss]` drops existing connections. | V | [fail2ban 1.1.0-8 config/action.d/ufw.conf](https://sources.debian.org/src/fail2ban/1.1.0-8/config/action.d/ufw.conf/) |
| Read order is `jail.conf`, `jail.d/*.conf` (alphabetical), `jail.local`, `jail.d/*.local` (alphabetical); later wins. A kiln file must sort after `defaults-debian.conf` (or be a `.local`) to override its `banaction`. | V | [jail.conf(5) trixie](https://manpages.debian.org/trixie/fail2ban/jail.conf.5.en.html) |
| jail.conf 1.1.0 defaults: `bantime = 10m`, `findtime = 10m`, `maxretry = 5`; `bantime.increment`, `bantime.factor`, `bantime.maxtime`, `bantime.formula`/`bantime.multipliers` exist (commented out). | V | fail2ban 1.1.0-8 `config/jail.conf` |
| A jail-level `journalmatch` overrides the filter's, so `_COMM=sshd-session` must be listed. | V | fail2ban 1.1.0-8 configreader/jailreader, filter.d/sshd.conf |
| There's no `/var/log/auth.log`: rsyslog isn't installed by default, and fail2ban's Debian config uses the journal for sshd. | V | fail2ban 1.1.0-8 `paths-debian.conf` / `defaults-debian.conf`; rsyslog is Priority optional |
| Docker: "Docker and ufw use firewall rules in ways that make them incompatible"; published-port traffic is handled in the `nat` table before ufw's INPUT/OUTPUT chains, so ufw rules don't apply to published ports. | V | [docs.docker.com packet filtering and firewalls](https://docs.docker.com/engine/network/packet-filtering-firewalls/) |
| Docker publishes to all host addresses (`0.0.0.0` and `[::]`) unless an address is given; `-p 127.0.0.1:8080:80` (and `'[::1]:8080:80'`) makes a port reachable only from the host. | V | [docs.docker.com port publishing](https://docs.docker.com/engine/network/port-publishing/) |

## 5. Automatic updates

| Fact | Status | Source |
|---|---|---|
| Debian's `50unattended-upgrades` allows `label=Debian` (point releases) as well as security. `Origins-Pattern` accumulates across files, and `#clear` resets it. `20auto-upgrades` is ucf-managed from debconf `enable_auto_updates`. Upgrades run from `apt-daily-upgrade.timer`. | V | unattended-upgrades 2.12 data/, postinst, templates; apt.conf(5) |
| unattended-upgrades matches `Origins-Pattern` values with `fnmatch`, so `origin=*` matches any origin. | V | unattended-upgrades 2.12 `unattended-upgrade` (`match_whitelist_string`) |
| Release `Origin`/`Label`: Docker `Docker`/`Docker CE`; GitHub CLI `gh`/`gh`; Chrome `Google LLC`/`Google`; NVIDIA `NVIDIA`/`NVIDIA CUDA`. | V | each repository's InRelease |
| NVIDIA Container Toolkit repo Release: `Origin: https://nvidia.github.io/libnvidia-container/stable`, `Label: NVIDIA CORPORATION <cudatools@nvidia.com>`, `Codename: stable` (covered by `origin=*`). | V | `https://nvidia.github.io/libnvidia-container/stable/deb/amd64/InRelease`, fetched 2026-11-08 |
| `apt-daily.timer`: `OnCalendar=*-*-* 6,18:00`, `RandomizedDelaySec=12h`, runs `apt.systemd.daily update`. `apt-daily-upgrade.timer`: `OnCalendar=*-*-* 6:00`, `RandomizedDelaySec=60m`, runs `apt.systemd.daily install` (unattended-upgrade). Both services have `ConditionACPower=true`. | V | apt 3.0.3 `debian/apt-daily*.timer`, `apt-daily*.service` |
| `APT::Periodic::*` intervals are in days, accept `s`/`m`/`h`/`d` suffixes, or `always` ("which can be used with systemd OnCalendar overrides to define custom schedules"). | V | apt 3.0.3 `debian/apt.systemd.daily` |
| `Persistent=true` timers fire immediately on (re)start if a run was missed; on the very first start (no stamp) they don't. | V | systemd.timer(5), `src/core/timer.c` |
| `systemctl show -p TimersCalendar` prints `TimersCalendar={ OnCalendar=*-*-* 06,18:00:00 ; next_elapse=… }` (normalized with seconds). | V (observed) | field check 2026-11-07 |
| `apt-config dump` prints list values as `Key:: "v";` after a header line `Key "";`. | V | observed with apt 3.0.4: `Unattended-Upgrade::Origins-Pattern "";` then `Unattended-Upgrade::Origins-Pattern:: "origin=*";`, field test 2026-11-07 (HP test machine) |
| needrestart 3.11 reads `/etc/needrestart/conf.d/*.conf` (sorted). The default restart mode `'i'` becomes list-only when non-interactive. | V | needrestart.conf |

## 6. Always on (no sleep, lid ignored, GNOME)

| Fact | Status | Source |
|---|---|---|
| logind is `Type=notify-reload`, and SIGHUP re-reads the config. `HandleSuspendKeyLongPress` defaults to hibernate. | V | systemd 257.13 units/systemd-logind.service.in, src/login/ |
| Debian ships no `/etc/dconf/profile/user` (only debian-edu-config does). | V | packages.debian.org contents search |
| `/etc/gdm3/greeter.dconf-defaults` is ucf-managed (three-way merged from `/usr/share/gdm/greeter.dconf-defaults`) and symlinked as `/usr/share/gdm/dconf/90-debian-settings`. | V | gdm3 48.0-2 postinst, README.Debian |
| GDM 48: greeter power keys are commented out in `greeter.dconf-defaults`. `systemctl reload gdm3` runs `/usr/share/gdm/generate-config` and SIGHUP, which keeps sessions. gsd 48.1 `power-button-action` defaults to `'suspend'`. | V | gdm3 48.0-2 debian/generate-config, data/gdm.service.in, daemon/main.c; gsd 48.1 gschema |

## 7. Balanced power profile

| Fact | Status | Source |
|---|---|---|
| power-profiles-daemon `Conflicts: laptop-mode-tools, tlp`. `tuned-ppd` `Conflicts/Provides: power-profiles-daemon`. | V | sources.debian.org control files (ppd 0.30-1.1, tuned 2.25.1-1) |
| ppd writes `/sys/firmware/acpi/platform_profile` itself, adopts external changes, and (amd-pstate active) sets EPP/governor/boost per profile. | V | [ppd 0.30 source](https://sources.debian.org/src/power-profiles-daemon/0.30-1.1/src/) |
| ppd 0.30 `balanced`: platform profile `balanced`; amd-pstate EPP `balance_performance` on AC, `balance_power` on battery; `balanced` is always offered. The choice persists in `/var/lib/power-profiles-daemon/state.ini`. | V | sources.debian.org power-profiles-daemon 0.30-1.1 src/ppd-driver-amd-pstate.c `profile_to_epp_pref`, src/ppd-driver-platform-profile.c, src/ |
| tuned 2.25.1 `balanced`: `[cpu] governor=schedutil\|ondemand\|powersave`, `energy_perf_bias=normal`, `energy_performance_preference=balance_performance`, `boost=1`; `[acpi] platform_profile=balanced`; no `vm.swappiness`. `tuned-adm active` prints `Current active profile: <name>` while tuned runs ("Preset profile:" when it doesn't). With it active the HP's cores idled at ~1.4 GHz on `schedutil`. | V | /usr/lib/tuned/profiles/balanced/tuned.conf on the test machine (tuned 2.25.1-1); field test 2026-11-07 |
| The amd-pstate default mode on each laptop. | R | detected at run time |

## 8. Docker (Portainer, NVIDIA Container Toolkit)

| Fact | Status | Source |
|---|---|---|
| `download.docker.com/linux/debian/dists/trixie` exists (stable, amd64). | V | fetched |
| Key at `/etc/apt/keyrings/docker.asc`; deb822 `docker.sources` with `Types/URIs/Suites/Components: stable/Architectures/Signed-By`. Remove: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`. | V | [Docker Debian install docs](https://docs.docker.com/engine/install/debian/) |
| docker-ce 29.9.3 Depends `iptables, nftables`. log-opts values must be strings and apply to new containers only. | V | download.docker.com Packages; docs.docker.com json-file driver |
| Whether `dockerd --validate --config-file` exists. kiln runs it only when `dockerd --help` lists `--validate`; otherwise it relies on its own JSON parse plus a restart-and-check. | R | run time |
| Portainer CE on Docker standalone: `docker volume create portainer_data`, then `docker run -d -p 8000:8000 -p 9443:9443 --name portainer --restart=always -v /var/run/docker.sock:/var/run/docker.sock -v portainer_data:/data portainer/portainer-ce:lts`. 9443 is the HTTPS UI (self-signed certificate); 8000 is the Edge-agent tunnel, "only needed" for Edge features; HTTP 9000 is off unless `-p 9000:9000` is added. | V | [docs.portainer.io Install CE on Docker (Linux)](https://docs.portainer.io/start/install-ce/server/docker/linux) |
| From Portainer 2.43 / 2.39.4, a new instance needs a one-time **setup token** read from `docker logs` (`setup_token=`); `--setup-token <v>`, `--no-setup-token`, or `--admin-password`/`--admin-password-file` change that. If setup isn't finished within **5 minutes** the Portainer service in the container stops; `docker stop portainer && docker start portainer` gives another 5 minutes. | V | [setup-token FAQ](https://docs.portainer.io/faqs/installing/setup-token), [timed-out FAQ](https://docs.portainer.io/faqs/installing/your-portainer-instance-has-timed-out-for-security-purposes-error-fix) |
| Watchtower (`containrrr/watchtower`) is archived; last commit 2025-12-17 "Add maintenance notice to README" ("This project is no longer maintained"). | V | [github.com/containrrr/watchtower](https://github.com/containrrr/watchtower) (API `archived: true`), fetched 2026-11-08 |
| NVIDIA Container Toolkit apt setup: key `https://nvidia.github.io/libnvidia-container/gpgkey` (ASCII-armored; primary `C95B321B61E88C1809C4F759DDCAE044F796ECB0`, sha256 `c880576d…9c9` on 2026-11-08), dearmored by the guide to `/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg`; list `…/stable/deb/nvidia-container-toolkit.list` = `deb https://nvidia.github.io/libnvidia-container/stable/deb/$(ARCH) /` (flat), with `signed-by=` added by `sed`, written to `/etc/apt/sources.list.d/nvidia-container-toolkit.list`. Packages: `nvidia-container-toolkit` (+ `-base`, `libnvidia-container-tools`, `libnvidia-container1`); current 1.20.1-1. Then `nvidia-ctk runtime configure --runtime=docker` (updates `/etc/docker/daemon.json`) and `systemctl restart docker`. Requires the NVIDIA driver already installed. | V | [NVIDIA Container Toolkit install guide](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html); list file, InRelease, Packages fetched 2026-11-08 |
| deb822 equivalent (kiln's form): `URIs: https://nvidia.github.io/libnvidia-container/stable/deb/amd64/`, `Suites: /`, no `Components:`, `Signed-By:` the armored key in `/etc/apt/keyrings/`. | V / derived | sources.list(5) flat-repo rule; not yet run through apt |
| Debian 13 is **not** on the toolkit's supported-platforms list (only Debian 11 is); the apt instructions "should work for any Debian-derived distribution". | V (not listed) / U (works on trixie) | [supported platforms](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/supported-platforms.html); confirm on an NVIDIA machine |
| `nvidia-ctk runtime configure` keeps kiln's existing `log-driver`/`log-opts` keys in `daemon.json`. | U | needs the nvidia-ctk source or a run |
| Debian's 550 driver set brings the libraries the toolkit mounts: `nvidia-driver` → `nvidia-driver-bin` → `libnvidia-ml1`; `nvidia-driver-libs` Recommends `libnvidia-encode1` → `libnvcuvid1` → `libcuda1`, which Recommends `nvidia-smi`. So `libcuda1` and `nvidia-smi` arrive **only with Recommends on**. Whether the toolkit needs more than these is not stated in its docs. | V (relations) / U (sufficiency) | packages.debian.org/trixie `nvidia-driver`, `nvidia-driver-bin`, `nvidia-driver-libs`, `libnvidia-encode1`, `libnvcuvid1`, `libcuda1`, `nvidia-smi` (550.163.01-2) |

## 9. Cockpit

| Fact | Status | Source |
|---|---|---|
| cockpit 337/371 Recommends `cockpit-storaged`, `cockpit-networkmanager` and `cockpit-packagekit` (not machines/podman). The socket is enabled on install. postinst writes `root` to `disallowed-users`. | V | cockpit 337-1+deb13u2 / 371-1~bpo13+1 debian/control, postinst |
| `cockpit-networkmanager` depends on network-manager and is "incompatible with systemd-networkd/ifupdown defined networks". | V | [packages.debian.org](https://packages.debian.org/trixie/cockpit-networkmanager) |
| NetworkManager.conf ships `plugins=ifupdown,keyfile` + `[ifupdown] managed=false`, so interfaces in `/etc/network/interfaces` stay unmanaged and NM leaves them alone. | V | [sources: network-manager 1.52.1 debian/NetworkManager.conf](https://sources.debian.org/src/network-manager/1.52.1-1/debian/NetworkManager.conf/), [wiki](https://wiki.debian.org/NetworkManager) |

## 10. CLI and developer tools

| Fact | Status | Source |
|---|---|---|
| GitHub CLI repo: keyring `https://cli.github.com/packages/githubcli-archive-keyring.gpg` (sha256 `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b`) → `/etc/apt/keyrings/`. `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main` (Release Codename `stable`). Package `gh`. The doc uses a one-line source; kiln writes the deb822 equivalent. | V | [install_linux.md](https://github.com/cli/cli/blob/trunk/docs/install_linux.md), repo Release file (2026-11-07) |
| Build tools `autoconf` 2.72-3.1, `automake` 1:1.17-4, `libtool` 2.5.4-4, `gdb` 16.3-1 are in trixie main (devel). | V | [packages.debian.org/trixie](https://packages.debian.org/trixie/autoconf) (`/autoconf`, `/automake`, `/libtool`, `/gdb`), 2026-11-08 |

## 11–13. Node, Python, Go

| Fact | Status | Source |
|---|---|---|
| nvm v0.41.0 (2026-11-04) is the latest release. install.sh honours `PROFILE=/dev/null`, `METHOD=script` and `NVM_DIR` (exits 1 if an explicit NVM_DIR doesn't exist). Node 26 is LTS "Lithium" since v26.13.0 (2026-10-28). | V | [nvm v0.41.0 install.sh](https://github.com/nvm-sh/nvm/blob/v0.41.0/install.sh), nodejs.org/dist/index.json |
| pipx 1.7.1 installs to `~/.local/bin`; a re-run exits 0. uv 0.13.7: `uv venv` errors on a non-empty path, `uv pip install --python <venv-dir>` works, and `uv tool install` takes one package per call. | V | pipx paths.py 1.7.1, docs.astral.sh/uv |
| Playwright 1.64 supports Debian 13. `install-deps --dry-run chromium` prints `Missing system dependencies (N):` plus one indented package per line and exits 1, or prints "All system dependencies are installed." and exits 0. | V | playwright v1.64.0 `registry/dependencies.ts`, playwright.dev |
| `go install` accepts several packages only from one module, so tools need separate runs. Debian's golang keeps `GOTOOLCHAIN=auto`. go.dev `/dl/?mode=json&include=all` returns every release (369 on 2026-11-07, go1.27.3 included) as `{version, stable, files[{filename, os, arch, version, sha256, size, kind}]}`. | V | go.dev/ref/mod, go.dev/dl JSON (fetched 2026-11-07), go1.27.3 |

## 14. Codecs and video acceleration

| Fact | Status | Source |
|---|---|---|
| `libavcodec-extra61` Conflicts/Provides `libavcodec61`. `intel-media-va-driver-non-free` Conflicts `intel-media-va-driver`. | V | ffmpeg 7:7.1.5 and intel-media-driver debian/control |

## 15. Flathub

| Fact | Status | Source |
|---|---|---|
| `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo` (flatpak 1.16.6). `gnome-software-plugin-flatpak` is 48.3. | V | [flatpak-remote-add(1) trixie](https://manpages.debian.org/trixie/flatpak/flatpak-remote-add.1.en.html), flathub.org/setup/Debian |

## 16. Google Chrome

| Fact | Status | Source |
|---|---|---|
| extrepo 0.14: `/etc/extrepo/config.yaml` key `enabled_policies:` (default `main` only). `google_chrome` has policy `non-free`. `extrepo enable` writes `/etc/apt/sources.list.d/extrepo_<name>.sources` with `Signed-By: /var/lib/extrepo/keys/<name>.asc`; a re-run only sets `Enabled: yes`. Chrome's post-enable command writes `/etc/default/google-chrome` (`repo_add_once="false"`) to stop a duplicate source. | V | [extrepo 0.14 source](https://sources.debian.org/src/extrepo/0.14/), extrepo-data live index |
| `extrepo search <re>` prints `Found <name>:` then a YAML dump of the entry (top-level keys unindented), and exits 1 with "No matches found". Entries carry `policy: <p>` or a `policies:` map of component → policy (289 and 44 entries in the trixie index). | V | extrepo 0.14 Commands/Search.pm, Commands/Enable.pm; extrepo-data debian/trixie/index.yaml, fetched 2026-11-07 |

## 17. NVIDIA driver

| Fact | Status | Source |
|---|---|---|
| `mokutil` is present only via shim-signed. The Secure Boot state can be read from efivar `SecureBoot-8be4df61-…` byte 5. | V | mokutil 0.7.2-1 |
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
| Container Toolkit needs: see Docker (Debian 550 relations; NVIDIA's full `nvidia-open` set). | — | see section 8 |

## 18. Battery limit (70–80%)

| Fact | Status | Source |
|---|---|---|
| Kernel ABI: `charge_control_{start,end}_threshold` (0–100; drivers may round). `charge_behaviour` is `auto`/`inhibit-charge`/`force-discharge` in 6.12; `inhibit-charge-awake` only in later kernels (present in 7.2). `charge_types` (`Custom`, `Long_Life`, …) is documented from 6.14, but dell-laptop exposes it in 6.12. `scope` exists in `power_supply_sysfs.c` but is undocumented, and the ACPI battery driver doesn't expose it. Threshold writes emit no uevent. | V | sysfs-class-power at v6.12 and v7.2; `drivers/power/supply/` |
| thinkpad_acpi rejects start > current stop and stop < current start, and allows start == stop. `fan_control=1` enables writes to `/proc/acpi/ibm/fan`. dell-laptop clamps thresholds silently; asus-wmi's read-back is a cached value. | V | thinkpad_acpi.c, [kernel docs](https://docs.kernel.org/admin-guide/laptops/thinkpad-acpi.html); linux v6.12 drivers/platform/x86/ |
| `tee <sysfs attr>` writes the value; an invalid value fails with "Invalid argument" and exit 1, and the attribute keeps its old value. | V | coreutils 9.7 tee(1); observed on `/sys/block/sda/queue/read_ahead_kb`, field test 2026-11-07 (HP test machine) |
| UPower 1.90.9 / GNOME 48 "Preserve Battery Health": enabled only when `charge_control_start_threshold` exists. Limits come from hwdb `CHARGE_LIMIT` (default `75,80`). The flag is stored in a `charging-threshold-status` file in UPower's state dir. Disabling writes 0/100. | V | [upower 1.90.9 source](https://sources.debian.org/src/upower/1.90.9-1/) |
| hp-wmi exposes only `charge_behaviour`, DMI-gated to two Spectre x360 boards, and only in kernels newer than 6.12. | V | mainline `drivers/platform/x86/hp/hp-wmi.c` |
| E14 Gen 5 AMD exposes thresholds, `/proc/acpi/ibm/fan` and DYTC platform_profile. | R | probed at run time |
| `acpi-call-dkms` 1.2.2-2.1 (main) builds the `acpi_call` module; a call is written as `<method> <args>` to `/proc/acpi/call` and the result read back from it. Under Secure Boot the DKMS module needs the dkms MOK enrolled (see NVIDIA). | V (package, interface) / R (Secure Boot) | [packages.debian.org/trixie/acpi-call-dkms](https://packages.debian.org/trixie/acpi-call-dkms); [acpi-call 1.2.2-2.1 README.md](https://sources.debian.org/src/acpi-call/1.2.2-2.1/README.md/) |
| HP Envy x360 13-ay0xxx (board 876E): no `charge_control_*`; `hp-bioscfg` shows only `Sure_Start`. Firmware has an undocumented HP WMI "battery charge operation", command type `0x2B`, via `\_SB.WMID.WMAA` → ACPI method `SBCO` (SSDT6), writing EC bits `WACL`, `SBIS`, `SBTC`, `BTNO`. | R (field, 2026-11-08) | test box 10.10.10.150, ACPI table dump (D56) |
| HP mode `0x0000` = normal charging; mode `0x0200` = force discharge (runs on battery while plugged in: 100% → 96%; back to normal takes effect at once). | R (field, verified 2026-11-08) | test box (D56) |
| HP mode `0x0100` = "no charge" (hold on AC). **Not yet proven**: at 96% the battery's own hysteresis looked the same as normal; deeper test (discharge to 85%, compare) pending. | R (field, pending) | test box (D56) |

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
- Second run on the configured machine (old tool, field test run 5): 1 done (upgrade, by design), 21 unchanged, 0 failed, 4 skipped, 15 s. node, npm, Python + Playwright (headless Chromium), uv, Go, gh, `docker run hello-world` as satya, Cockpit HTTPS 200 and the kiln services all worked.

## No longer used (D55)

Facts that served features the rewrite drops (D55). Verified status and source kept in brackets; no code may rely on them.

- **Mirrors:** `mirror+file:` URIs and mirrorlist `priority:` tags (apt-transport-mirror(1), apt 3.0.3 `methods/mirror.cc`); `Acquire::Retries` default 3 (apt.conf(5)); `Mirrors.masterlist` at mirror-master.debian.org, ~370 stanzas, HTTP only; `Contents-amd64.gz` ~12.7 MB, `Contents-all.gz` ~38 MB. [V]
- **Backports kernel:** trixie 6.12.x vs backports 7.2.x via `linux-signed-amd64`, backports image needs `linux-base ≥ 4.17~`, no `linux-image-6.12-amd64` metapackage, unattended-upgrades installs new image names only as upgrade deps (madison; u-u 2.12). [V]
- **Kernel comparison:** `dpkg --compare-versions A gt B` orders kernel release strings correctly (dpkg(1); HP box). [V]
- **DVD:** `libdvd-pkg` (contrib) 1.4.3 templates `build`, `post-invoke_hook-install`, `first-install`; `dpkg-reconfigure -f noninteractive` builds `libdvdcss.so.2`. [V]
- **fwupd** 2.0.20: exit 2 nothing to do, 101 no network; `-y`, `--no-reboot-check`, `--no-device-prompt`, `--json`, `check-reboot-needed`; LVFS on; capsule needs ESRT, ESP, `fwupd-amd64-signed` under Secure Boot (fwupd 2.0.20-1~deb13u1 source). [V]
- **Thermal/thinkfan:** `/sys/class/platform-profile/` needs kernel ≥ 6.14 (ABI doc); thinkfan 1.3.1 has no config-test flag, reads `/etc/thinkfan.yaml` then `.conf`, unit ships disabled; `sensors-detect --auto` writes only `/etc/modprobe.d/lm_sensors.conf`; Envy x360 13 hp-wmi platform_profile probe [R]; packages `thinkfan`, `lm-sensors`. [V]
- **Battery ladder (beyond kernel thresholds / D56):** TLP vendor matrix (linrunner.de; HP not listed); systemd.device(5) `SYSTEMD_WANTS` acts only on first activation, not change events; whether UPower re-reads hwdb `CHARGE_LIMIT` on a change event [U, dropped with D12]. [V]
- **Process model (D41):** `systemctl mask --runtime` writes `/run/systemd/system/`, doesn't stop a running unit; `packagekit.service` D-Bus activated, GNOME Software 48.3 refresh/download behaviour, PackageKit apt backend gives up on the dpkg lock after 10 s; `systemd-run --scope` as root → `system.slice`, `default-kill-user-processes=false`; `setsid(2)` new session has no controlling terminal; `systemd-inhibit --what=sleep:idle:handle-lid-switch` and block-shutdown inhibitor semantics (systemd 257.13). [V]
- **needrestart suspension:** the `NEEDRESTART_SUSPEND` part of the needrestart row above is only for the old suspension; `override_rc` and `-r a` still apply. [V]
- **Network migration:** `nmcli -t -f DEVICE,STATE device` terse output (nmcli(1) 1.52.1); `Type=simple` units report success right after fork (`ifup@wlo1` returned in 52 ms; systemd.service(5)). [V]
- **uv pinned installer:** `https://astral.sh/uv/<version>/install.sh` (0.13.7 redirects to releases.astral.sh, `APP_VERSION="0.13.7"`); unused since uv comes from `pipx install uv` (D11) (docs.astral.sh/uv installation). [V]
- **Other run-time unknowns:** whether the backports kernel works better with backported firmware. [R]

### Removed 2026-11-07 (D50–D53)

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
