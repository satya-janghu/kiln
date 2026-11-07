# Decisions log

Each entry has a date, the decision, why, and the alternatives considered. Superseded entries stay, marked as superseded.

**Status:** every entry carries one of these.
- **decided:** you made the choice, either in the brief, by answering a question, or by explicit instruction. Code may rely on it.
- **proposed:** I recommended it, or chose it while fixing something, and you haven't approved it. Nothing may be built on a proposed entry until you approve or change it.
- **forced:** the facts leave only one way to meet what you asked for. These are flagged, and you can still overrule them.

Proposals P1–P17 at the bottom are the choices I made on my own during the second verification pass.

> **Start here (2026-11-07):** D54 resets the direction. kiln is a one-time setup tool that leaves nothing running behind; the approach (A or B in D54) is still to be chosen.

## Log

### D1 · 2026-11-07 · Dependency versions (verified against the Go module proxy)

**Status: decided** (the stack is from your brief; the versions are verified facts).

| Dependency | Module path | Version |
|---|---|---|
| Go | — | 1.27.3 (latest stable; `go.mod` `go 1.27`) |
| Bubble Tea | `charm.land/bubbletea/v2` | v2.1.1 |
| Bubbles | `charm.land/bubbles/v2` | v2.2.1 |
| Lip Gloss | `charm.land/lipgloss/v2` | v2.0.6 |
| Huh | `charm.land/huh/v2` | v2.0.3 (built on Bubble Tea v2 / Lip Gloss v2; replaces `github.com/charmbracelet/huh`) |
| cobra | `github.com/spf13/cobra` | v1.10.2 (re-checked 2026-11-07 on the module proxy) |
| ~~YAML~~ | ~~`go.yaml.in/yaml/v4`~~ | **Removed by D27.** There are no profiles, and state uses stdlib `encoding/json`. |
| GoReleaser | — | v2 (`version: 2` config) |
| golangci-lint | — | v2 (`version: "2"` config) |

Re-check with `go list -m -versions` when adding each dependency.

CI actions (checked 2026-11-07 via the GitHub releases API): `actions/checkout@v7` (v7.0.1), `actions/setup-go@v7` (v7.0.0), `golangci/golangci-lint-action@v9` (v9.3.0) running golangci-lint v2.15.0.

Bubble Tea v2 notes for implementers: `View() tea.View` (`tea.NewView(s)`). Alt screen, mouse mode, cursor and window title are now `View` fields, not program options or commands. Match keys on `tea.KeyPressMsg` (`msg.Code`, `msg.Text`, `msg.Mod`). Space stringifies as `"space"`. `tea.Sequence` replaced `Sequentially`. Lip Gloss v2 has no implicit adaptive colors: handle `tea.BackgroundColorMsg` and use `lipgloss.LightDark(isDark)`. Source: bubbletea `UPGRADE_GUIDE_V2.md`.

### D2 · Plan and run share one path via a recording `sys.System`

**Status: decided** (from your brief).

`Apply` runs against `sys.Recorder` for `kiln plan` and the wizard's review screen. Alternatives: separate `Plan` text generation (drifts), or a dry-run flag inside each step (easy to forget).

### D3 · Firewall: firewalld

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

firewalld 2.3.1 (nftables backend) has first-class Docker integration (a `docker` zone plus a `docker-forwarding` policy, rules recreated on reload in Docker ≥ 28.1), a Cockpit page, zones with source CIDRs (which fit "LAN-only Cockpit/RDP"), and a runtime-vs-permanent split that makes verify-then-revert natural (apply runtime, check, `--runtime-to-permanent`). Plain nftables would mean hand-coexisting with Docker's iptables-nft rules, and Docker's own nftables backend is still experimental (29.x). nftables stays as an option (firewall backend `nftables`) for machines without Docker.

### D4 · Networking: migrate to NetworkManager

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

> The `systemd-run` rollback timer below is superseded by D28 (verify, then revert in-process). The step is now catalog step 6.

Cockpit's networking page needs NM, and the desktop tasks bring NM anyway, so one network owner is simplest. The migration is the `network-migration` step (6b), a new catalog step not in the original brief. It uses a `systemd-run` rollback timer. NM's checkpoint API isn't used, because it can't undo ifupdown state. Alternatives: leave ifupdown (Cockpit's network page broken, two owners) or systemd-networkd (Cockpit can't manage it).

### D5 · Power manager: power-profiles-daemon with GNOME, tuned without it

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

> Updated by D29: the choice follows the *detected* installer choice. There is no desktop option.

ppd 0.30 is what GNOME uses and integrate with (UI, D-Bus). It handles amd-pstate EPP and platform_profile itself. On desktop=none, `tuned` `throughput-performance` gives more server-oriented tuning, and there's no UI to integrate with. Never both, never TLP (ppd `Conflicts: tlp`).

### D6 · Thermal defers to the power manager

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

ppd writes `platform_profile` and adopts external writes, so kiln never writes the sysfs file when ppd is active. It sets the ppd profile to `performance` and verifies. `kiln-thermal.service` exists only for the tuned path, and only if tuned doesn't set the platform profile itself.

### D7 · Time sync: systemd-timesyncd

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

Debian's default, already installed, no daemon to serve, and enough for SNTP-client accuracy. chrony (option) replaces it via `time-daemon` Conflicts. Pick chrony only to serve time to the LAN or for better accuracy on flaky links.

### D8 · zram: systemd-zram-generator

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

It's already the Debian-integrated generator, it ships a sensible default (`min(ram/2, 4096)`), and it's configured declaratively in `/etc/systemd/zram-generator.conf`. kiln adds `zstd`. zram-tools is a shell-script service and is the older approach.

### D9 · fail2ban keeps Debian's nftables ban action under both firewall backends

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

Debian's defaults already use `banaction = nftables` + `backend = systemd`. The nftables action uses its own table, works alongside firewalld, and survives `firewall-cmd --reload`. That's fewer moving parts than `firewallcmd-rich-rules`.

### D10 · Mirrors: APT mirror method, no apt-fast

**Status: decided** (from your brief).

`mirror+file:` with `priority:N` tags gives parallel downloads across hosts and automatic failover, natively. apt-fast/aria2 adds dependencies and wraps apt. netselect-apt ranks by latency, not throughput.

### D11 · uv via `pipx install uv`

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

uv isn't packaged in trixie. pipx is, so this avoids a piped installer. The official installer remains an option.

### D12 · Battery: kiln owns the limit; UPower's hwdb is aligned to the same values

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

UPower's disable path writes 0/100, so kiln can't just switch it off. kiln writes hwdb `CHARGE_LIMIT` to its accepted values, re-applies on boot, on power-supply uevents and every 15 min, and tells the user to leave GNOME's toggle alone.

### D13 · Backports kernel is its own step (`kernel`)

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

It can be toggled separately in the TUI, and it has its own reboot reason.

### D14 · Release checksum file is named `checksums.txt`

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

GoReleaser's default is `kiln_<version>_checksums.txt`. A fixed name lets install.sh use `releases/latest/download/` URLs.

### D15 · 2026-11-07 · Repository is `github.com/satya-janghu/kiln`

**Status: decided** (your answer, 2026-11-07).

