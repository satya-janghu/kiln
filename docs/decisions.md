# Decisions log

## Current state (2026-11-07)

Where an older entry conflicts with D54, D54 wins.

**Scope and direction**
- kiln is a one-time setup tool for fresh Debian 13 installs; it should leave nothing running and ask few questions (D54).
- **Approach B: a small rewrite (D54, decided 2026-11-08).** Its scope, which steps and options survive, is proposed in D55.
- No profiles or headless mode; the wizard is the only way to apply (D27, D33).
- Run at the console in person; risky changes are checked and reverted in-process (D28).
- TUI: responsive, console-safe (D32), Windows XP text-mode Setup look (D42).

**Installer and desktop**
- The Debian installer owns the desktop and blends; kiln only detects them (D29). GNOME or no desktop, nothing else (D22).

**Packages and updates**
- Every package auto-updates every 12 hours, no automatic reboot (D40).
- Non-free and non-free firmware always on (D52). `gh` from GitHub's apt repo (D18). uv via `pipx` (D11). Playwright deps through kiln's apt helper (D25). timesyncd (D7). zram-generator (D8).

**Kernel and NVIDIA**
- Stock trixie kernel, latest, never pinned; backports kernel opt-in (D38).
- NVIDIA source per machine: NVIDIA latest (Turing+) or Debian stock (D39). Secure Boot: MOK enrollment queued (D24).

**SSH and sudo**
- ssh step installs sshd; password logins for user and root from everywhere (D45), On/Off only (D51).
- sudo works without re-login (D45). fail2ban bans via nftables (D9, D50).

**Power**
- Balanced everywhere: ppd `balanced` with GNOME, tuned `balanced` without (D52, D5). Thermal only checks the profile (D6, D52). GNOME no-suspend keys locked (D23). kiln owns the battery limit (D12).

**Mirrors**
- APT `mirror+file:` method (D10). Country from the timezone (D21). No neighbours; ranked by measured speed only (D52).

**Removed**
- Firewall, remote desktop, SSH key options, TLP; Wi-Fi power saving left default (D50).
- Network migration (D52). Fallback kernel tracking (D38).

**Process and quality**
- Review to sign-off, no tests (D30); static analysis (D34); read-only field check (D46).
- Plan and run share one path (D2). Backups under `/var/lib/kiln/backup/` (D36). Process model (D41).

**D54 answers:** most were re-run edge cases and are moot under the rewrite (2026-11-08). What survives depends on the D55 scope.

## How to read this log

Each entry has a date, the decision and its status. Superseded and fully implemented entries are shortened to the decision itself.

**Status:** every entry carries one of these.
- **decided:** you made the choice, either in the brief, by answering a question, or by explicit instruction. Code may rely on it.
- **proposed:** I recommended it, or chose it while fixing something, and you haven't approved it. Nothing may be built on a proposed entry until you approve or change it.
- **forced:** the facts leave only one way to meet what you asked for. These are flagged, and you can still overrule them.

Proposals P1–P18 at the bottom are choices I made on my own during the second verification pass and the field test.

## Log

### D1 · 2026-11-07 · Dependency versions (verified against the Go module proxy)

**Status: decided** (the stack is from your brief; the versions are verified facts).

| Dependency | Module path | Version |
|---|---|---|
| Go | — | 1.27.3 (`go.mod` `go 1.27`) |
| Bubble Tea | `charm.land/bubbletea/v2` | v2.1.1 |
| Bubbles | `charm.land/bubbles/v2` | v2.2.1 |
| Lip Gloss | `charm.land/lipgloss/v2` | v2.0.6 |
| Huh | `charm.land/huh/v2` | v2.0.3 |
| cobra | `github.com/spf13/cobra` | v1.10.2 |
| GoReleaser | — | v2 (`version: 2` config) |
| golangci-lint | — | v2 (`version: "2"` config) |

YAML (`go.yaml.in/yaml/v4`) was removed by D27; state uses stdlib `encoding/json`. Re-check with `go list -m -versions` when adding a dependency. CI actions: `actions/checkout@v7`, `actions/setup-go@v7`, `golangci/golangci-lint-action@v9` (golangci-lint v2.15.0). Bubble Tea v2 API notes: see bubbletea `UPGRADE_GUIDE_V2.md` (`View() tea.View`, `tea.KeyPressMsg`, Lip Gloss `LightDark`).

### D2 · Plan and run share one path via a recording `sys.System`

**Status: decided** (from your brief). `Apply` runs against `sys.Recorder` for `kiln plan` and the review screen. Rejected: separate plan text (drifts), per-step dry-run flag (easy to forget).

### D3 · Firewall: firewalld

**Superseded by D50** (firewall step removed). Was: firewalld with nftables backend, Docker zone integration, LAN-only zones.

### D4 · Networking: migrate to NetworkManager

**Superseded by D52** (network-migration removed). Was: migrate ifupdown machines to NetworkManager for Cockpit's networking page.

### D5 · Power manager: power-profiles-daemon with GNOME, tuned without it

**Status: decided**, accepted 2026-11-07; follows the *detected* installer choice (D29); profile is now `balanced` (D52). ppd with GNOME, tuned without. Never both, never TLP.

### D6 · Thermal defers to the power manager

