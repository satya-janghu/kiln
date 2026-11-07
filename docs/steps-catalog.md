# Steps catalog

**This file is the source of truth for behavior.** Code follows it. When behavior changes, update this file in the same change (rule 11).

Steps run in the order listed. Every step and sub-option defaults to **on** in the wizard (options are in [options-reference.md](options-reference.md)). "(verify)" marks a fact not yet confirmed against a current primary source. Code may not rely on it until [debian13-notes.md](debian13-notes.md) marks it **V** (see [review-process.md](review-process.md)).

**The Debian installer owns the desktop (GNOME or none), the SSH server and blends.** kiln never installs or removes them. It detects them in `preflight`, and steps that depend on them are "not applicable" when they're absent. Verified against tasksel 3.81 and blendsel 0.10:
- **"GNOME detected"** means `gnome-shell` and `gdm3` are installed. `task-gnome-desktop` → `gnome-core` pulls in both.
- **"SSH server detected"** means `openssh-server` is installed. `task-ssh-server` depends on it. Exception (D45): the ssh step installs and enables the SSH server when it's missing, and fail2ban treats SSH as present whenever that step is ticked.
- **"Standard system utilities"** is not a package: it's every Priority: standard package.
- **Blends** are the `task-*-blend` packages (astro, med, junior-desktop, science, freedombox, hamradio). Debian Edu isn't offered by the installer.

kiln must not depend on any Priority: standard package being present (`pciutils`, `wget`, `ca-certificates`, …). Without the "standard" task those packages are missing, so `preflight` installs what kiln itself needs.

One step isn't in the original brief's list: `kernel` (5b), split out so it can be toggled on its own (D13). Removed: `desktop` (D29), `network-migration`, `firewall` and `remote-desktop` (D50, D52).

