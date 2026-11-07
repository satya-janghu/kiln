# Decisions log

Each entry has a date, the decision, why, and the alternatives considered. Superseded entries stay, marked as superseded.

**Status:** every entry carries one of these.
- **decided:** you made the choice, either in the brief, by answering a question, or by explicit instruction. Code may rely on it.
- **proposed:** I recommended it, or chose it while fixing something, and you haven't approved it. Nothing may be built on a proposed entry until you approve or change it.
- **forced:** the facts leave only one way to meet what you asked for. These are flagged, and you can still overrule them.

Proposals P1–P17 at the bottom are the choices I made on my own during the second verification pass.

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

**Status: proposed.** These were decided while writing the code, because the catalog left them open or couldn't be implemented literally. Each one is applied in the code and listed here for you to confirm or change. **Forced** items are marked.

**Dependencies (forced by the runner's rule that a failed or user-skipped dependency skips the step):**
- **autoupdates depends only on apt-sources,** not on docker, devtools, repos or multimedia. Otherwise unticking Chrome would also turn off automatic updates. `origin=*` covers whatever repositories exist.
- **multimedia depends only on apt-sources.** The kernel step is opt-in and off by default, so a hard dependency would skip multimedia everywhere. NVIDIA headers come from backports only when the kernel step is ticked.

**Network and security:**
- **firewall:** the `nftables` backend is shown but disabled ("its ruleset isn't specified yet").
- **firewall:** the plan also sets the default zone to `public` and adds `ssh` to it if missing, which Check requires.
- **firewall:** extra ports open only in `public`.
- **firewall:** ports removed from the option aren't closed again.
- **firewall:** `rdp` opens whenever GNOME is detected.
- **ssh:** a hard dependency on the firewall step.
- **ssh:** `password_logins` has the values auto/off/on, and **on** wasn't in the brief.
- **ssh:** a GitHub 404 fails at plan time, not in the form.
- **network-migration:** the post-switch check polls for 60 s (architecture's deadline; the catalog said 30 s). `pending` is recorded before the keyfile is written.
- **network-migration:** `inet manual` stanzas, other methods, and VLAN/alias names are left as exceptions, not migrated.
- **mirrors:**
  - neighbors are the UN M49 sub-regions;
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
- **power:** when ppd doesn't offer `performance`, kiln sets `balanced` explicitly. TLP is stopped, not disabled. A missing greeter power section is appended.
- **thermal:** the first detected sensor is required and later ones are optional. HP is matched by DMI vendor.
- **firmware-packages:** Realtek is matched by PCI vendor 10ec or the r8169/r8152/rtw88/rtw89 drivers. isenkram is installed only with the non-free firmware option.
- **firmware-updates:** runs fwupdmgr on every kiln run, because state has no field for its last result. A refused update becomes a note.

**Apps and dev:**
- **NVIDIA:**
  - The Turing+ check uses a 299-ID table from NVIDIA's README (620.70.06); IDs ≥ 0x2900 count as Blackwell (Debian stock unavailable).
  - The NVIDIA options are declared only when an NVIDIA GPU is found.
  - `dkms` is installed before `dkms generate_mok`.
  - The one-time password goes to `mokutil --generate-hash` on stdin, and `--list-new` is dropped (`--test-key` covers MokNew).
  - The one-time password is shown once on the summary screen and stored in `/etc/kiln/secrets/mok`, never in a note (no secrets in the world-readable summary).
- **remote-desktop:** with the password left empty, kiln reuses the stored password and generates one only if none exists, so a re-run doesn't change it. Generated passwords are 20 characters.
- **Intel VA-API:** without a verified Gen8 ID table, the driver is installed for any Intel GPU, with a pre-Gen8 note. `vainfo` is installed only for AMD, as the catalog literally says.
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

### P7 · Custom tuned profile with `vm.swappiness=60` (step 11, machines without GNOME)

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

### P13 · NetworkManager keeps the old-style IPv6 address (`eui64`) (step 6)

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

## Open questions

None right now. New ones go here.
