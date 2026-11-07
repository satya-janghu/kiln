# Steps catalog

**This file is the source of truth for behavior.** Code follows it. When behavior changes, update this file in the same change (rule 11).

Steps run in the order listed (`internal/steps/all`). Every step and sub-option defaults to **on**, except the `kernel` step (D38). Options and their defaults are in [options-reference.md](options-reference.md); each step below names its option keys. A fact the code relies on must be **V** in [debian13-notes.md](debian13-notes.md) ([review-process.md](review-process.md)).

**The Debian installer owns the desktop (GNOME or none) and blends;** kiln never changes them, and steps that need them are "not applicable" without them. Exception: the ssh step installs the SSH server when missing (D45). Definitions (notes):
- **GNOME detected:** `gnome-shell` and `gdm3` are installed (`task-gnome-desktop` → `gnome-core` pulls in both).
- **SSH server detected:** `openssh-server` is installed.
- **Standard system utilities:** every Priority: standard package. kiln must not rely on them (`pciutils`, `wget`, `ca-certificates`, …); `preflight` installs what it needs.
- **Blends:** the `task-*-blend` packages. Reported only.

`kernel` (5b) is an extra step (D13). Removed: `desktop` (D29), `network-migration` (D52), `firewall` and `remote-desktop` (D50).

