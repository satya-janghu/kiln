# Roadmap

## Where things stand (2026-11-08)

- **The first version is retired.** It works and was field-tested on the HP Envy x360 test box (10.10.10.150), but it was over-engineered: persistent helpers, saved state, re-run machinery, subcommands and too many options (D54).
- **The rewrite is planned and not yet started.**
  - **Scope:** D55 in [decisions.md](decisions.md).
  - **Battery:** D56.
  - **Behaviour:** [steps-catalog.md](steps-catalog.md).
  - **Facts:** [debian13-notes.md](debian13-notes.md).
- **Open items before or during the rewrite:**
  1. **HP battery hold mode (`0x0100`):** a test was running on the test box on 2026-11-08 (discharge to 85%, then hold, then normal). If hold doesn't work, D56's fallback goes back to the owner.
  2. **Battery check interval:** the owner suggested 300 s; 60 s is proposed (D56).
  3. **The proposed package layout** in [architecture.md](architecture.md) needs the owner's OK.
  4. **Facts marked U** in debian13-notes.md must become V before the code relies on them.
- **Test box:** `acpica-tools` and `acpi-call-dkms` were installed on it during the battery investigation. It can be reinstalled freely.

## R1: Skeleton

- Detection: hardware (CPU vendor, GPUs with PCI IDs, battery, Wi-Fi), GNOME or not, and the target user.
- The safety check: Linux, root, Debian 13 amd64.
- The three TUI screens: checklist, progress and summary.
- A runner that does each step in order, streams output to the screen and `/var/log/kiln.log`, records failures and skips the steps that need them.
- `make build lint vuln` clean.

## R2: Steps

The items in [steps-catalog.md](steps-catalog.md), in catalog order. Old code from `internal/steps/*` is reused where it fits; the old framework (`sys.Recorder`, actions, options, state, revert, helpers) is removed.

## R3: Test box runs

1. A fresh Debian 13 install on the test box, then a kiln run with everything ticked.
2. A second run, to confirm rerunning is safe.
3. A GNOME machine and the NVIDIA machines (RTX 2070 Super, GTX 970).

## R4: Release

- `install.sh`, the GitHub Pages short URL, GoReleaser v2 and the release workflow ([distribution.md](distribution.md)).
- An `/audit` pass with nothing left to fix, and both the owner and Claude sign off.
- Tag `v1.0.0`.
