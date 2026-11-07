# Decisions log

Each entry: date, decision, why, alternatives considered. Superseded entries stay and are marked. Open questions are at the bottom. Move them up when they're answered.

## Decided

### D1 · 2026-11-07 · Dependency versions (verified against the Go module proxy)

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

`Apply` runs against `sys.Recorder` for `kiln plan` and the wizard's review screen. Alternatives: separate `Plan` text generation (drifts), or a dry-run flag inside each step (easy to forget).

### D3 · Firewall: firewalld

firewalld 2.3.1 (nftables backend) has first-class Docker integration (a `docker` zone plus a `docker-forwarding` policy, rules recreated on reload in Docker ≥ 28.1), a Cockpit page, zones with source CIDRs (which fit "LAN-only Cockpit/RDP"), and a runtime-vs-permanent split that makes verify-then-revert natural (apply runtime, check, `--runtime-to-permanent`). Plain nftables would mean hand-coexisting with Docker's iptables-nft rules, and Docker's own nftables backend is still experimental (29.x). nftables stays as an option (firewall backend `nftables`) for machines without Docker.

### D4 · Networking: migrate to NetworkManager

> The `systemd-run` rollback timer below is superseded by D28 (verify, then revert in-process). The step is now catalog step 6.

Cockpit's networking page needs NM, and the desktop tasks bring NM anyway, so one network owner is simplest. The migration is the `network-migration` step (6b), a new catalog step not in the original brief. It uses a `systemd-run` rollback timer. NM's checkpoint API isn't used, because it can't undo ifupdown state. Alternatives: leave ifupdown (Cockpit's network page broken, two owners) or systemd-networkd (Cockpit can't manage it).

### D5 · Power manager: power-profiles-daemon with GNOME, tuned without it

> Updated by D29: the choice follows the *detected* installer choice. There is no desktop option.

ppd 0.30 is what GNOME uses and integrate with (UI, D-Bus). It handles amd-pstate EPP and platform_profile itself. On desktop=none, `tuned` `throughput-performance` gives more server-oriented tuning, and there's no UI to integrate with. Never both, never TLP (ppd `Conflicts: tlp`).

### D6 · Thermal defers to the power manager

ppd writes `platform_profile` and adopts external writes, so kiln never writes the sysfs file when ppd is active. It sets the ppd profile to `performance` and verifies. `kiln-thermal.service` exists only for the tuned path, and only if tuned doesn't set the platform profile itself.

### D7 · Time sync: systemd-timesyncd

Debian's default, already installed, no daemon to serve, and enough for SNTP-client accuracy. chrony (option) replaces it via `time-daemon` Conflicts. Pick chrony only to serve time to the LAN or for better accuracy on flaky links.

### D8 · zram: systemd-zram-generator

It's already the Debian-integrated generator, it ships a sensible default (`min(ram/2, 4096)`), and it's configured declaratively in `/etc/systemd/zram-generator.conf`. kiln adds `zstd`. zram-tools is a shell-script service and is the older approach.

### D9 · fail2ban keeps Debian's nftables ban action under both firewall backends

Debian's defaults already use `banaction = nftables` + `backend = systemd`. The nftables action uses its own table, works alongside firewalld, and survives `firewall-cmd --reload`. That's fewer moving parts than `firewallcmd-rich-rules`.

### D10 · Mirrors: APT mirror method, no apt-fast

`mirror+file:` with `priority:N` tags gives parallel downloads across hosts and automatic failover, natively. apt-fast/aria2 adds dependencies and wraps apt. netselect-apt ranks by latency, not throughput.

### D11 · uv via `pipx install uv`

uv isn't packaged in trixie. pipx is, so this avoids a piped installer. The official installer remains an option.

### D12 · Battery: kiln owns the limit; UPower's hwdb is aligned to the same values

UPower's disable path writes 0/100, so kiln can't just switch it off. kiln writes hwdb `CHARGE_LIMIT` to its accepted values, re-applies on boot, on power-supply uevents and every 15 min, and tells the user to leave GNOME's toggle alone.

### D13 · Backports kernel is its own step (`kernel`)

It can be toggled separately in the TUI, and it has its own reboot reason.

### D14 · Release checksum file is named `checksums.txt`

GoReleaser's default is `kiln_<version>_checksums.txt`. A fixed name lets install.sh use `releases/latest/download/` URLs.

### D15 · 2026-11-07 · Repository is `github.com/satya-janghu/kiln`

Module path, install.sh URLs (`raw.githubusercontent.com/satya-janghu/kiln/main/install.sh`) and GoReleaser all use it.

### D16 · Docker published ports are left reachable; kiln warns

`firewall.strict_docker_ports` defaults to **off**, so Docker's normal `-p` behavior stays. The summary always says that published ports bypass the firewall and that private services should be bound to 127.0.0.1 or a LAN IP. Turning the option on sets firewalld `StrictForwardPorts=yes`.

### D17 · Backports kernel plus a maintained stable fallback

`kernel.track_stable_fallback` (on) keeps the newest stable `linux-image-6.12.*-amd64` installed and marked manual. A weekly `kiln kernel fallback` timer is needed because unattended-upgrades won't install a new ABI package name.

### D18 · `gh` comes from GitHub's official apt repo

`devtools.gh_source: github` (default) adds `cli.github.com/packages` (deb822 + signed-by keyring). Debian's 2.46 stays an option.

