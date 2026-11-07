# Options reference

There are no profile files. Options are set in the wizard each time (the checklist plus the per-step option forms). Each option's starting value is the **built-in default** below, or what you chose on **this machine's last run** if there is one (stored in `/etc/kiln/state.json`). Press `d` in the checklist to reset to the defaults. **All defaults are "on".**

Every step in [steps-catalog.md](steps-catalog.md) also has an on/off checkbox (default on). Only the options inside a step are listed here. The catalog describes what each one does.

## Options by step

| Step | Option | Default | Values / notes |
|---|---|---|---|
| (all) | target user | detected | `SUDO_USER`, then the first UID ≥ 1000 with a login shell. It isn't an option: if none is found, preflight stops and says to create one with `adduser` |
| sudo | passwordless sudo | on | on: `/etc/sudoers.d/90-<user>-nopasswd`; off: `/etc/sudoers.d/90-<user>` with a password rule (D45). Works without re-login either way |
| apt-sources | trixie-backports | on | packages come from backports only when a step asks for them |
| apt-sources | dpkg lock wait | 10 min | how long to wait for unattended-upgrades |
| mirrors | country | auto | ISO code; auto = from the timezone (Asia/Kolkata → IN); never geo-IP |
| mirrors | worldwide sample | 4 | random mirrors from outside the region |
| mirrors | mirrors to keep | 5 | the top N share `priority:1` in the mirrorlist (apt spreads files across them); deb.debian.org is the `priority:99` fallback |
| mirrors | test size / time cap | 40 MB / 6 s | per mirror, one at a time; the test file is `main/Contents-all.gz` (~38 MB) |
| upgrade | full-upgrade | on | |
| firmware-packages | CPU microcode | on | AMD or Intel, whichever is detected |
| kernel | backports kernel | **off** | the one deliberate exception to "everything on" (D38). Every machine runs the stock trixie kernel (6.12 LTS, always updated to the latest); select this to use `linux-image-amd64` from trixie-backports (7.2.x) on this machine |
| kernel | backports firmware | on | only when the backports kernel is selected: reinstall the chosen `firmware-*` packages from trixie-backports |
| multimedia | codecs | on | |
| multimedia | DVD playback (libdvd-pkg) | on | |
| multimedia | video acceleration (VA-API) | on | |
| multimedia | NVIDIA driver | on | only if an NVIDIA GPU is found; prepares MOK enrollment if Secure Boot is on; unticked = Debian's open nouveau driver |
| multimedia | NVIDIA driver source | NVIDIA latest (Turing or newer) / Debian stock (older GPUs) | `NVIDIA latest`: NVIDIA's Debian 13 repo, newest branch, desktop-only set `nvidia-driver nvidia-kernel-open-dkms`, Turing+ only. `Debian stock`: trixie's `nvidia-driver` 550, Maxwell–Ada (Debian's wiki: no longer maintained). You pick per machine (D39) |
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
| power | power manager | auto | power-profiles-daemon with GNOME, tuned without it; both on their `balanced` profile (D52) |
| thermal | check the balanced platform profile | on | set by the power manager; kiln only verifies |
| thermal | thinkfan (ThinkPads) | on | only if fan control is exposed |
| battery | target stop % | 80 | the ladder tries values below 90, nearest to this first |
| battery | start gap | 5 | start = stop − gap, where supported |
| battery | software fallback (inhibit-charge watcher) | on | |
| ssh | port | 22 | |
| ssh | password logins | on | `on` (D45) or `off` (D51) |
| ssh | allow root to log in | on | `PermitRootLogin yes` (D45) |
| ssh | give root your user's password if it has none | on | copies the password hash (D45) |
| ssh | MaxAuthTries | 4 | the owner's choice (upstream default 6); agents offering many keys need `IdentitiesOnly` on the client |
| fail2ban | ban time / increment / max retries | 1h / on / 5 | |
| docker | log rotation | 10m × 3 | json-file driver |
| docker | run hello-world | on | |
| cockpit | from backports | on | falls back to trixie main if backports is off |
| autoupdates | scope | all | `all` = every package from every configured repository (`origin=*`); `security` = Debian security only (D40) |
| autoupdates | schedule | every 12 h | refresh at about 06:00 and 18:00, upgrade 30 min later (timer overrides on Debian's 06/18 slots); skipped on battery |
| autoupdates | automatic reboot | off | |
| node | nvm / Node versions | v0.41.0 / 26 | the owner's pinned choice |
| python | venv path | `~/.venvs/automation` | |
| python | uv source | pipx | `pipx` or `installer` (official script, via the audited helper) |
| python | Playwright Chromium | on | system deps installed by kiln's apt helper |
| python | uv tools | ruff, pre-commit, httpie | |
| go | version | latest | latest stable from go.dev, or e.g. `1.27.3` |
| go | Debian fallback | on | `golang-go` from backports if go.dev can't be reached |
| go | tools | gopls, dlv, staticcheck, govulncheck | |
| go | update tools on this run | off | off by design: "on" re-installs `@latest` every run, which would break "a second run changes nothing" () |
| firmware-updates | fwupd updates | on | |

Defaults that are detected rather than chosen (the desktop, the SSH server, blends) are not options. See the facts screen.

## Option keys as implemented

This table is generated from the code with `go run ./cmd/kiln catalog options`; regenerate it whenever options change. The table above explains what each option means.

Additional cases:
- The NVIDIA options (`multimedia.nvidia`, `multimedia.nvidia_source`) appear only on machines with an NVIDIA GPU, so they're missing from a list generated on a machine without one.

| Key | Label | Type | Default |
|---|---|---|---|
| `preflight` | *step on/off* | bool | on |
| `sudo` | *step on/off* | bool | on |
| `sudo.nopasswd` | Passwordless sudo | bool | true |
| `apt-sources` | *step on/off* | bool | on |
| `apt-sources.backports` | Enable trixie-backports | bool | true |
| `apt-sources.lock_wait` | Wait for other package managers (minutes) | int | 10 |
| `mirrors` | *step on/off* | bool | on |
| `mirrors.country` | Country | string | auto |
| `mirrors.sample` | Worldwide sample | int | 4 |
| `mirrors.top` | Mirrors to keep | int | 5 |
| `mirrors.test_mb` | Test size (MB) | int | 40 |
| `mirrors.test_seconds` | Time cap per mirror (seconds) | int | 6 |
| `upgrade` | *step on/off* | bool | on |
| `upgrade.full_upgrade` | Full upgrade | bool | true |
| `firmware-packages` | *step on/off* | bool | on |
| `firmware-packages.microcode` | CPU microcode | bool | true |
| `kernel` | *step on/off* | bool | off |
| `kernel.backports_firmware` | Backports firmware | bool | true |
| `multimedia` | *step on/off* | bool | on |
| `multimedia.codecs` | Codecs | bool | true |
| `multimedia.dvd` | DVD playback (libdvd-pkg) | bool | true |
| `multimedia.vaapi` | Video acceleration (VA-API) | bool | true |
| `repos` | *step on/off* | bool | on |
| `repos.extrepo` | extrepo repositories | list | [google_chrome] |
| `repos.flathub` | Flathub | bool | true |
| `devtools` | *step on/off* | bool | on |
| `devtools.gh_source` | GitHub CLI source | choice | github [github/debian] |
| `devtools.extra_packages` | Extra packages | list | [] |
| `system` | *step on/off* | bool | on |
| `system.hostname` | Hostname | string | (empty) |
| `system.timezone` | Timezone | string | (empty) |
| `system.time_sync` | Time sync | choice | timesyncd [timesyncd/chrony] |
| `system.journal_size` | Journal size cap | string | 500M |
| `system.inotify_watches` | inotify max_user_watches | int | 524288 |
| `system.inotify_instances` | inotify max_user_instances | int | 1024 |
| `system.zram` | zram swap | bool | true |
| `power` | *step on/off* | bool | on |
| `power.always_on` | Always on (no sleep, lid ignored) | bool | true |
| `power.lock_gnome_settings` | Lock GNOME no-suspend settings | bool | true |
| `power.power_manager` | Power manager | choice | auto [auto/power-profiles-daemon/tuned] |
| `thermal` | *step on/off* | bool | on |
| `thermal.check_profile` | Check the balanced platform profile | bool | true |
| `thermal.thinkfan` | thinkfan (ThinkPads) | bool | true |
| `battery` | *step on/off* | bool | on |
| `battery.target_stop` | Stop charging at (%) | int | 80 |
| `battery.start_gap` | Start charging this many points lower | int | 5 |
| `battery.software_fallback` | Software fallback (inhibit-charge watcher) | bool | true |
| `ssh` | *step on/off* | bool | on |
| `ssh.port` | Port | int | 22 |
| `ssh.password_logins` | Password logins | choice | on [on/off] |
| `ssh.root_login` | Allow root to log in over SSH | bool | true |
| `ssh.root_password` | If root has no password, give it your user's password | bool | true |
| `ssh.max_auth_tries` | MaxAuthTries | int | 4 |
| `fail2ban` | *step on/off* | bool | on |
| `fail2ban.bantime` | Ban time | string | 1h |
| `fail2ban.bantime_increment` | Longer bans for repeat offenders | bool | true |
| `fail2ban.maxretry` | Failed logins before a ban | int | 5 |
| `docker` | *step on/off* | bool | on |
| `docker.log_max_size` | Log rotation: size per file | string | 10m |
| `docker.log_max_file` | Log rotation: files kept | int | 3 |
| `docker.hello_world` | Run hello-world | bool | true |
| `cockpit` | *step on/off* | bool | on |
| `cockpit.backports` | Cockpit from trixie-backports | bool | true |
| `autoupdates` | *step on/off* | bool | on |
| `autoupdates.scope` | What updates automatically | choice | all [all/security] |
| `autoupdates.schedule` | Schedule | choice | 12h [12h] |
| `autoupdates.automatic_reboot` | Reboot automatically when an update needs it | bool | false |
| `node` | *step on/off* | bool | on |
| `node.nvm_version` | nvm version | string | v0.41.0 |
| `node.node_version` | Node.js version | string | 26 |
| `python` | *step on/off* | bool | on |
| `python.venv_path` | Automation venv path | string | ~/.venvs/automation |
| `python.uv_source` | uv source | choice | pipx [pipx/installer] |
| `python.playwright` | Playwright Chromium | bool | true |
| `python.uv_tools` | uv tools | list | [ruff pre-commit httpie] |
| `go` | *step on/off* | bool | on |
| `go.version` | Go version | string | latest |
| `go.debian_fallback` | Debian fallback | bool | true |
| `go.tools` | Go tools | list | [gopls dlv staticcheck govulncheck] |
| `go.update_tools` | Update tools on this run | bool | false |
| `firmware-updates` | *step on/off* | bool | on |
| `firmware-updates.updates` | fwupd updates | bool | true |

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

`pending` is non-null only while a risky change is in progress. It is `{ "step": "ssh", "backups": [...], "revert": "..." }`. Only options that differ from the defaults are stored in `choices`.

## Environment variables

These are for debugging only. None of them is a preset:
- `KILN_ASCII=1` forces the console glyph set.
- `NO_COLOR` disables color.
