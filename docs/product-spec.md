# Product spec

## What kiln is

kiln is a one-time setup tool for a fresh Debian 13 "trixie" amd64 install (D54). You run it once, at the machine's own console, as root, right after the Debian installer finishes. It installs the packages and writes the configuration you would otherwise type by hand on every machine, SSH included. Then you never deal with it again.

It could have been a bash script. The TUI exists only so the same tool fits servers without a desktop and installs where you picked GNOME.

The plan is [decisions.md](decisions.md) D55 (scope) and D56 (battery). Where this file and those entries disagree, the entries win.

## Who uses it

One owner, several homelab machines. Each machine is set up once, by hand, at its console. Every machine is treated as always on, laptop or not.

## How it's used

1. Install Debian 13 with the official installer. The installer decides GNOME or no desktop (D22, D29). kiln doesn't change that.
2. Log in at the console as root (or `su -`, or with `sudo`).
3. Type the one-liner from [distribution.md](distribution.md).
4. kiln shows one checklist. Everything is ticked. Untick what this machine doesn't need and press Enter.
5. It runs each item in order, with live output.
6. It ends with a summary: what was done, what failed and what to do about it, and whether to reboot.

The checklist is the same on every install, GNOME or server. Only hardware hides an item: the NVIDIA items need an NVIDIA card, and the battery limit needs a battery. There are no sub-options and no other questions. Every value is fixed.

If a run fails halfway, run the one-liner again. That is safe.

## What it does

The full list, with exact behavior, is in [steps-catalog.md](steps-catalog.md) (D55 until the catalog is rewritten). In short:

- **Always:** a safety check (Debian 13, amd64, root), wait for first-boot updates, deb822 sources with contrib, non-free, non-free-firmware, security, updates and backports, a full upgrade, firmware and CPU microcode.
- **Checkboxes, all ticked:**
  - passwordless sudo, system basics (zram, journal cap, inotify, time sync);
  - SSH with password logins for the user and root (D45);
  - UFW and fail2ban, automatic updates every 12 h with no automatic reboot;
  - always on (no sleep, lid ignored), balanced power profile;
  - Docker with Portainer (and the NVIDIA Container Toolkit with an NVIDIA card), Cockpit;
  - CLI and build tools, Node (nvm), Python (uv and a venv), Go;
  - codecs and VA-API, Flathub, Google Chrome;
  - the NVIDIA driver, picked automatically;
  - the battery held between 70% and 80% (D56).

## What it leaves behind

- Ordinary Debian configuration and packages. None of it needs kiln.
- `/var/log/kiln.log`: every command, its exit code and its output. Secrets never go in it.
- On laptops that need it, the battery script and its systemd timer (D56). They are plain files, not part of kiln. This is the only thing that keeps running.

Nothing else. No saved choices, no `/etc/kiln`, no kiln binary in `PATH`.

## Out of scope

- Installing or switching desktops, Debian blends.
- Profiles, headless or unattended runs, use over SSH.
- Saved choices, re-run detection, "unchanged" reporting, a plan preview, revert snapshots, backups, subcommands.
- Mirror benchmarking (kiln uses `deb.debian.org`), fwupd, thermal and thinkfan, the backports kernel, DVD playback.
- VMs, containers, other architectures, other distros.
- Remote-access services (Tailscale, tunnels), router port forwarding.
- Automated tests ([review-process.md](review-process.md)).

## Done means

1. One run after the installer leaves the machine configured.
2. Running it again is safe and breaks nothing.
3. A failed item never stops the run. The summary says what failed and what to do next.