**Status: decided**, accepted 2026-11-07; profile is now `balanced` (D52). kiln never writes `platform_profile`; ppd or tuned sets it and kiln verifies. There is no `kiln-thermal.service` (P12); only the ThinkPad thinkfan boot helper remains.

### D7 · Time sync: systemd-timesyncd

**Status: decided**, accepted 2026-11-07. Debian's default. chrony stays an option (replaces it via `time-daemon` Conflicts), for serving time to the LAN or flaky links.

### D8 · zram: systemd-zram-generator

**Status: decided**, accepted 2026-11-07. Debian-integrated, declarative `/etc/systemd/zram-generator.conf`; kiln adds `zstd`. zram-tools rejected as the older approach.

### D9 · fail2ban keeps Debian's nftables ban action

**Status: decided**, accepted 2026-11-07. Debian's defaults (`banaction = nftables`, `backend = systemd`) are kept. The firewalld half is moot (D50).

### D10 · Mirrors: APT mirror method, no apt-fast

**Status: decided** (from your brief). `mirror+file:` with `priority:N` tags gives parallel downloads and failover natively. apt-fast/aria2 and netselect-apt rejected.

### D11 · uv via `pipx install uv`

**Status: decided**, accepted 2026-11-07. uv isn't packaged in trixie; pipx is. The official installer remains an option.

### D12 · Battery: kiln owns the limit; UPower's hwdb is aligned to the same values

**Status: decided**, accepted 2026-11-07. kiln writes hwdb `CHARGE_LIMIT` to its accepted values, re-applies on boot, on power-supply uevents and every 15 min, and tells the user to leave GNOME's toggle alone (UPower's disable path writes 0/100). The re-apply helpers are under review in D54.

### D13 · Backports kernel is its own step (`kernel`)

**Status: decided**, accepted 2026-11-07. Separate toggle, own reboot reason. Opt-in since D38.

### D14 · Release checksum file is named `checksums.txt`

**Status: decided**, accepted 2026-11-07. A fixed name lets install.sh use `releases/latest/download/` URLs.

### D15 · 2026-11-07 · Repository is `github.com/satya-janghu/kiln`

**Status: decided** (your answer). Module path, release URLs and GoReleaser use it. Install URL moved to GitHub Pages (D31).

### D16 · Docker published ports are left reachable; kiln warns

**Moot after D50** (no firewall). Was: `firewall.strict_docker_ports` off by default, with a summary warning that published ports bypass the firewall.

### D17 · Backports kernel plus a maintained stable fallback

**Superseded by D38.** Was: keep the newest stable 6.12 kernel installed and manual, with a weekly `kiln kernel fallback` timer.

### D18 · `gh` comes from GitHub's official apt repo

**Status: decided** (your answer, 2026-11-07). `devtools.gh_source: github` (default) adds `cli.github.com/packages` (deb822 + signed-by keyring). Debian's 2.46 stays an option.

### D19 · NM migration: a new DHCP address is success, not failure

**Superseded by D52** (network-migration removed). Was: a passing local probe on a new address keeps NM and shows the new IP; the old DHCP client ID was copied first.

### D20 · "LAN" means RFC1918 IPv4 + IPv6 ULA on physical NICs

**Status: decided** (your answer, 2026-11-07). Global IPv6 prefixes are never LAN. Now used for fail2ban's `ignoreip` (D50); the Cockpit/RDP part is moot.

### D21 · Mirror country comes from the timezone, never geo-IP

**Status: decided** (your answer, 2026-11-07). `mirrors.country: auto` maps the timezone through compiled-in zone1970.tab (and zone.tab, D43). Unknown timezone: worldwide sample + deb.debian.org. The neighbour table is gone (D52).

### D22 · GNOME or no desktop; nothing else

**Status: decided** (your answer, 2026-11-07); the installer makes the choice and kiln detects it (D29). KDE out of scope. Remote desktop removed (D50). ppd with GNOME, tuned without.

### D23 · GNOME no-suspend keys are locked

**Status: decided** (your answer, 2026-11-07). `power.lock_gnome_settings` (on) adds a dconf `locks/` file so GNOME Settings can't re-enable automatic suspend.

### D24 · NVIDIA + Secure Boot: prepare the MOK enrollment and tell the user

**Status: decided** (your answer, 2026-11-07). The driver is installed and MOK enrollment is queued with a one-time password shown in the summary. Until enrolled at the console, the NVIDIA module won't load, but the machine boots normally.

### D25 · Playwright system deps are installed through kiln's apt helper

**Status: decided** (your answer, 2026-11-07). kiln gets the package list from Playwright, installs it with its non-interactive apt helper, then runs `playwright install chromium` as the user. Never `--with-deps`.

### D26 · `kiln secrets show [name]`

**Status: decided** (your answer, 2026-11-07). Root-only; prints a stored secret from `/etc/kiln/secrets/`, never logs it. Subcommands go under D54 approach A.

### D27 · 2026-11-07 · No profiles, no headless mode

**Status: decided** (your instruction). Every machine is set up individually in the wizard. Removed: YAML profiles, `--config`, `KILN_*` presets, "save profile", `kiln run --yes`, `--reboot`, install.sh's no-TTY branch. Last choices are kept in `/etc/kiln/state.json` and pre-filled (under review in D54). `kiln plan` remains as a read-only preview.