Module path, release URLs and GoReleaser all use it. (The install URL later moved to GitHub Pages, D31.)

### D16 · Docker published ports are left reachable; kiln warns

**Status: decided** (your answer, 2026-11-07).

`firewall.strict_docker_ports` defaults to **off**, so Docker's normal `-p` behavior stays. The summary always says that published ports bypass the firewall and that private services should be bound to 127.0.0.1 or a LAN IP. Turning the option on sets firewalld `StrictForwardPorts=yes`.

### D17 · Backports kernel plus a maintained stable fallback

**Status: superseded** by D38 (2026-11-07). There's no fallback tracking and no pinning any more: kernels follow the stock metapackage.

`kernel.track_stable_fallback` (on) keeps the newest stable `linux-image-6.12.*-amd64` installed and marked manual. A weekly `kiln kernel fallback` timer is needed because unattended-upgrades won't install a new ABI package name.

### D18 · `gh` comes from GitHub's official apt repo

**Status: decided** (your answer, 2026-11-07).

`devtools.gh_source: github` (default) adds `cli.github.com/packages` (deb822 + signed-by keyring). Debian's 2.46 stays an option.

### D19 · NM migration: a new DHCP address is success, not failure

**Status: decided** (your answer, 2026-11-07).

> Still true, but with no SSH session to lose (D28), it only means "show the new IP". Copying the client ID is kept so the IP stays stable for later port forwarding.

The confirm is a local probe (route + DNS + HTTPS). If it passes on a new address, keep NetworkManager, print the new IP on the console and in the summary, and record `done` with a note. Only a failed local probe rolls back. kiln still copies the old DHCP client ID first, to make an address change unlikely.

### D20 · "LAN" means RFC1918 IPv4 + IPv6 ULA on physical NICs

**Status: decided** (your answer, 2026-11-07).

Global IPv6 prefixes are never treated as LAN. Cockpit and RDP from the LAN go over IPv4 (or ULA).

### D21 · Mirror country comes from the timezone, never geo-IP

**Status: decided** (your answer, 2026-11-07).

`mirrors.country: auto` maps the timezone through a compiled-in zone1970.tab table plus a small neighbor table. An unknown timezone falls back to a worldwide sample + deb.debian.org.

### D22 · GNOME or no desktop; nothing else

**Status: decided** (your answer, 2026-11-07).

> Updated by D29: the installer makes this choice and kiln only detects it.

`desktop.environment` is `gnome | none`. KDE (and krdp) are out of scope. Remote desktop is "not applicable: no GNOME desktop installed" on machines without GNOME. ppd is used with GNOME and tuned with none.

### D23 · GNOME no-suspend keys are locked

**Status: decided** (your answer, 2026-11-07).

`power.lock_gnome_settings` (on) adds a dconf `locks/` file so GNOME Settings can't re-enable automatic suspend.

### D24 · NVIDIA + Secure Boot: prepare the MOK enrollment and tell the user

**Status: decided** (your answer, 2026-11-07).

The driver is installed and MOK enrollment is queued with a one-time password, which is shown in the summary. Until someone enrolls at the console on the next boot, the NVIDIA module won't load, but the machine boots normally.

### D25 · Playwright system deps are installed through kiln's apt helper

**Status: decided** (your answer, 2026-11-07).

kiln gets the package list from Playwright, installs it with its own non-interactive apt helper (rule 8), then runs `playwright install chromium` as the user. It never uses `--with-deps`.

### D26 · `kiln secrets show [name]`

**Status: decided** (your answer, 2026-11-07).

A root-only command that prints a stored secret from `/etc/kiln/secrets/`. It never logs what it prints.

### D27 · 2026-11-07 · No profiles, no headless mode

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

Every machine is set up individually in the wizard. Removed: YAML profiles, `--config` (path or URL), `KILN_*` presets, "save profile", headless `kiln run --yes`, `--reboot`, and the no-TTY branch of install.sh. The machine's last choices are kept in `/etc/kiln/state.json` and pre-filled on the next run. `kiln plan` remains as a read-only preview. This supersedes the brief's headless mode and D1's YAML row.

### D28 · Run at the console, in person; no SSH use

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

The owner is always physically at the machine. kiln warns if it detects an SSH session but doesn't refuse. The safety rails for remote lockout are removed: the `systemd-run` rollback timers, the "press a key within N seconds" confirm, and the two-stage SSH port change. They're replaced by `internal/revert`: validate, record `pending`, apply, check locally, and revert from backups straight away on failure. The next start offers to revert if kiln died mid-change.

### D29 · The Debian installer owns the desktop, the SSH server and blends

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

kiln detects them and doesn't install or switch them. The `desktop` step is removed. GNOME-dependent parts (GNOME power settings, remote desktop, Flatpak/GNOME Software, GStreamer, desktop-only extrepo repos) apply only when GNOME is detected. `ssh` and `fail2ban` apply only when an SSH server is detected. `network-migration` is expected to matter mainly on machines installed without a desktop.

### D30 · No testing: review to sign-off

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

Removed:
- unit tests, golden files, fixture trees and `kiln facts --dump`;
- container package checks (`make test-pkgs`) and container integration runs (`make test-int`);
- hardware test runs and `make deploy`.

Quality comes from verifying every fact against current primary docs, plus static analysis (`make lint`, `make vuln`), `/review-step`, `/check-packages` and `/audit` passes until both parties sign off ([review-process.md](review-process.md)). The `Facts` layer stays as the single place for detection, but it no longer has a configurable root. The Recorder stays because the review screen and `kiln plan` need it.

### D31 · Short install URL on GitHub Pages

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

`wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash`. Pages is deployed by the release workflow from `site/` (Actions source), so the `docs/` folder isn't published.

### D32 · Responsive, console-safe TUI

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

The layout fills the terminal: 80x24 minimum, stacked panes below 120 columns, side by side from 120. There's a console glyph set (ASCII, 16 colors, no braille spinner) for `TERM=linux`, because the first run usually happens on the bare Linux console.

### D33 · The wizard is the only way to apply changes

**Status: decided** (you asked for this on 2026-11-07, or approved it by asking me to update the docs with it).

Read-only: `kiln plan`, `kiln facts`, `kiln battery status`. The systemd helpers (`battery apply|watch`, `thermal apply`, `kernel fallback`) and `mirrors bench --apply|reset` re-apply only what the wizard already chose (recorded in `state.json`), or reset it.

### D34 · Static analysis stays, as review tooling

