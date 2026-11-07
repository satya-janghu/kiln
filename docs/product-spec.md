# Product spec

## One sentence

`kiln` turns a fresh Debian 13 "trixie" amd64 bare-metal install into a configured, always-on machine in one sitting at its console, the Debian way (apt, deb822 sources, Debian package names, Debian defaults).

## How it's used

1. Install Debian 13 with the official installer. **The installer decides** whether there's a desktop (GNOME or none), whether to install the SSH server, and whether to use any Debian blend.
2. Log in at the machine's own console, as your user with sudo or as root via `su -`.
3. Type the one-liner (see [distribution.md](distribution.md)). It installs the `kiln` binary and starts the wizard.
4. The wizard works like this:
   - It shows what it detected.
   - Every step starts checked; you untick what you don't want on this machine and adjust the options.
   - You review the exact plan.
   - It runs with live progress.
   - It ends with a summary and offers a reboot.
5. Later runs on the same machine start with that machine's previous choices ticked. Re-running only fixes what drifted. Everything else reports "unchanged".

The owner is always physically present. kiln isn't designed to be run over SSH. If it detects an SSH session, it warns that network and firewall steps may disconnect you, but it doesn't refuse.

## Machines

- There's one owner and several machines, and each machine is set up individually. There are **no saved profiles**.
- Every machine is treated as an always-on server, laptop or not.
- Known hardware, which the catalog targets first:
  - Lenovo ThinkPad E14 Gen 5, AMD Ryzen 5 7530U, Radeon integrated graphics.
  - HP Envy x360 13, AMD Ryzen 5 4500U, 8 GB RAM (older, low RAM).
- Desktops may come later, so nothing may assume "laptop".
- Bare metal only. If kiln detects a VM, it warns and continues. A container is refused.

## Commands

| Command | What it does | Changes the system? |
|---|---|---|
| `kiln` (alias `kiln wizard`) | The TUI. This is the only way to apply changes. | Yes, after you confirm the plan |
| `kiln plan [--skip a,b]` | Prints the plan for this machine using defaults and last run's choices. Same code path as a real run. | No |
| `kiln facts [--json]` | Prints what was detected (hardware and installer choices) | No |
| `kiln battery status\|apply\|watch` | Battery charge-limit helper, also used by systemd units | `apply` and `watch` do |
| `kiln thermal apply` | Re-applies the platform profile at boot (tuned machines only) | Yes |
| `kiln kernel fallback` | Keeps the newest stable 6.12 kernel installed next to the backports kernel (weekly timer) | Yes |
| `kiln mirrors bench [--apply]\|reset` | Benchmarks mirrors by throughput, or goes back to `deb.debian.org` | `--apply` and `reset` do |
| `kiln secrets show [name]` | Root only. Prints a stored secret, such as the remote-desktop password. | No |

The wizard applies changes only on Linux, and only if `/etc/kiln/allow-apply` exists (`install.sh` creates it) or `--i-know-this-is-a-fresh-machine` is passed.

## Defaults: everything on

- Every kiln step and sub-option starts checked. That includes the NVIDIA driver, remote desktop, trixie-backports, the backports kernel, NetworkManager migration, zram, fail2ban, auto-updates, firmware updates, Flathub and the third-party repos.
- A step that doesn't fit this machine stays checked but shows `not applicable: <reason>` and is skipped. The reason can be hardware (no NVIDIA GPU, no battery) or an installer choice (no GNOME, no SSH server).
- To turn things off quickly, toggle a single item, toggle a whole category, or filter by text.
- "On by default" doesn't mean reckless. Network migration, SSH and firewall changes are validated first, checked after applying, and reverted automatically if the check fails.

## Secrets

- The only secret today is the remote-desktop password.
- The wizard asks for it, masked, with a "generate one for me" choice. A generated password is shown once on screen.
- It's stored in `/etc/kiln/secrets/<name>` (directory 0700, file 0600, owned by root). `sudo kiln secrets show <name>` prints it again.
- It's never written to logs, the summary file or `state.json`.

## What a run leaves behind

- `/var/log/kiln.log`: every command, its exit code and its output, with secrets redacted.
- `/var/log/kiln-summary.txt`: for each step, whether it was done, unchanged, failed or skipped, and why. Also the collected notes, and whether a reboot is needed and why.
- `/etc/kiln/state.json`: this machine's last choices, the battery method that worked, and any change still in progress.

## Out of scope

- Installing or switching desktops. That's the installer's job, and only GNOME or none is supported.
- Installing the SSH server (also the installer's job), and Debian blends.
- Saved profiles, headless or unattended runs, and use over SSH.
- Automated tests of any kind (see [review-process.md](review-process.md)).
- Backups and snapshots.
- Tailscale, Cloudflare Tunnel and other remote-access setups.
- Debugger and ptrace tweaks.
- VMs (libvirt, cockpit-machines), Podman, KDE, other distros, and other architectures.
- Router port forwarding. kiln only notes it.

## Success criteria

1. One wizard session after the OS install leaves the machine configured.
2. A second run changes nothing.
3. A failed step never leaves the machine broken. The summary says what failed and what to do next.
4. The owner and Claude both sign off on every catalog entry, following [review-process.md](review-process.md).
