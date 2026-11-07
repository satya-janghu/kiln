# Options reference

There are no profile files. Options are set in the wizard each time (the checklist plus the per-step option forms). Each option's starting value is the **built-in default** below, or what you chose on **this machine's last run** if there is one (stored in `/etc/kiln/state.json`). Press `d` in the checklist to reset to the defaults. **All defaults are "on".**

Every step in [steps-catalog.md](steps-catalog.md) also has an on/off checkbox (default on). Only the options inside a step are listed here. The catalog describes what each one does.

## Options by step

| Step | Option | Default | Values / notes |
|---|---|---|---|
| (all) | target user | auto | `SUDO_USER`, then the first UID ≥ 1000; asked if neither is found |
| sudo | passwordless sudo | on | writes `/etc/sudoers.d/90-<user>-nopasswd` |
| apt-sources | trixie-backports | on | packages come from backports only when a step asks for them |
| apt-sources | dpkg lock wait | 10 min | how long to wait for unattended-upgrades |
| mirrors | country | auto | ISO code; auto = from the timezone (Asia/Kolkata → IN); never geo-IP |
| mirrors | include neighboring countries | on | |
| mirrors | worldwide sample | 4 | random mirrors from outside the region |
| mirrors | mirrors to keep | 5 | the top N go in the mirrorlist; deb.debian.org is always the last fallback |
| mirrors | test size / time cap | 40 MB / 6 s | per mirror, tested one at a time |
| upgrade | full-upgrade | on | |
| firmware-packages | CPU microcode | on | AMD or Intel, whichever is detected |
| firmware-packages | non-free firmware | on | GPU / Wi-Fi / NIC firmware matched to the hardware |
| kernel | backports kernel | on | `linux-image-amd64` from trixie-backports |
| kernel | keep stable fallback current | on | newest `linux-image-6.12.*` kept installed; weekly timer |
| network-migration | move interfaces to NetworkManager | on | applies only if `/etc/network/interfaces` configures a physical NIC |
| network-migration | include Wi-Fi | on | |
| multimedia | codecs | on | |
| multimedia | DVD playback (libdvd-pkg) | on | |
| multimedia | video acceleration (VA-API) | on | |
| multimedia | NVIDIA driver | on | only if an NVIDIA GPU is found; prepares MOK enrollment if Secure Boot is on |
| repos | extrepo repos | `google_chrome` | names checked with `extrepo search`; desktop-only repos need GNOME |
| repos | Flathub | on | needs GNOME |
| devtools | GitHub CLI source | github | `github` (cli.github.com apt repo) or `debian` (2.46, old) |
| devtools | extra packages | none | free-text list, validated against apt before the review screen |
| system | hostname | keep current | |
| system | timezone | keep current | e.g. `Asia/Kolkata` |
| system | time sync | timesyncd | `timesyncd` or `chrony` |
| system | journal size cap | 500M | |
| system | inotify watches / instances | 524288 / 1024 | applied as max(current, this) |
| system | zram swap | on | `min(ram / 2, 4096)` MiB, zstd |
| power | always-on (no sleep, lid ignored) | on | |
| power | lock GNOME no-suspend settings | on | GNOME only |
| power | power manager | auto | power-profiles-daemon with GNOME, tuned without a desktop |
| thermal | performance platform profile | on | |
| thermal | thinkfan (ThinkPads) | on | only if fan control is exposed |
| battery | target stop % | 80 | the ladder tries values below 90, nearest to this first |
| battery | start gap | 5 | start = stop − gap, where supported |
| battery | software fallback (inhibit-charge watcher) | on | |
| firewall | backend | firewalld | `firewalld` or `nftables` |
| firewall | LAN subnets | auto | RFC1918 IPv4 + IPv6 ULA on physical NICs, or a list of CIDRs |
| firewall | extra open ports | none | open to the internet, e.g. `8080/tcp`; shown as a warning on review |
| firewall | block Docker published ports | off | firewalld `StrictForwardPorts=yes` |
| ssh | GitHub user for keys | empty | fetches `https://github.com/<user>.keys` |
| ssh | extra public keys | none | |
| ssh | port | 22 | |
| ssh | password logins | auto | auto = off once at least one key is installed |
| ssh | MaxAuthTries | 4 | |
| fail2ban | ban time / increment / max retries | 1h / on / 5 | |
| docker | log rotation | 10m × 3 | json-file driver |
| docker | run hello-world | on | |
| cockpit | from backports | on | falls back to trixie main if backports is off |
| remote-desktop | RDP user name | target user | |
| remote-desktop | RDP password | asked | masked, with "generate one"; stored in `/etc/kiln/secrets/rdp` |
| autoupdates | scope | security | `security` or `all` |
| autoupdates | automatic reboot | off | |
| node | nvm / Node versions | v0.41.0 / 26 | the owner's pinned choice |
| python | venv path | `~/.venvs/automation` | |
| python | uv source | pipx | `pipx` or `installer` (official script, via the audited helper) |
| python | Playwright Chromium | on | system deps installed by kiln's apt helper |
| python | uv tools | ruff, pre-commit, httpie | |
| go | version | latest | latest stable from go.dev, or e.g. `1.27.3` |
| go | Debian fallback | on | `golang-go` from backports if go.dev can't be reached |
| go | tools | gopls, dlv, staticcheck, govulncheck | |
| firmware-updates | fwupd updates | on | |

Defaults that are detected rather than chosen (the desktop, the SSH server, blends) are not options. See the facts screen.

## State file: `/etc/kiln/state.json`

Root-owned, mode 0600, written at the end of every run (and when `pending` changes). It never holds secrets. Shape:

```json
{
  "version": 1,
  "last_run": "2026-11-07T10:42:00+05:30",
  "kiln_version": "1.0.0",
  "choices": { "steps": { "nvidia": false }, "options": { "ssh.port": 22 } },
  "battery": { "method": "thresholds", "start": 75, "stop": 80, "battery": "BAT0" },
  "power_manager": "power-profiles-daemon",
  "pending": null
}
```

`pending` is non-null only while a risky change is in progress. It is `{ "step": "network-migration", "backups": [...], "revert": "..." }`. Only options that differ from the defaults are stored in `choices`.

## Environment variables

These are for debugging only. None of them is a preset:
- `KILN_ASCII=1` forces the console glyph set.
- `NO_COLOR` disables color.