### D28 · Run at the console, in person; no SSH use

**Status: decided** (your instruction, 2026-11-07). kiln warns on an SSH session but doesn't refuse. Remote-lockout rails (rollback timers, "press a key" confirm, two-stage SSH port change) are replaced by `internal/revert`: validate, record `pending`, apply, check locally, revert from backups on failure; the next start offers to revert if kiln died mid-change. Since D52, SSH is the only risky change.

### D29 · The Debian installer owns the desktop, the SSH server and blends

**Status: decided** (your instruction, 2026-11-07); D45 reverses it for the SSH server. kiln detects and never installs or switches them. The `desktop` step is removed. GNOME-only parts (power settings, Flatpak/GNOME Software, GStreamer, desktop-only extrepo repos) apply only when GNOME is detected.

### D30 · No testing: review to sign-off

**Status: decided** (your instruction, 2026-11-07). No unit tests, golden files, fixtures, `kiln facts --dump`, container checks (`make test-pkgs`, `make test-int`), hardware test runs or `make deploy`. Quality comes from verifying every fact against primary docs, static analysis, and `/review-step`, `/check-packages`, `/audit` until both sign off ([review-process.md](review-process.md)). `Facts` stays the single detection layer (no configurable root); the Recorder stays.

### D31 · Short install URL on GitHub Pages

**Status: decided** (your instruction, 2026-11-07). `wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash`. Pages deploys from `site/` via the release workflow; `docs/` isn't published.

### D32 · Responsive, console-safe TUI

**Status: decided** (your instruction, 2026-11-07). 80x24 minimum, stacked panes below 120 columns, side by side from 120. Console glyph set (ASCII, 16 colors, no braille spinner) for `TERM=linux`.

### D33 · The wizard is the only way to apply changes

**Status: decided** (your instruction, 2026-11-07). Read-only: `kiln plan`, `kiln facts`, `kiln battery status`. Helpers (`battery apply|watch`, `thermal apply`) and `mirrors bench --apply|reset` only re-apply or reset what the wizard chose. `kernel fallback` removed by D38.

### D34 · Static analysis stays, as review tooling

**Status: decided** (your answer). `make lint` (go vet, golangci-lint v2: errcheck, staticcheck, gosec, govet, ineffassign, unused, errorlint; shellcheck on install.sh), `make vuln`, `make release-check`. Run on every push in CI, must be clean before sign-off. Static only: nothing executed, no test code.

### D35 · 2026-11-07 · NVIDIA machines keep the stable kernel by default

**Superseded by D38.** Was: backports kernel starts off on NVIDIA machines, because `nvidia-kernel-dkms` 550 documents fixes only up to 6.17.

### D36 · Backups live in `/var/lib/kiln/backup/`, never next to the file

**Status: decided**, accepted 2026-11-07 (P2 option B). `*.kiln-bak` files in `sources.list.d/`, `apt.conf.d/` or `sudoers.d/` are read or warned about. Backups mirror the absolute path and keep mode, owner and timestamps.

### D37 · 2026-11-07 · Second verification pass: corrections the facts forced

**Status: forced, implemented.** The earlier catalog text couldn't work as written on trixie; each fix keeps what you asked for. Items for removed steps (network-migration, firewall, remote-desktop) and the kernel fallback are moot. Still relevant:
- **Mirrors:** test file `Contents-all.gz`; top mirrors share equal priority (for parallel bandwidth); HTTP-only URLs; freshness compares InRelease dates per suite.
- **apt-sources:** kiln writes `debian.sources` itself, comments out cdrom lines, runs modernize with `-y`.
- **autoupdates:** `#clear` on origins; backports origin included; check uses the apt-daily timers. (Scope later widened by D40.)
- **apt helper:** waits for apt-daily services; `UCF_FORCE_CONFFOLD`, `LC_ALL=C`, `APT_LISTCHANGES_FRONTEND=none`.
- **sudo:** `visudo -c` on the whole policy. **upgrade:** reboot detection doesn't rely on `/run/reboot-required`.
- **ssh:** `sshd -T -C`; warning on cumulative `Port` lines. **fail2ban:** `journalmatch` includes `sshd-session`. **cockpit:** recommends kept.
- **devtools:** gh keyring sha256 checked. **go:** separate `go install` runs; `GOTOOLCHAIN=local` offline. **python:** skip existing venv, one `uv tool install` per tool, parse Playwright dry-run. **node:** `NVM_DIR`, `METHOD=script`.
- **firmware:** trixie's split Intel/NVIDIA firmware packages; `update-initramfs -k all`. **kernel:** `-t trixie-backports`.
- **multimedia:** libdvd-pkg `build` question; MOK prepared before the NVIDIA install. **fwupd:** real exit codes and flags.
- **power:** logind long-press keys; `systemctl reload gdm3`; greeter keys uncommented. **thermal:** thinkfan validated with `timeout`, at boot if `fan_control` isn't active; kiln doesn't write `platform_profile`.
- **battery:** udev `RUN` rule; no `RemainAfterExit`; Dell clamps silently; ASUS read-back cached; UPower hwdb match key; `inhibit-charge-awake` only where available.
- **zram:** restart guard (P11). **install.sh:** `/dev/tty` open test; `cd` before `sha256sum`.