### D19 · NM migration: a new DHCP address is success, not failure

> Still true, but with no SSH session to lose (D28), it only means "show the new IP". Copying the client ID is kept so the IP stays stable for later port forwarding.

The confirm is a local probe (route + DNS + HTTPS). If it passes on a new address, keep NetworkManager, print the new IP on the console and in the summary, and record `done` with a note. Only a failed local probe rolls back. kiln still copies the old DHCP client ID first, to make an address change unlikely.

### D20 · "LAN" means RFC1918 IPv4 + IPv6 ULA on physical NICs

Global IPv6 prefixes are never treated as LAN. Cockpit and RDP from the LAN go over IPv4 (or ULA).

### D21 · Mirror country comes from the timezone, never geo-IP

`mirrors.country: auto` maps the timezone through a compiled-in zone1970.tab table plus a small neighbor table. An unknown timezone falls back to a worldwide sample + deb.debian.org.

### D22 · GNOME or no desktop; nothing else

> Updated by D29: the installer makes this choice and kiln only detects it.

`desktop.environment` is `gnome | none`. KDE (and krdp) are out of scope. Remote desktop is "not applicable: no GNOME desktop installed" on machines without GNOME. ppd is used with GNOME and tuned with none.

### D23 · GNOME no-suspend keys are locked

`power.lock_gnome_settings` (on) adds a dconf `locks/` file so GNOME Settings can't re-enable automatic suspend.

### D24 · NVIDIA + Secure Boot: prepare the MOK enrollment and tell the user

The driver is installed and MOK enrollment is queued with a one-time password, which is shown in the summary. Until someone enrolls at the console on the next boot, the NVIDIA module won't load, but the machine boots normally.

### D25 · Playwright system deps are installed through kiln's apt helper

kiln gets the package list from Playwright, installs it with its own non-interactive apt helper (rule 8), then runs `playwright install chromium` as the user. It never uses `--with-deps`.

### D26 · `kiln secrets show [name]`

A root-only command that prints a stored secret from `/etc/kiln/secrets/`. It never logs what it prints.

### D27 · 2026-11-07 · No profiles, no headless mode

Every machine is set up individually in the wizard. Removed: YAML profiles, `--config` (path or URL), `KILN_*` presets, "save profile", headless `kiln run --yes`, `--reboot`, and the no-TTY branch of install.sh. The machine's last choices are kept in `/etc/kiln/state.json` and pre-filled on the next run. `kiln plan` remains as a read-only preview. This supersedes the brief's headless mode and D1's YAML row.

### D28 · Run at the console, in person; no SSH use

The owner is always physically at the machine. kiln warns if it detects an SSH session but doesn't refuse. The safety rails for remote lockout are removed: the `systemd-run` rollback timers, the "press a key within N seconds" confirm, and the two-stage SSH port change. They're replaced by `internal/revert`: validate, record `pending`, apply, check locally, and revert from backups straight away on failure. The next start offers to revert if kiln died mid-change.

### D29 · The Debian installer owns the desktop, the SSH server and blends

kiln detects them and doesn't install or switch them. The `desktop` step is removed. GNOME-dependent parts (GNOME power settings, remote desktop, Flatpak/GNOME Software, GStreamer, desktop-only extrepo repos) apply only when GNOME is detected. `ssh` and `fail2ban` apply only when an SSH server is detected. `network-migration` is expected to matter mainly on machines installed without a desktop.

### D30 · No testing: review to sign-off

Removed:
- unit tests, golden files, fixture trees and `kiln facts --dump`;
- container package checks (`make test-pkgs`) and container integration runs (`make test-int`);
- hardware test runs and `make deploy`.

Quality comes from verifying every fact against current primary docs, plus static analysis (`make lint`, `make vuln`), `/review-step`, `/check-packages` and `/audit` passes until both parties sign off ([review-process.md](review-process.md)). The `Facts` layer stays as the single place for detection, but it no longer has a configurable root. The Recorder stays because the review screen and `kiln plan` need it.

### D31 · Short install URL on GitHub Pages

`wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash`. Pages is deployed by the release workflow from `site/` (Actions source), so the `docs/` folder isn't published.

### D32 · Responsive, console-safe TUI

The layout fills the terminal: 80x24 minimum, stacked panes below 120 columns, side by side from 120. There's a console glyph set (ASCII, 16 colors, no braille spinner) for `TERM=linux`, because the first run usually happens on the bare Linux console.

### D33 · The wizard is the only way to apply changes

Read-only: `kiln plan`, `kiln facts`, `kiln battery status`. The systemd helpers (`battery apply|watch`, `thermal apply`, `kernel fallback`) and `mirrors bench --apply|reset` re-apply only what the wizard already chose (recorded in `state.json`), or reset it.

### D34 · Static analysis stays, as review tooling

With no tests, `make lint` (go vet, golangci-lint v2 with errcheck, staticcheck, gosec, govet, ineffassign, unused, errorlint, plus shellcheck on install.sh), `make vuln` (govulncheck) and `make release-check` (goreleaser check) are the only mechanical checks. They catch classes of bugs that reading the code misses: unchecked errors, shadowed variables, unreachable or dead code, command-injection patterns, misuse of `errors.Is`/`As`, known-vulnerable dependencies, and shell quoting mistakes. They run on every push in CI and must be clean before sign-off. These are static checks only: nothing is executed against a system, and no test code is written.

## Open questions

None right now. New ones go here.
