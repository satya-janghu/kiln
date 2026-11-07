# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`kiln` is a single static Go binary with a TUI. It takes a fresh **Debian 13 "trixie" amd64 bare-metal** install and makes it a configured, always-on machine, the Debian way: apt, deb822 sources, Debian package names and Debian defaults. The owner runs it **at the machine's own console** (physically present, not over SSH) right after the Debian installer finishes. Every time, the wizard is used to pick the options for that machine. There are no saved profiles and no headless mode.

The **Debian installer decides** whether there is a desktop (GNOME or none), whether an SSH server is installed, and whether any Debian blend is used. kiln doesn't install or change those choices. It detects them, and steps that depend on them show "not applicable: <reason>" when they don't fit. Every kiln step and sub-option starts **on** in the wizard, except the backports kernel, which is opt-in per machine. Kernels are never pinned; they follow Debian's stock metapackage and stay on the latest version.

The name `kiln` is used everywhere: module `github.com/satya-janghu/kiln`, the binary, `/etc/kiln`, `/var/log/kiln.log`.

Development happens on **macOS (Apple Silicon)**, and the binary only ever runs on Debian 13 amd64.

## How quality is assured: review, not testing

**There are no tests.** That means no unit tests, no container tests and no test runs on hardware. Confidence comes from repeated review against current primary documentation until the owner and Claude both sign off. After that the binary goes straight to production. The process is in `docs/review-process.md`. In practice:

- Every Debian package name, path, config key, command flag and default that the code relies on is checked against a current primary source (packages.debian.org/trixie, manpages.debian.org/trixie, sources.debian.org, the trixie release notes, upstream docs). The source is recorded in `docs/debian13-notes.md`. A fact marked **U** there is not yet trusted, so code must not depend on it until it is **V**.
- Code is reviewed against `docs/steps-catalog.md` and the rules below, using `/review-step`, `/check-packages` and `/audit`.
- Static analysis (`make lint`, `make vuln`) is review tooling, not testing. It must stay clean.
- Don't add `_test.go` files or test harnesses unless the owner asks for them.

## Stack

- **Language:** Go 1.27, built as a static binary (`CGO_ENABLED=0`).
- **CLI:** cobra.
- **TUI:** Charm v2: `charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`, `charm.land/huh/v2`.
  - Bubble Tea v2 is declarative: `View()` returns `tea.View`, keys arrive as `tea.KeyPressMsg`, and alt screen and mouse mode are `View` fields.
  - Most examples online are v1, so follow `UPGRADE_GUIDE_V2.md` in the bubbletea repo.
- **Stored state:** stdlib `encoding/json` only. There is no YAML dependency.
- **Release:** GoReleaser v2 publishes to GitHub Releases. `install.sh` is served from GitHub Pages.
- Pinned versions and the reasons for them are in `docs/decisions.md`.

## Commands

```bash
make build    # GOOS=linux GOARCH=amd64 CGO_ENABLED=0 → dist/kiln
make lint     # go vet + golangci-lint (v2 config) + shellcheck install.sh
make vuln     # govulncheck ./...
make release-check   # goreleaser check
make tidy     # go mod tidy (commit go.sum)
```

Nothing to run locally on the Mac beyond these: kiln itself only runs on Debian.

## Engineering rules

