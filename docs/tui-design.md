# TUI design

Built with Bubble Tea v2, Bubbles v2, Lip Gloss v2 and Huh (for forms). Versions and import paths are in [decisions.md](decisions.md). Most examples online are v1, so follow the official v2 upgrade guide. In v2:

- `View()` returns a `tea.View` (content plus settings such as `AltScreen`);
- keys arrive as `tea.KeyPressMsg`;
- Lip Gloss has no implicit adaptive colors.

## Look: Windows XP Setup (D42)

The owner asked for the look of the Windows XP text-mode installer. It maps cleanly onto the Linux console's 16 colors, which is where kiln usually runs.

| Element | XP Setup | kiln (16-color / truecolor) |
|---|---|---|
| Screen background | solid blue | ANSI blue `4` / `#0000AA`, filling the whole terminal |
| Title | "Windows XP Professional Setup", top-left, with a double rule underneath | "kiln Setup" + a `═` rule (console: `=`) as wide as the title, light grey `7` / `#AAAAAA` |
| Body text | light grey; key words brighter | light grey `7`; emphasis bright white `15`; warnings bright yellow `11` |
| Panels | double-line frame | double-line border `╔═╗║╚╝` (console set: `+=+|`), light grey on blue |
| Progress bar | yellow fill inside a single-line frame, percentage above | bright yellow `11` fill on blue inside a single-line frame, percentage centred above |
| Selected list row | inverse | light grey `7` background, blue `4` text |
| Status bar | full-width light grey bar, black text, right part after `│` showing the current file | light grey `7` background, black `0` text. Left: key hints in XP style (`ENTER=Continue  ESC=Back  F3=Quit`). Right, after `│`: the current action, e.g. `Installing: docker-ce` |
| Errors | white text on blue | bright white `15` with a `!!` marker; failed steps in bright red `9` |

Rules:
- The whole screen is painted blue: every cell, not just the text, so there are no terminal-colored gaps.
- Keys follow XP Setup as well as the usual ones: `F3` quits (with the same confirmation as `ctrl+c`), and `ENTER` continues.
- No other colors. Light and dark terminal backgrounds don't matter, because kiln paints its own background.

## Where it runs

The usual place is the **Linux text console** of a freshly installed machine (`TERM=linux`), because the first run often happens before anyone logs into a desktop. On GNOME machines it can also run later in GNOME Terminal. Both must look right.

**Linux console limits:**
- Only 16 colors. Bubble Tea v2 downsamples automatically, but the styles must stay readable in 16 colors, so never use color as the only signal.
- The console font has no braille, emoji or Nerd Font glyphs, so no Bubbles spinner that uses braille.
- There is no mouse, so everything is keyboard-only.

**Two glyph sets, chosen at startup:**
- **Console set** (when `TERM` is `linux`, `vt100` or `dumb`, or `KILN_ASCII=1` is set): status markers `[ ]` `[x]` `ok` `==` `!!` `--` `>>`, spinner `| / - \`, and plain ASCII box borders.
- **Rich set** (any other terminal): `✓ ✗ = – ·`, rounded Lip Gloss borders, and a dot spinner.

Light or dark detection isn't needed: kiln paints every cell with its own blue background (D42).

## Responsive layout

The UI fills the whole terminal and re-lays itself out on every `tea.WindowSizeMsg`. 80x24 is the **minimum**, not the target size. A 1080p laptop console is often around 240x67.

| Width | Layout |
|---|---|
| < 80 or height < 24 | One line: "Make the window at least 80x24 (now W×H)". |
| 80–119 | Panes stacked: step list on top, log pane below it. The log takes all the remaining height. |
| ≥ 120 | Panes side by side: step list on the left (about 40 columns, or 30% of the width), log on the right, both full height. |

Other layout rules:
- Lists and the log scroll in viewports sized to the space they have.
- Long lines wrap in the log. In lists they're cut off with `…` (`...` with the console set).
- Help lines shrink to the most important keys when the window is narrow.

## Global keys

| Key | Action |
|---|---|
| `↑`/`↓` or `k`/`j` | move |
| `enter` | next / open |
| `esc` | back (your edits are kept) |
| `?` | help for this screen |
| `F3` | quit (XP Setup style), same as `ctrl+c` |
| `ctrl+c` | quit. During a run it asks "Stop after the current step?" and never kills apt halfway through an install. Closing the terminal doesn't stop a run: kiln keeps going in its own scope and logs to `/var/log/kiln.log`, and the next `sudo kiln` shows the result |

## Screens

### 0. Before anything else

These checks run in this order, each only when it applies:
1. **Another kiln is running:** "kiln is already running (pid N). [l] view its log  [q] quit". Nothing else is possible.
2. **Interrupted change** (`state.json` has `pending`): "A network change didn't finish last time. Revert it now? [y/n]".
3. **Interrupted run:** "The last run stopped during *Docker* (14:22, power lost?). kiln will repair the package system and then run again; every step is safe to repeat." If dpkg was interrupted, the repair runs with its own progress view.
4. **On battery below 30%:** "Plug in the charger. A run that loses power while installing packages can break the system. [c] continue anyway".

### 1. Welcome / facts

```
 kiln: set up this machine                               Debian 13 (trixie) amd64
 ─────────────────────────────────────────────────────────────────────────────────
  Machine     LENOVO ThinkPad E14 Gen 5 (21JR)          Laptop, on AC, battery 74%
  CPU / RAM   AMD Ryzen 5 7530U, 6 cores / 15.0 GiB
  Graphics    AMD Radeon (integrated)                   Secure Boot: on
  Installed   GNOME desktop: yes   SSH server: yes   Blend: none
  Network     wlp3s0 192.168.1.23/24 (NetworkManager)   enp2s0 no link
  User        satya (uid 1000)                          Running as root via sudo
  Warnings    none
 ─────────────────────────────────────────────────────────────────────────────────
  enter continue   f all facts   q quit
