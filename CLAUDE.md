# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Read this first: the small rewrite (2026-11-08)

The owner found the first version over-engineered and chose a **small rewrite** (approach B in `docs/decisions.md` D54). **D55** (scope) and **D56** (battery) in `docs/decisions.md` are the plan; later entries (D57–D61) refine it. They override anything older in the docs or the code.

The rewrite is implemented (2026-11-08): `cmd/kiln`, `internal/detect`, `internal/run`, `internal/steps` (one file per item, list in `steps.go`), `internal/tui` (three screens, `theme.go` holds the Okabe Night tokens, D57). About 4,800 lines. The old tool is only in git history.

## What this is

`kiln` is a **one-time setup tool** for a fresh **Debian 13 "trixie" amd64** install. The owner runs it once at the machine's console, as root, right after the Debian installer finishes. It installs the packages and writes the configuration (SSH included) that would otherwise be typed by hand on every homelab machine.

- **One checklist, the same on every install** (GNOME or server). All items are ticked by default. There are no sub-options, and every value is fixed. Only hardware hides an item (NVIDIA card, battery).
- **Nothing is saved, and nothing of kiln keeps running.** The two exceptions are D56's battery script and timer on laptops that need it, and D61's weekly Docker prune timer. Otherwise kiln leaves only its log at `/var/log/kiln.log`.
- **Rerunning is safe.** It isn't a feature: apt installs and file writes are naturally repeatable. There's no machinery for it.
- The Debian installer decides GNOME or no desktop; kiln never changes that.

Development happens on **macOS (Apple Silicon)**. The binary only ever runs on Debian 13 amd64.

## Owner's rules (most important)

1. **Keep it simple and one-time (D54).** Don't add anything that keeps kiln on the machine, runs it again later, saves state, or asks another question. Prefer Debian's defaults and fewer options. Think "fresh install, run once": edge cases from re-runs or hand-modified machines aren't worth code or questions.
2. **Don't make design decisions for the owner.** New options, changed defaults, adding or dropping packages or behaviour, and changing how or where something is done are the owner's call. Explain, recommend, record it as **proposed** in `docs/decisions.md`, and wait. A correction the facts force may go in directly, recorded as **forced** and called out. Never implement a **Status: proposed** item until it's approved.
3. **Verify before relying on any Debian fact.** Every package name, path, config key, command flag and default is checked against a current primary source (packages.debian.org/trixie, manpages.debian.org/trixie, sources.debian.org, release notes, upstream docs) and recorded in `docs/debian13-notes.md`. A **U** fact can't be relied on until it's **V**. Never write code from memory.
4. **When behaviour changes, update `docs/steps-catalog.md` in the same change.** The catalog is the behaviour contract.

## Engineering rules

- **Never apply anything on the development machine.** kiln refuses to change anything unless it's on Linux, as root, on Debian 13 amd64. Never run it against a real system from this Mac.
- **No `bash -c` with interpolated strings.** Use `exec.Command(name, args...)`. Piped installers (nvm, uv) are downloaded to a file and run with `bash <file>` as the user.
- **apt runs fully non-interactive:**
  - `DEBIAN_FRONTEND=noninteractive`, `NEEDRESTART_MODE=a`, `APT_LISTCHANGES_FRONTEND=none`, `UCF_FORCE_CONFFOLD=1`, `LC_ALL=C`;
  - `--force-confdef --force-confold`;
  - debconf preseeded where needed.
- **Wait for the dpkg/apt lock instead of failing.** Unattended-upgrades often runs on first boot. Wait for `apt-daily.service` and `apt-daily-upgrade.service` to finish, and use `-o DPkg::Lock::Timeout`.
- **A failing step never stops the run.** It's recorded, steps that need it are skipped, the rest continue, and the summary says what failed and what to do.
- **Write files atomically** (temp file, then rename). User commands run via `runuser` with a clean environment.
- **SSH changes are checked with `sshd -t` before reloading.**
- **Large downloads go to `/var/tmp`, never `/tmp`** (a tmpfs on trixie).
- **Secrets stay out of logs.** The Secure Boot one-time password is shown only on screen.
- **UI text is plain language,** and every error says what to do next.
- **The code builds and lints on darwin:** Linux-only code lives in `*_linux.go`, with stubs elsewhere.

## How quality is assured: review, not testing

**There are no tests.** No `_test.go` files or harnesses unless the owner asks. Confidence comes from review against primary documentation plus runs on the owner's throwaway test box (D46; it may be reinstalled freely). `make lint` and `make vuln` must stay clean. The process is in `docs/review-process.md`.

## Stack

- **Language:** Go 1.27, static binary (`CGO_ENABLED=0`).
- **TUI:** Charm v2 (`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`, `charm.land/lipgloss/v2`). Bubble Tea v2 is declarative: `View()` returns `tea.View` and keys arrive as `tea.KeyPressMsg`. Most examples online are v1, so follow `UPGRADE_GUIDE_V2.md` in the bubbletea repo.
- **Release:** GoReleaser v2 to GitHub Releases, with `install.sh` attached; the one-liner points at it (D58, D60).
- **The one-liner is always curl** (owner's rule, D60; short form, D62): `apt install -y curl && curl -fsSL https://tinyurl.com/kilnsetup | bash`. Every doc, message and script uses exactly this form; never switch it to wget.
- Pinned versions are in `docs/decisions.md` D1.

## Commands

```bash
make build    # GOOS=linux GOARCH=amd64 CGO_ENABLED=0 → dist/kiln
make lint     # go vet + golangci-lint (v2 config) + shellcheck install.sh
make vuln     # govulncheck ./...  (install govulncheck on the Mac first)
make release-check   # goreleaser check
make tidy     # go mod tidy (commit go.sum)
```

## Docs reading order

1. `docs/decisions.md`: "Current state", then D54, D55, D56
2. `docs/roadmap.md`: where things stand and the next steps
3. `docs/product-spec.md`: what and why, in and out of scope
4. `docs/steps-catalog.md`: what each item does (**source of truth**)
5. `docs/debian13-notes.md`: verified trixie facts with sources
6. `docs/architecture.md`: how the binary is put together
7. `docs/tui-design.md`: the three screens
8. `docs/review-process.md` and `docs/distribution.md`

Slash commands: `/new-step <id>`, `/review-step <id>`, `/check-packages [step]`, `/audit`.