**Risky** marks a step that uses verify-then-revert ([architecture.md](architecture.md#risky-changes-verify-then-revert-on-failure)). Only `ssh` is risky.

All apt work goes through the `apt` helper (non-interactive, confold, lock wait; architecture). "Install X" means `AptInstall`, a no-op when X is installed. "As the user" means the user-run rule in architecture.md.

---

## 0. preflight

**ID** `preflight` · base · always runs, can't be skipped (critical)

**At wizard start** (screen 0, before any step; D41):
- **Single instance:** exclusive lock `/run/kiln/kiln.lock`. If held: "kiln is already running (pid N)", log offered read-only.
- **Repair** (D54: re-run machinery), in order: the `pending` revert prompt; an unfinished run record ("The last run stopped during <step>"); interrupted dpkg (`dpkg --audit` output or `/var/lib/dpkg/updates/` entries) → `dpkg --configure -a` + `apt-get -f install -y`; stale runtime masks removed and enabled apt-daily timers restarted.
- **Battery:** on battery, warn. Below 30% and not on AC, ask before continuing.
- **Own scope:** first thing in the wizard command, kiln re-executes itself via `systemd-run --scope --quiet --collect --unit=kiln-run-<id>`, so closing the terminal or session can't kill it mid-dpkg. Skipped when `/run/systemd/system` is missing.

**At run start** (runner lifecycle):
- **Inhibitor:** `systemd-inhibit --what=sleep:idle:handle-lid-switch:handle-power-key:shutdown --mode=block --who=kiln --why="installing packages; rebooting now could break the system" cat`, with `cat` reading a pipe only kiln holds, so the block ends if kiln dies. `handle-lid-switch` is needed (logind's `LidSwitchIgnoreInhibited=yes`). `shutdown` blocks every logind reboot, root's included; escapes: `systemctl reboot -i`, `--force`, the power button.
- **Quiet other package managers:** stop active `apt-daily*.timer`, runtime-mask `apt-daily*.timer`/`.service` and `packagekit.service`, wait for `apt-daily*.service` to finish and the dpkg lock to be free, then stop packagekit. Undone in the end-of-run restore (architecture).

**Facts** (`facts` package):
- **Root;** under plain `su` (no `ALWAYS_SET_PATH` in Debian's `login.defs`) kiln prepends `/usr/local/sbin:/usr/sbin:/sbin` to its PATH before anything runs (D53).
- **OS:** `/etc/os-release` (os-release(5), quotes stripped) and `dpkg --print-architecture`.
- **Virtualization:** container = `systemd-detect-virt --container` exits 0, or `/.dockerenv` / `/run/.containerenv` exists; VM = `--vm`.
- **Installer choices:** GNOME and SSH server detected (above); installed `task-*` and `task-*-blend` packages (information only); whether the "standard" task is installed (`tasksel --list-tasks` marks it `i`; tasksel may be absent); the default systemd target.
- **Hardware:**
  - DMI vendor, product, family, chassis type; CPU vendor, model, cores; RAM;
  - GPUs from `/sys/bus/pci/devices/*/{class,vendor}` (classes 0x0300/0x0302/0x0380), so lspci isn't needed;
  - Wi-Fi: PCI class 0x0280 devices (vendor ID) and each NIC's driver and `device/vendor`;
  - Secure Boot: efivar `SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c`, byte 5 = 1. No efivars means BIOS boot, "Secure Boot: n/a";
  - batteries (`/sys/class/power_supply/*`, type=Battery, scope≠Device) and the AC adapter;
  - `platform_profile` and its choices, amd-pstate status, thinkpad_acpi / hp-wmi presence.
- **Network:** NICs (physical vs virtual), IPv4/IPv6 addresses, LAN subnets, default route, and whether each NIC is managed by ifupdown, NetworkManager or nothing.
- **Session:** the target user; SSH session (`SSH_CONNECTION`).

**Actions:**
- Validate; any failure stops the run: not root → "Run it with sudo, or as root via `su -`"; not `ID=debian` + `VERSION_ID=13` + `amd64`; a container → "kiln sets up real machines, not containers" (a VM only warns); no target user. Notes: plain `su`, VM, SSH session.
- Install `ca-certificates` and `pciutils` if missing (P15).
- **Internet:** DNS lookup of `deb.debian.org`, then GET `http://deb.debian.org/debian/`, then GET `https://github.com/robots.txt` (60 s timeout each). Errors:
  - DNS → "check the network and DNS (cable, Wi-Fi, router)";
  - deb.debian.org unreachable → "Check the cable or Wi-Fi";
  - certificate error → "install ca-certificates";
  - other github.com failure → "can't reach github.com over HTTPS".

**Check:** done when validation passes, the prerequisites are installed and the internet check passes. The runner then skips Plan, so its notes don't appear (the VM and SSH warnings are also on the facts screen). **Failure** stops the run (the only step that does).

---

## 1. sudo

**ID** `sudo` · base · depends: preflight · option: `nopasswd` (on)

**Actions:**
1. Install `sudo` if missing. d-i installs sudo and adds the user to group `sudo` only when the root password is left empty (user-setup 1.107; notes).
2. `adduser <user> sudo` (no-op if already a member).
3. File `/etc/sudoers.d/90-<sanitized>-nopasswd` with `<user> ALL=(ALL:ALL) NOPASSWD: ALL`. With `nopasswd` off: `/etc/sudoers.d/90-<sanitized>` with `<user> ALL=(ALL:ALL) ALL` (D45); an existing `-nopasswd` file is left in place and reported (D44, D53).
   - Name sanitized to `[a-z0-9_-]` (others → `_`), because sudoers(5) skips names ending in `~` or containing `.`.
   - Naming the user means sudo works without re-login. `/etc/sudoers` ends with `@includedir /etc/sudoers.d`, after `%sudo`, so this rule wins.
   - Write `/etc/sudoers.d/.kiln-check.<name>` (0440) and validate with `visudo -c -q -f`.
   - Install 0440 root:root through the managed writer, then `visudo -c` on the whole policy (visudo(8): checking one include isn't enough).
   - If that fails, put the previous file back (or remove the new one) and fail. Never leave an invalid file.

**Check:** sudo installed, user in `sudo`, file present with identical content and mode 0440, `visudo -c` passes.

**Gotchas:** group membership needs a new login ("log out and back in" note).

---

## 2. apt-sources

**ID** `apt-sources` · base · depends: preflight · options: `backports` (on), `lock_wait` (minutes, 10, 1–240; sets the apt lock wait for this run, D53)

**Installer leaves** (apt-setup 0.198): a one-line `/etc/apt/sources.list` with `deb` and `deb-src` lines for `http://deb.debian.org/debian` (trixie, trixie-updates) and `http://security.debian.org/debian-security trixie-security`, components `main non-free-firmware`. Cdrom lines are commented out unless the medium was a real CD.

**Actions** (apt 3.0.3), under a `revert` snapshot of `sources.list` and `sources.list.d/`; any failure, including `apt update`, restores it (D44):
1. Comment out active `deb cdrom:` lines first (bracketed labels contain spaces, which would break conversion).
2. `sources.list`: comment out every active line under `# Debian entries now live in sources.list.d/debian.sources (managed by kiln)` (backed up first). `deb-src` isn't carried over.
3. Other active `sources.list.d/*.list` files: `apt -y modernize-sources` (`X.list` → `X.sources`, original renamed `X.list.bak`). A converted file with an empty `Signed-By:` fails the step.
4. Write `/etc/apt/sources.list.d/debian.sources` (all stanzas `Types: deb`, `Components: main contrib non-free non-free-firmware`, `Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp`):
   - `URIs: http://deb.debian.org/debian`, `Suites: trixie trixie-updates`;
   - `URIs: http://security.debian.org/debian-security`, `Suites: trixie-security`;
   - backports (option): `URIs: http://deb.debian.org/debian`, `Suites: trixie-backports`. Its Release has `NotAutomatic: yes` / `ButAutomaticUpgrades: yes`, so nothing comes from it without `-t trixie-backports`.
5. Set `Enabled: no` on `.sources` stanzas with a `cdrom:` URI, and on other stanzas for the same suites whose URI is exactly the Debian archive (`http(s)://deb.debian.org/debian[/]`, `deb.debian.org/debian-security`, `security.debian.org/debian-security`) or kiln's mirror list. Others (e.g. `debian-debug`) are left alone.
6. `AptUpdate`.

**Check:** `debian.sources` is byte-identical to the generated content (main/backports URIs may be the mirror form), no active cdrom entries, no other stanza targets the Debian suites, and `sources.list` has no active lines.

**Gotchas:** backups go to `/var/lib/kiln/backup/…`, never into `sources.list.d/` (P2).

---

## 3. mirrors

**ID** `mirrors` · base · depends: apt-sources · options: `country` (`auto` or a two-letter code), `sample` (4), `top` (5), `test_mb` (40), `test_seconds` (6) · subcommands `kiln mirrors bench [--apply]`, `kiln mirrors reset` (both honor the apply gate and run lock, D53)

**Actions** (rank by throughput, not latency):
1. **Candidates** from `https://mirror-master.debian.org/status/Mirrors.masterlist` (RFC822 stanzas; de-duplicated by site, Site/path validated). Usable: `Archive-architecture` includes `amd64` and `Archive-http` is set; URL `http://<Site><Archive-http>`. Selected: mirrors in the country, `sample` random others, and `deb.debian.org`.
   - `country: auto` maps the timezone (`system.timezone`, else `timedatectl show -p Timezone`, else `/etc/localtime`) to a country via the compiled-in zone1970.tab (Asia/Kolkata → IN). Unknown zone → sample + deb.debian.org only. No geo-IP (D21), no neighbouring countries (D52).
2. **Freshness:** fetch `dists/trixie/InRelease` and `dists/trixie-updates/InRelease` (and `trixie-backports` if enabled), 15 s each. Drop a mirror if any fetch fails, its trixie `Date:` differs from deb.debian.org's, its trixie-updates/backports `Date:` is over 12 h older, or any `Valid-Until` has passed.
3. **Throughput,** one mirror at a time. Test file `main/Contents-all.gz` (~38 MB, size from the signed InRelease). `GET` with `Range: bytes=0-<min(test_mb, size)-1>` (at least 1 MB), stopped at `test_seconds` (normal). Score = bytes after the first 0.5 s ÷ time after 0.5 s (whole transfer if it finished sooner). Score 0 on HTTP error, status other than 206/200, or under 1 MB. Not measured in preview.
4. **Mirrorlist** `/etc/apt/mirrors/debian.list` (apt-transport-mirror(1)): URI, TAB, `key:value` tags.
   - The best `top` mirrors with score > 0, all `priority:1` (apt picks randomly among equal priorities, with failover).
   - `http://deb.debian.org/debian<TAB>priority:99` last, unless deb.debian.org is already in the top list.
   - No mirror scored → fail: "no mirror could be measured, so kiln kept deb.debian.org…".
5. **Sources:** rewrite `debian.sources` with main and backports URIs `mirror+file:/etc/apt/mirrors/debian.list`. Security stays direct (not mirrored).
6. **Retries:** `/etc/apt/apt.conf.d/80kiln-acquire` with `Acquire::Retries "5";`.
7. **Verify:** `apt update`; on failure restore the previous `debian.sources` and mirror list (or remove a new one) and fail with "…kept deb.debian.org" or "…kept the previous mirror list".

**Check:** the mirrorlist exists, is under 30 days old, every listed mirror passes freshness, `debian.sources` and `80kiln-acquire` match exactly. Re-benchmarking is `kiln mirrors bench --apply`, not Check.

**Gotchas:** deb.debian.org (Fastly) often wins on its own.

---

## 4. upgrade

**ID** `upgrade` · base · depends: apt-sources · option: `full_upgrade` (on; off = "not applicable")

**Actions:** `AptUpdate`, then `apt-get full-upgrade -y`. Simulate before and after with `LC_ALL=C apt-get -s full-upgrade` (`Inst`/`Remv` lines are untranslated; the summary line isn't parsed). Reboot reason if:
- the simulation listed `Inst linux-image-`;
- `uname -v` ("Debian <ver>") differs from `dpkg-query -W -f '${Version}' linux-image-$(uname -r)` (same-ABI rebuilds);
- `/run/reboot-required` exists (fallback; only unattended-upgrades' hook writes it).

**Check:** `LC_ALL=C apt-get -s full-upgrade` prints no `Inst`/`Remv` lines (after this run's update).

---

## 5. firmware-packages

**ID** `firmware-packages` · hardware · depends: apt-sources · option: `microcode` (on)

**Actions** (non-free-firmware, always on, D52):
- Microcode (option): `amd64-microcode` (AMD) or `intel-microcode` (Intel).
- `firmware-misc-nonfree --no-install-recommends` (its Recommends pull firmware the machine may not need). It lacks Intel iGPU, NVIDIA and MediaTek firmware.
- By GPU: AMD → `firmware-amd-graphics`; Intel → `firmware-intel-graphics firmware-intel-misc`; NVIDIA → `firmware-nvidia-graphics` (nouveau; `firmware-nvidia-gsp` comes via the driver in multimedia).
- By NIC driver: `iwlwifi` → `firmware-iwlwifi`; `ath*` → `firmware-atheros`; `mt7*` → `firmware-mediatek`; `brcmfmac` → `firmware-brcm80211`; Realtek (`r8169`, `r8152`, `rtw_*`, `rtw89*`, `rtl8*`) → `firmware-realtek`.
- By PCI Wi-Fi vendor (class 0280; a card missing firmware has no interface): `8086` → iwlwifi, `14c3` → mediatek, `10ec` → realtek, `168c`/`17cb` → atheros, `14e4` → brcm80211.
- `isenkram-cli`.
- If any firmware or microcode package was missing: `update-initramfs -u -k all` (the triggers rebuild only the newest image). On failure the error says to free space in `/boot` and re-run.
- Report remaining gaps with `isenkram-autoinstall-firmware -l` (list-only; stdout decides, exit code ignored) as a summary note. Install mode is never used: it writes one-line `.list` sources.
- Reboot note when microcode was missing at Plan time.

**Check:** all selected packages and `isenkram-cli` are installed.

**Gotchas:** Bluetooth/USB devices aren't detected, so a Realtek Bluetooth-only device doesn't trigger `firmware-realtek`.

---

## 5b. kernel

**ID** `kernel` · hardware · depends: apt-sources, firmware-packages · **default off** (D38) · option: `backports_firmware` (on)

**Applies:** when ticked; backports disabled → "not applicable: backports disabled". Otherwise the machine keeps trixie's `linux-image-amd64` (6.12 LTS).

**Kernel policy** (D38): never pin, hold or mark a kernel manual; the metapackage tracks its suite and updates normally (D40); old kernels go through normal autoremove (P14).

Backports `linux-image-amd64` (7.2.x, signed) needs backports' `linux-base`, so `-t trixie-backports` is required.

**Actions:**
1. `apt-get install -t trixie-backports linux-image-amd64`, plus `linux-headers-amd64` when an NVIDIA GPU is present.
2. Backports firmware (option): reinstall step 5's `firmware-*` packages with `-t trixie-backports` (`firmware-misc-nonfree` again `--no-install-recommends`), then `update-initramfs -u -k all`.
3. Reboot note: "new kernel <ver>".

**Check:** a backports `linux-image-amd64` is installed, plus the backports headers (NVIDIA) and selected firmware where backports offers them. Failed parts are retried (D53).

**Gotchas:**
- Autoremove eventually removes the stable 6.12 image. To go back, install `linux-image-amd64` from trixie by hand.
- With multimedia's NVIDIA on and source `debian-stock`, the review screen warns that 550 may not build for 7.2 (P1).

---

## 6. network-migration

Removed (D52).

---

## 7. multimedia

**ID** `multimedia` · media · depends: apt-sources · options: `codecs`, `dvd`, `vaapi`, `nvidia` (all on), `nvidia_source`

**Actions:**
- **Codecs** (GStreamer parts only with GNOME): `libavcodec-extra`, `gstreamer1.0-plugins-good`, `gstreamer1.0-plugins-bad`, `gstreamer1.0-plugins-ugly`, `gstreamer1.0-libav`. `libavcodec-extra61` replaces `libavcodec61` (Conflicts/Provides); a warning note says so.
- **DVD** (libdvd-pkg 1.4.3, contrib), after NVIDIA:
  1. Preseed `libdvd-pkg libdvd-pkg/build boolean true` and `libdvd-pkg libdvd-pkg/post-invoke_hook-install boolean true`.
  2. Install `libdvd-pkg` (its build is postponed while dpkg holds the lock).
  3. Wait for the automatic apt jobs and the dpkg lock, then `dpkg-reconfigure -f noninteractive libdvd-pkg`, which builds `libdvdcss2` from the videolan.org source. A failed build, or exit 0 with nothing built, is a note; the rest of the step still counts (D44, D53).
- **VA-API** (option): `vainfo` with any GPU (D51); AMD → `mesa-va-drivers`; Intel → `intel-media-va-driver-non-free`, or `intel-media-va-driver` (main) only when non-free isn't available. They conflict, so only one is installed; non-free replaces main even if GNOME pulled main in (D53). Pre-Gen8 Intel isn't covered (noted).
- **NVIDIA** (option), only with an NVIDIA GPU. Source per machine (D39):
  - `nvidia-latest`: NVIDIA's repository, newest branch (620.x), open kernel modules only (Turing and later, Linux ≥ 4.15). Default for Turing and newer.
  - `debian-stock`: trixie's `nvidia-driver` 550.163 (non-free; Maxwell to Ada, not Blackwell). The form shows Debian's wiki warning that 550 "is no longer maintained".
  - **Generation** from the PCI device ID: ID ≥ `0x2900` → Blackwell (latest only); in the compiled-in table from NVIDIA's open-gpu-kernel-modules README → Turing–Ada (both); otherwise pre-Turing (stock only, latest shown disabled with the reason).
  - Mixed GPUs no single source covers → NVIDIA skipped with a note. A source the GPU doesn't support fails the step. Unticked → nouveau stays, with step 5's firmware.
  1. **Secure Boot on** (both sources; DKMS; NVIDIA's DKMS needs `dkms ≥ 3.1.8`, trixie has 3.2.2):
     - install `mokutil openssl dkms`; `dkms generate_mok` (creates `/var/lib/dkms/mok.key`/`mok.pub` if missing);
     - if `mokutil --test-key /var/lib/dkms/mok.pub` says not enrolled or pending: generate an 8-character one-time password into `/etc/kiln/secrets/mok` (0600; shown once on the summary screen, never in notes); `mokutil --generate-hash` with the password on stdin, hash to `/root/.kiln-mok.hash` (0600); `mokutil --import /var/lib/dkms/mok.pub --hash-file /root/.kiln-mok.hash`; delete the hash file; reboot reason and note "At next boot choose *Enroll MOK* → *Continue* → enter the one-time password shown on the summary screen (also: sudo kiln secrets show mok)";
     - a key blocked in dbx/MokListX, an unexpected answer, or any failure → **skip NVIDIA** with a note. Never install a driver that can't load; never disable Secure Boot.
  2. `linux-headers-amd64` from the suite of the **installed** kernel metapackage (`~bpo13` version or `apt-cache policy` source; the preview predicts from the kernel checkbox) (D53).
  3. **`nvidia-latest`:**
     1. Key: download `https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb`; sha256 must be `d0d4ef986a44400f9db33c600ef33a985175e7cc63d805a10e1839c7a1e78f5f`; extract `usr/share/keyrings/cuda-archive-keyring.gpg` with `dpkg-deb -x` (package not installed); fingerprint must be `02182E60104FCDC26EAE1B8597A5D4CB8793F200`; install as `/etc/apt/keyrings/nvidia-cuda.gpg` (0644). Mismatch → fail: "NVIDIA's repository key changed; kiln needs an update".
     2. `/etc/apt/sources.list.d/nvidia-cuda.sources`: `Types: deb`, `URIs: https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/`, `Suites: /` (flat, no `Components:`), `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/nvidia-cuda.gpg`. Then `AptUpdate`.
     3. No `nvidia-driver-pinning-*`: apt follows NVIDIA's newest branch.
     4. Install `nvidia-driver nvidia-kernel-open-dkms` (desktop set, no CUDA). Same names as Debian's at higher versions, so apt picks NVIDIA's; the review screen shows origins.
  4. **`debian-stock`:** set any existing `nvidia-cuda.sources` to `Enabled: no`, then install `nvidia-driver` from trixie.
  5. **Switching:** stock → latest is an upgrade. Latest → stock is a downgrade kiln doesn't automate: the driver is skipped this run and the summary gives the manual steps (disable the NVIDIA source, `apt install nvidia-driver=<550 version>` and dependencies).

**Check:**
- selected packages installed;
- DVD: `libdvd-pkg` and `libdvdcss2` (`*~local`) installed, `/usr/lib/x86_64-linux-gnu/libdvdcss.so.2` exists;
- NVIDIA: `linux-headers-amd64` installed; latest: `nvidia-cuda.sources` and key match, `nvidia-kernel-open-dkms` installed, `apt-cache policy nvidia-driver` shows the installed version from `developer.download.nvidia.com`; stock: installed version from trixie and no enabled `nvidia-cuda.sources` stanza; `dkms status` shows the module for the running kernel or a **newer** installed kernel with `/boot/vmlinuz-<k>` (`dpkg --compare-versions`);
- NVIDIA + Secure Boot: MOK enrolled or pending (`mokutil --test-key`).

---

## 8. repos

**ID** `repos` · apps · depends: apt-sources · options: `extrepo` (list, `[google_chrome]`), `flathub` (on)

**Applies:** "not applicable" when there's nothing to do (no usable repo and no Flathub): without GNOME "no GNOME desktop installed (the chosen repositories are for desktop apps)", else "no repositories chosen".

**Actions:**
1. Install `extrepo` (0.14). For each chosen repo:
   1. Ensure its policy is in `enabled_policies:` in `/etc/extrepo/config.yaml` (default lists only `main`). `google_chrome` is `non-free`, so add `- non-free` (key-level edit). Unknown repos get their policy from `extrepo search`.
   2. `extrepo enable <name>` → `/etc/apt/sources.list.d/extrepo_<name>.sources`, `Signed-By: /var/lib/extrepo/keys/<name>.asc`.
   3. Let the post-enable command finish uninterrupted, stdin open (10 s countdown, then `/etc/default/google-chrome` with `repo_add_once="false"` and `repo_reenable_on_distupgrade="false"`, so Chrome adds no duplicate `google-chrome.list`).
   4. `AptUpdate`, then install the package (`google-chrome-stable`). Unknown repos: enabled only, with a note.
   - If `extrepo_google_chrome.sources` is enabled but `/etc/default/google-chrome` lacks the keys, Plan fails and says to `extrepo disable`, delete the file and re-run.
2. Flathub (option; GNOME only, else a note "Flathub: not applicable: no GNOME desktop installed."): install `flatpak` and `gnome-software-plugin-flatpak`, then `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo`.

**Check:** `extrepo_<name>.sources` has `Enabled: yes`, packages installed, `/etc/default/google-chrome` has both keys, `flatpak remotes --system` lists flathub.

---

## 9. devtools

**ID** `devtools` · dev · depends: apt-sources · options: `gh_source` (`github`, D18, or `debian`), `extra_packages` (names matching `^[a-z0-9][a-z0-9+.-]+$`)

**Actions:**
1. `gh_source=github`: download `https://cli.github.com/packages/githubcli-archive-keyring.gpg`, sha256 must be `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b` (mismatch fails); install as `/etc/apt/keyrings/githubcli-archive-keyring.gpg` (0644). Write `/etc/apt/sources.list.d/github-cli.sources`: `Types: deb`, `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main`, `Architectures: amd64`, `Signed-By:` that keyring. `AptUpdate`. With `debian`, trixie's `gh` 2.46 is used and an existing GitHub source and keyring are left in place.
2. Install `build-essential git git-lfs gh curl wget jq vim neovim tmux htop btop ripgrep fd-find fzf bat tree unzip zip rsync lsof strace bind9-dnsutils iproute2 traceroute mtr-tiny netcat-openbsd cmake ninja-build pkgconf libssl-dev shellcheck bash-completion man-db` (`pkgconf`, not the transitional `pkg-config`). Extra packages are checked with `apt-cache policy` before review; a missing one is a note.
3. As the user: `mkdir -p <home>/.local/bin`; symlinks `<home>/.local/bin/fd → /usr/bin/fdfind` and `<home>/.local/bin/bat → /usr/bin/batcat`, replaced only if they point elsewhere (a regular file there is left alone with a note); `git lfs install --skip-repo`.

**Check:** available packages installed, the GitHub source and keyring match, the links are correct (or a regular file is there), `git config --global --get filter.lfs.process` is set.

---

## 10. system

**ID** `system` · base · depends: preflight · options: `hostname`, `timezone` (empty = leave), `time_sync` (`timesyncd`|`chrony`), `journal_size` (`500M`), `inotify_watches` (524288), `inotify_instances` (1024), `zram` (on)

**Actions:**
- **Hostname** (if set and different): `hostnamectl set-hostname <name>`, and a key-level edit of the `127.0.1.1` line in `/etc/hosts` (inserted after `127.0.0.1` if missing; an existing domain suffix is kept). Note "log out and back in" (X11).
- **Timezone** (if set; must exist under `/usr/share/zoneinfo/`, else Plan fails): `timedatectl set-timezone <zone>`.
- **Time sync:** `systemd-timesyncd` (D7) + `timedatectl set-ntp true`; or install `chrony`, which replaces timesyncd (`time-daemon`).
- **journald:** `/etc/systemd/journald.conf.d/60-kiln.conf` with `[Journal]` and `SystemMaxUse=<journal_size>`, then `systemctl restart systemd-journald`.
- **inotify:** `/etc/sysctl.d/60-kiln-inotify.conf` with max(current, configured) for `fs.inotify.max_user_watches` and `max_user_instances` (never lowered), applied with `sysctl -q -p <file>`.
- **zram** (option; `systemd-zram-generator`, D8): Debian's config is `[zram0]` with the default size `min(ram / 2, 4096)`.
  - Write `/etc/systemd/zram-generator.conf.d/60-kiln.conf`: `[zram0]` + `compression-algorithm = zstd`. Then `systemctl daemon-reload`.
  - `dev-zram0.swap` inactive → `systemctl start dev-zram0.swap`.
  - Active and config changed → restart `systemd-zram-setup@zram0.service` only when used swap is under 50% of MemAvailable (the restart runs `swapoff`); else note "zram settings apply at the next boot" (P11).
  - Remove `zram-tools` if present. Turning the option off leaves an existing setup alone.

**Check:** `/etc/hostname`, the hosts line, timezone and NTP as chosen (with chrony: chrony installed); journald and sysctl files match and live sysctl values ≥ configured; zram drop-in matches and `/proc/swaps` lists `/dev/zram0` (a pending "next boot" note counts as done).

---

## 11. power

**ID** `power` · power · depends: preflight · options: `always_on` (on), `lock_gnome_settings` (on, D23), `power_manager` (`auto`|`power-profiles-daemon`|`tuned`)

**Actions.** With `always_on` off, the sleep, logind and GNOME parts are skipped.
- **Sleep targets:** `systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target suspend-then-hibernate.target`.
- **logind:** `/etc/systemd/logind.conf.d/60-kiln-always-on.conf`: `[Login]`, `HandleLidSwitch=ignore`, `HandleLidSwitchExternalPower=ignore`, `HandleLidSwitchDocked=ignore`, `HandleSuspendKey=ignore`, `HandleSuspendKeyLongPress=ignore`, `HandleHibernateKey=ignore`, `HandleHibernateKeyLongPress=ignore`, `IdleAction=ignore`. `HandlePowerKey` stays poweroff. Apply with `systemctl reload systemd-logind` (`Type=notify-reload`; notes).
- **GNOME** (detected):
  - `/etc/dconf/profile/user`: create with `user-db:user` + `system-db:local`, or add `system-db:local` if missing.
  - `/etc/dconf/db/local.d/60-kiln-power`: `[org/gnome/settings-daemon/plugins/power]` `sleep-inactive-ac-type='nothing'`, `sleep-inactive-battery-type='nothing'`, `power-button-action='interactive'` (P10).
  - Lock option: `/etc/dconf/db/local.d/locks/60-kiln-power` listing those three keys; option off removes it. Then `dconf update`.
  - Greeter: `/etc/gdm3/greeter.dconf-defaults` (ucf-managed, keys commented out): uncomment or insert `sleep-inactive-ac-type='nothing'` and `sleep-inactive-battery-type='nothing'` under the power header (key-level edit), then `systemctl reload gdm3` (runs `/usr/share/gdm/generate-config`; sessions survive).
- **Power manager,** balanced everywhere (D52). `auto` = ppd with GNOME, tuned without. Never both: the other is stopped, disabled and removed (ppd mode also removes `tuned-ppd`; forcing tuned on GNOME warns and removes ppd). The chosen daemon gets `enable --now` with a 30 s readiness wait (D53). Recorded in state.
  - **ppd:** `powerprofilesctl set balanced` (persists in `/var/lib/power-profiles-daemon/state.ini`).
  - **tuned:** `tuned-adm profile balanced` (stock profile; notes). Remove the old `/etc/tuned/profiles/kiln-performance/` on both paths.
  - TLP is never installed or looked for (D50).

**Check:** targets masked, logind drop-in matches, dconf keys and locks present with `/etc/dconf/db/local` newer than them (GNOME), greeter keys uncommented, active profile ppd `balanced` / tuned `balanced`, no `kiln-performance` left.

**Gotchas:** plain `tuned` and ppd don't conflict as packages, so kiln enforces one owner. Both set the platform profile and AMD EPP, so `thermal` only verifies.

---

## 12. thermal

**ID** `thermal` · power · depends: power · options: `check_profile` (on), `thinkfan` (on) · helper `kiln thermal apply`

**Actions:**
- Install `lm-sensors` (never `sensors-detect`, P3).
- **Platform profile** (`check_profile`): never written by kiln. If `/sys/firmware/acpi/platform_profile_choices` contains `balanced`, the value should be `balanced`. `custom` (kernel ≥ 6.14, handlers disagree) is a difference; per-handler values from `/sys/class/platform-profile/*/profile` are reported. No `kiln-thermal.service` (P12).
- **AMD:** report `/sys/devices/system/cpu/amd_pstate/status` and each policy's `energy_performance_preference`; expected `balance_performance` (or `balance_power`, ppd on battery). Differences are notes.
- **HP:** note "Check the BIOS for *Fan Always On*" (Linux can't drive the fans).
- **ThinkPad fan** (`thinkfan`; vendor LENOVO, thinkpad_acpi loaded, `/proc/acpi/ibm/fan` exists, and a k10temp or amdgpu sensor). Otherwise a note: "not applicable: this ThinkPad doesn't expose fan control" or "…no supported temperature sensor (k10temp or amdgpu) was found".
  1. `/etc/modprobe.d/kiln-thinkpad.conf`: `options thinkpad_acpi fan_control=1`; install `thinkfan` (ships disabled).
  2. `/etc/thinkfan.yaml` (read before `/etc/thinkfan.conf`; Debian ships neither), sensors from what's detected:
     ```yaml
     sensors:
       - hwmon: /sys/class/hwmon
         name: k10temp
         indices: [1]
       - hwmon: /sys/class/hwmon
         name: amdgpu
         indices: [1]
         optional: true
     fans:
       - tpacpi: /proc/acpi/ibm/fan
     levels:
       - [1, 0, 50]
       - [3, 45, 58]
       - [5, 55, 66]
       - [7, 63, 80]
       - ["level disengaged", 78, 32767]
     ```
     The fan never stops; level 7 (highest in-spec) from ~65–70 °C; `level disengaged` only from 78 °C (P8).
  3. **Validation** (thinkfan has no config-test flag), under `/run/kiln/thermal.lock` (2 min wait):
     - `/sys/module/thinkpad_acpi/parameters/fan_control` is `Y`: stop a running `thinkfan`, run `timeout 10 thinkfan -n -q -c /etc/thinkfan.yaml`; exit 124 = valid → `systemctl enable --now thinkfan`, then wait 15 s for it to stay active. Any other exit, or not staying active: write `level auto` to `/proc/acpi/ibm/fan`, `systemctl disable thinkfan.service`, fail the step. A leftover boot unit is disabled.
     - Otherwise: leave thinkfan disabled, reboot note, install `kiln-thinkfan-enable.service` (oneshot `kiln thermal apply` at boot: waits up to 60 s for `fan_control=Y`, validates the same way, enables thinkfan, disables itself).

**Check:** `lm-sensors` installed. Profile and EPP are notes only. ThinkPad fan: modprobe file and config match, thinkfan installed, and thinkfan active or the boot helper pending (enabled, `fan_control` not `Y`, not failed). A failed helper is "not done" with a `journalctl -b -u kiln-thinkfan-enable.service` hint.

---

## 13. battery

**ID** `battery` · power · depends: preflight · options: `target_stop` (80), `start_gap` (5), `software_fallback` (on) · helpers `kiln battery status|apply|watch`

**Applies:** at least one system battery (`type=Battery`, `scope` not `Device`; a missing `scope` counts). Else "not applicable: no battery".

**Goal:** charge limit below 90%, preferably 80%. The first method whose value reads back < 90 wins.

### Method ladder

**Method 1: kernel thresholds** (`/sys/class/power_supply/BAT*/charge_control_end_threshold`, plus `charge_control_start_threshold` if present).
- Stop candidates `80, 85, 75, 70, 60, 50` (never ≥ 90); start = stop − `start_gap`.
- Write order: lowering (new stop < current start) writes start first, raising writes stop first (thinkpad_acpi returns EINVAL otherwise). On a write error or read-back mismatch, retry once in the other order, then the next candidate.
- If both orders mismatch, an end read-back with 0 < value < 90 is accepted (drivers round or clamp). asus-wmi caches its read-back: "set, not verifiable".
- `charge_types` present: write `Custom` first and verify it (Dell); if that fails on the primary battery, skip method 1.
- Only the first battery runs the ladder; the others get its pair, and only those that take it are recorded. Every try is logged. Vendor ranges: notes.

**Method 2: vendor fixed modes** (`internal/battery/vendors.go`; rows match on kernel module, IdeaPad also on DMI vendor):
- IdeaPad conservation (`ideapad_laptop`, LENOVO): `/sys/bus/platform/drivers/ideapad_acpi/VPC2004:*/conservation_mode` = `1` (cap ~60 or 80%, not readable).
- Long_Life: `{bat}/charge_types` = `Long_Life` on any battery that lists it.
- Samsung (`samsung_laptop`): `/sys/devices/platform/samsung/battery_life_extender` = `1` (80%).
- LG (`lg_laptop`): `/sys/devices/platform/lg-laptop/battery_care_limit` = `80`.
- Huawei (`huawei_wmi`): `/sys/devices/platform/huawei-wmi/charge_control_thresholds` = `75 80`.
- Sony (`sony_laptop`): `/sys/devices/platform/sony-laptop/battery_care_limiter` = `80`, then `50`.

Adding a vendor is a table row plus its source in debian13-notes.md.

**Method 3: software fallback** (`software_fallback`): if `charge_behaviour` lists `inhibit-charge`, install `kiln-battery-watch.service` (long-running, `Restart=on-failure`, `RestartSec=30`), which polls `capacity` every minute: `inhibit-charge` at ≥ `target_stop`, `auto` at ≤ `target_stop − start_gap`. It logs only on transitions, takes the lock non-blocking per write, and resets to `auto` on exit or when another method takes over (`disable --now`).

**Method 4: nothing works.** The summary names the DMI vendor + product, lists what was probed, and suggests BIOS options: "Battery Care", "Adaptive Battery Optimizer", "Battery Health Manager", "Primarily AC use", "Charge limit".

### Persistence (D54: re-run machinery)

- `kiln-battery.service`: oneshot `ExecStart=/usr/local/bin/kiln battery apply`, no `RemainAfterExit`, enabled for boot.
- `/etc/udev/rules.d/90-kiln-battery.rules`:
  ```
  SUBSYSTEM=="power_supply", ACTION=="change", ATTR{type}=="Mains", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"
  SUBSYSTEM=="power_supply", ACTION=="add", ATTR{type}=="Battery", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"
  ```
  (`ENV{SYSTEMD_WANTS}` doesn't fire on change events; notes.)
- `kiln-battery.timer` every 15 min (other writers emit no uevent).
- Lock `/run/kiln/battery.lock` for the step, `apply` and `watch` (D41). `kiln battery apply` exits 0 quietly if it's busy.
- `kiln battery apply` re-applies the method recorded in `/etc/kiln/state.json` (`battery`; no writes if values hold), else runs the full ladder. It logs, doesn't print.

### GNOME "Preserve Battery Health" (UPower 1.90.9)

UPower's toggle needs `charge_control_start_threshold`, takes limits from hwdb `CHARGE_LIMIT`, and writes 0/100 when turned off (notes). kiln owns the limit:
1. Before writing thresholds, write `/etc/udev/hwdb.d/61-kiln-battery.hwdb` matching `battery:<kernel name>:<model_name>:<dmi modalias>` with `CHARGE_LIMIT=<start>,<stop>` (first candidate; rewritten with the accepted pair if different, then re-verified). It sorts after `60-upower-battery.hwdb`. When the file changed: `systemd-hwdb update`, `udevadm trigger -s power_supply`, `systemctl try-restart upower`. Written whenever thresholds are the method, GNOME or not.
2. With thresholds and GNOME, the summary says: "Battery limit is managed by kiln. Leave GNOME's *Preserve Battery Health* switch alone; turning it **off** resets the limit to 100% until kiln re-applies it (within 15 minutes)."
3. `kiln battery status` prints batteries, interfaces, current and accepted values, the method, and UPower's `ChargeThresholdEnabled` (D-Bus).

Expected (fact R, probed at run time): ThinkPad E14 Gen 5 → method 1 at 75/80; HP Envy x360 13 → method 4.

**Check:** the recorded method's values read back as desired; `kiln-battery.service`, `.timer` and the udev rule installed, service and timer enabled; for method 3, the watch unit present and active. "Not done" when a better method has appeared (thresholds now available over a vendor mode or the watcher, or any interface when none was recorded) or `target_stop`/`start_gap` changed.

---

## 14. firewall

Removed (D50).

---

## 15. ssh

**ID** `ssh` · security · **risky** · options: `port` (22), `password_logins` (`on`|`off`, D45/D51), `root_login` (on), `root_password` (on), `max_auth_tries` (4, 1–20)

**Applies:** always. Installs and enables the SSH server if missing (D45).

**Facts used** (notes): drop-ins are included early and the first value wins; `Port` lines add up across files; `ssh.service` ships enabled, `ssh.socket` disabled.

**Actions:**
1. Install `openssh-server` if missing; `systemctl enable --now ssh.service` if not enabled (unless `ssh.socket` was enabled by hand).
2. **Root password** (`root_password`): if root's `/etc/shadow` field is locked or empty (`!`, `*`, blank) and the user has a password, copy the user's hash with `chpasswd -e` (`root:<hash>` on stdin, never argv or logs). Otherwise root is left as is.
3. **Keys:** none added (D50). Valid keys in `<home>/.ssh/authorized_keys` are counted for the warning below.
4. **Drop-in** `/etc/ssh/sshd_config.d/10-kiln.conf`: `PermitRootLogin yes|no` (`root_login`), `MaxAuthTries <n>`, `X11Forwarding no`, `PasswordAuthentication yes|no` (`password_logins`).
   - `off` with no key → review warns nobody could log in over SSH. `on` → review and summary warn on every run that password logins for the user and root are open wherever SSH is reachable (D53).
5. **Port:** `Port <n>` in the drop-in when not 22. With 22 and another file setting `Port`, review warns that sshd won't listen on 22 and the check will fail. An uncommented `Port` elsewhere is warned about and left alone. If another program holds the chosen port (`ss -p`), apply is refused and nothing changes. Socket mode: `/etc/systemd/system/ssh.socket.d/60-kiln.conf` with `ListenStream=` + `ListenStream=<n>` (removed for port 22), `daemon-reload`, `restart ssh.socket`.
6. **Verify, then revert:**
   1. Record `pending`; `sshd -t`.
   2. `systemctl reload-or-restart ssh` (D53).
   3. Poll up to 15 s: `ssh` active (socket mode: `ssh.socket`); port listening (`ss -ltn`); `sshd -T -C user=<target>,host=<hostname>,addr=<lan-ip>` shows the expected values (`-C` makes `Match` blocks count; the port part is skipped in socket mode).
   4. On failure restore the previous files, `reload-or-restart ssh` (socket: `daemon-reload` + `restart ssh.socket`), and fail. An unfinished earlier change keeps its first snapshot (D53).

**Check:** `openssh-server` installed, `ssh.service` enabled, root has a password (when the option is on and the user has one), drop-in matches, `sshd -T -C …` values match, port listening.

**Gotchas:** low `MaxAuthTries` can lock out agents offering many keys (summary suggests `IdentitiesOnly yes`). Router port forwarding is manual (noted).

---

## 16. fail2ban

**ID** `fail2ban` · security · depends: ssh (hard) · options: `bantime` (`1h`), `bantime_increment` (on), `maxretry` (5)

**Applies:** an SSH server is installed or the ssh step will install one; else "not applicable: no SSH server installed, and the SSH step is off". Because the dependency is hard, unticking the ssh step skips fail2ban even when a server exists (D54).

**Facts used** (notes): Debian's `jail.d/defaults-debian.conf` sets `banaction_allports = nftables[type=allports]`; `jail.d/*.local` is read last; the nftables action uses its own table, so no firewall is needed.

**Actions:** install `fail2ban python3-systemd`, write `/etc/fail2ban/jail.d/50-kiln.local` (managed header; values from the options):
```
[DEFAULT]
ignoreip = 127.0.0.1/8 ::1 <LAN subnets detected at run time>
bantime = 1h
bantime.increment = true
maxretry = 5

[sshd]
enabled = true
backend = systemd
port = <ssh ports, comma-separated>
banaction = %(banaction_allports)s
journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd + _COMM=sshd-session
```
- `banaction_allports` bans on every port (P9).
- The jail `journalmatch` replaces the filter's, so it adds `_COMM=sshd-session` (covers `ssh.socket` units).

Restart fail2ban, then `fail2ban-client ping` and `fail2ban-client status sshd`.

**Check:** packages installed, file matches, service active, `ping` answers `pong`, the `sshd` jail runs.

**Gotchas:** trixie has no `/var/log/auth.log` by default, so the systemd backend is essential.

---

## 17. docker

**ID** `docker` · apps · depends: apt-sources · options: `log_max_size` (`10m`), `log_max_file` (3), `hello_world` (on)

**Actions** (Docker's Debian install docs):
1. Remove if present: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`.
2. `install -m 0755 -d /etc/apt/keyrings`; download `https://download.docker.com/linux/debian/gpg` to `/etc/apt/keyrings/docker.asc` (0644) when missing or not an armored key.
3. `/etc/apt/sources.list.d/docker.sources`: `Types: deb`, `URIs: https://download.docker.com/linux/debian`, `Suites: trixie`, `Components: stable`, `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/docker.asc`. `AptUpdate` when the key or source changed or `docker-ce` is missing.
4. Install `docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`.
5. `/etc/docker/daemon.json` (no header): merge `{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}` into existing keys (strings; new containers only). Parse in Go; run `dockerd --validate --config-file /etc/docker/daemon.json` if `dockerd --help` lists `--validate`. Restart docker only when the file changed (it restarts containers); not active afterwards → restore, restart, fail.
6. `UserAddGroup(user, docker)`. Wait for `docker info` (up to 60 s), then `docker run --rm hello-world` as root (option).

**Check:** packages installed, daemon.json has the keys, user in `docker`, docker active.

**Notes:** "log out and back in for docker group membership"; "the docker group is root-equivalent"; "existing containers keep their old log settings".

---

## 18. cockpit

**ID** `cockpit` · apps · depends: apt-sources · option: `backports` (on)

**Actions:**
- Install `cockpit` and `cockpit-networkmanager` (named explicitly, D53). With apt-sources backports and this option on: `apt-get install -t trixie-backports cockpit` (371; `(>= source:Version)` pulls matching binaries). Otherwise main (337); an installed backports version is kept, never downgraded (note).
- Recommends kept (P17): `cockpit-storaged`, `cockpit-networkmanager`, `cockpit-packagekit`, which pull `network-manager`, `packagekit`, `udisks2` (listed on review). NetworkManager leaves ifupdown interfaces alone (D52). `cockpit-machines` and `cockpit-podman` are never installed.
- `cockpit.socket` (port 9090) is enabled by the package; kiln only verifies.

**Check:** `cockpit` from the expected suite (or a kept backports version), `cockpit-networkmanager` installed, `cockpit.socket` enabled and active.

**Notes:** `https://<lan-ip>:9090`; listens on every network (no firewall, D50); log in with the user's password (the postinst puts `root` in `/etc/cockpit/disallowed-users`).

---

## 19. remote-desktop

Removed (D50).

---

## 20. autoupdates

**ID** `autoupdates` · security · depends: apt-sources · runs after every repo-adding step · options: `scope` (`all`|`security`), `schedule` (`12h`, only choice), `automatic_reboot` (off)

**Policy** (D40): every installed package from every configured repository, refreshed and upgraded every 12 hours.

**Facts used** (notes): both apt-daily services have `ConditionACPower=true`; `APT::Periodic` accepts `always` for OnCalendar-driven schedules; `Origins-Pattern` uses `fnmatch` and accumulates across files (`#clear` resets); only installed packages are upgraded; `20auto-upgrades` is ucf-managed.

**Actions:**
1. Preseed `unattended-upgrades unattended-upgrades/enable_auto_updates boolean true`, install `unattended-upgrades apt-listchanges needrestart`. `20auto-upgrades` is never rewritten.
2. `/etc/apt/apt.conf.d/52kiln-unattended-upgrades` (sorts after 20/50, so it wins):
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
   - `scope=security`: the pattern is `"origin=Debian,codename=${distro_codename}-security,label=Debian-Security"`.
   - `automatic_reboot` on: `"true"`, and the reboot note changes.
3. Timer drop-ins, then `systemctl daemon-reload` only (timers stay masked until the end-of-run restore, D41):
   - `/etc/systemd/system/apt-daily.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=`, `OnCalendar=*-*-* 06,18:00`, `RandomizedDelaySec=15m`.
   - `/etc/systemd/system/apt-daily-upgrade.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=`, `OnCalendar=*-*-* 06,18:30`, `RandomizedDelaySec=15m`.
4. `/etc/needrestart/conf.d/50-kiln.conf`: `$nrconf{restart} = 'a';` (the default `'i'` only lists when non-interactive).

**Check:** `apt-config dump` shows both Periodic values `always` and exactly the expected `Origins-Pattern`; the drop-ins, apt.conf file and needrestart file match; each timer's `systemctl show -p ActiveState -p TimersCalendar` is active with its slot. The timer part is skipped while kiln's runtime mask is in place (always, during a run); the restore doesn't start timers that were inactive before the run.

**Summary notes:**
- "Updates run at about 06:00 and 18:00, refresh then upgrade 30 minutes later, and skip while on battery. Logs are in `/var/log/unattended-upgrades/`."
- "Kernel and NVIDIA driver updates need a reboot to take effect. kiln never reboots automatically; `/run/reboot-required` shows when one is pending." (changed when `automatic_reboot` is on)
- `scope=all` only: "A Docker update restarts the Docker daemon. Containers come back only if they have a restart policy." and "A new NVIDIA driver branch can arrive unattended, because the branch isn't pinned (D39)."

---

## 21. node

**ID** `node` · dev · depends: devtools · runs as the user · options: `nvm_version` (`v0.41.0`), `node_version` (`26`)

**Actions:**
1. `mkdir -p <home>/.nvm <home>/.cache/kiln` as the user.
2. Pinned `sys.Fetch` of `https://raw.githubusercontent.com/nvm-sh/nvm/<nvm_version>/install.sh` into `~/.cache/kiln` (0600), run with `bash <file>` as the user with `PROFILE=/dev/null NVM_DIR=<home>/.nvm METHOD=script` (no shell-file edits; `NVM_DIR` must exist; deterministic download method). Re-run when `nvm --version` differs from the option.
3. As the user, one `bash -s` **without errexit**, fixed script on stdin, the version passed as an argument (D53): `. "$NVM_DIR/nvm.sh"; nvm install "$2"; nvm alias default "$2"; node -v && npm -v`.
4. `<home>/.bashrc.d/50-kiln-nvm.sh`: `export NVM_DIR="$HOME/.nvm"; [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"; [ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"`, plus the `~/.bashrc.d` loader (step 22).

**Check:** `<home>/.nvm/nvm.sh` exists, `nvm --version` equals the option, `nvm version default` and `node -v` match `node_version` (same no-errexit form), drop-in and loader present.

**Gotchas:** never source nvm under `set -e`. The user-run rule's clean environment keeps `XDG_CONFIG_HOME` from moving nvm's dir.

---

## 22. python

**ID** `python` · dev · depends: devtools · user parts as the user · options: `venv_path` (`~/.venvs/automation`), `uv_source` (`pipx`|`installer`), `playwright` (on), `uv_tools` (`ruff`, `pre-commit`, `httpie`)

**Actions:**
1. apt: `python3 python3-venv python3-dev pipx libxml2-dev libxslt1-dev libffi-dev`.
2. uv (not in trixie): as the user, `pipx install uv` → `<home>/.local/bin`, skipped when `~/.local/bin/uv` exists. `installer`: pinned `sys.Fetch` of `https://astral.sh/uv/0.13.7/install.sh`, run with `bash <file>` as the user, `UV_NO_MODIFY_PATH=1`.
3. Venv: `uv venv --python /usr/bin/python3 <venv>`, skipped when `<venv>/pyvenv.cfg` exists. Then `uv pip install --python <venv> requests "httpx[http2]" aiohttp tenacity beautifulsoup4 lxml selectolax parsel scrapy playwright selenium pandas pyyaml python-dotenv rich typer ipython`.
4. Playwright (option), after `AptUpdate`, as the user: `<venv>/bin/playwright install-deps --dry-run chromium`; on exit 1, `AptInstall` the indented lines after `Missing system dependencies (N):` (Playwright never runs apt, D25). Then `<venv>/bin/playwright install chromium`.
5. One `uv tool install <tool>` per tool (binaries in `<home>/.local/bin`).
6. Shell (shared `internal/userenv`, also used by node and go):
   - Missing `<home>/.bashrc` → copy `/etc/skel/.bashrc`.
   - Append once (marker line):
     ```bash
     # kiln: load ~/.bashrc.d
     if [ -d "$HOME/.bashrc.d" ]; then for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f; fi
     ```
   - `<home>/.bashrc.d/50-kiln-python.sh`:
     ```bash
     case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:$PATH" ;; esac
     alias pyauto='. "$HOME/.venvs/automation/bin/activate"'
     ```

**Check:** `pyvenv.cfg` exists, `uv pip list --python <venv>` has every package, (Playwright) `install-deps --dry-run chromium` exits 0 and a chromium dir exists under `<home>/.cache/ms-playwright/`, `uv tool list` shows the tools, marker and drop-ins present.

---

## 23. go

**ID** `go` · dev · depends: devtools · options: `version` (`latest` or a release), `debian_fallback` (on), `tools` (subset of the four below), `update_tools` (off; per run, never saved, D53)

**Actions:**
1. Resolve: `latest` → `https://go.dev/dl/?mode=json`, first `stable: true` release; a specific version → `?mode=json&include=all`. Take the file with `kind=archive`, `os=linux`, `arch=amd64` (filename and sha256 from the JSON).
2. Unless `/usr/local/go/VERSION` starts with the resolved version: download to the run's download dir, verify sha256, extract into `/usr/local/go.kiln-new`, rename `/usr/local/go` → `go.kiln-old` and `go.kiln-new` → `go`, remove `go.kiln-old`. Leftovers: if `/usr/local/go` is missing and `go.kiln-old` works, it's renamed back; other leftovers are removed and make Check re-plan (D53).
3. `/etc/profile.d/kiln-go.sh` and `<home>/.bashrc.d/50-kiln-go.sh`: `export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin` (`/usr/local/go/bin` first, over `/usr/bin/go`).
4. As the user, one `/usr/local/go/bin/go install <path>@latest` per tool (one module per call): `golang.org/x/tools/gopls`, `github.com/go-delve/delve/cmd/dlv`, `honnef.co/go/tools/cmd/staticcheck`, `golang.org/x/vuln/cmd/govulncheck`. Skipped if `<home>/go/bin/<tool>` exists, unless `update_tools`.
5. Fallback (`debian_fallback`): go.dev unreachable → install `golang-go` from trixie-backports (note "1.27"), or main (note "1.24") without backports. `go install` runs with `GOTOOLCHAIN=local`; a tool failing with "requires go >=" is skipped with a note. Fallback off and go.dev unreachable → the step fails.

**Check:** `/usr/local/go/bin/go version` matches the resolved version (go.dev unreachable: any working `/usr/local/go`; fallback: `golang-go` installed), and the chosen tools exist in `<home>/go/bin`.

---

## 24. firmware-updates

**ID** `firmware-updates` · hardware · depends: apt-sources · option: `updates` (on; off = install only)

**Applies:** always (UEFI capsules need an ESRT and an ESP).

**Actions** (exit codes from fwupd's source). **Never reboots:** with `--no-reboot-check` and non-TTY stdin, fwupdmgr returns before its "Restart now?" prompt, the only reboot path.
1. Install `fwupd fwupd-amd64-signed udisks2` (signed EFI binary for Secure Boot; LVFS enabled by default).
2. `fwupdmgr refresh --force`: 0 OK; 101 = offline → done with a note; other codes fail the step.
3. `fwupdmgr get-updates --json`: 0 = updates; 2 = nothing → done; other codes fail.
4. `fwupdmgr update -y --no-reboot-check --no-device-prompt`: 0 or 2 OK; anything else is a note, not a failure.
5. `fwupdmgr check-reboot-needed --json` exits 0 → reboot reason "firmware update staged".

**Check:** fwupd installed, and one of: `updates` off; this run's outcome was ok, nothing, offline or noted; a firmware reboot is pending. So LVFS is checked on every run (D51) unless a reboot is already pending. `get-updates` isn't the check: an update that can never apply would keep the step "not done".

---

## 25. summary

**ID** `summary` · always runs · the runner's final phase, not a registered step (D42)

**End-of-run tasks** (before the summary, every run, D41): one needrestart pass (adds reboot reasons such as "a newer kernel is installed" or "new CPU microcode", and a note listing services that restart at reboot), then restore automatic updates and PackageKit. Then the inhibitor is released, `/var/log/kiln-summary.txt` (0644, secrets redacted) and `/etc/kiln/state.json` (0600: choices, run record, battery result, power manager; never secrets) are written, and `sync` runs. The full log is `/var/log/kiln.log` (0600).

**Screen:** per step done/unchanged/failed/skipped and why; the notes; reboot needed yes/no with reasons; any generated secret (the MOK one-time password) once, in a "Write this down now" panel. kiln never reboots on its own. "Reboot now" releases the single-instance lock, then runs `sync` and `systemctl reboot` after the screen exits; when a secret was shown, the reboot dialog says to write it down first.
