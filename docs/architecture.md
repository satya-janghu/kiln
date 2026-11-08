# Architecture

The plan is [decisions.md](decisions.md) D55 and D56. Behavior per item is in [steps-catalog.md](steps-catalog.md). Facts the code relies on are in [debian13-notes.md](debian13-notes.md).

## Shape

One static Go binary: `CGO_ENABLED=0`, linux/amd64. It also builds and lints on darwin: Linux-only code lives in `*_linux.go` files, with stubs for other platforms.

A run is a straight line:

1. **Safety check.** Refuse unless the machine runs Debian 13 amd64 and kiln runs as root. On anything but Linux, kiln refuses to change anything at all.
2. **Detect.** Hardware (CPU, GPU, battery, Secure Boot), GNOME or no desktop, and the target user. Detection lives in one package; steps don't read sysfs, procfs or dpkg state themselves.
3. **Checklist.** One screen of checkboxes, all ticked. Items that don't fit the hardware are hidden.
4. **Run.** The always-done items, then each ticked item, one at a time, in catalog order.
5. **Summary.** What was done, what failed and what to do next, notes, and whether a reboot is needed.

## Steps

A step is a function that runs plain commands and writes plain files. It gets the detected facts, the target user and a way to run commands and write files. It returns success or an error that says what to do next.

- **A failed step is recorded and the rest continue.** A step that needs a failed one is skipped, and the summary says why.
- **Running again is safe.** Each step is written so a second run does no harm: installs of installed packages are no-ops, files are rewritten with the same content, services are enabled again. There is no "already done" detection and no "unchanged" status.
- Output of every command goes live to the progress screen and to `/var/log/kiln.log`.

## Rules that still matter

| What | Rule |
|---|---|
| Commands | `exec.Command(name, args...)`. Never `bash -c` with interpolated strings. Children get stdin `/dev/null`. kiln never kills apt or dpkg. |
| apt | `DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a APT_LISTCHANGES_FRONTEND=none UCF_FORCE_CONFFOLD=1 LC_ALL=C`, plus `-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`. Debconf answers are preseeded where needed. |
| Waiting for apt | `-o DPkg::Lock::Timeout=<sec>` covers only the dpkg locks. Before `apt-get update` or any download, also wait for `apt-daily.service` and `apt-daily-upgrade.service` to be inactive, and retry on "Could not get lock". First-boot unattended-upgrades is common. |
| Files | Written atomically: temp file in the same directory with the final mode, fsync, rename. A `# Managed by kiln` header where the format allows comments. ucf-managed files are only key-edited, never rewritten. |
| Downloads | HTTPS into a fresh directory under `/var/tmp` (never `/tmp`, a tmpfs on trixie), checked against a sha256 where one is known, removed at the end. |
| Piped installers | Downloaded first (pinned URL), then run as `bash <file>` as the user. Used for nvm. |
| User commands | `runuser -u <user> -- env -i HOME=… USER=… LOGNAME=… SHELL=/bin/bash LANG=C.UTF-8 PATH=… <cmd>`, with cwd the user's home. Never relies on `~/.bashrc`. |
| Target user | `SUDO_USER` if not root, else `PKEXEC_UID`, else the lowest UID ≥ 1000 with a login shell. If there is none, kiln stops with that reason. Under plain `su`, `/usr/local/sbin:/usr/sbin:/sbin` is added to `PATH`. |
| SSH | `sshd -t` must pass before SSH is reloaded. If it fails, SSH is not reloaded and the step fails. |
| Secrets | The Secure Boot MOK one-time password goes to commands only on stdin, never in argv, never in the log. It is shown once on the summary screen. |

## What's gone

From the old tool, none of this exists any more:

- the `sys.Recorder`, `kiln plan` and the review screen;
- `/etc/kiln/state.json`, saved choices and the options layer;
- the backup directory `/var/lib/kiln/backup/`;
- revert snapshots and the "revert the unfinished change?" prompt;
- subcommands (`battery`, `thermal`, `mirrors`, `secrets`, `facts`, `plan`) and the helpers that systemd units called;
- the systemd scope, runtime masking of apt-daily and PackageKit, the sleep inhibitor;
- the `/run/kiln` flock locks;
- the allow-apply file and `--i-know-this-is-a-fresh-machine`;
- `/etc/kiln/secrets/`.

## Package layout (proposed)

**Status: proposed** (D55, "Found while writing the docs"). The owner approves structure.

```
cmd/kiln/        main: safety check, detect, start the TUI
internal/detect/ hardware, desktop, target user (the only place that reads sysfs/procfs/dpkg)
internal/run/    running commands, apt with its env and lock waits, atomic writes, downloads, the log
internal/steps/  one file per checklist item, in catalog order
internal/tui/    checklist, progress and summary screens
```

No cobra, no `action` types, no options or state packages.
