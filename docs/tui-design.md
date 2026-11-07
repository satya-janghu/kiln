# TUI design

Built with Bubble Tea v2, Bubbles v2, Lip Gloss v2 and Huh (for forms). Versions and import paths are in [decisions.md](decisions.md). Most examples online are v1, so follow the official v2 upgrade guide. In v2:

- `View()` returns a `tea.View` (content plus settings such as `AltScreen`);
- keys arrive as `tea.KeyPressMsg`;
- Lip Gloss has no implicit adaptive colors.

## Where it runs

The usual place is the **Linux text console** of a freshly installed machine (`TERM=linux`), because the first run often happens before anyone logs into a desktop. On GNOME machines it can also run later in GNOME Terminal. Both must look right.

**Linux console limits:**
- Only 16 colors. Bubble Tea v2 downsamples automatically, but the styles must stay readable in 16 colors, so never use color as the only signal.
- The console font has no braille, emoji or Nerd Font glyphs, so no Bubbles spinner that uses braille.
- There is no mouse, so everything is keyboard-only.

**Two glyph sets, chosen at startup:**
- **Console set** (when `TERM` is `linux`, `vt100` or `dumb`, or `KILN_ASCII=1` is set): status markers `[ ]` `[x]` `ok` `==` `!!` `--` `>>`, spinner `| / - \`, and plain ASCII box borders.
- **Rich set** (any other terminal): `✓ ✗ = – ·`, rounded Lip Gloss borders, and a dot spinner.

Choosing light or dark colors uses `tea.BackgroundColorMsg` with `lipgloss.LightDark`. On the Linux console, assume a dark background.

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
| `ctrl+c` | quit. During a run it asks "Stop after the current step?" and never kills apt halfway through an install |

## Screens

### 0. Interrupted change (only if `state.json` has a `pending` entry)

"A network change didn't finish last time. Revert it now? [y/n]". This screen is shown before anything else.

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

If the session is over SSH, the Warnings line says: "You're connected over SSH. Network and firewall steps may disconnect you; kiln is meant to run at the machine."

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
- GitHub user for SSH keys, extra keys and port;
- remote-desktop password: masked, with a confirm field and a "generate one" choice;
- battery target;
- update scope;
- mirror country.

Forms validate as you type: hostname rules, port range, and "GitHub user has no public keys".

### 4. Review plan

This screen is drawn from the same plan the runner will execute (rule 7). It has tabs: **Summary · Packages · Files · Services · Firewall · Commands**, all in scrollable viewports. Warnings come first:

```
 Review: nothing has changed yet
  ! SSH becomes key-only (3 keys from github.com/satya). Password logins will stop working.
  ! Network: enp2s0 moves from ifupdown to NetworkManager (reverted automatically if it fails).
  214 packages · 31 files · 18 services · 6 firewall rules · reboot likely (new kernel)
 [Summary] Packages  Files  Services  Firewall  Commands
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
   - Cockpit: https://192.168.1.23:9090 (open to 192.168.1.0/24), log in as satya
   - Docker: -p published ports bypass the firewall; bind private services to 127.0.0.1
   - Log out and back in for docker group membership
  Reboot needed: new kernel 7.2 (backports)
 ──────────────────────────────────────────────────────────────────────────────
  r reboot now   l full log   q quit
```

The summary is also saved to `/var/log/kiln-summary.txt`, without secrets.

## `kiln plan` and `kiln facts` output

These print plain text, with no ANSI codes when stdout isn't a TTY or `NO_COLOR` is set.
