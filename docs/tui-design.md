# TUI design

Three screens: **checklist**, **progress**, **summary** (D55). Built with Bubble Tea v2, Bubbles v2 and Lip Gloss v2; versions are in [decisions.md](decisions.md). Most examples online are v1, so follow the v2 upgrade guide: `View()` returns a `tea.View`, keys arrive as `tea.KeyPressMsg`, and alt screen is a `View` field.

## Look: Okabe Night (D57)

The owner's palette, kept in [color-system.md](color-system.md) with its tokens in `color-tokens.json`: dark only and colorblind-safe (Okabe-Ito hues). The screens use its semantic tokens only (`internal/tui/theme.go`), never raw colors.

| Element | Token |
|---|---|
| Every cell | text `text.primary` `#EDF0F4` on `bg.canvas` `#090E13` |
| Title "kiln Setup" | bold `primary.text`, with a `─` rule in `border.default` |
| Descriptions, command output | `text.secondary`, `text.muted` |
| Ticked box `[x]` | `primary.text`; unticked `[ ]` in `text.muted` |
| Cursor row | `>` marker in `accent.text` on `bg.selected` |
| Status bar | `text.secondary` on `bg.raised`; key names in bold `accent.text` |
| Done / running / failed / reboot | `success.text` "+ done", `info.text` "> running", `danger.text` "x FAILED", `warning.text` "! A reboot is needed" |

- **Color is never the only signal:** every status has a word and a marker (`+ done`, `> running`, `x FAILED`, `- skipped`, `! reboot`, `i` info), and the cursor has `>`.
- **No red/green:** failures are vermilion, success is bluish green.
- The tokens are truecolor. Bubble Tea downsamples them to what the terminal supports, so on the Linux console (16 colors) they become the nearest console colors and the words and markers still carry the meaning.

## Where it runs

The Linux text console (`TERM=linux`) of a freshly installed machine, usually before anyone logs into a desktop.

- **Size:** 80x24 minimum (D32); the VT's default 80x25 fits. Smaller shows one line: "Make the window at least 80x24". Larger sizes use the extra room for the output pane.
- **Glyphs:** the console font has no braille, emoji or Nerd Font glyphs, so kiln uses ASCII markers (`[ ]` `[x]` `>` `+` `x` `-` `!`) and only the `─` rule, which the console font has.
- **Keyboard only.** No mouse needed.

## Keys

| Key | Action |
|---|---|
| `↑`/`↓` | move |
| `space` | tick or untick (checklist) |
| `enter` | start the run (checklist) |
| `PgUp`/`PgDn` | scroll (summary) |
| `Q` (also `Esc`, `ctrl+c`, `F3`) | quit. During a run it asks "Stop after the current step?"; kiln never kills apt or dpkg mid-install. The status bar says `Q`, because the Linux console's F3 code isn't recognised by Bubble Tea v2 (review, 2026-11-08). |

## 1. Checklist

```
 kiln Setup  one-time setup for Debian 13
 ────────────────────────────────────────────────────────────────────────
  This sets up HP ENVY x360 Convertible 13-ay0xxx (no desktop) for satya.
  Always done: package sources, full upgrade, firmware and microcode.
  Untick anything you don't want, then press ENTER to start.

  > [x] Passwordless sudo                Your user can use sudo without...
    [x] System basics                    zram swap as big as RAM, log s...
    ...
    [x] Battery limit (70-80%)           Keeps the battery between 70%...
 ENTER=Start  SPACE=Tick/untick  A=All/none  Up/Down=Move  Q=Quit
```

- One line of what was detected.
- Every item is ticked. Items for missing hardware are not shown. If the list doesn't fit, "... N more below" / "more above" lines say so.
- No sub-options, no forms, no filter.

## 2. Progress

- The list of items with markers: waiting, running, done, FAILED, skipped.
- A live output pane with the current command's output, also written to `/var/log/kiln.log`.
- The status bar shows how many items are done out of the total.

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
- Keys: `PgUp`/`PgDn` scroll, `ENTER` reboots when a reboot is needed, `Q` quits without rebooting. When a one-time password is shown, ENTER first asks "Did you write down the one-time password? Y/N".

## Gone

The welcome/facts screen, the review/plan screen, option forms, category toggles and filters, the "revert it?" and "already running" prompts, the battery-below-30% question, `kiln plan`/`kiln facts` output, and `/var/log/kiln-summary.txt`.
