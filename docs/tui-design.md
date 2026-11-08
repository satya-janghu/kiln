# TUI design

Three screens: **checklist**, **progress**, **summary** (D55). Built with Bubble Tea v2, Bubbles v2 and Lip Gloss v2; versions are in [decisions.md](decisions.md). Most examples online are v1, so follow the v2 upgrade guide: `View()` returns a `tea.View`, keys arrive as `tea.KeyPressMsg`, and alt screen is a `View` field.

## Look: Windows XP text-mode Setup (D42)

| Element | kiln |
|---|---|
| Background | ANSI blue `4`, every cell of the terminal |
| Title | "kiln Setup" top-left, with a `═` rule under it (console: `=`) |
| Text | light grey `7`; emphasis bright white `15`; warnings bright yellow `11` |
| Panels | double-line border `╔═╗║╚╝` (console: `+=+|`) |
| Progress bar | bright yellow `11` fill in a single-line frame, percentage above |
| Selected row | inverse: light grey background, blue text |
| Status bar | full-width light grey, black text. Left: key hints (`ENTER=Start  F3=Quit`). Right, after `│`: the current action, e.g. `Installing: docker-ce` |
| Failures | bright red `9` with a `!!` marker |

Only these 16 colors. Color is never the only signal: every status also has a marker.

## Where it runs

The Linux text console (`TERM=linux`) of a freshly installed machine, usually before anyone logs into a desktop.

- **Size:** 80x24 minimum (D32); the VT's default 80x25 fits. Smaller shows one line: "Make the window at least 80x24". Larger sizes use the extra room for the output pane.
- **Glyphs:** the console font has no braille, emoji or Nerd Font glyphs. With `TERM` `linux`, `vt100` or `dumb`, kiln uses ASCII: `[ ]` `[x]` `ok` `!!` `--`, a `| / - \` spinner and `+=|` borders.
- **Keyboard only.** No mouse needed.

## Keys

| Key | Action |
|---|---|
| `↑`/`↓` | move |
| `space` | tick or untick (checklist) |
| `enter` | start the run (checklist) |
| `PgUp`/`PgDn` | scroll the output (progress, summary) |
| `F3` or `ctrl+c` | quit. During a run it asks "Stop after the current step?"; kiln never kills apt or dpkg mid-install. |

## 1. Checklist

```
 kiln Setup
 ══════════
  HP ENVY x360 13 · AMD Ryzen 5 4500U · 8 GB · no desktop · battery · no NVIDIA

  Always done: safety check, package sources, full upgrade, firmware

  [x] Passwordless sudo for satya
  [x] System basics (zram, journal, inotify, time sync)
  [x] SSH with password logins
  [x] Firewall (UFW) and fail2ban
  ...
  [x] Battery limit (70-80%)
 ───────────────────────────────────────────────────────────────────────
  ENTER=Start  SPACE=Toggle  F3=Quit
```

- One line of what was detected.
- Every item is ticked. Items for missing hardware are not shown.
- No sub-options, no forms, no filter.

## 2. Progress

- The list of items with markers: pending, running, done, failed, skipped.
- A progress bar: items done out of the total.
- A live output pane with the current command's output, also written to `/var/log/kiln.log`.
- The status bar shows the current action.

## 3. Summary

```
 Done in 14m 32s: 17 done · 1 failed · 0 skipped
  !! Battery limit: no charge control found. Use the BIOS option if there is one.
  Notes
   - SSH accepts passwords for satya and root from anywhere SSH is reachable.
   - Cockpit: https://192.168.1.23:9090   Portainer: https://192.168.1.23:9443
   - Docker's published ports bypass UFW. Bind to 127.0.0.1 to keep one private.
   - Log out and back in for the docker group.
  Reboot needed: new kernel
```

- Failures come first, each with what to do next.
- With Secure Boot and NVIDIA, the MOK one-time password is shown here, once, with what to do at the next boot. It is not written anywhere.
- Keys: `PgUp`/`PgDn`, `F3` quit. **Proposed:** `ENTER=Reboot` when a reboot is needed.

## Gone

The welcome/facts screen, the review/plan screen, option forms, category toggles and filters, the "revert it?" and "already running" prompts, the battery-below-30% question, `kiln plan`/`kiln facts` output, and `/var/log/kiln-summary.txt`.