### D38 · 2026-11-07 · Kernel policy: stock trixie kernel, always latest, never pinned

**Status: decided** (your instruction, 2026-11-07).
- Every machine runs Debian's stock `linux-image-amd64` from trixie (6.12 LTS), kept latest by the `upgrade` step and unattended-upgrades.
- The backports kernel is used **only when you select it** per machine; its option defaults to **off**, the one exception to "everything on".
- No kernel is pinned, held or marked manual; no fallback tracking. Old kernels: Debian's normal autoremove protection.
- Removes D17's timer and `kiln kernel fallback`; supersedes D35, P1, P14.

### D39 · 2026-11-07 · NVIDIA driver: you choose the source per machine

**Status: decided** (your answers, 2026-11-07).
- **Two sources,** chosen in the wizard per machine with an NVIDIA GPU:
  - **NVIDIA latest:** NVIDIA's Debian 13 repository; default for Turing and newer (e.g. the RTX 2070 Super desktop).
  - **Debian stock:** trixie's `nvidia-driver` 550; the only choice for pre-Turing (e.g. the GTX 970, Maxwell).
- **NVIDIA latest:** packages `nvidia-driver nvidia-kernel-open-dkms` (no CUDA); no branch pinning; repo written as deb822, key taken from `cuda-keyring` (checksum + fingerprint verified, never installed as a package).
- **Flagged for your review:** "NVIDIA latest" as the Turing+ default (from your "NVIDIA's 620 repo" answer); GPU generation detected from the PCI ID via NVIDIA's compatible-GPU table.
- **Forced:** switching back to Debian stock is a downgrade, so kiln explains it rather than automating it.
- **Not adopted:** the Debian 12 repo with signature checks disabled.

Supersedes the former open question 1.

### D40 · 2026-11-07 · Every package updates automatically every 12 hours

