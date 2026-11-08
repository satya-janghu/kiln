# Steps catalog

**Source of truth for behavior** (rule 11). The plan is [decisions.md](decisions.md) D55 and D56; the run model is in [architecture.md](architecture.md). Facts the code relies on must be **V** in [debian13-notes.md](debian13-notes.md); "(verify: debian13-notes)" marks one that isn't yet, to be checked against a primary source before coding.

## How a run works

kiln is a one-time tool, run once at the console as root on a fresh Debian 13 amd64 install.

- **The same checklist on every install,** GNOME or server, all ticked. Only hardware hides an item (NVIDIA, battery). No sub-options, no other questions; every value is fixed.
- **Nothing is saved** and nothing of kiln keeps running, except the battery script and timer on HP laptops that need it (D56). The log is `/var/log/kiln.log`.
- **Order:** the always-done items, then each ticked checkbox, one at a time, in the order below.
- **A failed step doesn't stop the run.** Steps that need it are skipped with the reason; the rest continue.
- **Running again is safe:** installing an installed package is a no-op, files are rewritten with the same content, services are enabled again. There is no "already done" detection.
- **"Check"** is how a step confirms its work afterwards. A failed check fails the step.

Rules (architecture.md): apt is non-interactive (`DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a APT_LISTCHANGES_FRONTEND=none UCF_FORCE_CONFFOLD=1 LC_ALL=C`, `--force-confdef --force-confold`) and waits for the locks; "install X" means `apt-get install -y X`. Commands use `exec.Command(name, args...)`, never `bash -c` with interpolated strings. Downloads go under `/var/tmp`, never `/tmp`. "As the user" follows the user-command rule. ucf-managed files are only key-edited.

---

## Always done

### Safety check and waiting for apt

- Refuse unless `/etc/os-release` has `ID=debian`, `VERSION_ID=13`, `dpkg --print-architecture` is `amd64`, kiln runs as root ("Run it as root via `su -`, or with sudo") and a target user is found.
- Under plain `su`, add `/usr/local/sbin:/usr/sbin:/sbin` to `PATH` (D53).
- Install `ca-certificates` and `pciutils` if missing (P15).
- Wait for first-boot automatic updates: `apt-daily.service` and `apt-daily-upgrade.service` inactive, then the dpkg lock free. Every apt call also passes `-o DPkg::Lock::Timeout=600` and retries on "Could not get lock".
- If dpkg was left half-done (`dpkg --audit` prints anything), run `dpkg --configure -a`, then `apt-get -f install -y` (**proposed**, D55).

### Package sources

1. Comment out every active line in `/etc/apt/sources.list` (cdrom lines included), under `# Debian entries now live in sources.list.d/debian.sources`.
2. Write `/etc/apt/sources.list.d/debian.sources`. Every stanza has `Types: deb`, `Components: main contrib non-free non-free-firmware` and `Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp`:
   - `URIs: http://deb.debian.org/debian`, `Suites: trixie trixie-updates`;
   - `URIs: http://security.debian.org/debian-security`, `Suites: trixie-security`;
   - `URIs: http://deb.debian.org/debian`, `Suites: trixie-backports` (`NotAutomatic: yes`, so nothing comes from it without `-t trixie-backports`).
3. `apt-get update`.

**Check:** `apt-get update` succeeds and `sources.list` has no active lines.

### Full upgrade

`apt-get update`, then `apt-get full-upgrade -y`. A reboot is needed if `LC_ALL=C apt-get -s full-upgrade` beforehand listed `Inst linux-image-`, or `/run/reboot-required` exists afterwards.

**Check:** `LC_ALL=C apt-get -s full-upgrade` prints no `Inst` or `Remv` lines.

### Firmware and microcode

Non-free firmware is always used (D52).
- Microcode: `amd64-microcode` (AMD CPU) or `intel-microcode` (Intel CPU).
- `firmware-misc-nonfree --no-install-recommends`.
- By GPU: AMD → `firmware-amd-graphics`; Intel → `firmware-intel-graphics firmware-intel-misc`; NVIDIA → `firmware-nvidia-graphics` (for nouveau; `firmware-nvidia-gsp` comes with the driver).
- By network driver: `iwlwifi` → `firmware-iwlwifi`; `ath*` → `firmware-atheros`; `mt7*` → `firmware-mediatek`; `brcmfmac` → `firmware-brcm80211`; Realtek (`r8169`, `r8152`, `rtw_*`, `rtw89*`, `rtl8*`) → `firmware-realtek`.
- By PCI Wi-Fi vendor (class 0280; a card missing its firmware has no interface): `8086` → iwlwifi, `14c3` → mediatek, `10ec` → realtek, `168c`/`17cb` → atheros, `14e4` → brcm80211.
- `isenkram-cli`; `isenkram-autoinstall-firmware -l` (list only, never install mode) reports remaining gaps as a note.
- If anything was newly installed: `update-initramfs -u -k all` (on failure: "free space in `/boot` and run kiln again"), and a reboot is needed.