```

If the session is over SSH, the Warnings line says: "You're connected over SSH. The network and SSH steps may disconnect you; kiln is meant to run at the machine."

### 2. Step checklist

Steps are grouped by category, and everything starts checked, or pre-set from this machine's last run (`d` resets to defaults). Steps that don't apply stay checked, with the reason shown dimmed.

```
 Choose what to set up                         24 of 26 on · filter: _
  v [x] Base system
      [x] sudo access for satya
      [x] Software sources (deb822, backports)
      [x] Fastest mirrors (by download speed)
  v [x] Graphics and media
      [x] Codecs and video acceleration
      [x] NVIDIA driver                    not applicable: no NVIDIA GPU found
  > [x] Security (4)
      [x] SSH hardening                    not applicable: no SSH server installed
 ──────────────────────────────────────────────────────────────────────────────
  space toggle  c category  / filter  a all on  n all off  o options  d defaults  enter review
```

- `space` toggles one item.
- `c` toggles the whole category under the cursor.
- `/` filters live by title, ID or category.
- `a` turns everything on. `n` turns everything off, after asking first.
- Turning off a step that others depend on shows "Also skipped: Cockpit, fail2ban".

### 3. Options

Press `o` on an item to open a Huh form for that step's options, for example:
- hostname and timezone;
- SSH port and password logins;
- battery target;
- update scope;
- mirror country.

Forms validate as you type: hostname rules, port range, and "GitHub user has no public keys".

### 4. Review plan

This screen is drawn from the same plan the runner will execute (rule 7). It has tabs: **Summary · Packages · Files · Services · Commands**, all in scrollable viewports. Warnings come first:

```
 Review: nothing has changed yet
  ! SSH password logins are on for satya and root, from anywhere SSH is reachable (D45). Use strong passwords.
  ! Network: enp2s0 moves from ifupdown to NetworkManager (reverted automatically if it fails).
  214 packages · 31 files · 18 services · reboot likely (new kernel)
 [Summary] Packages  Files  Services  Commands
  ...
  enter start   esc back
```

### 5. Run

The run screen shows:
- **Step list**, with status markers (pending, running, done, unchanged, failed, skipped).
- **Live log pane**, which shows the current command's output. `PgUp`/`PgDn` scroll it, and `l` makes the log full screen.
- **Progress bar on top**, with steps done out of the total and the current step's elapsed time.
- **Status line for risky steps**, for example "Checking network after the switch… ok" or "…failed, reverted".

### 6. Summary

```
 Done in 14m 32s: 21 done · 3 unchanged · 1 failed · 1 skipped
  !! Battery limit: no charge control found on HP ENVY x360 13-ay0xxx.
      Look in BIOS/UEFI for "Battery Health Manager" or "Adaptive Battery Optimizer".
  Notes
   - Cockpit: https://192.168.1.23:9090, log in as satya
   - Log out and back in for docker group membership
  Reboot needed: new kernel 7.2 (backports)
 ──────────────────────────────────────────────────────────────────────────────
  r reboot now   l full log   q quit
```

The summary is also saved to `/var/log/kiln-summary.txt`, without secrets.

## `kiln plan` and `kiln facts` output

These print plain text, with no ANSI codes when stdout isn't a TTY or `NO_COLOR` is set.