1. **Never apply anything on the development machine.** Changes are applied only from the wizard, only on Linux, and only when `/etc/kiln/allow-apply` exists (`install.sh` creates it) or `--i-know-this-is-a-fresh-machine` is passed. The real `sys.System` refuses to mutate anything unless `GOOS` is linux. Never run kiln against a real system from this Mac.
2. **Every step is idempotent.** `Check` is read-only and detects "already done". `Apply` is safe to run twice. A second run on a configured machine changes nothing and finishes fast. Because nothing is tested, this has to be argued in review for every step.
3. **No `bash -c` with interpolated strings.** Use `exec.Command(name, args...)`. Piped installers (nvm, and uv's fallback) go only through `a pinned `sys.Fetch` download run with `bash <file>` as the user`.
4. **All file writes go through `sys.System.WriteFile (managed write)`.** It writes atomically. On the first write of an existing file it makes a backup under `/var/lib/kiln/backup/<absolute path>` (P2), never next to the file, because a stray file in `sources.list.d/` or `apt.conf.d/` gets read or warned about. It adds a `# Managed by kiln` header where the format allows comments. ucf-managed files (for example `20auto-upgrades`, GDM's `greeter.dconf-defaults`) are only key-edited or verified, never rewritten.
5. **A failing step never aborts the run.** The failure is recorded, steps that depend on it are skipped, and the rest continue. The summary says what failed and how to fix it.
6. **Hardware and installer detection goes only through the `facts` package.** Steps never read sysfs, procfs or dpkg state directly.
7. **`plan` and the real run share one code path.** `Apply` runs against `sys.Recorder` for `kiln plan` and the wizard's review screen, so the preview can't drift from what actually runs.
8. **apt runs fully non-interactive.** It sets `DEBIAN_FRONTEND=noninteractive`, `NEEDRESTART_MODE=a`, `APT_LISTCHANGES_FRONTEND=none`, `UCF_FORCE_CONFFOLD=1` and `LC_ALL=C`, preseeds debconf, and passes `-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`. confdef/confold don't cover ucf-managed files, which is why `UCF_FORCE_CONFFOLD` is set.
9. **Wait for the dpkg/apt lock instead of failing,** because unattended-upgrades often runs on first boot. `-o DPkg::Lock::Timeout=<sec>` only covers the dpkg locks. Before `apt-get update` or any download, also wait for `apt-daily.service` and `apt-daily-upgrade.service` to be inactive, and retry on "Could not get lock".
10. **UI text is in plain language.** Every error says what failed and what to do next.
11. **When behavior changes, update `docs/steps-catalog.md` in the same change.** The catalog is the source of truth for behavior.
12. **Verify before relying on any Debian package name, path or default.** Use current primary sources and record each one in `docs/debian13-notes.md`. Never write code from memory.

13. **Don't make design decisions for the owner.** New options, changed defaults, dropping or adding packages or behavior, and changing how or where something is done are all the owner's call. Explain the situation and the options, give a recommendation, record it as **proposed** in `docs/decisions.md`, and wait. A correction the facts force (only one way to meet the brief) may go in directly, recorded as **forced** and called out in the report. Never implement anything marked "(proposed: Pn)" or any entry with **Status: proposed** until the owner approves it.

These also follow from the design:
- **Process model** (architecture.md, D41):
  - one action at a time, each waiting for exit;
  - checks poll until a deadline;
  - other package managers are runtime-masked during a run;
  - kiln runs in its own systemd scope, and children get their own session with stdin `/dev/null`;
  - single-instance and per-helper `flock`s;
  - interrupted dpkg is repaired at the next start;
  - kiln never kills apt or dpkg.
- **Risky changes are reverted on failure.** Network migration, SSH and firewall changes are validated first. Then the change is applied, checked, and reverted from the backup straight away if the check fails. A "change in progress" marker in `/etc/kiln/state.json` lets the next start offer a revert if kiln died mid-change.
- **Secrets stay out of logs.** The remote-desktop password never reaches logs or the summary. It lives only in `/etc/kiln/secrets/` (0600).
- **Large downloads go to `/var/tmp`, never `/tmp`,** because `/tmp` is a tmpfs on trixie.
- **The code builds on darwin.** Linux-only code lives in `*_linux.go` files, with stubs for other platforms, so everything builds and lints there.

## Architecture in one paragraph

`cli` (cobra) gathers `facts.Facts`: hardware, plus what the installer chose. It then loads the previous choices from `/etc/kiln/state.json` and starts the wizard (`internal/tui`). The order of choices is defaults, then last run's choices, then what the user edits in the wizard. `runner` walks the steps from `internal/steps/<id>` in catalog order. For each step it decides "skipped by you", "not applicable" or "dependency failed". Otherwise it runs `Check` (if already done, the step is `unchanged`), then `Plan`, then `Apply`, then `Check` again. Steps change the system only through typed `action.Action`s, executed by a `sys.System`: `Real`, or `Recorder` for plan and review. The runner emits events to the TUI and the log. Helpers that systemd units need are subcommands of the same binary and share logic with their step packages: `kiln battery apply|watch`, `kiln thermal apply`, `kiln mirrors bench|reset`. Details are in `docs/architecture.md`.

## Docs reading order

1. `docs/product-spec.md`: what and why, how it's used, what's out of scope
2. `docs/architecture.md`: interfaces, runner, revert-on-failure, helpers, state
3. `docs/steps-catalog.md`: the per-step behavior contract (**source of truth**)
4. `docs/debian13-notes.md`: verified trixie facts with sources
5. `docs/options-reference.md`: every wizard option, its default, and the state file
6. `docs/tui-design.md`: screens, keys, responsive layout, console constraints
7. `docs/review-process.md`: how we reach sign-off without tests
8. `docs/distribution.md`: install.sh, the one-liner, releases
9. `docs/roadmap.md` and `docs/decisions.md`: milestones, decisions log

Slash commands: `/new-step <id>`, `/review-step <id>`, `/check-packages [step]`, `/audit`.