**Check:** all selected packages are installed.

---

## Checkboxes

### 1. Passwordless sudo

Shown: always.
1. Install `sudo`; `adduser <user> sudo`.
2. `/etc/sudoers.d/90-<name>-nopasswd`, 0440 root:root: `<user> ALL=(ALL:ALL) NOPASSWD: ALL`. `<name>` is the user name with characters outside `[a-z0-9_-]` replaced by `_` (sudoers skips names with `.` or ending in `~`). Naming the user means it works without logging out.
3. Check a dot-named temp copy with `visudo -c -q -f`, rename it into place, then `visudo -c` on the whole policy. On failure remove the file and fail; never leave an invalid file.

**Check:** `visudo -c` passes and `sudo -l -U <user>` shows the NOPASSWD rule.

Note: "log out and back in" for the `sudo` group itself.

### 2. System basics

Shown: always.
- **zram:** install `systemd-zram-generator` (D8). Write `/etc/systemd/zram-generator.conf.d/60-kiln.conf`:
  ```
  [zram0]
  zram-size = ram
  compression-algorithm = zstd
  ```
  The size equals RAM (Debian's default is `min(ram / 2, 4096)`) (verify: debian13-notes). Then `systemctl daemon-reload`, `systemctl start dev-zram0.swap`; if zram was already active, the new size applies at next boot (note). The installer's disk swap stays as overflow.
- **sysctl:** `/etc/sysctl.d/60-kiln.conf` with `vm.swappiness = 180` (**proposed**, D55; zram is already used before disk through swap priority, and a high value moves idle memory into it sooner), `fs.inotify.max_user_watches = 524288`, `fs.inotify.max_user_instances = 1024`; `sysctl -q -p` that file.
- **journald:** `/etc/systemd/journald.conf.d/60-kiln.conf` with `[Journal]` and `SystemMaxUse=500M`, then `systemctl restart systemd-journald`.
- **Time sync:** install `systemd-timesyncd` (D7), `timedatectl set-ntp true`.

**Check:** `/proc/swaps` lists `/dev/zram0` (or the next-boot note was given), live sysctl values match, `timedatectl show -p NTP` is `yes`.

### 3. SSH

Shown: always. Password logins for the user and root, from everywhere (D45).
1. Install `openssh-server`; `systemctl enable --now ssh.service`.
2. **Root password:** if root's `/etc/shadow` field is locked or empty (`!`, `*` or blank) and the user has a password, copy the user's hash with `chpasswd -e` (`root:<hash>` on stdin, never in argv or the log). Otherwise root is left alone.
3. **Drop-in** `/etc/ssh/sshd_config.d/10-kiln.conf` (read early; first value wins):
   ```
   PermitRootLogin yes
   PasswordAuthentication yes
   MaxAuthTries 4
   X11Forwarding no
   ```
   No `Port` line: sshd listens on 22.
4. `sshd -t`. If it fails, remove the drop-in, don't reload, and fail. Otherwise `systemctl reload-or-restart ssh`.

**Check** (polled up to 15 s): `ssh.service` active, port 22 listening (`ss -ltn`), and `sshd -T -C user=<user>,host=<hostname>,addr=<lan-ip>` shows the four values.

Summary note: password logins for the user and root are open wherever SSH is reachable.

### 4. Firewall and fail2ban

Shown: always.

**UFW:**
1. Install `ufw`.
2. Default deny incoming, allow outgoing; allow `22/tcp` and `9090/tcp` from everywhere; then enable without the prompt, so it also starts at boot (verify: debian13-notes, the exact `ufw` commands, non-interactive enable and boot start).

**fail2ban** (only when the SSH checkbox is also ticked; owner's answer in D54):
1. Install `fail2ban python3-systemd`.
2. Write `/etc/fail2ban/jail.d/50-kiln.local` (`jail.d/*.local` is read last):
   ```
   [DEFAULT]
   ignoreip = 127.0.0.1/8 ::1 <LAN subnets detected at run time>
   bantime = 1h
   bantime.increment = true
   maxretry = 5

   [sshd]
   enabled = true
   backend = systemd
   port = 22
   banaction = <fail2ban's ufw action>
   journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd + _COMM=sshd-session
   ```
   Bans go through UFW, not Debian's default nftables action (verify: debian13-notes, the ufw action's name and whether it bans on all ports, as P9 had). trixie has no `/var/log/auth.log`, hence the systemd backend; the jail's `journalmatch` replaces the filter's, so it adds `_COMM=sshd-session`.
3. `systemctl restart fail2ban`.

**Check:** `ufw status verbose` shows active, deny incoming, allow outgoing and both rules; `fail2ban-client ping` answers `pong` and `fail2ban-client status sshd` runs.

Summary note: ports Docker publishes bypass UFW (Docker's docs), so Portainer's 9443 and any `-p` port are open to every network; publish as `127.0.0.1:<port>:<port>` to keep one private.

### 5. Automatic updates

Shown: always. Every package from every repository, every 12 hours, never an automatic reboot (D40).
1. Preseed `unattended-upgrades unattended-upgrades/enable_auto_updates boolean true`; install `unattended-upgrades apt-listchanges needrestart`. `20auto-upgrades` (ucf) is never rewritten.
2. `/etc/apt/apt.conf.d/52kiln-unattended-upgrades` (sorts after 20 and 50):
   ```
   // Managed by kiln
   APT::Periodic::Update-Package-Lists "always";
   APT::Periodic::Unattended-Upgrade "always";
   #clear Unattended-Upgrade::Origins-Pattern;
   Unattended-Upgrade::Origins-Pattern {
       "origin=*";
   };
   Unattended-Upgrade::Automatic-Reboot "false";
   ```
3. Timer drop-ins:
   - `/etc/systemd/system/apt-daily.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=`, `OnCalendar=*-*-* 06,18:00`, `RandomizedDelaySec=15m`;
   - `/etc/systemd/system/apt-daily-upgrade.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=`, `OnCalendar=*-*-* 06,18:30`, `RandomizedDelaySec=15m`.

   Then `systemctl daemon-reload` and `systemctl restart apt-daily.timer apt-daily-upgrade.timer`.
4. `/etc/needrestart/conf.d/50-kiln.conf`: `$nrconf{restart} = 'a';`.

**Check:** `apt-config dump` shows both Periodic values as `always` and exactly the one `Origins-Pattern`; both timers are active with their new calendar (`systemctl show -p TimersCalendar`).

Summary notes: updates run at about 06:00 and 18:00 and skip on battery (logs in `/var/log/unattended-upgrades/`); kernel and NVIDIA updates need a reboot, which never happens automatically (`/run/reboot-required` shows one is pending); a Docker update restarts containers only if they have a restart policy; a new NVIDIA branch can arrive unattended.

### 6. Always on

Shown: always.
- **Sleep:** `systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target suspend-then-hibernate.target`.
- **logind:** `/etc/systemd/logind.conf.d/60-kiln-always-on.conf`: `[Login]`, `HandleLidSwitch=ignore`, `HandleLidSwitchExternalPower=ignore`, `HandleLidSwitchDocked=ignore`, `HandleSuspendKey=ignore`, `HandleSuspendKeyLongPress=ignore`, `HandleHibernateKey=ignore`, `HandleHibernateKeyLongPress=ignore`, `IdleAction=ignore`. `HandlePowerKey` stays poweroff. Then `systemctl reload systemd-logind`.
- **GNOME only** (GNOME detected):
  - `/etc/dconf/profile/user`: create with `user-db:user` and `system-db:local`, or add `system-db:local` if missing.
  - `/etc/dconf/db/local.d/60-kiln-power`: `[org/gnome/settings-daemon/plugins/power]` with `sleep-inactive-ac-type='nothing'`, `sleep-inactive-battery-type='nothing'`, `power-button-action='interactive'` (P10).
  - `/etc/dconf/db/local.d/locks/60-kiln-power` listing those three keys, so users can't change them (D23). Then `dconf update`.
  - Greeter: in `/etc/gdm3/greeter.dconf-defaults` (ucf), uncomment or insert the two `sleep-inactive-*-type='nothing'` keys under the power header, then `systemctl reload gdm3`.

**Check:** the five targets are masked; GNOME: `dconf read` as the user shows the three values.

### 7. Balanced power profile

Shown: always (D5, D52).
- **With GNOME:** remove `tuned tuned-ppd` if present; install and `enable --now` `power-profiles-daemon`, wait up to 30 s, `powerprofilesctl set balanced` (it persists itself).
- **Without GNOME:** remove `power-profiles-daemon` if present; install and `enable --now` `tuned`, wait up to 30 s, `tuned-adm profile balanced`.
- Never both (the packages don't conflict, so kiln removes the other). TLP is never touched (D50).

**Check:** `powerprofilesctl get` prints `balanced`, or `tuned-adm active` prints `Current active profile: balanced`.

### 8. Docker

Shown: always. From Docker's repository, following Docker's Debian install docs.
1. Remove if present: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`.
2. `install -m 0755 -d /etc/apt/keyrings`; download `https://download.docker.com/linux/debian/gpg` to `/etc/apt/keyrings/docker.asc` (0644).
3. `/etc/apt/sources.list.d/docker.sources`: `Types: deb`, `URIs: https://download.docker.com/linux/debian`, `Suites: trixie`, `Components: stable`, `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/docker.asc`. `apt-get update`.
4. Install `docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`.
5. `/etc/docker/daemon.json` (no header): merge `{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}` into existing keys; `dockerd --validate --config-file /etc/docker/daemon.json` if `dockerd --help` lists `--validate`; restart docker if the file changed.
6. `adduser <user> docker`. Wait up to 60 s for `docker info`, then `docker run --rm hello-world` as root.
7. **Portainer CE:** a volume for its data and a container named `portainer` from `portainer/portainer-ce:lts`, publishing only 9443 (8000 is for Edge agents only, **proposed** D55), with the Docker socket mounted and `--restart=always`, so it starts with the machine (commands: debian13-notes). If a container named `portainer` already exists, it is left alone.
8. **NVIDIA Container Toolkit** (only when checkbox 17 is ticked and will install a driver for this card): NVIDIA's toolkit apt repository and key as a deb822 `.sources` file, the toolkit package, the NVIDIA runtime registered in `daemon.json` with `nvidia-ctk`, then restart docker (verify: debian13-notes, repository URL, key, package names, configure command). GPU containers work after the reboot that loads the driver.

**Check:** `docker info` works, hello-world ran, `docker inspect portainer` shows it running with restart policy `always`; with the toolkit, `docker info` lists the `nvidia` runtime.

Summary notes: log out and back in for the docker group; the docker group is root-equivalent; "Portainer: open `https://<lan-ip>:9443` within 5 minutes of a boot and create the admin account; if it says the time ran out, run `docker restart portainer`" (**proposed**, D55; the 5-minute limit is in debian13-notes). Not installed: lazydocker, What's Up Docker, Watchtower (archived).

### 9. Cockpit

Shown: always.
- `apt-get install -y -t trixie-backports cockpit cockpit-networkmanager`.
- Recommends kept (P17): `cockpit-storaged`, `cockpit-packagekit`, bringing `network-manager`, `packagekit`, `udisks2`. NetworkManager leaves ifupdown interfaces alone (D52). No `cockpit-machines` or `cockpit-podman`.
- The package enables `cockpit.socket` (9090).

**Check:** `cockpit.socket` is enabled and active.

Summary note: "Cockpit: `https://<lan-ip>:9090`, with your user's password" (root is in `/etc/cockpit/disallowed-users`).

### 10. Command-line and developer tools

Shown: always.
1. **gh** from GitHub's repository (D18): download `https://cli.github.com/packages/githubcli-archive-keyring.gpg`; its sha256 must be `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b` (a mismatch fails the step); install as `/etc/apt/keyrings/githubcli-archive-keyring.gpg` (0644). Write `/etc/apt/sources.list.d/github-cli.sources`: `Types: deb`, `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main`, `Architectures: amd64`, `Signed-By:` that keyring. `apt-get update`.
2. Install `build-essential git git-lfs gh curl wget jq vim neovim tmux htop btop ripgrep fd-find fzf bat tree unzip zip rsync lsof strace bind9-dnsutils iproute2 traceroute mtr-tiny netcat-openbsd cmake ninja-build pkgconf libssl-dev shellcheck bash-completion man-db` (`pkgconf`, not the transitional `pkg-config`), plus `autoconf automake libtool gdb` (verify: debian13-notes).
3. As the user: `mkdir -p ~/.local/bin`; symlinks `~/.local/bin/fd → /usr/bin/fdfind` and `~/.local/bin/bat → /usr/bin/batcat` (a regular file already there is left alone, with a note); `git lfs install --skip-repo`.

**Check:** the packages are installed, `gh --version` runs, the links resolve, `git config --global --get filter.lfs.process` is set.

### 11. Node.js

Shown: always. Needs checkbox 10 (curl, git); skipped if that one is unticked or failed. Not from apt.
1. The owner's `curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.41.0/install.sh | bash`, with the same effect but no pipe into a shell: download that URL to a user-readable file under `/var/tmp`, run `bash <file>` as the user. The script behaves as usual, including adding its lines to `~/.bashrc`.
2. As the user, `bash -s` with a fixed script on stdin and `26` as the argument, **without errexit** (nvm breaks under `set -e`): `. "$HOME/.nvm/nvm.sh"; nvm install "$1"; nvm alias default "$1"; node -v && npm -v`.

**Check:** the same no-errexit form prints `0.41.0` for `nvm --version` and a `v26.` version for `node -v`; `~/.bashrc` loads nvm.

### 12. Python

Shown: always. Needs checkbox 10; skipped if that one is unticked or failed.
1. Install `python3 python3-venv python3-dev pipx libxml2-dev libxslt1-dev libffi-dev`.
2. uv (not in trixie; D11): as the user, `pipx install uv` → `~/.local/bin/uv`.
3. Venv, as the user: `uv venv --python /usr/bin/python3 ~/.venvs/automation` unless `~/.venvs/automation/pyvenv.cfg` exists. Then `uv pip install --python ~/.venvs/automation requests "httpx[http2]" aiohttp tenacity beautifulsoup4 lxml selectolax parsel scrapy playwright selenium pandas pyyaml python-dotenv rich typer ipython`.
4. Playwright Chromium, as the user: `~/.venvs/automation/bin/playwright install-deps --dry-run chromium`; on exit 1, kiln installs the indented names after `Missing system dependencies (N):` (Playwright never runs apt, D25). Then `playwright install chromium`.
5. As the user, `uv tool install ruff`, `uv tool install pre-commit`, `uv tool install httpie` (binaries in `~/.local/bin`).
6. Shell setup (shared with Go):
   - No `~/.bashrc` → copy `/etc/skel/.bashrc`.
   - Append once, found by its marker line:
     ```bash
     # kiln: load ~/.bashrc.d
     if [ -d "$HOME/.bashrc.d" ]; then for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f; fi
     ```
   - `~/.bashrc.d/50-kiln-python.sh`:
     ```bash
     case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:$PATH" ;; esac
     alias pyauto='. "$HOME/.venvs/automation/bin/activate"'
     ```

**Check:** `uv pip list` lists every package; `install-deps --dry-run chromium` exits 0 and `~/.cache/ms-playwright/` has a chromium directory; `uv tool list` shows the three tools.

### 13. Go

Shown: always. Needs checkbox 10; skipped if that one is unticked or failed.
1. Read `https://go.dev/dl/?mode=json`, take the first `stable: true` release and its file with `kind=archive`, `os=linux`, `arch=amd64` (filename and sha256 from the JSON).
2. Unless `/usr/local/go/VERSION` starts with that version: download to `/var/tmp`, verify the sha256, extract into `/usr/local/go.kiln-new`, swap it in for `/usr/local/go` by rename, remove the old tree.
3. `/etc/profile.d/kiln-go.sh` and `~/.bashrc.d/50-kiln-go.sh`: `export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin`.
4. As the user, one `/usr/local/go/bin/go install <path>@latest` per tool: `golang.org/x/tools/gopls`, `github.com/go-delve/delve/cmd/dlv`, `honnef.co/go/tools/cmd/staticcheck`, `golang.org/x/vuln/cmd/govulncheck`.
5. go.dev unreachable: install `golang-go` from trixie-backports (note) and run `go install` with `GOTOOLCHAIN=local`; a tool failing with "requires go >=" is skipped (note).

**Check:** `/usr/local/go/bin/go version` shows the resolved version (or `golang-go` is installed), and the four tools exist in `~/go/bin`.

### 14. Codecs and video acceleration

Shown: always. Non-free preferred for performance.
- `libavcodec-extra` (it replaces `libavcodec61`; a note says so).
- With GNOME only (the GStreamer plugins serve desktop apps): `gstreamer1.0-plugins-good gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly gstreamer1.0-libav`.
- VA-API: `vainfo` with any GPU; AMD → `mesa-va-drivers`; Intel → `intel-media-va-driver-non-free` (replaces `intel-media-va-driver` if GNOME pulled it in). Pre-Gen8 Intel isn't covered (note).
- Any further non-free codec or VA-API package that applies (for example on NVIDIA cards) (verify: debian13-notes).

**Check:** the selected packages are installed.

### 15. Flathub

Shown: always.
- Install `flatpak`; with GNOME also `gnome-software-plugin-flatpak`.
- `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo`.

**Check:** `flatpak remotes --system` lists `flathub`.

### 16. Google Chrome

Shown: always. From Google's repository through `extrepo`.
1. Install `extrepo`; add `- non-free` under `enabled_policies:` in `/etc/extrepo/config.yaml` (default is only `main`).
2. `extrepo enable google_chrome` → `/etc/apt/sources.list.d/extrepo_google_chrome.sources`, `Signed-By: /var/lib/extrepo/keys/google_chrome.asc`.
3. `/etc/default/google-chrome` with `repo_add_once="false"` and `repo_reenable_on_distupgrade="false"`, so the package adds no duplicate `google-chrome.list`.
4. `apt-get update`, install `google-chrome-stable`.

**Check:** `extrepo_google_chrome.sources` has `Enabled: yes`, `google-chrome-stable` is installed, and no `google-chrome.list` exists.

### 17. NVIDIA driver

Shown: only with an NVIDIA GPU. The source is picked from the card, with no question (D39, D55). Generation comes from the PCI device ID:
- **Turing and newer** (in the compiled-in Turing–Ada table from NVIDIA's open-gpu-kernel-modules README, or ID ≥ `0x2900` for Blackwell): NVIDIA's repository, newest branch, open kernel modules.
- **Maxwell, Pascal, Volta:** Debian's `nvidia-driver` 550 (non-free; it supports Maxwell to Ada). Telling these apart from older cards needs an ID list (verify: debian13-notes, its source).
- **Older, including Kepler:** stays on nouveau with `firmware-nvidia-graphics`; summary note "this card is too old for a supported NVIDIA driver; it uses nouveau".
- Several NVIDIA GPUs that no single source covers: skipped with a note.

Steps:
1. **Secure Boot on:** install `mokutil openssl dkms`; `dkms generate_mok` (creates `/var/lib/dkms/mok.key`/`mok.pub` if missing). If `mokutil --test-key /var/lib/dkms/mok.pub` says not enrolled: make an 8-character one-time password (memory only, never logged), `mokutil --generate-hash` with it on stdin, hash to `/root/.kiln-mok.hash` (0600), `mokutil --import /var/lib/dkms/mok.pub --hash-file /root/.kiln-mok.hash`, delete the hash file. Reboot needed; note "At the next boot choose *Enroll MOK* → *Continue* and enter the one-time password". A key blocked in dbx/MokListX or any mokutil failure → skip the driver with a note. Never install a driver that can't load; never turn Secure Boot off.
2. Install `linux-headers-amd64`.
3. **NVIDIA's repository:**
   1. Download `https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb`; sha256 must be `d0d4ef986a44400f9db33c600ef33a985175e7cc63d805a10e1839c7a1e78f5f`. Extract `usr/share/keyrings/cuda-archive-keyring.gpg` with `dpkg-deb -x` (the package isn't installed); its fingerprint must be `02182E60104FCDC26EAE1B8597A5D4CB8793F200`. Install it as `/etc/apt/keyrings/nvidia-cuda.gpg` (0644). A mismatch fails the step: "NVIDIA's repository key changed; kiln needs an update".
   2. `/etc/apt/sources.list.d/nvidia-cuda.sources`: `Types: deb`, `URIs: https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/`, `Suites: /` (flat, no `Components:`), `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/nvidia-cuda.gpg`. `apt-get update`.
   3. Install `nvidia-driver nvidia-kernel-open-dkms` (no CUDA). Same names as Debian's at higher versions, so apt picks NVIDIA's; no `nvidia-driver-pinning-*`, so it follows the newest branch.
4. **Debian's 550:** install `nvidia-driver` from trixie, with Recommends (they bring `libcuda1` and `nvidia-smi`, which the Container Toolkit needs; forced, D55).
5. The installed set must include what the Container Toolkit needs (verify: debian13-notes).

A reboot is needed. **Check:** `dkms status` shows the module built for the installed kernel; for NVIDIA's repository, `apt-cache policy nvidia-driver` shows the installed version from `developer.download.nvidia.com`; with Secure Boot, `mokutil --test-key` says enrolled or pending.

### 18. Battery limit (70–80%)

Shown: on any machine with a system battery (`type=Battery`, `scope` not `Device`). The goal is to keep the battery between 70% and 80% (D56). The first method that fits is used.

1. **Kernel thresholds** (**proposed**, D56), when the battery offers `charge_control_end_threshold`: write start 70, then end 80 (thinkpad_acpi rejects an end below the current start), and read both back. `/etc/udev/rules.d/90-kiln-battery.rules` sets the same values whenever the battery appears (verify: debian13-notes, udev `ATTR{}` assignment for these files). No script or timer: the hardware enforces it. With GNOME, the summary says to leave *Preserve Battery Health* alone (turning it off resets the limit to 100% until next boot).
2. **HP WMI** (HP's battery charge command, type `0x2B` through `\_SB.WMID.WMAA`, as on the HP Envy x360 13-ay0xxx), when the laptop is an HP and that ACPI method exists (verify: debian13-notes, how to detect it):
   - Install `acpi-call-dkms` (verify: debian13-notes) and load `acpi_call` at boot through `/etc/modules-load.d/acpi_call.conf`. With Secure Boot on, the module is signed with the DKMS MOK and needs the same enrollment as checkbox 17 (verify: debian13-notes).
   - A plain shell script `/usr/local/sbin/battery-limit`, with `battery-limit.service` (oneshot) and `battery-limit.timer`; none of them call kiln. It reads `capacity`: above 80, mode `0x0100` ("no charge", hold) through `/proc/acpi/call`; at 70 or below, mode `0x0000` (normal); otherwise nothing (verify: debian13-notes, the exact call string and that `0x0100` holds on AC). On AC it sits near 80% and drops to 70% only when unplugged.
   - The timer runs the script at boot and every 60 s (**proposed**, D56; the alternative is 300 s).
   - If `0x0100` turns out not to hold, the fallback (force discharge `0x0200` from 80% down to 70%, then normal) goes back to the owner first (**proposed**, D56).
3. **Neither** (**proposed**, D56): nothing is installed; the summary names the laptop's DMI vendor and model and says to use the BIOS charge-limit option if it has one.

**Check:** thresholds: both values read back as 70 and 80. HP: `acpi_call` is loaded, the timer is active, and one run of the script exits 0.

---

## Summary screen

The run's last screen. kiln never reboots on its own.
- Each checkbox: done, failed (what failed and what to do), or skipped (unticked, or a step it needs failed).
- The steps' notes, including: password SSH open for the user and root; Docker-published ports bypass UFW; Cockpit and Portainer addresses; log out and back in for `sudo` and `docker`.
- Reboot needed, yes or no, with reasons (kernel, microcode or firmware, NVIDIA driver, MOK enrollment, `/run/reboot-required`).
- The MOK one-time password, if any, once, in a "Write this down now" panel.
- The summary text is appended to `/var/log/kiln.log`, without the password.
- "Reboot now" runs `sync` and `systemctl reboot` after the screen exits, first reminding to write down any password.

---

## Dropped (D55)

Gone: mirror benchmarking (deb.debian.org directly), thermal and thinkfan, the backports kernel, DVD playback, fwupd, hostname and timezone settings, chrony, the battery threshold ladder, vendor battery modes and the software watcher, every sub-option, saved choices and `/etc/kiln` / `/var/lib/kiln`, "unchanged" detection, the plan preview and review screen, revert snapshots, file backups, subcommands and units that call kiln, the separate summary file, the systemd scope, apt-daily and PackageKit masking, the inhibitor, the single-instance lock, and the allow-apply file. Network migration (D52) and remote desktop (D50) stay gone; the firewall is back as UFW.