**Status: decided** (your instruction, 2026-11-07). Replaces the brief's "security only by default"; the `security` scope stays as an option.
- unattended-upgrades with `Origins-Pattern "origin=*"`: Debian, backports (installed packages only), Docker, GitHub CLI, Chrome, NVIDIA.
- `Update-Package-Lists` and `Unattended-Upgrade` set to `always`; refresh about 06:00 and 18:00, upgrade 30 min later. Automatic reboot off.
- **Flagged for your review:** the 06:00/18:00 times (Debian's `apt-daily.timer` slots), the 30-minute gap, the 15-minute random delay.
- **Facts:** apt services skip on battery (`ConditionACPower=true`); kernel and NVIDIA updates need a reboot; a Docker update restarts the daemon; with no branch pinning (D39), a new NVIDIA branch can arrive unattended.

### D41 · 2026-11-07 · Ordering, concurrency and interruption safety (audit)

**Status: decided, implemented.** Forced items fix real bugs; "my choice" items you accepted on 2026-11-07. Details: architecture.md, "Process model, ordering and interruption safety".

**Forced:** apt timers start only in the end-of-run restore; every post-change check polls until a deadline; battery writes share `/run/kiln/battery.lock` and UPower restarts before kiln writes; thinkfan stopped before validation; Go extracted to `go.kiln-new` and swapped by rename; the shutdown inhibitor is released last before reboot; interrupted dpkg repaired at start (`dpkg --configure -a`), revert prompt before the internet check; temp files created with final mode (`O_EXCL`) in a fresh 0700 `mkdtemp` dir. (Network-migration and firewall/Docker-restart fixes are moot.)

**My choice, accepted:**
1. Stop, wait for and runtime-mask `apt-daily*` and `packagekit.service` for the whole run.
2. Defer needrestart (`NEEDRESTART_SUSPEND=1`), one `needrestart -r a` pass at the end.
3. Run in its own `systemd-run --scope`, ignore SIGHUP; children in their own session with stdin `/dev/null`.
4. The inhibitor also blocks shutdown (`shutdown`, `handle-power-key`, block mode). Escapes: `systemctl reboot -i`, `--force`, holding the power button.
5. Warn on battery; ask below 30% without AC.
6. Single-instance lock `/run/kiln/kiln.lock`, per-helper locks, run record in `state.json`.
7. Per-command deadlines plus a 10-minute no-output watchdog that only reports. kiln never kills apt or dpkg.

### D42 · 2026-11-07 · TUI look: Windows XP text-mode Setup

**Status: decided** (your instruction and screenshot, 2026-11-07). Details in tui-design.md, "Look". Blue background, light grey text, double-line panels, yellow progress bar, light grey status bar with XP-style key hints, 16 console colors only.

Flagged for your review: `F3` quits; inverse rows mark the selection; bright red marks failed steps; console fallback uses `+=|` borders.

## Implementation notes (2026-11-07)

- **Summary is a runner phase,** not a registered step, because it runs after the end-of-run tasks.
- **Domain packages read sysfs through `sys.System`** (`internal/battery`, `internal/thermal`, `internal/mirrors`), the same read path facts uses.

### D43 · 2026-11-07 · Implementation choices

**Status: decided** (you accepted the remaining items on 2026-11-07). Choices made while writing code where the catalog left them open. Firewall, remote-desktop, network-migration and mirror-neighbour items are moot (D50, D52).

- **Dependencies (forced):** autoupdates and multimedia depend only on apt-sources.
- **ssh:** `password_logins` is On/Off only (D51).
- **mirrors:** zone.tab used alongside zone1970.tab; deb.debian.org never listed twice; the list carries a timestamp for the 30-day freshness check.
- **Sysfs writes** go through `tee <attr>` with the value on stdin.
- **battery:** first battery's accepted values applied to all; requested stop/gap recorded so a changed option re-runs the ladder; method "none" is done until an interface appears; watch timer every 15 min.
- **system:** zram installs `systemd-zram-generator` if missing.
- **power:** a missing greeter power section is appended. (Performance/TLP items moot: D50, D52.)
- **thermal:** first detected sensor required, later ones optional; HP matched by DMI vendor.
- **firmware-packages:** Realtek matched by PCI vendor 10ec or r8169/r8152/rtw88/rtw89; isenkram always installed (D52).
- **firmware-updates:** fwupdmgr on every run; a refused update becomes a note (D51).
- **NVIDIA:** Turing+ check from a 299-ID table (README 620.70.06), IDs ≥ 0x2900 are Blackwell (Debian stock unavailable); options declared only with an NVIDIA GPU; `dkms` before `dkms generate_mok`; MOK password via stdin to `mokutil --generate-hash`, `--list-new` dropped; password shown once and stored in `/etc/kiln/secrets/mok`, never in a note.
- **VA-API:** Intel driver for any Intel GPU, with a pre-Gen8 note; `vainfo` for every GPU (D51).
- **cockpit:** an installed backports cockpit is accepted, not downgraded. **docker:** signing key has no sha256. **devtools:** existing non-symlink `fd`/`bat` left alone with a note. **repos:** other extrepo names get their policy from `extrepo search`, no package installed.
- **TUI:** "dimmed" is normal grey; console progress fill `#`; prechecks never repair when the apply gate fails; a failed single-instance lock disables Start.

### D44 · 2026-11-07 · First code review round: fixes

**Status: forced, implemented.** Bugs from three read-only reviews, fixed to match the catalog and D41: inhibitor child dies with kiln; end-of-run tasks always run; quiesce undoes exactly what it changed; PackageKit handling; `revert.Begin` won't overwrite another step's pending change; atomic first-write backups and symlink handling; dpkg repair waits for locks; deadlines kill the process group; TUI signal handling survives a lost terminal; summary redacted; real skip reasons; `Cmd.Quiet` hides password hashes; SSH revert uses reload-or-restart with a free-port check; mirror/apt-sources restored when `apt update` fails; upgrade refreshes lists every run; battery watcher restores `charge_behaviour=auto`; DVD build failure doesn't block NVIDIA; failing thinkfan boot helper reported; preview never generates a MOK password; shared `~/.bashrc.d` loader. (Network-migration and firewall fixes moot.)

Left for you at the time: the NOPASSWD sudoers file is kept when passwordless sudo is turned off (you later answered "remove it", D54, moot under a one-time tool); TLP stopped not disabled (moot, D50).

### D45 · 2026-11-07 · SSH works with passwords for the user and root; sudo works without re-login

**Status: decided** (your request and answers, 2026-11-07). **Reverses** the brief's "key-only auth when keys exist, no root login", and D29 for SSH only.

- **ssh step, on by default:** installs `openssh-server` if missing, enables `ssh.service`; `PasswordAuthentication yes` and `PermitRootLogin yes` from everywhere (you chose this over LAN-only). Review screen and summary warn that password SSH for user and root is reachable wherever SSH is, with fail2ban as the only brake.
- **Root without a password:** if root's password is locked or empty, root gets the user's password hash (`chpasswd -e`, on stdin, never logged). If the user has none either, a note says to run `sudo passwd root`.
- **fail2ban** treats SSH as present whenever the ssh step is ticked. (Firewall part moot, D50.)
- **sudo step:** passwordless **off** writes `/etc/sudoers.d/90-<user>` with `<user> ALL=(ALL:ALL) ALL`, validated with visudo; **on** writes the NOPASSWD rule. Either way sudo works without logging out.
- **Flagged for your review:** option names `root_login` and `root_password`; "Password logins" defaults to `on` (now On/Off only, D51).

### D46 · 2026-11-07 · Read-only field check on real hardware

**Status: decided** (you offered a test machine; the script is review tooling, not a test suite, D30).
- `scripts/field-check.sh` collects read-only evidence (review-process.md, "Field check"); shellcheck-ed by `make lint` and CI.
- Test machine 10.10.10.150: fresh Debian 13 server, no desktop, Wi-Fi (AzureWave); root password set, so no `sudo`.
- The owner runs the commands; Claude doesn't log in with passwords.

Observations are in debian13-notes.md, "Field observations".

### D47 · 2026-11-07 · Field-test fixes (forced)

**Status: forced, implemented.** Firewall and network-migration fixes are moot (D50, D52). Still relevant: LAN subnets skip /32 and /128 host routes; `MachineName()` drops a repeated vendor and DMI placeholders; step failures show `action.Short` instead of the whole error chain.

### D48 · 2026-11-07 · Network-migration undo uses ifupdown's own units (P18 option A)

**Superseded by D52** (network-migration removed). Was: undo via `systemctl restart ifup@<iface>.service` (allow-hotplug), `networking.service` (auto-only), or plain `ifup`.

### D49 · 2026-11-07 · Network-migration undo waits for Wi-Fi to be released and for ifup to finish (forced)

**Superseded by D52** (network-migration removed, along with the registered restore steps). Was: wait for `/run/wpa_supplicant/<iface>` to go and poll `ifup@` until it finished.

### D50 · 2026-11-07 · Firewall and remote desktop dropped; no SSH key options; no TLP handling; Wi-Fi power saving left alone

**Status: decided** (your answers on 2026-11-07).
- **Firewall step removed.** A fresh Debian install has no active firewall and kiln leaves it that way (the brief's step 14 asked for firewalld; you've decided against it).
  - fail2ban keeps banning through nftables (trixie's `defaults-debian.conf` sets `banaction_allports = nftables[type=allports]`); its `ignoreip` uses the auto-detected LAN subnets.
  - Cockpit listens on 9090 on every network. ssh, docker and cockpit no longer depend on a firewall step. The review screen loses its Firewall tab.
  - Machines where an earlier run installed firewalld keep it.
- **Remote-desktop step removed** (brief's step 19). The only stored secret left is the Secure Boot one-time password.
- **SSH key options removed** (`ssh.github_user`, `ssh.extra_keys`). (The note that your own keys count for `auto` is moot: `auto` is gone, D51.)
- **TLP handling removed.** No trixie task installs it.
- **Wi-Fi power saving:** left at Debian's default.
- **Download pinning:** nothing more; kiln trusts the official sources the docs name.
- Saved choices for removed options are ignored with a log line.

### D51 · 2026-11-07 · First D43 walk-through

**Status: decided** (your answers on 2026-11-07).
- **SSH password logins:** On (default) and Off only; `auto` gone, a saved `auto` falls back to On with a log line.
- **Firmware updates:** fwupdmgr runs on every kiln run.
- **vainfo:** on every machine with a GPU, any vendor. Intel driver handling unchanged.
- **Network migration exceptions** stayed on ifupdown. (Moot, D52.)

### D52 · 2026-11-07 · Second D43 walk-through: no network migration, balanced power, no mirror neighbours, non-free always

**Status: decided** (your answers on 2026-11-07).
- **network-migration step removed.** Networking stays as the installer set it up (ifupdown on servers, NetworkManager on GNOME). Cockpit can show but not edit ifupdown interfaces. D48/D49's undo machinery went with it. SSH is the only risky change left.
- **Balanced power everywhere,** so an idle CPU doesn't sit at high clocks (the test box held 2.18 GHz idle on performance).
  - GNOME: ppd `balanced` (balanced platform profile; with amd-pstate, EPP `balance_performance` on AC, `balance_power` on battery).
  - No GNOME: tuned's stock `balanced` (schedutil, EPP balance_performance, `platform_profile=balanced`, boost on). The `kiln-performance` profile (P7) is removed where present.
  - The thermal step checks for `balanced` and the balanced EPP values; option renamed `thermal.check_profile`.
- **Mirrors:** no neighbouring-countries option, no UN sub-region table. Candidates: your country, a random worldwide sample, deb.debian.org, ranked only by measured download speed.
- **Non-free:** always on. `firmware-packages.nonfree_firmware` is gone; non-free firmware and the isenkram report are always included; apt-sources always enables `non-free non-free-firmware`.
- **Kept as they are:** battery, watcher and fallback choices; Intel driver for any Intel GPU; Secure Boot one-time password handling; Cockpit not downgraded, fd/bat left alone, other extrepo repos only enabled.

### D53 · 2026-11-07 · Step-review round: forced fixes

**Status: forced, implemented.** From the per-step reviews:
- **preflight (blocker):** under plain `su`, `/usr/local/sbin:/usr/sbin:/sbin` is put on PATH first.
- **revert:** a step with a pending change keeps its first snapshot.
- **ssh:** `reload-or-restart`; warnings on every run; warns when another file's `Port` means no port 22; correct undo advice.
- **sudo:** reports when an earlier NOPASSWD file still applies.
- **go:** interrupted swap restores `go.kiln-old`; "Update tools on this run" is per-run, never saved.
- **node:** nvm helper fed on bash's stdin.
- **multimedia:** libdvdcss2 build waits for the dpkg lock, a silent non-build is a note; DKMS on an older kernel doesn't count; Intel gets the non-free VA-API driver even if main's is present; headers follow the installed kernel metapackage's suite.
- **cockpit:** installs `cockpit-networkmanager` explicitly.
- **firmware-packages:** rtw88 matches as `rtw_*`; Wi-Fi cards also found on PCI by vendor; `update-initramfs` failures say what to do.
- **kernel:** Check requires the backports headers and firmware.
- **power:** stopped/disabled tuned or ppd is started; old kiln profile removed on both paths.
- **thermal:** not-applicable reason names a missing sensor.
- **battery:** only batteries that took the limit recorded; reapply writes nothing when thresholds hold; helpers log at Info.
- **apt:** wizard's lock wait applies the same run; "lists refreshed" resets per run; `Signed-By` checked only on modernize-converted files.
- **mirrors:** `bench --apply` and `reset` respect the apply gate and run lock.
- **Docs:** stale firewall/NM text removed; new V rows (dpkg version comparison, tuned and ppd balanced, chpasswd, PCI Wi-Fi IDs).

### D54 · 2026-11-07 · Direction reset: kiln is a one-time setup tool

**Status: decided.** You chose approach **B (small rewrite)** on 2026-11-08; the scope is proposed in D55. This is your direction from the end of the 2026-11-07 session. Read it before any other entry: where an older entry conflicts with it, this one wins.

**What you said kiln is (decided):**
- A **one-time** tool. You run it once on a fresh Debian install and never have to deal with it again. It is not something that lives on the machine, keeps running, or gets re-run to manage the system.
- Its whole job is to install the packages and set up the configuration you'd otherwise type by hand on every homelab machine, so you don't repeat the same commands half a dozen times. That includes configuring things like SSH.
- It could have been a bash script. The TUI and the per-step modularity exist only so the same tool fits both Debian servers without a desktop and Debian installs where you chose GNOME.
- **Fewer questions, fewer options.** Every extra question is a sign of over-engineering. Prefer Debian's defaults and the simplest behavior that does the job.
- "kiln" is just the app's name, not a separate framework. A rename (for example to something plainly descriptive) is open and cheap.

**How far today's code is from that (facts, from the code on 2026-11-07):**
- **kiln keeps running after setup:**
  - battery charge limit: `/etc/udev/rules.d/90-kiln-battery.rules`, `kiln-battery.service`, `kiln-battery.timer` (every 15 min), and `kiln-battery-watch.service`, all calling `/usr/local/bin/kiln battery …`;
  - ThinkPads: `kiln-thinkfan-enable.service`, calling `kiln thermal apply` at boot.
- **kiln keeps its own state:** `/etc/kiln/` (state.json with saved choices, allow-apply, secrets), `/var/lib/kiln/` (backups, pending reverts), `/var/log/kiln.log`.
- **Machinery that only exists for re-runs:**
  - saved choices pre-filled next time;
  - "unchanged" reporting;
  - the "revert the unfinished change?" prompt at the next start;
  - handling for options changed between runs;
  - the `kiln mirrors|battery|thermal|secrets` subcommands.
- Everything else kiln writes is ordinary Debian configuration that needs no kiln afterwards: apt sources, the sshd drop-in, the fail2ban jail, logind and journald drop-ins, the tuned profile choice, the update timers, the bashrc.d drop-ins and so on. That part is the job.

**Approaches (you chose B on 2026-11-08):**
- **A. One-shot, leave nothing behind.** Keep the wizard and the server/GNOME modularity, but strip the persistence:
  - no kiln units or helpers: the battery limit becomes a plain udev rule, the software charge-limit fallback (it needs a daemon) and the thinkfan boot helper go;
  - no saved choices, no revert prompt, no subcommands;
  - at the end kiln deletes its binary and `/etc/kiln`, leaving only the summary in `/var/log`;
  - steps still skip what's already done, so a run that failed halfway can simply be run again.

  Mostly deletions; most of today's verified and field-tested work carries over.
- **B. Rewrite much simpler (chosen).** A small tool (or a bash script with a server/GNOME switch) with far fewer steps and options. Smallest result. The verified facts (debian13-notes.md) and config contents carry over; the framework and the field testing don't.

**Your answers in the last review round (2026-11-07), recorded but not implemented**, because most of them only matter if kiln is re-run:

| Question | Your answer | Under a one-time tool |
|---|---|---|
| fail2ban when SSH is installed but the ssh step is unticked | skip it (keep current) | still applies (catalog text to match) |
| remove kiln's old NOPASSWD sudo file when passwordless is switched off later | remove it | moot (no later runs) |
| remove GitHub CLI's apt source when switching gh to Debian's later | remove them | moot |
| "No" to "revert the unfinished change?" | keep the change, stop asking | moot if the prompt goes (A) |
| write `Port 22` when another sshd file sets a Port | write it | moot: a fresh install has no other Port setting (2026-11-08) |
| thinkfan on Intel ThinkPads | AMD only (no change) | still applies |
| rotate `/var/log/kiln.log` | add logrotate | conflicts with "leave nothing behind" |
| mirror test: skip cloud-only (GeoDNS) mirrors, cap at 10 in-country | yes | still applies |
| thinkfan no longer depends on the power step | yes | still applies |
| remove battery helpers when the battery step is unticked later | yes | moot under A (no helpers) |
| warn when backports is switched off under a backports kernel | yes | moot |
| startup internet check: the catalog wins (HTTPS to deb.debian.org and github.com, 5 s each) | the catalog | moot: apt already fails clearly with no network (2026-11-08) |
| the remaining mechanical D43 items | accept them | D43 is now decided |

Earlier the same day you also decided D50–D52 (no firewall, remote desktop, network migration, SSH keys or TLP; balanced power; no mirror neighbours; non-free always). Those stand, and they already point the same way: less kiln, more Debian defaults.

### D55 · 2026-11-08 · Rewrite scope

**Status: proposed** (my recommendation, 2026-11-08; waiting for your cuts).

**Shape:** Go, one static binary, the same one-liner install. Three screens: a checklist, progress with live output, and a summary. The only choices are the step checkboxes; every value below is fixed. GNOME-only items appear only when GNOME is detected. Nothing is saved, nothing keeps running, and kiln leaves nothing of its own behind except one log of the run (`/var/log/kiln.log`). If a run fails halfway, running it again is safe, because apt installs and file writes are naturally repeatable. There is no special machinery for that.

**Keep (the job):**
1. sudo for your user, passwordless.
2. apt sources in deb822: main, contrib, non-free, non-free-firmware, security, updates, backports.
3. Full upgrade.
4. Firmware and CPU microcode for the detected hardware.
5. System: zram swap, a 500M journal cap, raised inotify limits, timesyncd. The hostname and timezone stay as the installer set them.
6. SSH: install the server, allow password logins for your user and root, and give root your user's password if root has none. Port 22, MaxAuthTries 4. `sshd -t` runs before the reload.
7. fail2ban for SSH (1 h ban, increasing, 5 tries).
8. Automatic updates: every package, every 12 h, never an automatic reboot (D40).
9. Always on: no sleep, lid ignored; GNOME's no-suspend settings locked.
10. Power profile balanced: power-profiles-daemon with GNOME, tuned without.
11. Battery stop at 80%: one plain udev rule where the kernel offers `charge_control_end_threshold`; otherwise a note.
12. Docker, with log rotation of 10 MB × 3.
13. Cockpit.
14. Dev tools: gh (GitHub's repo), node through nvm, the Python venv with uv, Playwright Chromium, ruff, pre-commit and httpie, and Go (latest) with its tools.
15. GNOME only: codecs, VA-API, Flathub, Google Chrome.
16. NVIDIA, picked automatically by GPU: Turing or newer gets NVIDIA's repo, Maxwell to Ada gets Debian's 550, and anything older stays on nouveau. With Secure Boot, MOK enrollment is queued and its password is shown once on the last screen.

**Drop:**
- mirror benchmarking (use deb.debian.org, Debian's CDN);
- the battery ladder, software fallback and helper services;
- the thermal check and thinkfan;
- the backports kernel step;
- DVD playback (libdvd-pkg);
- fwupd firmware updates (you earlier wanted these every run);
- every sub-option and free-text field.

**Drop (machinery):**
- saved choices and `/etc/kiln`;
- "unchanged" detection;
- the plan preview and review screen;
- revert snapshots and the revert prompt;
- subcommands;
- the systemd scope, apt-daily masking and the sleep inhibitor;
- the allow-apply file. In its place: refuse unless the system is Debian 13 amd64 and kiln runs as root, then a confirm before starting.

**Kept from the old safety rules:** apt runs non-interactive and waits for the dpkg lock, because unattended-upgrades often runs on first boot. Large downloads go to `/var/tmp`.

## Proposals from the second verification pass

You accepted all of these on 2026-11-07 as written (the "current text" option), except P1 and P14, which D38 replaces.

- **P1 · NVIDIA GPU + backports kernel (step 5b; D35):** superseded by D38 (backports kernel opt-in on every machine).
- **P2 · Backup location (D36):** accepted B, central `/var/lib/kiln/backup/`.
- **P3 · `sensors-detect --auto` dropped (step 12):** accepted B, not run (the brief asked for it).
- **P4 · "Update Go tools" defaults to off (step 23):** accepted A, off; per-run, never saved (D53).
- **P5 · "Backports firmware" option, on (step 5b):** accepted A, kept, default on.
- **P6 · `isenkram-cli` missing-firmware report (step 5):** accepted A, kept; always included since D52.
- **P7 · Custom tuned profile with `vm.swappiness=60` (step 11):** accepted A, then superseded by D52 (tuned's stock balanced).
- **P8 · thinkfan "disengaged" from 78 °C (step 12):** accepted A, keep the curve (level 7 by about 65–70 °C, disengaged from 78 °C).
- **P9 · fail2ban bans on all ports (step 16):** accepted A, all ports.
- **P10 · GNOME power button "ask what to do", locked (step 11):** accepted A, set and lock.
- **P11 · zram changes wait for next boot when a lot of swap is in use (step 10):** accepted A, wait.
- **P12 · `kiln-thermal.service` removed (step 12):** accepted A; the boot unit exists only for the ThinkPad thinkfan enable.
- **P13 · NetworkManager keeps `eui64` IPv6 (step 6):** accepted A; moot (network-migration removed, D52).
- **P14 · Older fallback kernels removed automatically (step 5b):** superseded by D38 (no fallback handling).
- **P15 · preflight installs `ca-certificates` and `pciutils` if missing (step 0):** accepted A, install them.
- **P16 · Remote-desktop certificate valid 10 years (step 19):** accepted A; moot (remote desktop removed, D50).
- **P17 · Cockpit's recommended packages kept (step 18):** accepted A, keep them.

## Proposals from the field test

- **P18 · Undo brings the interface back through `ifup@<iface>.service` (step 6):** approved option A 2026-11-07, implemented as D48; moot since D52 (network-migration removed).

## Open questions

1. **Closed (D50):** Wi-Fi power saving on always-on machines stays at Debian's default. (The test box showed 19–572 ms latency and 6.7% loss with power saving on, 3–7 ms and none with it off; the proposed NM `wifi.powersave = 2` option was not adopted.)

Open: the rewrite's scope (D55, proposed).