**Risky** marks a step that uses the verify-then-revert pattern from [architecture.md](architecture.md#risky-changes-verify-then-revert-on-failure).

Each entry has: **ID · Category · Depends on**, then **Purpose**, **Applies**, **Options**, **Actions**, **Check (already done when)**, **Gotchas**.

All apt operations use the `apt` helper: non-interactive, confold, lock wait (see architecture). "Install X" means `AptInstall`, which is a no-op when X is already installed.

---

## 0. preflight

**ID** `preflight` · base · always runs, can't be skipped

**Purpose:** make sure it's safe and possible to continue, and gather Facts.

**Actions (read-only, except the inhibitor and kiln's own prerequisites):**
- **Root check.** If not root, stop with "Run with sudo, or as root via `su -`". If started via plain `su`, PATH lacks `/usr/sbin`: Debian's `login.defs` has no `ALWAYS_SET_PATH`, so plain `su` keeps the caller's PATH. In that case prepend `/usr/local/sbin:/usr/sbin:/sbin` to kiln's own PATH and note it.
- **OS check.** Parse `/etc/os-release` (a symlink to `/usr/lib/os-release`) per os-release(5), stripping quotes. Require `ID=debian`, `VERSION_ID=13` and `dpkg --print-architecture` = `amd64`. Anything else stops the run.
- **Virtualization.** If `systemd-detect-virt --container` exits 0, stop ("kiln sets up real machines, not containers"). If `systemd-detect-virt --vm` exits 0, show a warning only.
- **Single instance.** Take the exclusive lock `/run/kiln/kiln.lock`. If another kiln holds it: "kiln is already running (pid N)", and offer the log read-only.
- **Repair, before the internet check** (architecture: "Interrupted runs and the next start"):
  1. the `pending` revert prompt;
  2. a run record left unfinished → "The last run stopped during <step>";
  3. interrupted dpkg (`dpkg --audit` output, or `/var/lib/dpkg/updates/` entries) → `dpkg --configure -a` + `apt-get -f install -y`;
  4. stale runtime masks are cleaned up. (A leftover `/usr/local/go.kiln-new`/`.kiln-old` from an interrupted Go swap is handled by the go step, step 23.)
- **Power.** On battery, warn. Below 30% and not on AC, ask for confirmation before continuing.
- **Internet.** HTTPS HEAD to `deb.debian.org` and `github.com` (5 s timeout each), plus a DNS check. Report the failure precisely:
  - DNS failure → "check the network / DNS";
  - TLS verification failure → "install ca-certificates", not "no network";
  - timeout → "check the cable or Wi-Fi".
- **kiln's own prerequisites**. `ca-certificates` and `pciutils` are Priority standard and may be missing, so install them (through the apt helper) if absent.
- **Installer choices:**
  - GNOME detected (`gnome-shell` + `gdm3` installed);
  - SSH server detected (`openssh-server` installed, `ssh.service` enabled);
  - installed `task-*` packages and any `task-*-blend` package, reported for information only;
  - whether the "standard" task is installed (`tasksel --list-tasks` marks it with `i`; tasksel may itself be absent);
  - the default systemd target.
- **Hardware facts:**
  - DMI vendor, product, family and chassis type;
  - CPU vendor, model and cores; RAM;
  - GPUs, read from `/sys/bus/pci/devices/*/{class,vendor}` (classes 0x0300/0x0302/0x0380; vendors 0x1002/0x10de/0x8086), so this doesn't depend on lspci;
  - the Wi-Fi chip (PCI/USB IDs and driver);
  - Secure Boot: the efivar `SecureBoot-8be4df61-93ca-11d2-aa0d-00e098032b8c`, byte 5 = 1. `mokutil --sb-state` is optional, because mokutil exists only when shim-signed is installed. No efivars means BIOS boot and "Secure Boot: n/a".
  - batteries (`/sys/class/power_supply/*` with type=Battery and scope≠Device), and the AC adapter;
  - `platform_profile` and its choices, amd-pstate status, and whether thinkpad_acpi or hp-wmi is present.
- **Network facts:** NICs (physical vs virtual), IPv4/IPv6 addresses, LAN subnets, the default route, and whether each NIC is managed by ifupdown, NetworkManager, systemd-networkd or nothing.
- **Session:** the target user, and whether kiln runs inside an SSH session (`SSH_CONNECTION`), which only produces a warning.
- **Inhibitor.** Start `systemd-inhibit --what=sleep:idle:handle-lid-switch:handle-power-key:shutdown --mode=block --who=kiln --why="installing packages; rebooting now could break the system" sleep infinity` for the whole run.
  - `handle-lid-switch` is needed because logind's `LidSwitchIgnoreInhibited=yes` default would otherwise let a lid close suspend the machine.
  - `shutdown` in block mode refuses every logind reboot or poweroff, root's included. Escapes: `systemctl reboot -i`, `--force`, or holding the power button.
- **Quiet other package managers** for the whole run, just before the first apt action: stop and wait for, then runtime-mask, `apt-daily*.timer`/`.service` and `packagekit.service`. Undone in the end-of-run restore (architecture: "Nothing else changes packages during a run").
- **Own scope.** When applying starts, kiln re-executes itself in `systemd-run --scope` (system slice), so closing the terminal or ending the desktop session can't kill it mid-dpkg.

**Check:** n/a (always runs). **Failure** stops the run (the only step that does).

---

## 1. sudo

**ID** `sudo` · base · depends: preflight

**Purpose:** give the target user passwordless sudo.

**Options:** passwordless sudo (on).

**Actions:**
1. Install `sudo` if missing. Verified in user-setup 1.107: debian-installer installs sudo and runs `adduser <user> sudo` only when the root password is left empty (`passwd/root-login=false`). With a root password set, the user isn't in the group, and sudo is usually absent.
2. `adduser <user> sudo` (adduser 3.152: two arguments add an existing user to an existing group; a no-op if already a member).
3. File name: `/etc/sudoers.d/90-<sanitized>-nopasswd`. sudoers(5) (sudo 1.9.16) skips files whose names end in `~` or contain a `.`. Sanitize to `[a-z0-9_-]`, replacing anything else with `_`.
4. Content: `<user> ALL=(ALL:ALL) NOPASSWD: ALL`. With "passwordless sudo" **off**, kiln writes `/etc/sudoers.d/90-<sanitized>` with `<user> ALL=(ALL:ALL) ALL` instead (D45). The rule names the user directly, so sudo works immediately, without the re-login a new sudo-group membership needs. An existing `-nopasswd` file is left in place (D44). Debian's `/etc/sudoers` ends with `@includedir /etc/sudoers.d`, after `%sudo ALL=(ALL:ALL) ALL`, so this later rule wins.
   - Write it to a temp file and validate with `visudo -cf <tmp>`.
   - Install it 0440 root:root through the managed writer.
   - Then run `visudo -c` to check the **whole** policy, including ownership and modes. visudo(8) says checking a single include file isn't sufficient. If that fails, delete the file and fail the step.
   - Never leave an invalid file in place.

**Check:** sudo is installed, the user is in group `sudo`, the file exists with identical content and mode 0440, and `visudo -c` passes.

**Gotchas:** group membership takes effect at the next login, so the summary notes "log out and back in". Passwordless sudo plus password SSH on the internet is exactly the risk the ssh step warns about.

---

## 2. apt-sources

**ID** `apt-sources` · base · depends: preflight

**Purpose:** deb822 sources with the right components, no cdrom entries, and backports.

**Options:** trixie-backports (on).

**What the installer leaves** (apt-setup 0.198): a **one-line `/etc/apt/sources.list`**, not deb822. It has `deb` **and active `deb-src`** lines for `http://deb.debian.org/debian` (trixie, trixie-updates) and `http://security.debian.org/debian-security trixie-security`, with components `main non-free-firmware`. Since 0.195, cdrom lines are commented out unless the medium was a real CD.

**Actions** (apt 3.0.3):
1. Comment out any active `deb cdrom:` line, and set `Enabled: no` on any `.sources` stanza with a `cdrom:` URI. This comes first: bracketed cdrom labels contain spaces, which would break deb822 `URIs:` during conversion.
2. `sources.list`: if it has active lines, comment every one out, under the header `# Debian entries now live in sources.list.d/debian.sources (managed by kiln)`. The original is backed up first. kiln writes the Debian entries itself instead of converting them, which also drops d-i's `deb-src` lines (not needed).
3. Other active `sources.list.d/*.list` files (third-party): `apt -y modernize-sources`. It converts each `X.list` to `X.sources` and renames the original to `X.list.bak`; main-file entries would be appended to `<origin>.sources`. After it runs, any output file with an empty `Signed-By:` fails the step, because modernize leaves Signed-By empty with a warning when the Release file isn't cached. Using `-y` (`APT::Get::Assume-Yes`) avoids depending on stdin answers.
4. Write `/etc/apt/sources.list.d/debian.sources` (managed, so it replaces anything modernize appended for `Origin: Debian`) with these stanzas:
   - `Types: deb`, `URIs: http://deb.debian.org/debian`, `Suites: trixie trixie-updates`, `Components: main contrib non-free non-free-firmware`, `Signed-By: /usr/share/keyrings/debian-archive-keyring.pgp` (the `.pgp` path from the release notes, §4.3).
   - Security: `URIs: http://security.debian.org/debian-security`, `Suites: trixie-security`, with the same components and keyring.
   - Backports (if enabled): `URIs: http://deb.debian.org/debian`, `Suites: trixie-backports`, with the same components. Backports' Release has `NotAutomatic: yes` / `ButAutomaticUpgrades: yes`, so nothing comes from it unless a step asks with `-t trixie-backports`.

   The `mirrors` step later changes only the first and third `URIs:` lines.
5. Set `Enabled: no` on any other `.sources` stanza that targets the same suites. A stanza counts as the same only when its URI is exactly the Debian archive (`http(s)://deb.debian.org/debian[/]`, `security.debian.org/debian-security`) or kiln's mirror list; `debian-debug` and others are left alone. Duplicates only cause a warning ("Target … is configured multiple times"), but kiln's `debian.sources` is the one owner of Debian entries.
6. `AptUpdate`.

All of this runs under a `revert` snapshot of `sources.list` and `sources.list.d/`. If anything fails, including `apt update`, the snapshot is restored (D44).

**Check:** `debian.sources` parses to the wanted stanzas (URIs may be the `mirror+file:` form), there are no active cdrom entries, no other stanza targets the Debian suites, and `sources.list` has no active lines.

**Gotchas:**
- Backups live in `/var/lib/kiln/backup/…`, never next to the file. A `*.kiln-bak` inside `sources.list.d/` makes apt print "Ignoring file … invalid filename extension" on every run.
- If `apt update` fails after this step, restore the backups and fail with the apt error text.

---

## 3. mirrors

**ID** `mirrors` · base · depends: apt-sources

**Purpose:** fastest download **throughput** (not latency), with automatic failover.

**Options:** see options-reference. Subcommands: `kiln mirrors bench [--apply]`, `kiln mirrors reset`.

**Actions:**
1. **Candidates.** Parse the Debian mirror masterlist at `https://mirror-master.debian.org/status/Mirrors.masterlist` (about 370 RFC822 stanzas with `Site:`, `Country:` (e.g. `DE Germany`), `Archive-architecture:`, `Archive-http:` (a path such as `/debian/`)). Select:
   - mirrors whose `Archive-architecture` includes `amd64` and that have `Archive-http`. Their URL is `http://<Site><Archive-http>`; the masterlist advertises no HTTPS.
   - mirrors in the configured country;
   - `worldwide_sample` random others (a farther mirror can be faster; only measured download speed counts);
   - `deb.debian.org`.

   `country: auto` maps the configured timezone (`system.timezone`, else `timedatectl show -p Timezone`) to a country code via the zone1970.tab table compiled into kiln (Asia/Kolkata → IN). There's no neighbouring-countries option (D52). kiln never calls a geo-IP service (D21). An unknown timezone means worldwide sample + deb.debian.org only.
2. **Freshness.** For each candidate, fetch `dists/trixie/InRelease` and `dists/trixie-updates/InRelease` (and `trixie-backports` if enabled), and parse `Date:` and `Valid-Until:`. Drop a mirror if:
   - its trixie `Date:` differs from deb.debian.org's (it's missing a point release; trixie only changes at point releases);
   - its trixie-updates or trixie-backports `Date:` is more than 12 h older than deb.debian.org's (those change on every push);
   - or any `Valid-Until` has passed.
3. **Throughput,** tested **sequentially** so tests don't compete for bandwidth.
   - The test file is taken from the signed trixie InRelease: `main/Contents-all.gz`, about 38 MB. `main/Contents-amd64.gz` is only ~12.7 MB, too small for a 40 MB range.
   - `GET` with `Range: bytes=0-<min(test_bytes, size)-1>`, stopping at `test_timeout`. Reaching the timeout is normal.
   - Score = bytes received after the first 0.5 s ÷ the time after the first 0.5 s, which skips TCP slow start.
   - Score 0 only on an HTTP error, a status other than 206/200, or less than 1 MB received.
4. **Mirrorlist.** Write `/etc/apt/mirrors/debian.list`. Format verified in apt-transport-mirror(1) and `methods/mirror.cc` (apt 3.0.3): one URI per line, a **TAB**, then `key:value` tags; `#` comments allowed.
   - The top `mirrors.top` mirrors all get the **same** `priority:1`. apt sorts by priority and picks **randomly per file among equal priorities**, with failover on error. That's what spreads downloads across hosts. Distinct priorities 1..N would send everything to mirror #1.
   - `http://deb.debian.org/debian<TAB>priority:99` goes last, as the fallback.
5. **Sources.** Change the `debian.sources` URIs for main and backports to `mirror+file:/etc/apt/mirrors/debian.list`. `mirror+scheme` URIs are documented in sources.list(5) and work alongside `Signed-By`. Security stays as it is, because `security.debian.org` isn't mirrored.
6. **Retries.** Drop-in `/etc/apt/apt.conf.d/80kiln-acquire` with `Acquire::Retries "5";` (apt.conf(5); the default is 3). `Acquire::Queue-Mode` (default `host`) and `Acquire::http::Pipeline-Depth` (apt-transport-http(1), default 10) are left alone.
7. **Verify.** `apt update`. On failure, restore the previous `debian.sources` **and the previous mirror list** (or remove a new one), and fail with "Mirrors couldn't be used, kept the previous sources".

**Check:** the mirrorlist exists, is less than 30 days old, every listed mirror still passes the freshness rule, and the sources point at it. Re-benchmarking isn't part of Check; use `kiln mirrors bench --apply`.

**Gotchas:** listing several equal-priority mirrors gives parallel bandwidth across hosts, and deb.debian.org (Fastly) often wins the bench on its own. Rejected alternatives:
- netselect-apt: ranks by latency, not throughput;
- apt-fast/aria2: extra dependencies, and it wraps apt.

---

## 4. upgrade

**ID** `upgrade` · base · depends: apt-sources

**Actions:** `AptUpdate`, then `apt-get full-upgrade -y` (with the apt helper options). Before and after, simulate with `LC_ALL=C apt-get -s full-upgrade`. `apt-get` keeps the classic output (`Inst`/`Remv` lines are untranslated, and the summary line is translated, so it's never parsed). A reboot reason is added if any of these is true:
- the simulation listed `Inst linux-image-`;
- the running kernel's build (`uname -v`, "Debian <ver>") differs from `dpkg-query -W -f '${Version}' linux-image-$(uname -r)`, which catches same-ABI rebuilds;
- `/run/reboot-required` exists. In Debian, only unattended-upgrades' kernel postinst hook writes it, and that package isn't installed until step 20, so this check is only a fallback.

**Check:** `LC_ALL=C apt-get -s full-upgrade` prints no `Inst`/`Remv` lines (after an update in this run).

---

## 5. firmware-packages

**ID** `firmware-packages` · hardware · depends: apt-sources

**Applies:** always (containers are refused in preflight).

**Actions:** install (all in **non-free-firmware**; firmware-nonfree 20250410 in trixie):
- CPU microcode: `amd64-microcode` if the CPU vendor is AMD, `intel-microcode` if Intel.
- `firmware-misc-nonfree`, with `--no-install-recommends`. Its Recommends pull in GPU and other firmware the machine may not have. It does **not** contain Intel iGPU, NVIDIA or MediaTek firmware; trixie splits those into separate packages.
- By detection:
  - `firmware-amd-graphics` for an AMD GPU (both target laptops);
  - `firmware-intel-graphics` and `firmware-intel-misc` for Intel graphics;
  - `firmware-nvidia-graphics` for an NVIDIA GPU (nouveau firmware; the proprietary driver's `firmware-nvidia-gsp` comes in via `nvidia-kernel-dkms` in multimedia);
  - `firmware-realtek` for any Realtek NIC, Wi-Fi or Bluetooth device.
- Wi-Fi, by driver: Intel `iwlwifi` → `firmware-iwlwifi`; Qualcomm Atheros `ath*` → `firmware-atheros`; MediaTek `mt7*` → `firmware-mediatek`; Broadcom `brcmfmac` → `firmware-brcm80211`. Realtek drivers match as `r8169`, `r8152`, `rtw_*` (rtw88 registers as `rtw_<chip>`), `rtw89*` and `rtl8*`.
- Wi-Fi cards on PCI (class `0280`) are also matched by vendor, because a card whose firmware is missing never gets a network interface: Intel `8086` → `firmware-iwlwifi`, MediaTek `14c3` → `firmware-mediatek`, Realtek `10ec` → `firmware-realtek`, Qualcomm `168c`/`17cb` → `firmware-atheros`, Broadcom `14e4` → `firmware-brcm80211` (pci.ids).
- If `update-initramfs -u -k all` fails, the error says to free space in `/boot` and run it again.
- After installing, run `update-initramfs -u -k all`. The firmware triggers rebuild only the newest image's initramfs, so the stable fallback kernel would otherwise boot without the new firmware.
- Then report anything still missing using `isenkram-autoinstall-firmware -l` (from `isenkram-cli`, **list-only**). kiln never runs isenkram in install mode, because it writes one-line `.list` sources that clash with kiln's deb822 and mirror setup. Missing firmware becomes a summary note.

Reboot note if microcode changed.

**Check:** all the selected packages are installed.

---

## 5b. kernel

**ID** `kernel` · hardware · depends: apt-sources, firmware-packages · options: backports kernel (**off**), backports firmware

**Applies:** only when you select "backports kernel" for this machine (D38).
- **By default,** every machine runs Debian's stock trixie kernel: `linux-image-amd64` from trixie, 6.12 LTS. The Debian installer installs it, and the `upgrade` step and unattended-upgrades keep it on the latest version like any other package.
- **Not selected:** "skipped (by you)".
- **Backports disabled:** "not applicable: backports disabled".

**Kernel policy** (D38): kiln never pins, holds or marks any kernel manual. Kernels follow the `linux-image-amd64` metapackage from whichever suite is in use, and update to the latest version through normal upgrades. Old kernels are cleaned up by Debian's normal autoremove protection, which keeps the running kernel and the newest ones.

**Package facts** (2026-11-07):
- `linux-image-amd64` is 6.12.x in trixie/trixie-security and 7.2.x in trixie-backports. Both are built from `linux-signed-amd64`, so both are signed for Secure Boot.
- The backports image depends on `linux-base (>= 4.17~)`, which only exists in backports, so `-t trixie-backports` is required.

**Actions (only when selected):**
1. `apt-get install -t trixie-backports linux-image-amd64`, plus `linux-headers-amd64` from backports when an NVIDIA GPU is present (DKMS).
2. Backports firmware (option, on): reinstall the `firmware-*` packages chosen in step 5 with `-t trixie-backports`, because backports carries newer firmware (20260916). Then run `update-initramfs -u -k all`.
3. Reboot note: "new kernel <ver>".

After that, the backports metapackage brings each newer backports kernel. The `autoupdates` step upgrades packages from every repository (D40), so those kernel updates also arrive automatically.

**Check:** a backports `linux-image-amd64` is installed, and so is everything else this step installs from backports when backports offers it: `linux-headers-amd64` (with an NVIDIA GPU) and the selected firmware (with the backports-firmware option). A part that failed, or an option turned on later, is retried.

**Gotchas:**
- Once the metapackage comes from backports, nothing depends on the stable 6.12 image any more. Debian's normal autoremove will eventually remove it once newer kernels arrive, which this policy accepts. To switch back to stable, install `linux-image-amd64` from trixie by hand.
- **NVIDIA:** NVIDIA's open modules (the "NVIDIA latest" source) support Linux ≥ 4.15, so both kernels are fine. Debian's 550 builds for the stock 6.12 kernel, but its newest backports build documents fixes only up to Linux 6.17. So if you select the backports kernel on a machine using "Debian stock" NVIDIA, the review screen warns that the driver may not build for 7.2.

---

## 6. network-migration

**Removed (D52).** kiln leaves networking as the installer set it up: ifupdown on servers, NetworkManager on GNOME machines. Cockpit's networking page can then only show, not edit, ifupdown interfaces.

---

## 7. multimedia

**ID** `multimedia` · media · depends: apt-sources (the kernel step is optional; NVIDIA headers come from backports only when it's ticked, D43) The GStreamer parts apply only when GNOME is detected. The rest applies everywhere.

**Actions:**
- **Codecs:** `libavcodec-extra`, `gstreamer1.0-plugins-good`, `gstreamer1.0-plugins-bad`, `gstreamer1.0-plugins-ugly`, `gstreamer1.0-libav`.
  - `libavcodec-extra61` `Conflicts`/`Provides: libavcodec61`, so apt swaps it in for `libavcodec61`. That's expected, and dependents stay satisfied through the Provides. The review screen lists the removal.
- **DVD** (libdvd-pkg 1.4.3, contrib):
  1. Preseed `libdvd-pkg libdvd-pkg/build boolean true` and `libdvd-pkg libdvd-pkg/post-invoke_hook-install boolean true`. `first-install` is only a `note`, so preseeding it does nothing.
  2. Install `libdvd-pkg`. During the install the build is postponed, because dpkg holds the lock.
  3. Run `dpkg-reconfigure -f noninteractive libdvd-pkg`. The DVD part runs after NVIDIA. If the build fails (for example, videolan.org is unreachable), that becomes a note and the rest of the step still counts as done (D44). That builds `libdvdcss2` from the videolan.org source tarball, using `build-essential` and `debhelper`.
- **VA-API:**
  - `vainfo` on every machine with a GPU, whatever the vendor (D51).
  - AMD → `mesa-va-drivers`.
  - Intel graphics (Gen8+) → `intel-media-va-driver-non-free` (non-free). It `Conflicts` with `intel-media-va-driver` (main), so only one of the two is ever installed; main is used if non-free is disabled. Pre-Gen8 Intel isn't covered, and that's noted.
- **NVIDIA** (option on), only if an NVIDIA GPU is detected. The driver source is your choice per machine (D39):
  - **Source choice.** "NVIDIA latest" uses NVIDIA's own repository and the newest branch (620.x today). It's the default when the GPU is **Turing or newer** (GTX 16xx / RTX 20xx and later; e.g. your RTX 2070 Super). The repository ships only NVIDIA's open kernel modules, which support "any Turing or later GPU" and Linux ≥ 4.15.
  - **Debian stock.** "Debian stock" uses trixie's `nvidia-driver` 550.163 (non-free). It supports Maxwell, Pascal, Volta, Turing, Ampere and Ada/Hopper, but not Blackwell. Debian's wiki says the 550 series "is no longer maintained and contains several security issues"; the option form shows that sentence.
  - **GPU generation.** For pre-Turing GPUs (e.g. your GTX 970, Maxwell), "Debian stock" is the only choice; "NVIDIA latest" is shown disabled with the reason. For Blackwell, only "NVIDIA latest" is offered. kiln decides the generation from the GPU's PCI device ID, matched against the compatible-GPU table in NVIDIA's open-gpu-kernel-modules README at the matching release tag. That table is compiled into kiln and refreshed with each kiln release.
  - **Unticking.** Unticking the NVIDIA option leaves Debian's open `nouveau` driver in place, using `firmware-nvidia-graphics` from step 5.
  1. **Secure Boot on** (the efivar from preflight's facts), for **both** sources: prepare signing **before** installing the driver. Both build the module with DKMS; NVIDIA's `nvidia-kernel-open-dkms` needs `dkms ≥ 3.1.8`, and trixie has 3.2.2.
     - Install `mokutil openssl dkms`, then `dkms generate_mok`. This creates `/var/lib/dkms/mok.key` and `mok.pub` (DER, RSA-2048) if missing, and dkms signs modules with them.
     - Check enrollment: if `mokutil --test-key /var/lib/dkms/mok.pub` says the key isn't enrolled or pending (in mokutil 0.7.2 it also checks MokNew):
       - generate a one-time password, stored in `/etc/kiln/secrets/mok` (0600) and shown once on the summary screen, never in notes;
       - run `mokutil --generate-hash` with the password on stdin (never in argv), writing the hash to `/root/.kiln-mok.hash` (0600);
       - run `mokutil --import /var/lib/dkms/mok.pub --hash-file /root/.kiln-mok.hash`;
       - delete the hash file;
       - summary note: "At next boot choose *Enroll MOK* → *Continue* → enter the one-time password shown on the summary screen (also: sudo kiln secrets show mok)".
     - If any of this fails, **skip NVIDIA** with a note. Never install a driver that can't load, and never disable Secure Boot.
  2. Install `linux-headers-amd64` from the same suite as the kernel metapackage: trixie by default, backports only if you selected the backports kernel.
  3. **"NVIDIA latest"** (verified 2026-11-07 against NVIDIA's driver installation guide for Debian 12/13 and the repository's own metadata):
     1. **Repository**, in deb822 per your brief, not NVIDIA's one-line `.list`. NVIDIA publishes the signing key only inside its `cuda-keyring` package, so kiln:
        - downloads `https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/cuda-keyring_1.1-1_all.deb`;
        - verifies its sha256 is `d0d4ef986a44400f9db33c600ef33a985175e7cc63d805a10e1839c7a1e78f5f`;
        - extracts `usr/share/keyrings/cuda-archive-keyring.gpg` with `dpkg-deb -x` into a temp dir, **without installing the package**;
        - checks the key fingerprint is `02182E60104FCDC26EAE1B8597A5D4CB8793F200` ("Kitmaker (Debian 13 Trixie) <kitmaker@nvidia.com>");
        - installs the key as `/etc/apt/keyrings/nvidia-cuda.gpg` (0644).

        A mismatch on either check fails the step with "NVIDIA's repository key changed; kiln needs an update".
     2. Write `/etc/apt/sources.list.d/nvidia-cuda.sources`: `Types: deb`, `URIs: https://developer.download.nvidia.com/compute/cuda/repos/debian13/x86_64/`, `Suites: /` (a flat repository; deb822 then needs no `Components:`), `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/nvidia-cuda.gpg`. Then `AptUpdate`. The repository's Release says `Origin: NVIDIA`, `Label: NVIDIA CUDA`.
     3. **No branch pinning.** No `nvidia-driver-pinning-*` package, so apt follows NVIDIA's newest branch, which has been NVIDIA's default since 590.
     4. Install the **desktop-only** set from NVIDIA's guide: `nvidia-driver nvidia-kernel-open-dkms`. That's the display driver with GL/Vulkan/VDPAU, the open DKMS module, and `firmware-nvidia-gsp`, with no CUDA libraries.
        - NVIDIA's packages use the same names as Debian's, at higher versions (620 > 550), so apt picks NVIDIA's.
        - The review screen lists them with their origin.
  4. **"Debian stock":** install `nvidia-driver` from trixie (550.163, non-free, DKMS; `nvidia-kernel-dkms` pulls in `firmware-nvidia-gsp`). If NVIDIA's repository file exists from an earlier run, it's set to `Enabled: no` first, so apt doesn't pick 620.
  5. **Switching sources:** going from "Debian stock" to "NVIDIA latest" is an ordinary upgrade. Going back is a downgrade, which kiln doesn't automate: the summary explains the manual steps (disable the NVIDIA source, then `apt install nvidia-driver=<550 version>` and its dependencies). This is a consequence of the shared package names, so it's flagged here rather than decided silently.

**Check:**
- the selected packages are installed;
- `libdvdcss2` is installed (version `*~local`) and `/usr/lib/x86_64-linux-gnu/libdvdcss.so.2` exists;
- with NVIDIA: the chosen source is in effect. For "NVIDIA latest": `nvidia-cuda.sources` and the key match, and `apt-cache policy nvidia-driver` shows the installed version from origin NVIDIA. For "Debian stock": the installed version is from trixie. In both cases `dkms status` shows the module built for the running kernel, or for a **newer** installed kernel waiting for a reboot (`dpkg --compare-versions`); an older kernel with the module doesn't count;
- with NVIDIA and Secure Boot, the MOK is enrolled or pending (`mokutil --test-key`, which also covers the pending request in mokutil 0.7.2).

---

## 8. repos

**ID** `repos` · apps · depends: apt-sources

**Actions:**
1. Install `extrepo` (0.14). For each chosen extrepo repo (repos whose packages need a desktop, such as `google_chrome`, apply only when GNOME is detected):
   1. Make sure the repo's policy is in `enabled_policies:` in `/etc/extrepo/config.yaml`. The default lists only `main`, with `contrib` and `non-free` commented out. `google_chrome` has policy `non-free`, so kiln adds `- non-free` (key-level edit, with backup). If the policy is missing, extrepo exits with "Please edit /etc/extrepo/config.yaml…".
   2. Run `extrepo enable <name>` as root. It writes `/etc/apt/sources.list.d/extrepo_<name>.sources` with `Signed-By: /var/lib/extrepo/keys/<name>.asc`. A re-run only sets `Enabled: yes`, so it's idempotent.
   3. Let extrepo's post-enable command finish. On first enable it waits through a 10 s countdown, then writes `/etc/default/google-chrome` with `repo_add_once="false"` and `repo_reenable_on_distupgrade="false"`. This stops Chrome's postinst from adding its own duplicate `google-chrome.list` with a conflicting Signed-By, so kiln must not interrupt it or close its stdin early.
   4. `AptUpdate`, then install the repo's package (`google-chrome-stable`).
2. Flatpak (only when GNOME is detected; otherwise "not applicable: no GNOME desktop"): install `flatpak` (1.16.x) and `gnome-software-plugin-flatpak` (48.x). Then run `flatpak remote-add --system --if-not-exists flathub https://dl.flathub.org/repo/flathub.flatpakrepo`, the URL from Flathub's Debian setup page.

**Check:** `extrepo_<name>.sources` exists with `Enabled: yes`, the packages are installed, `/etc/default/google-chrome` has both keys, and `flatpak remotes --system` lists flathub.

---

## 9. devtools

**ID** `devtools` · dev · depends: apt-sources

**Actions:**
1. GitHub CLI repo (option `github`, default, D18): download `https://cli.github.com/packages/githubcli-archive-keyring.gpg` and verify its sha256 is `6084d5d7bd8e288441e0e94fc6275570895da18e6751f70f057485dc2d1a811b` (the value in GitHub's install doc on 2026-11-07; a mismatch fails the step, and the doc must be re-checked). Install it as `/etc/apt/keyrings/githubcli-archive-keyring.gpg` (0644). Write `/etc/apt/sources.list.d/github-cli.sources` with `Types: deb`, `URIs: https://cli.github.com/packages`, `Suites: stable`, `Components: main`, `Architectures: amd64`, and `Signed-By:` set to that keyring. This is the deb822 form of the official one-line source; the repo's Release file has Codename `stable` and Components `main`. Then `AptUpdate`. With option `debian`, use trixie's `gh` 2.46.
2. Install `build-essential git git-lfs gh curl wget jq vim neovim tmux htop btop ripgrep fd-find fzf bat tree unzip zip rsync lsof strace bind9-dnsutils iproute2 traceroute mtr-tiny netcat-openbsd cmake ninja-build pkgconf libssl-dev shellcheck bash-completion man-db`. All of these are verified in trixie main. Use `pkgconf`, because `pkg-config` is a transitional package. Extra packages from the options are checked with `apt-cache policy` before the review screen. A missing package is noted, not a step failure.
3. As the user (see the user-run rule in architecture.md):
   - `mkdir -p <home>/.local/bin`;
   - symlinks `<home>/.local/bin/fd → /usr/bin/fdfind` (fd-find 10.2.0) and `<home>/.local/bin/bat → /usr/bin/batcat` (bat 0.25.0), replaced only if they point elsewhere;
   - `git lfs install --skip-repo`.

**Check:** all available packages are installed, the GitHub CLI source and keyring match, the symlinks point correctly, and `git config --global --get filter.lfs.process` is set.

---

## 10. system

**ID** `system` · base · depends: preflight

**Actions** (systemd 257):
- **Hostname** (if set and different): `hostnamectl set-hostname <name>`. Changing it under a running X11 session can stop new windows opening until you log out, because the X authority is tied to the hostname. The summary notes "log out and back in" when the hostname changed. hostnamectl never touches `/etc/hosts`, so kiln also rewrites the `127.0.1.1` line there (Debian Reference §5.1 convention). That's a key-level edit, with backup.
- **Timezone** (if set): `timedatectl set-timezone <zone>`.
- **Time sync:** `systemd-timesyncd` (Debian's default; D7), then `timedatectl set-ntp true`. With the `chrony` option, install chrony; both packages `Provides/Conflicts/Replaces: time-daemon`, so apt removes timesyncd.
- **journald:** drop-in `/etc/systemd/journald.conf.d/60-kiln.conf` with `[Journal]` and `SystemMaxUse=500M`. journald.conf(5) recommends prefixes 60–90 for drop-ins in /etc. Then `systemctl restart systemd-journald`. The default cap is 10% of the filesystem, up to 4G.
- **inotify:**
  - Read the current `fs.inotify.max_user_watches` / `max_user_instances`. In 6.12, watches scale with RAM (1% of lowmem divided by the per-watch cost, clamped to 8192…1048576), roughly 60k on 8 GB. Instances is fixed at 128.
  - Write `/etc/sysctl.d/60-kiln-inotify.conf` with max(current, configured), so a value is never lowered. No Debian package ships a sysctl.d file for these keys.
  - Apply with `sysctl -q -p /etc/sysctl.d/60-kiln-inotify.conf`, not `--system`, so only kiln's file is re-applied.
- **zram** (`systemd-zram-generator` 1.2.1; D8): Debian's `/usr/lib/systemd/zram-generator.conf` is just `[zram0]`, which uses the built-in default `zram-size = min(ram / 2, 4096)`.
  - kiln writes the drop-in `/etc/systemd/zram-generator.conf.d/60-kiln.conf` with `[zram0]` and `compression-algorithm = zstd` (Debian's kernel has `CONFIG_ZRAM_BACKEND_ZSTD=y`). The size option only appears if the owner changes it.
  - Then `systemctl daemon-reload`.
  - If `dev-zram0.swap` is inactive, `systemctl start dev-zram0.swap`.
  - If it's active and the config changed: restarting `systemd-zram-setup@zram0.service` also restarts the swap unit, which runs `swapoff`. That can fail or invoke the OOM killer when used swap exceeds available memory (likely on the 8 GB HP). So restart only when used swap is under 50% of MemAvailable; otherwise add the note "zram settings apply at the next boot".
  - Never install `zram-tools` alongside it, and remove it if present.

**Check:** the hostname, `/etc/hosts` line, timezone and NTP are as chosen; the journald and sysctl files match and the live sysctl values are ≥ the configured ones; the zram drop-in matches; and `/proc/swaps` lists `/dev/zram0`. A pending "next boot" note counts as done.

---

## 11. power

**ID** `power` · power · depends: preflight · option: power manager

**Applies:** always.

**Actions** (systemd 257, gnome-settings-daemon 48.1, gdm3 48.0, power-profiles-daemon 0.30, tuned 2.25.1):
- **Sleep targets:** `systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target suspend-then-hibernate.target`. These are the exact unit names, and logind reports masked targets as "na" for CanSuspend and friends.
- **logind:** drop-in `/etc/systemd/logind.conf.d/60-kiln-always-on.conf` with:
  - `[Login]`;
  - `HandleLidSwitch=ignore`, `HandleLidSwitchExternalPower=ignore`, `HandleLidSwitchDocked=ignore`;
  - `HandleSuspendKey=ignore`, `HandleSuspendKeyLongPress=ignore` (its default is hibernate);
  - `HandleHibernateKey=ignore`, `HandleHibernateKeyLongPress=ignore`;
  - `IdleAction=ignore` (already the default, stated explicitly).

  `HandlePowerKey` is left at poweroff.

  Apply with `systemctl reload systemd-logind`. Verified: the unit is `Type=notify-reload`, SIGHUP re-reads the config, and the lid handler reads the setting at event time, so no restart is needed. This applies on machines without a desktop too.
- **GNOME** (only when GNOME is detected):
  - **User sessions:**
    - Debian ships **no** `/etc/dconf/profile/user`, so kiln creates it with `user-db:user` and `system-db:local`. If one exists, it only adds `system-db:local` when that's missing.
    - Write `/etc/dconf/db/local.d/60-kiln-power` with `[org/gnome/settings-daemon/plugins/power]` `sleep-inactive-ac-type='nothing'`, `sleep-inactive-battery-type='nothing'` and `power-button-action='interactive'`. The default for all three is `'suspend'`.
    - With the lock option (on, D23), also write `/etc/dconf/db/local.d/locks/60-kiln-power` listing those three key paths.
    - Then `dconf update`. Screen blanking is left alone.
  - **Greeter:**
    - `/etc/gdm3/greeter.dconf-defaults` is ucf-managed, and in Debian's version the power keys are **commented out**. kiln uncomments or inserts `sleep-inactive-ac-type='nothing'` and `sleep-inactive-battery-type='nothing'` under the `[org/gnome/settings-daemon/plugins/power]` header (key-level edit with backup).
    - Then `systemctl reload gdm3`. `ExecReload=` runs `/usr/share/gdm/generate-config`, which rebuilds `/var/lib/gdm3/greeter-dconf-defaults`, followed by SIGHUP; gdm's SIGHUP handler only reloads settings, so sessions survive. `dpkg-reconfigure gdm3` does **not** do this.
- **Power manager:**
  - **Balanced everywhere (D52),** so the CPU doesn't sit at high clocks, using power and making heat, when idle.
  - **With GNOME:** `power-profiles-daemon` (pulled in by GNOME), set with `powerprofilesctl set balanced`, which ppd always offers. The choice persists in `/var/lib/power-profiles-daemon/state.ini`. ppd 0.30's balanced sets the `balanced` platform profile and, with amd-pstate, EPP `balance_performance` on AC (`balance_power` on battery).
  - **Without GNOME:** `tuned` with trixie's stock `balanced` profile (`tuned-adm profile balanced`): `schedutil` governor, EPP `balance_performance`, boost on, `[acpi] platform_profile=balanced`, no `vm.swappiness` change (tuned 2.25.1). kiln's old `/etc/tuned/profiles/kiln-performance/` from earlier versions is removed.
  - **Never both.** If switching, stop, disable and remove the other. Never install TLP. No trixie task installs TLP, so kiln doesn't look for it (D50).

**Check:** the targets are masked, the logind drop-in matches, the dconf keys and locks are present (GNOME), the greeter keys are uncommented, and the active profile is ppd's `balanced` / tuned's `balanced`, with no old `kiln-performance` profile left.

**Gotchas:** power-profiles-daemon `Conflicts: tlp, laptop-mode-tools`, and `tuned-ppd` `Conflicts/Provides: power-profiles-daemon`. Plain `tuned` and ppd don't conflict at the package level, so kiln enforces the one-owner rule itself. ppd and tuned's `balanced` both drive the platform profile and AMD EPP themselves, so `thermal` only verifies.

---

## 12. thermal

**ID** `thermal` · power · depends: power · helper `kiln thermal apply` (boot-time thinkfan validation)

**Applies:** each sub-part has its own applicability.

**Actions:**
- **platform_profile:** kiln **never writes** it. ppd 0.30 writes `/sys/firmware/acpi/platform_profile` (and adopts external writes), and tuned's `balanced` sets `[acpi] platform_profile=balanced`. kiln only verifies and reports (option "Check the balanced platform profile"):
  - If `/sys/firmware/acpi/platform_profile_choices` contains `balanced`, the current value should be `balanced`.
  - On kernel ≥ 6.14 (the backports kernel), the legacy file shows the **intersection** of all handlers' choices and reads `custom` when handlers disagree. `custom` is treated as a difference, and the per-handler values from `/sys/class/platform-profile/*/profile` are reported.
  - There is no `kiln-thermal.service`.
- **AMD:** report `/sys/devices/system/cpu/amd_pstate/status` (active/passive/guided/disable) and the per-policy `energy_performance_preference`. Under the balanced profiles, EPP should be `balance_performance` (or `balance_power`, ppd on battery). A difference is a note. No kernel command-line changes. The default amd-pstate mode is fact R.
- **ThinkPad** (vendor LENOVO and thinkpad_acpi loaded, and `/proc/acpi/ibm/fan` exists):
  1. Write `/etc/modprobe.d/kiln-thinkpad.conf` with `options thinkpad_acpi fan_control=1`, then install `thinkfan` (1.3.1). Its unit ships disabled and reads `/etc/default/thinkfan` (`DAEMON_ARGS="-q"`).
  2. Write `/etc/thinkfan.yaml`. thinkfan reads it before `/etc/thinkfan.conf`, and Debian ships neither. The curve keeps the fan on and ramps early:
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
     - The fan **never stops** (minimum level 1).
     - It reaches the highest **in-spec** speed (level 7) from about 65–70 °C, which is the owner's "full speed from ~70 °C".
     - `level disengaged` (out of spec, per thinkfan.conf(5)) is used only from 78 °C upward.
     - The sensor list is built from what's detected (fact R).
  3. **Validation:** thinkfan has **no config-test flag** (`-n` = foreground, `-c` = config file, `-D` = "DANGEROUS mode"), and it can't run until `fan_control=1` is active.
     - **If** `/sys/module/thinkpad_acpi/parameters/fan_control` is already `Y`: stop `thinkfan` first if it's running (two instances would fight over the fan), then run `timeout 10 thinkfan -n -q -c /etc/thinkfan.yaml`. Exit 124 (still running when the timeout killed it) means the config is valid. Then enable and start `thinkfan`. Any other exit is a failure: write `level auto` to `/proc/acpi/ibm/fan` and leave thinkfan disabled.
     - **Otherwise:** leave thinkfan disabled, add a reboot note, and install `kiln-thinkfan-enable.service`, a oneshot that runs `kiln thermal apply` at boot. That runs the same validation, then enables and starts thinkfan and disables itself.
  4. thinkfan pings the fan watchdog itself.
  - If the fan file or usable sensors are missing: "not applicable: this ThinkPad doesn't expose fan control".
- **HP:** Linux can't drive the fans. Rely on the platform profile, and note "check the BIOS for *Fan Always On*".
- **lm-sensors:** install `lm-sensors` only. Don't run `sensors-detect`: on these laptops k10temp, amdgpu, thinkpad and hp-wmi hwmon drivers load automatically, and its I2C/Super I/O probing adds risk for no gain.

**Check:** the platform profile and AMD EPP are reported as notes (a machine whose firmware offers no `balanced` profile is still done). On ThinkPads, the modprobe file and thinkfan config must match, and thinkfan must be active, or the boot-time enable service must be pending: enabled, `fan_control` not yet `Y`, and not `failed`. A failed boot helper reports "not done" with a `journalctl -b -u kiln-thinkfan-enable.service` hint.

---

## 13. battery

**ID** `battery` · power · depends: preflight · helpers `kiln battery status|apply|watch`

**Applies:** at least one system battery (`type=Battery` and `scope` not `Device`). Otherwise "not applicable: no battery".

**Goal:** keep the charge below 90%, preferring 80%. The ladder below stops at the first method that works. "Works" means the value reads back as an acceptable value (< 90). A missing `scope` attribute counts as a system battery, because the ACPI battery driver doesn't expose it.

### Method ladder

**Method 1: standard kernel thresholds** (`/sys/class/power_supply/BAT*/charge_control_end_threshold`, plus `charge_control_start_threshold` if present).
- Stop candidates in order: `80, 85, 75, 70, 60, 50` (anything ≥ `battery.max_stop`, i.e. 90+, is never tried). Start = stop − `start_gap` (75 for 80).
- Write order. thinkpad_acpi rejects start > current stop and stop < current start with EINVAL; start == stop is allowed. When **lowering** (new stop < current start), write start first. When **raising**, write stop first. If a write fails, **or the read-back doesn't match**, retry once in the opposite order before moving to the next candidate.
- Driver quirks:
  - **dell-laptop:** silently **clamps** (start 50–95, end 55–100) and pushes the other value to keep a 5-point gap, instead of returning EINVAL. Accept whatever reads back if it's < 90.
  - **asus-wmi:** the read-back is a value cached by the driver ("There isn't any method in the DSDT to read the threshold"), so the result is recorded as "set, not verifiable".
- `charge_types` present (Dell ≥ 6.12, IdeaPad ≥ 6.17): Dell needs `Custom` written before the thresholds take effect, and verify it stuck. Writing `Long_Life` is a fixed-mode fallback (method 2) where that's the only choice.
- Vendor ranges (TLP matrix, verified 2026-11 at linrunner.de/tlp/settings/bc-vendors.html): ThinkPad start 0–99 / stop 1–100. Dell start 50–95 / stop 55–100 with start ≤ stop−5 (a BIOS admin password blocks writes, so note it). ASUS is stop only, and some models accept only 40/60/80, which also reset at power-on (so the udev/boot re-apply matters). LG Gram accepts only 80/100. Huawei and System76 use full ranges. Framework works only through `cros_charge-control` with `probe_with_fwk_charge_control=1`. The kernel ABI says drivers may **round** to a supported value, so always compare the read-back to what was written and accept the rounded value only if it's still < 90.
- Log every tried value and its result. Record the accepted pair.

**Method 2: vendor fixed modes** (only if method 1 doesn't exist or doesn't work):
- Lenovo IdeaPad / ThinkBook: `/sys/bus/platform/drivers/ideapad_acpi/VPC2004:*/conservation_mode = 1` (caps around 60% or 80% depending on the model; the actual cap can't be read, so record "conservation mode"). On kernel ≥ 6.17, `charge_types=Long_Life` is the same thing.
- Samsung: `/sys/devices/platform/samsung/battery_life_extender = 1` (80%).
- LG (older): `/sys/devices/platform/lg-laptop/battery_care_limit = 80`.
- Huawei: `/sys/devices/platform/huawei-wmi/charge_control_thresholds = "75 80"`.
- Sony (50/80/100), Toshiba (80/100), MSI (`msi_ec`, specific models, start forced to stop−10): table entries with path, accepted values, read-back.
- The ladder is a data table (`internal/battery/vendors.go`): vendor match (DMI + driver), path glob, value candidates, write order, read-back rule. Adding a vendor is a table row plus its primary-source citation in debian13-notes.md.

**Method 3: software fallback.** If `charge_behaviour` exists and lists `inhibit-charge` (6.12 offers `auto`, `inhibit-charge` and `force-discharge`; `inhibit-charge-awake` exists only on newer kernels such as the backports 7.2, and is used only where listed): install `kiln-battery-watch.service` (or a 1-minute timer). It reads `capacity`, sets `inhibit-charge` at ≥ 80%, and sets `auto` at ≤ 75% (hysteresis). It's idempotent and logs only on transitions.

**Method 4: nothing works.** The summary says so clearly with the DMI vendor + product, lists what was probed, and suggests BIOS/UEFI options: "Battery Care", "Adaptive Battery Optimizer", "Battery Health Manager", "Primarily AC use", "Charge limit". It may mention that a smart plug driven by battery percentage is a possible idea (not built).

### Persistence

- `kiln-battery.service` (oneshot, `ExecStart=/usr/local/bin/kiln battery apply`, **no** `RemainAfterExit=yes`, so every later start runs again), enabled for boot.
- udev rule `/etc/udev/rules.d/90-kiln-battery.rules`:
  ```
  SUBSYSTEM=="power_supply", ACTION=="change", ATTR{type}=="Mains", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"
  SUBSYSTEM=="power_supply", ACTION=="add", ATTR{type}=="Battery", RUN+="/usr/bin/systemctl --no-block start kiln-battery.service"
  ```
  `ENV{SYSTEMD_WANTS}` can't be used: systemd.device(5) acts on device Wants only when a device first becomes active, never on change events such as AC plug and unplug. Starting the same unit from boot, udev and the timer means systemd never runs two copies at once.
- `kiln-battery.timer` (every 15 min) as a safety net: threshold writes by anything else (UPower, firmware) emit no uevent.
- **Locking.** The battery step, `kiln battery apply` and `kiln battery watch` all take `/run/kiln/battery.lock`, so a udev- or timer-triggered apply can never write at the same time as the step (D41).
- `kiln battery apply` re-runs the ladder starting from the recorded method (stored in `/etc/kiln/state.json` under `battery`) and falls back to the full ladder if that method stops working.

### GNOME "Preserve Battery Health" (UPower)

Verified against trixie (GNOME 48, UPower 1.90.9). UPower's udev rule enables the feature only when `charge_control_start_threshold` exists. The limits come from hwdb `CHARGE_LIMIT` (default `75,80`). The on/off flag lives in a `charging-threshold-status` file in UPower's state dir, not in `UPower.conf`. **Enabling** writes start then end, which can fail on ThinkPads because of write order. **Disabling writes 0/100** and clobbers our limits.

**Owner: kiln.** So that the two can't disagree:
1. **Before** kiln writes any threshold (so UPower's restart can't overwrite kiln's values afterwards), write `/etc/udev/hwdb.d/61-kiln-battery.hwdb` with a match `battery:<kernel name>:<model_name>:<dmi modalias>` (UPower 1.90.9's key format) setting `CHARGE_LIMIT=<accepted start>,<accepted stop>`. A later-sorting hwdb file wins over UPower's `60-upower-battery.hwdb`. Then run `systemd-hwdb update`, `udevadm trigger -s power_supply` and `systemctl try-restart upower`, so UPower picks up the new limit (that last part is inferred, not verified). If someone turns GNOME's toggle on, it applies the same values. At startup UPower re-applies start then end when its saved state is "enabled".
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

**Removed (D50).** kiln doesn't set up a firewall: a fresh Debian install has none active, and kiln leaves it that way. fail2ban still bans through nftables (step 16), Cockpit listens on all networks (step 18), and SSH needs no port opening.

---

## 15. ssh

**ID** `ssh` · security · **risky**

**Purpose:** make sure SSH works with your user's and root's passwords (D45), for later remote access. kiln itself is run at the console.

**Applies:** always. If the installer didn't install an SSH server, this step installs `openssh-server` and enables `ssh.service` (D45; this reverses D29 for SSH only).

**Facts used** (openssh 1:10.0p1-7+deb13u4):
- **Include order:** Debian's `sshd_config` has `Include /etc/ssh/sshd_config.d/*.conf` at line 13. Drop-ins are read in lexical order and the first value wins, and debconf answers are written after the Include, so a low-numbered drop-in wins.
- **Debian's defaults:** `KbdInteractiveAuthentication no`, `UsePAM yes` and `X11Forwarding yes`. Upstream defaults are `PermitRootLogin prohibit-password` and `MaxAuthTries 6`.
- **`Port` is cumulative.** Every `Port` line across all files is added, rather than the first one winning.
- **Units:** `ssh.service` is enabled, and its `ExecReload` runs `sshd -t` then HUP. `ssh.socket` ships disabled.
- **Keys:** DSA is gone. `ssh-rsa` keys still work, using rsa-sha2 signatures.

**Actions:**
1. **Server.** Install `openssh-server` if missing, and `systemctl enable --now ssh.service` if it isn't enabled (unless `ssh.socket` was enabled by hand).
2. **Root password** (option on): if root's `/etc/shadow` password field is locked or empty (`!`, `*`, or blank) and the target user has a password, copy the user's password **hash** to root with `chpasswd -e` (the `root:<hash>` line goes on stdin, never argv, and the output isn't logged). root then has the same password as the user, and kiln never sees the password itself. If the user has no password either, a note says to run `sudo passwd root`.
3. **Keys:** kiln doesn't add SSH keys (D50). It counts the valid keys already in `<home>/.ssh/authorized_keys`, so it can warn when password logins are off and no key exists.
4. **Drop-in** `/etc/ssh/sshd_config.d/10-kiln.conf`:
   - `PermitRootLogin yes` (option "Allow root to log in over SSH", on by default, D45), `MaxAuthTries 4` (the owner's choice), `X11Forwarding no`.
   - `PasswordAuthentication yes` by default (option "Password logins": `on`, D45). The other value is `off` (D51); with `off` and no key in `authorized_keys`, the review screen warns that nobody could log in over SSH. When passwords are on, the review screen and summary warn on every run, also when nothing changes, that password logins for the user and root are open to anywhere SSH is reachable, with fail2ban slowing down guessing.
   - `KbdInteractiveAuthentication no` is left out, because Debian already sets it.
5. **Port** (when it isn't 22): `Port <n>` in the drop-in. With port 22 kiln writes no `Port` line, so if another file sets one, sshd doesn't listen on 22 at all: the review screen warns that the step will fail its check and names the port to choose instead. Port lines add up, so if the main `sshd_config` (or another drop-in) has an uncommented `Port`, kiln warns that sshd will also listen there, and leaves the file as is. If `ssh.socket` was enabled by hand instead of `ssh.service`, the port goes in an `ssh.socket` drop-in instead (`ListenStream=` then `ListenStream=<n>`), which needs `daemon-reload` and `restart ssh.socket`.
6. **Verify, then revert on failure:**
   1. Record `pending` and validate with `sshd -t`.
   2. Apply with `systemctl reload-or-restart ssh`: a reload when sshd is running, a start when `ssh.service` is enabled but stopped.
   3. Check:
      - `ssh` is active;
      - something is listening on the port (`ss -ltn`);
      - `sshd -T -C user=<target>,host=<hostname>,addr=<lan-ip>` shows the expected values. Without `-C`, `-T` ignores `Match` blocks, so a `Match User` block that re-enables passwords would go unseen.
   4. If anything fails, restore the previous drop-in, `systemctl reload-or-restart ssh` (socket mode: `daemon-reload` + `restart ssh.socket`), and record `failed`. If an earlier SSH change was left unfinished, its first snapshot is kept, so the revert goes back to the last known good files.

**Check:** `openssh-server` is installed and `ssh.service` enabled, root has a password (when the option is on and the user has one), the drop-in matches, the `sshd -T -C …` values match, and the port is listening.

**Gotchas:** the service is `ssh` (`sshd.service` is an alias). `MaxAuthTries 4` can lock out SSH agents that offer more than 4 keys before the right one. The summary notes `IdentitiesOnly yes` / `IdentityFile` as the client-side fix. Router port forwarding is manual, so it's only noted.

---

## 16. fail2ban

**ID** `fail2ban` · security · depends: ssh

**Applies:** an SSH server was detected, or the ssh step is ticked (it installs one, D45), because the only jail is `sshd`. Otherwise "not applicable: no SSH server installed, and the SSH step is off".

**Facts used** (fail2ban 1.1.0-8):
- **Debian defaults:** `jail.d/defaults-debian.conf` sets `banaction = nftables`, `banaction_allports = nftables[type=allports]`, and `[sshd] enabled = true`, `backend = systemd`, `journalmatch = _SYSTEMD_UNIT=ssh.service + _COMM=sshd`.
- **Read order** (later wins): `jail.conf` → `jail.d/*.conf` → `jail.local` → `jail.d/*.local`, alphabetically.
- **nftables action:** uses its own `table inet f2b-table` (input hook, priority −1), so it works with no firewall set up. `nftables` has priority "important", so every trixie install has `nft`; fail2ban also recommends it.

**Actions:** install `fail2ban python3-systemd`, then write `/etc/fail2ban/jail.d/50-kiln.local`:
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
- `banaction_allports` bans the attacker on every port, so a later SSH port change can't leave the new port unprotected.
- The jail-level `journalmatch` replaces the filter's own, so kiln includes `_COMM=sshd-session`. That's what keeps bans working if `ssh.socket` (`sshd@…` units) is ever used.

Restart fail2ban, then run `fail2ban-client ping` and `fail2ban-client status sshd`.

**Check:** the file matches, the service is active, and the `sshd` jail is running.

**Gotchas:** trixie has no `/var/log/auth.log` by default (no rsyslog), so the journal backend is essential. Debian already sets it for sshd; kiln restates it in the jail.

---

## 17. docker

**ID** `docker` · apps · depends: apt-sources

**Actions** (Docker's Debian install docs, current release 29.9.3):
1. Remove the conflicting packages if present: `docker.io docker-compose docker-doc docker-buildx podman-docker containerd runc`.
2. `install -m 0755 -d /etc/apt/keyrings`, then download `https://download.docker.com/linux/debian/gpg` to `/etc/apt/keyrings/docker.asc`, mode 0644 (world-readable, as the docs require).
3. `/etc/apt/sources.list.d/docker.sources`: `Types: deb`, `URIs: https://download.docker.com/linux/debian`, `Suites: trixie`, `Components: stable`, `Architectures: amd64`, `Signed-By: /etc/apt/keyrings/docker.asc`. Then `AptUpdate`.
4. Install `docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin`. `docker-ce` depends on `iptables` and `nftables`; Debian's iptables is the nft variant, which Docker supports.
5. `/etc/docker/daemon.json` (JSON, no comment header): merge `{"log-driver":"json-file","log-opts":{"max-size":"10m","max-file":"3"}}` into the existing keys. log-opts values must be strings, and they apply to **new** containers only (noted).
   - Parse the merged result in Go before writing.
   - If `dockerd --help` lists `--validate` (detected at run time, fact R), also run `dockerd --validate --config-file /etc/docker/daemon.json` before `systemctl restart docker`.
   - If docker doesn't come back active, restore the previous file, restart, and record `failed`.
6. `UserAddGroup(user, docker)`. Wait until `docker info` succeeds (up to 60 s; docker starts asynchronously after install or restart), then run `docker run --rm hello-world` as root. `systemctl restart docker` happens only when `daemon.json` actually changed, because it restarts running containers.

**Check:** the packages are installed, daemon.json contains the keys, the user is in the group, and docker is active.

**Notes:**
- "log out and back in for docker group membership";
- "the docker group is root-equivalent";
- "existing containers keep their old log settings".

---

## 18. cockpit

**ID** `cockpit` · apps · depends: apt-sources

**Actions:**
- Install `cockpit` and `cockpit-networkmanager` (named, because apt adds no Recommends to a package that's already installed). With backports on, use `apt-get install -t trixie-backports cockpit` (371). The `(>= source:Version)` dependencies pull the matching 371 binaries from backports. Without backports, install from main (337).
- **Keep Recommends**. In both versions they're `cockpit-storaged`, `cockpit-networkmanager` and `cockpit-packagekit`, and `cockpit-networkmanager` is the networking page.
- `cockpit-machines` and `cockpit-podman` are not recommended, and kiln never installs them.
- The Recommends pull in `network-manager`, `packagekit` and `udisks2`. The review screen lists them. NetworkManager then runs but leaves the installer's ifupdown interfaces alone (D52).
- `cockpit.socket` is enabled and started by the package (`ListenStream=9090`). kiln only verifies this.

**Check:** `cockpit` is installed from the expected suite, `cockpit-networkmanager` is installed, and `cockpit.socket` is enabled and active.

**Notes:**
- `https://<lan-ip>:9090`;
- that it listens on port 9090 on every network the machine is connected to (no firewall, D50);
- login uses the target user's password. On a fresh install the postinst writes `root` into `/etc/cockpit/disallowed-users`.

---

## 19. remote-desktop

**Removed (D50).** kiln doesn't set up remote desktop.

---

## 20. autoupdates

**ID** `autoupdates` · security · depends: apt-sources. It runs after every step that adds a repository, and `origin=*` covers whatever repositories exist (D43).

**Policy** (D40): **every package from every configured repository** updates automatically **every 12 hours** (refresh, then upgrade). That covers Debian (trixie, trixie-updates, trixie-security, backports), Docker, the GitHub CLI, Google Chrome and NVIDIA.

**Facts used** (apt 3.0.3, unattended-upgrades 2.12, needrestart 3.11, apt-listchanges 4.8):
- **Debian's units:**
  - `apt-daily.timer` (`OnCalendar=*-*-* 6,18:00`, `RandomizedDelaySec=12h`) runs `apt.systemd.daily update`.
  - `apt-daily-upgrade.timer` (`OnCalendar=*-*-* 6:00`, `RandomizedDelaySec=60m`, `After=apt-daily.timer`) runs `apt.systemd.daily install`, which runs `unattended-upgrade`.
  - Both services have `ConditionACPower=true`, so they skip while running on battery.
- **Intervals:** `apt.systemd.daily` counts its intervals in days by default. It also accepts `s`/`m`/`h`/`d` suffixes or `always`, "which can be used with systemd OnCalendar overrides to define custom schedules".
- **Origin matching:** unattended-upgrades matches `Origins-Pattern` values with `fnmatch`, so `origin=*` matches every repository. It only **upgrades installed packages** (and pulls in their new dependencies); it never installs a package just because a repository has one. For backports, that means only packages you already took from backports.
- **Accumulating config:** Debian's `50unattended-upgrades` origins add up across files, and `#clear` resets them. `20auto-upgrades` is ucf-managed from debconf `unattended-upgrades/enable_auto_updates`.
- **Repository origins:** Debian `Debian` / `Debian Backports`, Docker `Docker`, GitHub CLI `gh`, Chrome `Google LLC`, NVIDIA `NVIDIA` (label `NVIDIA CUDA`).

**Actions:**
1. Preseed `unattended-upgrades unattended-upgrades/enable_auto_updates boolean true`, then install `unattended-upgrades apt-listchanges needrestart`. The package writes `20auto-upgrades`; kiln doesn't rewrite that ucf-managed file.
2. Write `/etc/apt/apt.conf.d/52kiln-unattended-upgrades`, which sorts after `20auto-upgrades` and `50unattended-upgrades`, so its values win:
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
   - `always` hands the schedule to the timers below; without it, the day-based stamps would skip the second run of the day.
   - `origin=*` is the literal "all packages".
   - With the `security` scope option, the pattern is instead the single line `"origin=Debian,codename=${distro_codename}-security,label=Debian-Security"`.
3. Timer overrides, so a refresh and an upgrade both run twice a day. They keep Debian's 06:00/18:00 slots, and the upgrade runs 30 minutes after the refresh:
   - `/etc/systemd/system/apt-daily.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=` (clears Debian's), `OnCalendar=*-*-* 06,18:00`, `RandomizedDelaySec=15m`.
   - `/etc/systemd/system/apt-daily-upgrade.timer.d/60-kiln.conf`: `[Timer]`, `OnCalendar=`, `OnCalendar=*-*-* 06,18:30`, `RandomizedDelaySec=15m`.
   - Then `systemctl daemon-reload` only. The timers are runtime-masked for the whole kiln run (architecture: "Nothing else changes packages during a run"). Restarting them here would let unattended-upgrades start while later steps still use apt, so they start again only in the end-of-run restore (D41).
4. `/etc/needrestart/conf.d/50-kiln.conf`: `$nrconf{restart} = 'a';`. needrestart's default `'i'` becomes list-only when nothing is interactive, so without this, services wouldn't restart after their libraries update.

**Check:**
- `apt-config dump` shows both Periodic values as `always`, and exactly the expected `Origins-Pattern` entry.
- The two timer drop-ins match.
- `systemctl list-timers apt-daily.timer apt-daily-upgrade.timer` shows both active, with next runs at the 06/18 slots.
- The needrestart file matches.

**Notes in the summary:**
- "Updates run at about 06:00 and 18:00, refresh then upgrade 30 minutes later, and skip while on battery. Logs are in `/var/log/unattended-upgrades/`."
- "Kernel and NVIDIA driver updates need a reboot to take effect. kiln never reboots automatically; `/run/reboot-required` shows when one is pending."
- "A Docker update restarts the Docker daemon. Containers come back only if they have a restart policy."
- "A new NVIDIA driver branch can arrive unattended, because the branch isn't pinned (D39)."

---

## 21. node

**ID** `node` · dev · depends: devtools · runs as the user

**Actions:**
1. `mkdir -p <home>/.nvm` as the user.
2. Through `a pinned `sys.Fetch` download run with `bash <file>` as the user`: download `https://raw.githubusercontent.com/nvm-sh/nvm/v0.41.0/install.sh` (pinned), and run it with `bash` as the user with `PROFILE=/dev/null NVM_DIR=<home>/.nvm METHOD=script`:
   - `PROFILE=/dev/null` stops it editing shell files, because kiln's drop-in sources nvm instead;
   - an explicit `NVM_DIR` that doesn't exist makes install.sh exit 1, hence step 1;
   - `METHOD=script` makes the download method deterministic (curl or wget) instead of depending on whether git is installed.
3. As the user, run one `bash -s` invocation **without errexit**, with a fixed script (not interpolated) on its stdin, so nothing has to survive in `~/.cache` between runs: `. "$NVM_DIR/nvm.sh"; nvm install 26; nvm alias default 26; node -v && npm -v`.
4. Write `<home>/.bashrc.d/50-kiln-nvm.sh`: `export NVM_DIR="$HOME/.nvm"; [ -s "$NVM_DIR/nvm.sh" ] && . "$NVM_DIR/nvm.sh"; [ -s "$NVM_DIR/bash_completion" ] && . "$NVM_DIR/bash_completion"`.

**Check:** `<home>/.nvm/nvm.sh` exists, `nvm version default` resolves to v26.x, and `node -v` reports v26 (all run through the same no-errexit bash form).

**Gotchas:**
- Never source nvm under `set -e`.
- nvm v0.41.0 (released 2026-11-04) is the latest nvm, and Node 26 is LTS ("Lithium", since v26.13.0 on 2026-10-28). Both are the owner's pinned choices.
- If `XDG_CONFIG_HOME` were set, nvm's default dir would move; the user-run rule's clean environment prevents that.

---

## 22. python

**ID** `python` · dev · depends: devtools · user parts run as the user

**Actions:**
1. apt: `python3 python3-venv python3-dev pipx libxml2-dev libxslt1-dev libffi-dev` (Python 3.13.5, pipx 1.7.1). lxml ships wheels, so the `-dev` libraries are only needed for source builds. They're kept because the owner asked for them.
2. uv (**not packaged in trixie**): as the user, `pipx install uv`, which puts it in `<home>/.local/bin`. A re-run exits 0 with "already seems to be installed". The fallback option `installer` uses the official script via `a pinned `sys.Fetch` download run with `bash <file>` as the user` (pinned, `UV_NO_MODIFY_PATH=1`).
3. Venv: `uv venv --python /usr/bin/python3 <home>/.venvs/automation`, **skipped when `<home>/.venvs/automation/pyvenv.cfg` exists** (`uv venv` errors on a non-empty path). Then `uv pip install --python <home>/.venvs/automation requests "httpx[http2]" aiohttp tenacity beautifulsoup4 lxml selectolax parsel scrapy playwright selenium pandas pyyaml python-dotenv rich typer ipython`. Every package has cp313 or pure wheels for linux x86_64, and re-running is a no-op.
4. Playwright (1.64 supports Debian 12 and 13):
   - as the user, `<venv>/bin/playwright install-deps --dry-run chromium`, after kiln's `AptUpdate`;
   - exit 0 means nothing is missing;
   - exit 1 means kiln parses the indented package lines after `Missing system dependencies (N):` and installs them with `AptInstall`, through kiln's apt helper, never letting Playwright run apt itself (D25);
   - then, as the user, `<venv>/bin/playwright install chromium`.
5. `uv tool install` takes **one** package per call: `uv tool install ruff`, `uv tool install pre-commit`, `uv tool install httpie`. Binaries go to `<home>/.local/bin`.
6. Shell:
   - If `<home>/.bashrc` is missing, copy it from `/etc/skel/.bashrc`.
   - Append once (guarded by the marker line):
     ```bash
     # kiln: load ~/.bashrc.d
     if [ -d "$HOME/.bashrc.d" ]; then for f in "$HOME"/.bashrc.d/*.sh; do [ -r "$f" ] && . "$f"; done; unset f; fi
     ```
   - Write `<home>/.bashrc.d/50-kiln-python.sh`:
     ```bash
     case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *) PATH="$HOME/.local/bin:$PATH" ;; esac
     alias pyauto='. "$HOME/.venvs/automation/bin/activate"'
     ```
     Debian's skel `.profile` already adds `~/.local/bin` at login. The `case` guard avoids duplicates in terminals.

**Check:** `pyvenv.cfg` exists, `uv pip list --python <venv>` includes every package, `playwright install-deps --dry-run chromium` exits 0, the chromium browser directory exists under `<home>/.cache/ms-playwright/`, `uv tool list` shows the three tools, and the marker and drop-ins are present.

---

## 23. go

**ID** `go` · dev · depends: devtools

**Actions:**
1. Resolve the version. With option `latest`, read `https://go.dev/dl/?mode=json` and take the first release with `stable: true`, then the file with `kind=archive`, `os=linux` and `arch=amd64`. The filename and sha256 come from the JSON.
2. Download into the run's private download directory (architecture: "Files and downloads") and verify the sha256. Then extract into `/usr/local/go.kiln-new`, and swap: rename `/usr/local/go` to `go.kiln-old`, `go.kiln-new` to `go`, and remove `go.kiln-old`. That has the same result as go.dev/doc/install's `rm -rf` + extract, but an interruption never leaves the machine with no Go. Leftover `.kiln-new`/`.kiln-old` directories are removed on the next run. Skip this sub-step when `/usr/local/go/VERSION` already starts with the resolved version.
3. `/etc/profile.d/kiln-go.sh`: `export PATH=/usr/local/go/bin:$PATH:$HOME/go/bin`. `/usr/local/go/bin` goes **first** so it wins over `/usr/bin/go` if `golang-go` is also installed. The same lines go in `<home>/.bashrc.d/50-kiln-go.sh`, for terminals that aren't login shells.
4. As the user, **four separate** runs of `/usr/local/go/bin/go install <path>@latest`, for:
   - `golang.org/x/tools/gopls`;
   - `github.com/go-delve/delve/cmd/dlv`;
   - `honnef.co/go/tools/cmd/staticcheck`;
   - `golang.org/x/vuln/cmd/govulncheck`.

   One `go install` can't take several modules ("all arguments must refer to packages in the same module at the same version"). Each tool is skipped if `<home>/go/bin/<tool>` exists, and the option "update tools" re-runs them.
5. Fallback (option on): if go.dev can't be reached, install `golang-go` from **trixie-backports** (1.27), or from main (1.24) if backports is off, and note the version. Run `go install` with `GOTOOLCHAIN=local`. Debian keeps `GOTOOLCHAIN=auto`, so a tool needing a newer Go would otherwise try to download a toolchain from the same network that just failed. Skip, with a note, any tool whose `go` directive is newer than the installed Go.

**Check:** `/usr/local/go/bin/go version` matches the resolved version (or, with the fallback, `golang-go` is installed), and the four tools exist in `<home>/go/bin`.

---

## 24. firmware-updates

**ID** `firmware-updates` · hardware · depends: apt-sources

**Applies:** always. UEFI capsule updates need an ESRT and an ESP at `/boot/efi`, `/boot` or `/efi`. Without them, only non-UEFI devices can update.

**Actions** (fwupd 2.0.20; flags and exit codes checked in the source, since the man page leaves many out). **This step can never reboot the machine.** With `--no-reboot-check` (and with stdin not a TTY, which kiln guarantees), fwupdmgr returns before its "Restart now?" prompt; `-y` doesn't apply to that prompt; and the only reboot path, logind `Reboot`, goes through that prompt.
1. Install `fwupd fwupd-amd64-signed udisks2`. The signed EFI binary is needed under Secure Boot. The LVFS remote is enabled by default on Debian.
2. `fwupdmgr refresh --force`.
   - Exit 0 is OK.
   - Exit 101 means the network is unreachable: the step counts as done, with a note that LVFS couldn't be reached.
   - Without `--force`, recent metadata returns 2.
3. `fwupdmgr get-updates --json`. Exit 0 means updates are available. Exit 2 means nothing to do: stop here, `done`.
4. `fwupdmgr update -y --no-reboot-check --no-device-prompt`.
   - Exit 0 or 2 is OK.
   - Anything else becomes a note, not a failure (for example "on battery", or a device that refuses).
5. If `fwupdmgr check-reboot-needed --json` exits 0, add the reboot reason "firmware update staged".

**Check:** fwupd is installed, and either this run's update returned 0 or 2, or a reboot is pending. So every kiln run checks LVFS again and reports done, by design (D51). `get-updates` is deliberately not the check, because a device whose update can never apply would otherwise keep the step "not done" forever.

---

## 25. summary

**ID** `summary` · always runs · implemented as the runner's final phase, after the end-of-run tasks (not a registered step; D42 implementation notes)

**Actions:** for each step, show done/unchanged/failed/skipped and why. Show the collected notes. Reboot needed: yes/no + reasons. The wizard offers a reboot and never reboots on its own. "Reboot now" first flushes and fsyncs `state.json`, the summary and the log, restores the runtime masks, and **stops kiln's own inhibitor** (it blocks shutdown), and only then runs `systemctl reboot`. Before writing the summary, kiln runs the end-of-run tasks: the deferred needrestart pass and the restore of automatic updates and PackageKit (D41). Write `/var/log/kiln-summary.txt` (0644, no secrets), the full log at `/var/log/kiln.log` (0600), and `/etc/kiln/state.json` (0600: this run's choices, battery result, power manager; never secrets). A generated secret was already shown once by the remote-desktop step. The summary names its file.
