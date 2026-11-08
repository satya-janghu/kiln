# Roadmap

## Where things stand (2026-11-08)

- **The small rewrite is done** (D54–D58): about 4,800 lines of Go, down from 18,500. `make lint` is clean on darwin and linux.
- **Test box (HP Envy x360, server install):**
  - The first run did everything except Chrome. A check bug failed it; it's fixed now.
  - The second run did every item, which confirms that running kiln twice is safe.
  - The battery limit works: at 97% on AC the battery reports "Not charging" (HP mode 0x0500).
- **Published:** a public GitHub repo, a release with the binary, `checksums.txt` and `install.sh`, and the one-liner `apt install -y curl && curl -fsSL https://tinyurl.com/kilnsetup | bash` (D58, D60, D62).
- **Not yet run:**
  - a truly fresh install (the test box had been set up by the old kiln);
  - a GNOME machine;
  - the NVIDIA machines (RTX 2070 Super, GTX 970);
  - a ThinkPad (kernel thresholds);
  - Secure Boot.
- **Test box leftovers from the battery investigation:** `acpica-tools` and `/root/acpi`, `/root/hpbat-test*`. It can be reinstalled freely.

## R1: Skeleton (done)

- Detection: hardware (CPU vendor, GPUs with PCI IDs, battery, Wi-Fi), GNOME or not, and the target user.
- The safety check: Linux, root, Debian 13 amd64.
- The three TUI screens: checklist, progress and summary.
- A runner that does each step in order, streams output to the screen and `/var/log/kiln.log`, records failures and skips the steps that need them.
- `make build lint vuln` clean.

## R2: Steps (done)

The items in [steps-catalog.md](steps-catalog.md), in catalog order. Old code from `internal/steps/*` is reused where it fits; the old framework (`sys.Recorder`, actions, options, state, revert, helpers) is removed.

## R3: Test box runs

1. A fresh Debian 13 install on the test box, then a kiln run with everything ticked.
2. A second run, to confirm rerunning is safe.
3. A GNOME machine and the NVIDIA machines (RTX 2070 Super, GTX 970).

## R4: Release

- `install.sh`, the TinyURL one-liner, GoReleaser v2 and the release workflow ([distribution.md](distribution.md)): done.
- An `/audit` pass with nothing left to fix, and both the owner and Claude sign off.
- Tag `v1.0.0`.