**Status: decided** (your answer: keep them if they're needed).

With no tests, `make lint` (go vet, golangci-lint v2 with errcheck, staticcheck, gosec, govet, ineffassign, unused, errorlint, plus shellcheck on install.sh), `make vuln` (govulncheck) and `make release-check` (goreleaser check) are the only mechanical checks. They catch classes of bugs that reading the code misses: unchecked errors, shadowed variables, unreachable or dead code, command-injection patterns, misuse of `errors.Is`/`As`, known-vulnerable dependencies, and shell quoting mistakes. They run on every push in CI and must be clean before sign-off. These are static checks only: nothing is executed against a system, and no test code is written.

### D35 · 2026-11-07 · NVIDIA machines keep the stable kernel by default

**Status: superseded** by D38 (2026-11-07). The stock trixie kernel is now the default on every machine, not just NVIDIA ones.

`nvidia-kernel-dkms` 550 documents fixes only up to Linux 6.17, and the backports kernel is 7.2. When an NVIDIA GPU is detected and the NVIDIA driver is on, the backports kernel option starts **off** on that machine, with the reason shown. Turning it on gives a warning on the review screen. Neither target laptop has an NVIDIA GPU.

### D36 · Backups live in `/var/lib/kiln/backup/`, never next to the file

**Status: decided** (proposed by me; you accepted it on 2026-11-07).

`*.kiln-bak` files inside `sources.list.d/`, `apt.conf.d/` or `sudoers.d/` are read or warned about by apt and sudo. Backups mirror the absolute path and keep mode, owner and timestamps.

### D37 · 2026-11-07 · Second verification pass: corrections the facts forced

**Status: forced.** These corrections were needed because the earlier catalog text couldn't work as written on trixie. Each one keeps what you asked for and changes only how it's done. You can still overrule any of them.

- **Mirrors:**
  - test file `Contents-all.gz`: `Contents-amd64.gz` is 12.7 MB, so the 40 MB range in your brief can't be served;
  - the top mirrors share equal priority, because distinct priorities send every file to mirror #1, which defeats your parallel-bandwidth goal;
  - HTTP-only URLs: the masterlist lists no HTTPS;
  - freshness compares trixie InRelease dates per suite.
- **apt-sources:** the installer writes one-line `sources.list`. kiln writes `debian.sources` itself, because `modernize-sources` would append to kiln's own file. cdrom lines are commented out first. modernize runs with `-y`.
- **autoupdates:** `#clear` on the origins, because Debian's file also allows point-release updates and your brief says security only. The backports origin, because the backports kernel you asked for would otherwise never get security fixes. The check uses the apt-daily timers, because `unattended-upgrades.service` is only a shutdown helper.
- **apt helper:** `DPkg::Lock::Timeout` covers only dpkg locks, so kiln also waits for the apt-daily services (your rule 9). `UCF_FORCE_CONFFOLD` covers ucf-managed files, which confold doesn't touch (your rule 8). `LC_ALL=C` is set for parsing, and `APT_LISTCHANGES_FRONTEND=none` for non-interactive runs.
- **sudo:** `visudo -c` on the whole policy, because visudo(8) says checking one include file isn't sufficient.
- **upgrade:** the reboot detection doesn't rely on `/run/reboot-required`, which nothing writes yet at that point.
- **network-migration:**
  - `ifdown` runs before the stanza is commented out; otherwise it does nothing;
  - `nmcli connection reload` runs;
  - the DHCP client ID is sent as hex, because NM's `duid` mode doesn't match dhcpcd;
  - the Wi-Fi password goes in a 0600 keyfile, because on the command line it's visible in `ps`;
  - the revert flushes addresses;
  - unsupported stanzas are excluded.
- **firewall:**
  - zones are written as permanent files plus `--reload` (`--new-zone` is permanent-only);
  - the custom SSH port goes in `kiln-lan` too, because LAN clients would otherwise be locked out;
  - installing firewalld starts it immediately;
  - the nftables backend must never use Debian's `nftables.service`, which flushes every table.
- **ssh:** `sshd -T -C`, because `-T` ignores `Match` without `-C`. A warning about cumulative `Port` lines. Dedupe on key type + blob.
- **fail2ban:** the `journalmatch` includes `sshd-session`, because a jail-level match overrides the filter's.
- **cockpit:** removed `--no-install-recommends`, which would have dropped the networking page you need.
- **remote-desktop:**
  - grdctl runs as the daemon user without `--system`, because the `--system` form always exits 0;
  - only the password goes on stdin, because sending both can crash grdctl;
  - `openssl -subj -noenc`;
  - `polkitd` is installed.
- **devtools:** the gh keyring sha256 is checked against GitHub's install doc.
- **go:** four separate `go install` runs (one invocation can't take several modules), and `GOTOOLCHAIN=local` on the offline fallback.
- **python:** `uv venv` is skipped when the venv exists (it errors otherwise). One `uv tool install` per tool. The Playwright dry-run output is parsed, because it doesn't print an apt command.
- **node:** `NVM_DIR` and `METHOD=script` are set explicitly.
- **firmware:** trixie splits Intel and NVIDIA firmware out of `firmware-misc-nonfree`. `update-initramfs -k all`, so the fallback kernel gets the firmware.
- **kernel:** `-t trixie-backports` is required for the backports kernel (it needs `linux-base` from backports). The fallback regex and `apt-mark manual` handling from this pass were later removed by D38.
- **multimedia:** libdvd-pkg's `build` question (the first-install question is only a note). MOK preparation runs before the NVIDIA driver install, so "skip if enrollment can't be prepared" can actually work.
- **fwupd:** the real exit codes and flags.
- **power:** the logind long-press keys (the suspend key long-press defaults to hibernate). `systemctl reload gdm3` (`dpkg-reconfigure` doesn't apply greeter changes). The greeter keys are uncommented.
- **thermal:** thinkfan has no config-test flag, so validation uses `timeout`, and runs at boot when `fan_control` isn't active yet. kiln doesn't write `platform_profile`, because ppd and tuned both set it.
- **battery:**
  - the udev `RUN` rule, because `SYSTEMD_WANTS` doesn't fire on charger events;
  - no `RemainAfterExit`;
  - Dell clamps silently;
  - the ASUS read-back is cached;
  - the UPower hwdb match key;
  - `inhibit-charge-awake` is used only on kernels that have it.
- **zram:** the restart guard runs because restarting the setup unit runs swapoff. Whether to restart at all is P11.
- **install.sh:** the `/dev/tty` open test (`-r` is always true), and `cd` before `sha256sum`.

### D38 · 2026-11-07 · Kernel policy: stock trixie kernel, always latest, never pinned

**Status: decided** (your instruction, 2026-11-07).

- Every machine runs Debian's stock trixie kernel (`linux-image-amd64` from trixie, 6.12 LTS). It's updated to the latest version through normal upgrades: the `upgrade` step and unattended-upgrades.
- The backports kernel is used **only when you select it** for a machine. Its option defaults to **off**, the one deliberate exception to "everything on".
- No kernel is pinned, held or marked manual, and there's no fallback tracking. Old kernels are handled by Debian's normal autoremove protection.
- This removes D17's fallback timer and the `kiln kernel fallback` command, and it supersedes D35, P1 and P14.

### D39 · 2026-11-07 · NVIDIA driver: you choose the source per machine

**Status: decided** (your answers, 2026-11-07).

- **Two sources,** chosen in the wizard for each machine with an NVIDIA GPU:
  - **NVIDIA latest:** NVIDIA's Debian 13 repository. It's the default for Turing and newer GPUs (e.g. the RTX 2070 Super desktop).
  - **Debian stock:** trixie's `nvidia-driver` 550. It's the only choice for pre-Turing GPUs (e.g. the GTX 970 desktop, Maxwell), because NVIDIA's repository supports Turing and newer only.
- **NVIDIA latest** works like this:
  - **Package set:** the desktop-only set `nvidia-driver nvidia-kernel-open-dkms` (no CUDA libraries).
  - **Branch:** no branch pinning, so it follows NVIDIA's newest branch.
  - **Repository:** written by kiln as deb822. The key comes from NVIDIA's `cuda-keyring` package, verified by checksum and fingerprint but never installed as a package.
- **Defaults I set, flagged for your review.** The "NVIDIA latest" default on Turing and newer comes from your earlier answer ("NVIDIA's 620 repo"). Detecting the GPU generation from the PCI device ID using NVIDIA's compatible-GPU table is how I propose to tell the two cases apart.
- **Forced by the facts:** switching from "NVIDIA latest" back to "Debian stock" is a package downgrade, so kiln explains it rather than automating it.
- **Not adopted from the Reddit workaround:** the Debian 12 repository with a disabled signature check, for Pascal and older. It weakens apt's signature checking, and "Debian stock" already covers those GPUs.

This supersedes open question 1.

### D40 · 2026-11-07 · Every package updates automatically every 12 hours

**Status: decided** (your instruction, 2026-11-07). It replaces the brief's "security updates only by default". The `security` scope stays available as an option.

- unattended-upgrades runs with `Origins-Pattern "origin=*"`, so every repository kiln configures is covered: Debian, backports (installed packages only), Docker, GitHub CLI, Chrome and NVIDIA.
- `APT::Periodic::Update-Package-Lists` and `Unattended-Upgrade` are set to `always`. Debian's timers are overridden so a refresh runs at about 06:00 and 18:00, and an upgrade 30 minutes later. Automatic reboot stays off.
- **Details I picked, flagged for your review:**
  - the 06:00/18:00 times, which reuse Debian's own `apt-daily.timer` slots;
  - the 30-minute gap;
  - the 15-minute random delay.
- **Facts you should know:**
  - Debian's apt services skip while running on battery (`ConditionACPower=true`).
  - Kernel and NVIDIA updates need a reboot to take effect.
  - A Docker update restarts the daemon.
  - With branch pinning off (D39), a new NVIDIA branch can arrive unattended.

This answers the former open question about NVIDIA updates.

### D41 · 2026-11-07 · Ordering, concurrency and interruption safety (audit)

**Status: decided.** You asked me to audit for races and interruptions and make suitable changes. The **forced** items fix real bugs. The **my choice** items were decisions I made under that instruction, and you accepted all of them on 2026-11-07. Details are in architecture.md, "Process model, ordering and interruption safety".

**Forced** (bugs or races in the design as it stood):
1. **Step 20 restarted the apt timers mid-run.** unattended-upgrades could then start while later steps still used apt. Now step 20 only writes the timer drop-ins; the timers start in the end-of-run restore.
2. **Post-change checks ran once.** `systemctl reload ssh` returns before sshd has re-executed, and sshd closes its listening sockets during the re-exec (sshd(8), systemd 257), so a single check could wrongly trigger a revert. Every check now polls until a deadline.
3. **Network migration had an autoconnect race.** `nmcli connection up` on a profile that's already activating tears it down and restarts it. The profile now starts with `autoconnect=false`, is brought up once with `nmcli --wait 90`, and is set to autoconnect only after the check passes. A leftover DHCP client is stopped first.
4. **Battery writes could race.** The udev/timer-triggered `kiln battery apply` could write at the same time as the battery step. They now share `/run/kiln/battery.lock`. UPower is restarted **before** kiln writes thresholds, not after.
5. **thinkfan validation while thinkfan was running** would have had two instances fighting over the fan. The running service is now stopped first.
6. **Docker restarts.** A firewall re-run restarted Docker (and its containers) even when nothing had changed. Now that happens only when this run changed the firewall config or `daemon.json`.
7. **Go install interrupted between `rm -rf` and extract** left no Go. It's now extracted to `go.kiln-new` and swapped by rename.
8. **"Reboot now" with kiln's own shutdown inhibitor still held** would be refused by logind. The reboot sequence now stops the inhibitor last, after flushing state and logs.
9. **Interrupted dpkg** (power loss, kill) wasn't detected. preflight now repairs it (`dpkg --configure -a`) before anything else. The revert prompt runs before the internet check, because a half-migrated network would fail it.
10. **Temp files** created and then chmod-ed could expose a secret for an instant. The fixed `/var/tmp/kiln` path was open to pre-creation by another user. Now files are created with their final mode (`O_EXCL`), and downloads go to a fresh 0700 `mkdtemp` directory.

**My choice** (accepted by you, 2026-11-07):
1. **Quiet the other package managers.** For the whole run, kiln stops and waits for, then runtime-masks, `apt-daily*` and `packagekit.service`, so nothing else touches packages. The masks vanish at reboot. The alternative is to rely only on lock waiting, but then unattended-upgrades or GNOME Software could interleave changes between kiln's apt calls.
2. **Defer needrestart** with `NEEDRESTART_SUSPEND=1`, then do one `needrestart -r a` pass at the end. Debian's own exclusion list already protects gdm, logind, dbus, NetworkManager and docker. The alternative is Debian's default: restarts after every apt call, mid-run.
3. **Run in its own systemd scope.** kiln re-executes itself in `systemd-run --scope` (system slice), ignores SIGHUP, and keeps going if the terminal disappears. Children run in their own session (`Setsid`) with stdin `/dev/null`. The alternative is to stay a plain child of the terminal, where closing the window kills kiln mid-dpkg.
4. **Block shutdown in the inhibitor** as well (`shutdown`, `handle-power-key`, block mode). That refuses every logind reboot or poweroff during a run, root's included. Escapes: `systemctl reboot -i`, `--force`, or the power button held down.
5. **Battery check.** Warn when on battery, and ask for confirmation below 30% charge without AC.
6. **Locks and the run record.** A single-instance lock (`/run/kiln/kiln.lock`), per-helper locks, and a run record in `state.json`, so the next start can say where the last run stopped.
7. **Deadlines and watchdog.** Per-command deadlines plus a 10-minute no-output watchdog that only **reports** a hang. kiln never kills apt or dpkg.

### D42 · 2026-11-07 · TUI look: Windows XP text-mode Setup

**Status: decided** (your instruction and reference screenshot, 2026-11-07). The details are in tui-design.md, "Look".

The palette is: blue background, light grey text, double-line panels, a yellow progress bar, and a light grey status bar with XP-style key hints. It uses only the 16 console colors, so it looks the same on the Linux console and in GNOME Terminal.

Details I filled in, flagged for your review:
- `F3` quits;
- inverse rows mark the selection;
- bright red marks failed steps;
- the console fallback uses `+=|` borders.

## Implementation notes (2026-11-07)

- **Summary is a runner phase.** The catalog's step 25 `summary` is the runner's final phase, not a registered step, because it has to run after the end-of-run tasks (needrestart, restore).
- **Domain packages read sysfs through `sys.System`.** `internal/battery`, `internal/thermal` and `internal/mirrors` do this rather than adding every probe to `facts`, and `sys.System` is the same read path facts uses.

### D43 · 2026-11-07 · Implementation choices (to confirm)

**Status: decided** (you accepted the remaining items on 2026-11-07; D51 and D52 changed some, marked inline). Originally: **proposed.** These were decided while writing the code, because the catalog left them open or couldn't be implemented literally. Each one is applied in the code and listed here for you to confirm or change. **Forced** items are marked.

**Dependencies (forced by the runner's rule that a failed or user-skipped dependency skips the step):**
- **autoupdates depends only on apt-sources,** not on docker, devtools, repos or multimedia. Otherwise unticking Chrome would also turn off automatic updates. `origin=*` covers whatever repositories exist.
- **multimedia depends only on apt-sources.** The kernel step is opt-in and off by default, so a hard dependency would skip multimedia everywhere. NVIDIA headers come from backports only when the kernel step is ticked.

**Network and security:**
- **firewall:** the `nftables` backend is shown but disabled ("its ruleset isn't specified yet"). *Moot: step removed (D50).*
- **firewall:** the plan also sets the default zone to `public` and adds `ssh` to it if missing, which Check requires. *Moot: step removed (D50).*
- **firewall:** extra ports open only in `public`. *Moot: step removed (D50).*
- **firewall:** ports removed from the option aren't closed again. *Moot: step removed (D50).*
- **firewall:** `rdp` opens whenever GNOME is detected. *Moot: step removed (D50).*
- **ssh:** a hard dependency on the firewall step. *Moot (D50).*
- **ssh:** `password_logins` has the values auto/off/on, and **on** wasn't in the brief. *Decided: on/off only (D51).*
- **ssh:** a GitHub 404 fails at plan time, not in the form. *Moot (D50).*
- **network-migration:** the post-switch check polls for 60 s (architecture's deadline; the catalog said 30 s). `pending` is recorded before the keyfile is written. *Moot: step removed (D52).*
- **network-migration:** `inet manual` stanzas, other methods, and VLAN/alias names are left as exceptions, not migrated. *Confirmed (D51).* *Moot: step removed (D52).*
- **mirrors:**
  - neighbors are the UN M49 sub-regions; *Moot: option removed (D52).*
  - zone.tab is used as well as zone1970.tab, to cover link names;
  - if deb.debian.org is in the top N, it isn't listed twice;
  - the list carries a timestamp comment for the 30-day freshness check.

**Hardware and power:**
- **Sysfs writes go through `tee <attr>`** with the value on stdin, because sysfs can't take an atomic rename.
- **battery:**
  - with several batteries, the first one's accepted values are applied to all;
  - the requested stop/gap is recorded with the result, so a changed option re-runs the ladder;
  - method "none" counts as done until an interface appears;
  - the watch timer runs every 15 min.
- **system:** zram installs `systemd-zram-generator` if it's missing.
- **power:** when ppd doesn't offer `performance`, kiln sets `balanced` explicitly. ~~TLP is stopped, not disabled.~~ (TLP handling removed, D50.) A missing greeter power section is appended. *Moot: balanced profile (D52).*
- **thermal:** the first detected sensor is required and later ones are optional. HP is matched by DMI vendor.
- **firmware-packages:** Realtek is matched by PCI vendor 10ec or the r8169/r8152/rtw88/rtw89 drivers. isenkram is installed only with the non-free firmware option. *isenkram: always installed now (D52).*
- **firmware-updates:** runs fwupdmgr on every kiln run, because state has no field for its last result. A refused update becomes a note. *Confirmed (D51).*

**Apps and dev:**
- **NVIDIA:**
  - The Turing+ check uses a 299-ID table from NVIDIA's README (620.70.06); IDs ≥ 0x2900 count as Blackwell (Debian stock unavailable).
  - The NVIDIA options are declared only when an NVIDIA GPU is found.
  - `dkms` is installed before `dkms generate_mok`.
  - The one-time password goes to `mokutil --generate-hash` on stdin, and `--list-new` is dropped (`--test-key` covers MokNew).
  - The one-time password is shown once on the summary screen and stored in `/etc/kiln/secrets/mok`, never in a note (no secrets in the world-readable summary).
- **remote-desktop:** with the password left empty, kiln reuses the stored password and generates one only if none exists, so a re-run doesn't change it. Generated passwords are 20 characters. *Moot: step removed (D50).*
- **Intel VA-API:** without a verified Gen8 ID table, the driver is installed for any Intel GPU, with a pre-Gen8 note. `vainfo` is installed only for AMD, as the catalog literally says. *Changed: vainfo for every GPU (D51).*
- **cockpit:** with backports off, an already-installed backports cockpit is accepted and noted, not downgraded.
- **docker:** the signing key download has no sha256 (the catalog gives none; apt verifies packages with it).
- **devtools:** an existing non-symlink `fd`/`bat` is left alone with a note.
- **repos:** extrepo names other than `google_chrome` get their policy from `extrepo search`, and no package is installed.

**TUI:**
- "Dimmed" text is normal grey, because it isn't in the 16-color palette.
- The console-set progress fill is `#`.
- Prechecks never repair anything when the apply gate fails.
- A failed single-instance lock disables Start.

### D44 · 2026-11-07 · First code review round: fixes

**Status: forced.** These are bugs found by three independent read-only reviews of the code, fixed to match the catalog and the process model (D41).

**Core:**
- The inhibitor child dies with kiln. It's `systemd-inhibit … cat` on a pipe only kiln holds, so a crash can't leave reboots blocked.
- End-of-run tasks always run, even after an internal error.
- Quiesce records what it changed and undoes exactly that, restarting only the timers that were active.
- PackageKit is stopped only when no dpkg lock is held, and is no longer counted as "busy".
- `revert.Begin` refuses to overwrite another step's unresolved pending change.
- First-write backups are written to a temp file and renamed into place. Symlinks back up a file they replace.
- dpkg repair waits for locks.
- Commands can't hang past their deadline: a deadline kills the whole process group, and output pipes are released after exit.
- The TUI's signal handling survives a lost terminal.
- The summary file is redacted.
- After a failed critical step, the skipped steps give the real reason.
- `Cmd.Quiet` keeps password hashes out of the log.

**Network and security:**
- Migration revert follows how far the migration got, so it never flushes a live address.
- Static IPs keep their host address.
- The old SSH port stays open until the ssh step has verified the new one. The firewall opens the ports `sshd -T` reports plus the requested one, and the ssh step closes old ports after its own commit (`ssh.FirewallSync`).
- SSH revert uses reload-or-restart, and the new port is checked to be free first.
- An inactive firewalld is started.
- The migration Check tolerates an unplugged cable.
- Mirror and apt-sources changes are restored when `apt update` fails.
- upgrade refreshes the package lists in every run.

**Hardware and apps:**
- Stopping the battery watcher restores `charge_behaviour=auto`, and the watcher restarts when the target changes.
- A failed DVD build no longer blocks NVIDIA.
- A thinkfan boot helper that keeps failing is reported.
- The preview never generates a MOK password.
- The `~/.bashrc.d` loader is shared by python, node and go, so each works without the others.

**Still the owner's decision** (not changed):
- When "passwordless sudo" is turned **off** on a machine that already has kiln's sudoers file, the file is left in place. Removing it would be new behavior.
- TLP is stopped, not disabled (D43).

### D45 · 2026-11-07 · SSH works with passwords for the user and root; sudo works without re-login

**Status: decided** (your request and answers, 2026-11-07).

This **reverses** your brief's "key-only auth when keys exist, no root login", and D29 for SSH only (kiln now installs the SSH server).

- **ssh step, on by default:**
  - Installs `openssh-server` if missing and enables `ssh.service`.
  - `PasswordAuthentication yes` and `PermitRootLogin yes`, from everywhere. You chose this over LAN-only.
  - The review screen and summary warn that password SSH for the user and root is reachable wherever SSH is (internet included), with fail2ban as the only brake.
- **Root without a password:** if root's password is locked or empty, kiln gives root the target user's password by copying the user's password hash (`chpasswd -e`, hash on stdin, never logged). If the user has no password either, kiln adds a note to run `sudo passwd root`.
- **Firewall and fail2ban** treat SSH as present whenever the ssh step is ticked, so SSH is never blocked and the sshd jail is active.
- **sudo step:** with passwordless sudo **off**, it writes `/etc/sudoers.d/90-<user>` with `<user> ALL=(ALL:ALL) ALL`, validated with visudo. Either way, sudo works immediately, without logging out. With it **on**, nothing changes: the NOPASSWD rule is written as before.
- **Defaults I set,** flagged for your review:
  - the option names `root_login` and `root_password`;
  - "Password logins" is now `on` by default, with `auto` and `off` still available.

### D46 · 2026-11-07 · Read-only field check on real hardware

**Status: decided** (you offered a test machine and asked for testing; the script is review tooling, not a test suite, D30).

- **The script:** `scripts/field-check.sh` collects read-only evidence from a real machine (review-process.md, "Field check"). It's shellcheck-ed by `make lint` and CI.
- **Test machine:** 10.10.10.150, a fresh Debian 13 server install with no desktop, on Wi-Fi (an AzureWave module). A root password was set, so `sudo` isn't installed.
- **How it's run:** the owner runs the commands. Claude doesn't log in with passwords, so it can't run them itself.

Field observations so far are in debian13-notes.md, "Field observations".

### D47 · 2026-11-07 · Field-test fixes (forced)

**Status: forced.** Each fix is the only way to meet the existing catalog text on real hardware; none changes what kiln does, only how reliably it does it. Evidence is in debian13-notes.md, "Field observations".

- **firewall:** `closeOldSSHPorts` read SSH options through the firewall step's option view and panicked ("option ssh.extra_ports was not declared"). It now uses a firewall view of the same environment.
- **network-migration:**
  - restart NetworkManager instead of `nmcli connection reload`, because reload left the device unmanaged;
  - wait for the device to become available (and, for Wi-Fi, for scan results) before `nmcli --wait 90 connection up <profile> ifname <iface>`;
  - after an undo, wait for connectivity again, so the next step doesn't fail with "Network is unreachable".
- **network-migration, IPv6:** with no `inet6` stanza, IPv6 was mapped to `ignore`, but ifupdown's dhcpcd also does IPv6. The box lost its IPv6 addresses. The profile now gets `method=auto` with dhcpcd's SLAAC style (`stable-privacy` for `slaac private`, else `eui64`, in line with P13), DUID and IAID, unless `dhcpcd.conf` has `ipv4only` or `noipv6`.
- **facts:**
  - LAN subnets skip /32 and /128 host routes;
  - `MachineName()` drops a vendor repeated in the product name and DMI placeholders such as `Type1ProductConfigId`, and the facts screen no longer adds the version back.
- **Readability:** step failures show the action's short description instead of the whole wrapped error chain (`action.Short`).

### D48 · 2026-11-07 · Network-migration undo uses ifupdown's own units (P18 option A)

**Status: decided** (you chose option A).

- **allow-hotplug interfaces:** the undo runs `systemctl restart ifup@<iface>.service`. It uses restart rather than start, because kiln's `ifdown` leaves the unit active and `start` would do nothing; the unit's `ifdown` on an interface that's already down succeeds.
  - An interface that is both `allow-hotplug` and `auto` takes this path, because it only touches that one interface. This is my reading of option A, which doesn't name that case.
- **auto-only interfaces:** `systemctl restart networking.service`.
- **Interfaces in neither class:** plain `ifup`.
- The same commands are recorded in `state.json`, so a revert offered at the next start does the same thing.

### D49 · 2026-11-07 · Network-migration undo waits for Wi-Fi to be released and for ifup to finish (forced)

**Status: forced.** The forced-failure test of D48 left the test machine offline, and the journal showed two bugs. Both fixes are the only way for the catalog's revert to work.

- **The race:** NetworkManager's wpa_supplicant released wlo1 asynchronously after `managed no`. ifupdown's wpa_supplicant started first, found the control socket still in use and refused to start. The undo now waits, for up to 30 s, until `/run/wpa_supplicant/<iface>` is gone.
- **The missed failure:** `ifup@.service` is `Type=simple`, so `systemctl restart` returned before `ifup` had finished, and kiln reported a good undo. kiln now polls the unit until it is `active/exited` or `failed`, for up to 6 min, and reports a failure with the `journalctl` command to run.
- **How it's recorded:** a restore step can be a Go function a step registers (`revert.Register`, recorded as `kiln: <name> <args>`), so a revert at the next start waits the same way. `revert.Poll` stops early on `revert.Final` errors.

### D50 · 2026-11-07 · Firewall and remote desktop dropped; no SSH key options; no TLP handling; Wi-Fi power saving left alone

**Status: decided** (your answers on 2026-11-07).

- **Firewall step removed.** A fresh Debian install has no active firewall, and kiln leaves it that way. Your original brief (step 14) had asked for firewalld; you've now decided against it.
  - fail2ban keeps banning, through nftables: trixie's `defaults-debian.conf` already sets `banaction_allports = nftables[type=allports]`, and nftables is on every install. Its `ignoreip` uses the auto-detected LAN subnets (the firewall's LAN override option is gone).
  - Cockpit now listens on port 9090 on every network the machine is on.
  - SSH needs no port opening. The ssh, docker and cockpit steps no longer depend on a firewall step.
  - The review screen loses its Firewall tab and rule count.
  - Machines where an earlier kiln run installed firewalld keep it; kiln doesn't remove it.
- **Remote-desktop step removed** (your brief's step 19). The only stored secret left is the Secure Boot one-time password.
- **SSH key options removed** (`ssh.github_user`, `ssh.extra_keys`): kiln no longer adds keys. Keys you add to `~/.ssh/authorized_keys` yourself still count for the `auto` password setting.
- **TLP handling removed.** No trixie task installs TLP, and kiln runs on fresh installs.
- **Wi-Fi power saving:** left at Debian's default (the open question is closed).
- **Download pinning:** nothing more. kiln downloads from the official sources the docs name and trusts them.
- Saved choices for the removed options are ignored with a log line, so older `state.json` files still load.

### D51 · 2026-11-07 · First D43 walk-through

**Status: decided** (your answers on 2026-11-07).

- **SSH password logins:** the option offers On (default, D45) and Off only. "Auto" (off as soon as a key appears) is gone. A saved `auto` is ignored with a log line and falls back to On.
- **Firmware updates:** fwupdmgr runs on every kiln run, as today.
- **vainfo:** installed on every machine with a GPU, whatever the vendor, not only AMD. Intel driver handling is unchanged.
- **Network migration:** `inet manual` stanzas, methods kiln can't translate, and VLAN/alias interfaces stay on ifupdown with a note, as today.

### D52 · 2026-11-07 · Second D43 walk-through: no network migration, balanced power, no mirror neighbours, non-free always

**Status: decided** (your answers on 2026-11-07).

- **network-migration step removed.** kiln leaves networking as the installer set it up: ifupdown on servers, NetworkManager on GNOME machines. Its only reason was Cockpit's networking page (brief, section 6), which can now show but not edit ifupdown interfaces. The undo machinery added for it (D48, D49's registered restore steps) went with it. The SSH step is the only risky change left.
- **Balanced power everywhere,** so an idle CPU doesn't sit at high clocks using power and making heat (on the test box, kiln's performance profile held 2.18 GHz while idle).
  - With GNOME: power-profiles-daemon `balanced`, which sets the `balanced` platform profile and, with amd-pstate, EPP `balance_performance` on AC, `balance_power` on battery (ppd 0.30 source).
  - Without GNOME: tuned's stock `balanced` profile (schedutil, EPP balance_performance, `platform_profile=balanced`, boost on). kiln's `kiln-performance` profile (P7) is removed from machines that have it.
  - The thermal step now checks for `balanced` and the balanced EPP values; its option is renamed `thermal.check_profile`.
- **Mirrors:** no "neighbouring countries" option, and the UN sub-region table is gone. Candidates are your country, a random worldwide sample and deb.debian.org, ranked only by measured download speed.
- **Non-free:** always on. The `firmware-packages.nonfree_firmware` option is gone, so non-free firmware and the isenkram report are always included; apt-sources already always enables `non-free non-free-firmware`.
- **Kept as they are:** the battery, watcher and fallback choices; the Intel driver for any Intel GPU; the Secure Boot one-time password handling; and the three app items (Cockpit not downgraded, fd/bat left alone, other extrepo repos only enabled).

### D53 · 2026-11-07 · Step-review round: forced fixes

**Status: forced.** These came from the per-step reviews. Each is the only way to meet the catalog or an engineering rule; owner questions from the same round are asked separately.

- **preflight (blocker):** under plain `su`, kiln now really puts `/usr/local/sbin:/usr/sbin:/sbin` on PATH before anything runs. The note already claimed it did; without it, `sshd`, `visudo` and `chpasswd` weren't found.
- **revert:** a step whose earlier change is still pending keeps its first snapshot instead of re-snapshotting half-applied files.
- **ssh:**
  - applies with `reload-or-restart`, so an enabled but stopped sshd no longer fails;
  - its warnings show on every run (catalog: "the review screen and summary warn");
  - it warns when another file's `Port` means sshd won't listen on 22;
  - the undo advice names the right command.
- **sudo:** reports when an earlier NOPASSWD file still applies, instead of claiming "with a password".
- **go:**
  - an interrupted swap no longer loses the only working Go: `go.kiln-old` is put back, and leftovers make `Check` re-plan;
  - "Update tools on this run" is really per-run (never saved, rule 2).
- **node:** the nvm helper script is fed on bash's stdin, so a cleared `~/.cache` no longer makes a configured machine look unconfigured.
- **multimedia:**
  - the libdvdcss2 build waits for the dpkg lock (rule 9), and a build that quietly didn't happen becomes a note (D44);
  - DKMS on an *older* kernel no longer counts as built;
  - Intel machines get the non-free VA-API driver even when GNOME already pulled in main's;
  - kernel headers follow the installed kernel metapackage's suite, not the kernel step's checkbox.
- **cockpit:** installs `cockpit-networkmanager` explicitly, because Check requires it.
- **firmware-packages:**
  - Realtek rtw88 drivers match as `rtw_*`;
  - Wi-Fi cards are also found on the PCI bus, by vendor, since a card missing firmware has no network interface;
  - an `update-initramfs` failure says what to do.
- **kernel:** Check also requires the backports headers and firmware it installs, so failed parts are retried.
- **power:** a stopped or disabled tuned or power-profiles-daemon is started, and the old kiln profile is removed on both paths.
- **thermal:** the not-applicable reason names a missing sensor when that's the cause.
- **battery:**
  - only batteries that took the limit are recorded;
  - reapply writes nothing when the thresholds already hold;
  - helpers log at Info, so the log doesn't grow by every read many times a day.
- **apt:**
  - the lock wait chosen in the wizard applies to that same run;
  - the "lists refreshed this run" flag resets per run;
  - apt-sources checks `Signed-By` only on files modernize converted.
- **mirrors:** `kiln mirrors bench --apply` and `reset` respect the apply gate (rule 1) and the run lock.
- **Docs:** stale firewall and NetworkManager text was removed; new V rows for dpkg version comparison, tuned and ppd balanced, chpasswd and PCI Wi-Fi IDs.

### D54 · 2026-11-07 · Direction reset: kiln is a one-time setup tool

**Status: decided (intent), approach proposed.** This is your direction from the end of the 2026-11-07 session. Read it before any other entry: where an older entry conflicts with it, this one wins.

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

**Approaches (proposed; your call next session):**
- **A. One-shot, leave nothing behind (recommended).** Keep the wizard and the server/GNOME modularity, but strip the persistence:
  - no kiln units or helpers: the battery limit becomes a plain udev rule, the software charge-limit fallback (it needs a daemon) and the thinkfan boot helper go;
  - no saved choices, no revert prompt, no subcommands;
  - at the end kiln deletes its binary and `/etc/kiln`, leaving only the summary in `/var/log`;
  - steps still skip what's already done, so a run that failed halfway can simply be run again.

  Mostly deletions; most of today's verified and field-tested work carries over.
- **B. Rewrite much simpler.** A small tool (or a bash script with a server/GNOME switch) with far fewer steps and options. Smallest result; much of the work starts over.

**Your answers in the last review round (2026-11-07), recorded but not implemented**, because most of them only matter if kiln is re-run:

| Question | Your answer | Under a one-time tool |
|---|---|---|
| fail2ban when SSH is installed but the ssh step is unticked | skip it (keep current) | still applies (catalog text to match) |
| remove kiln's old NOPASSWD sudo file when passwordless is switched off later | remove it | moot (no later runs) |
| remove GitHub CLI's apt source when switching gh to Debian's later | remove them | moot |
| "No" to "revert the unfinished change?" | keep the change, stop asking | moot if the prompt goes (A) |
| write `Port 22` when another sshd file sets a Port | write it | still applies |
| thinkfan on Intel ThinkPads | AMD only (no change) | still applies |
| rotate `/var/log/kiln.log` | add logrotate | conflicts with "leave nothing behind" |
| mirror test: skip cloud-only (GeoDNS) mirrors, cap at 10 in-country | yes | still applies |
| thinkfan no longer depends on the power step | yes | still applies |
| remove battery helpers when the battery step is unticked later | yes | moot under A (no helpers) |
| warn when backports is switched off under a backports kernel | yes | moot |
| startup internet check: the catalog wins (HTTPS to deb.debian.org and github.com, 5 s each) | the catalog | still applies |
| the remaining mechanical D43 items | accept them | D43 is now decided |

Earlier the same day you also decided D50–D52 (no firewall, remote desktop, network migration, SSH keys or TLP; balanced power; no mirror neighbours; non-free always). Those stand, and they already point the same way: less kiln, more Debian defaults.

## Proposals from the second verification pass

**Status:** you accepted all of these on 2026-11-07 as written (the "current text" option), except P1 and P14, which D38 replaces. They're kept here as a record of the choices and the alternatives.

### P1 · NVIDIA GPU + backports kernel (catalog step 5b; D35)

**Status: superseded** by D38: the backports kernel is opt-in on every machine, and you choose it per machine.

**Situation:**
- Neither of your laptops has an NVIDIA GPU, so this only matters for a future desktop.
- trixie's NVIDIA driver 550 is built against the installed kernel (DKMS). The backports kernel is 7.2, but the newest NVIDIA build only documents fixes up to Linux 6.17.
- If the module doesn't build on 7.2, booting that kernel gives a basic, slow display with no NVIDIA driver. The 6.12 kernel stays in the boot menu and works.

**Options:**
- **A.** Stable kernel by default on NVIDIA machines (the current catalog text).
- **B.** Backports kernel everywhere. If the driver fails to build, the summary says so and you boot 6.12 from the menu.
- **C.** Like B, and kiln also makes 6.12 the default boot entry when the driver didn't build for 7.2. **My recommendation.**
- **D.** Use the open nouveau driver instead of the proprietary one.
- **E.** Ask per machine in the wizard.

### P2 · Backup location (all file writes; D36)

**Situation:** your rule 4 says first-write backup `<file>.kiln-bak`. apt reads every file in `sources.list.d/` and `apt.conf.d/`, so a `.kiln-bak` there makes apt print a notice on every run.

**Options:**
- **A.** Next to the file as specified, accepting those notices.
- **B.** One central folder, `/var/lib/kiln/backup/` (the current text).
- **C.** Next to the file, under a name apt ignores silently (for example `.kiln.bak`). Not verified yet.

### P3 · `sensors-detect --auto` dropped (step 12)

Your brief says to run it. I removed it because on these laptops the sensor drivers load by themselves, and its probing adds risk.

**Options:**
- **A.** Run it as the brief says.
- **B.** Don't run it (current text).

### P4 · "Update Go tools" defaults to off (step 23)

"On" would reinstall `@latest` on every run, which breaks "a second run changes nothing".

**Options:**
- **A.** Off (current text).
- **B.** On, as "everything on" says, accepting that re-runs change things.
- **C.** Remove the option.

### P5 · New "backports firmware" option, on (step 5b)

It reinstalls firmware from backports along with the backports kernel. No dependency requires this.

**Options:**
- **A.** Keep it, default on (current text).
- **B.** Don't add it.

### P6 · `isenkram-cli` missing-firmware report (step 5)

It adds a package and lists firmware that's still missing, without installing anything.

**Options:**
- **A.** Keep it (current text).
- **B.** Drop it.

### P7 · Custom tuned profile with `vm.swappiness=60` (step 11, machines without GNOME) *(Superseded by D52: tuned's stock balanced profile.)*

tuned's `throughput-performance` sets swappiness to 10, which works against zram.

**Options:**
- **A.** kiln's own profile with 60 (current text).
- **B.** Plain `throughput-performance`, as the brief says.
- **C.** Another value.

### P8 · thinkfan "disengaged" from 78 °C (step 12)

Your brief: never stop, ramp early, full speed from about 70 °C. The current curve reaches level 7 (the highest in-spec speed) by about 65–70 °C, then switches to "disengaged" (beyond the rated maximum, per thinkfan's docs) from 78 °C.

**Options:**
- **A.** Keep that curve.
- **B.** Never go past level 7.
- **C.** Use "full-speed" as the top level at 70 °C, which is the same thing as disengaged.

### P9 · fail2ban bans on all ports, not just SSH (step 16)

**Options:**
- **A.** All ports (current text).
- **B.** SSH port only, as in Debian's default.

### P10 · GNOME power button set to "ask what to do" and locked (step 11)

Its default is suspend. That's harmless once suspend is masked, but it isn't in your brief.

**Options:**
- **A.** Set and lock it (current text).
- **B.** Leave it alone.

### P11 · zram changes wait for the next boot when a lot of swap is in use (step 10)

**Options:**
- **A.** Wait (current text).
- **B.** Always restart now.
- **C.** Always wait for the next boot.

### P12 · Removed `kiln-thermal.service` (step 12)

The platform profile is set by ppd or tuned, and kiln only checks it. Your brief asked for a boot unit calling `kiln thermal apply`. That unit now exists only for the ThinkPad thinkfan enable.

**Options:**
- **A.** Keep it as now.
- **B.** Restore a boot unit that also re-applies the platform profile.

### P13 · NetworkManager keeps the old-style IPv6 address (`eui64`) (step 6) *(Moot: network-migration removed, D52.)*

NM's default is a privacy-stable address, which would differ from what ifupdown used.

**Options:**
- **A.** Keep the same address (current text).
- **B.** Use NM's default.

### P14 · Older fallback kernels are removed automatically (step 5b)

**Status: superseded** by D38: no fallback handling at all; kernels follow the stock metapackage.

**Options:**
- **A.** Mark the previous fallback "auto", so automatic cleanup removes it (current text).
- **B.** Keep every fallback until you remove them yourself.

### P15 · preflight installs `ca-certificates` and `pciutils` if missing (step 0)

**Options:**
- **A.** Install them (current text).
- **B.** Stop and tell you to install them.

### P16 · Remote-desktop certificate valid for 10 years (step 19)

openssl's default would be 30 days.

**Options:**
- **A.** 10 years (current text).
- **B.** Another length, with kiln renewing it on re-runs.

### P17 · Cockpit's recommended packages are kept (step 18)

These are what Cockpit's networking, storage and updates pages need, and they pull in NetworkManager, PackageKit and UDisks even on machines without a desktop.

**Options:**
- **A.** Keep them (current text).
- **B.** Install only `cockpit-networkmanager`.
- **C.** Choose per page.

## Proposals from the field test

### P18 · Undo brings the interface back through `ifup@<iface>.service` (step 6)

**Status: approved 2026-11-07 (option A), implemented as D48.**

When a migration fails its check, the undo runs `ifup <iface>` from kiln's process. That works (field test run 2), but wpa_supplicant and dhcpcd then live in kiln's `kiln-run-*.scope` instead of `ifup@<iface>.service`, where Debian puts them at boot. They survive kiln exiting, but anything that stops that scope would take the network down, and `systemctl status ifup@<iface>` won't show them.

**Options:**
- **A.** For `allow-hotplug` interfaces, run `systemctl start ifup@<iface>.service`; for `auto` ones, `systemctl restart networking.service`. The daemons end up where they are at boot. (Recommended.)
- **B.** Keep the plain `ifup <iface>` (current code).

## Open questions

1. **Closed (D50): left at Debian's default.** ~~Wi-Fi power saving on always-on machines.~~ On the test machine, the Wi-Fi link had 19–572 ms latency and 6.7% packet loss. After `iw dev <wlan> set power_save off` it dropped to 3–7 ms with no loss. kiln doesn't change Wi-Fi power saving today. The proposal is a new default-on option in the power step:
   - a NetworkManager drop-in `[connection] wifi.powersave = 2`, on machines where NetworkManager manages Wi-Fi;
   - plus `iw dev <wlan> set power_save off` for the current session.

   Both facts would need verifying for trixie first. Your call.
