# Architecture

> **Direction (D54).** The owner wants a one-time tool that leaves nothing running behind. This file describes the code as it is today; parts marked **(persistent, D54)** are the ones under review.

## Shape

One static Go binary (`CGO_ENABLED=0`, linux/amd64). Helpers that systemd units or udev rules start are subcommands of the same binary (`kiln battery apply|watch`, `kiln thermal apply`) **(persistent, D54)**.

```
cmd/kiln/            main → internal/cli
internal/cli/        cobra commands: wizard, plan, facts, battery, thermal, mirrors, secrets, catalog (hidden)
internal/app/        loads logger, System, facts, state, options and steps; apply gate; scope re-exec
internal/facts/      hardware + installer choices (desktop, SSH server) + network; the only place that reads sysfs/procfs/dpkg for detection
internal/options/    option schema, defaults, last-run overlay, validation
internal/state/      /etc/kiln/state.json
internal/step/       Step interface, Env, Notes, categories
internal/steps/<id>/ one package per catalog step; steps/all lists them in catalog order
internal/action/     typed actions
internal/sys/        System interface: Real (exec, managed writes, downloads) and Recorder (preview)
internal/runner/     orchestration, dependency skipping, events, quiescing, inhibitor, locks, summary
internal/revert/     snapshot / undo / commit for risky changes (SSH)
internal/apt/        apt/dpkg wrapper (env, lock waits, debconf, repair), deb822
internal/logx/       /var/log/kiln.log (0600), secrets redacted
internal/secrets/    /etc/kiln/secrets (0600), log redaction
internal/battery/    charge-limit ladder (step + `kiln battery`)
internal/thermal/    platform-profile check, thinkfan (step + `kiln thermal`)
internal/mirrors/    masterlist, freshness, throughput bench, mirror list
internal/userenv/    the ~/.bashrc.d loader shared by node, python, go
internal/tui/        Bubble Tea v2 wizard
```

**Platform boundary.** Everything builds and lints on darwin. Linux-only code lives in `*_linux.go` with stubs elsewhere. `sys.Real` refuses to change anything unless `GOOS == linux` (last line of defence for rule 1).

## Steps

```go
type Step interface {
    ID() string; Title() string; Category() string // base, hardware, media, apps, power, security, dev
    DependsOn() []string          // a failed or skipped dependency skips this step
    Critical() bool               // only preflight: its failure stops the run
    DefaultEnabled(*facts.Facts) bool
    Options(*facts.Facts) []options.Field
    Applies(*Env) Applicability   // "not applicable: <reason>"
    Check(ctx, *Env) (Status, error)              // READ-ONLY
    Plan(ctx, *Env) ([]action.Action, error)
    Apply(ctx, *Env, []action.Action) error        // step.Base runs the actions in order
    Packages() []PackageRef       // for /check-packages
}
```

`Env` carries Facts, the step's option view, the full option set, `Sys`, the logger, `Notes` (summary notes, review warnings, reboot reasons; all deduplicated), secrets, state and the target user.

**Actions:** `AptUpdate`, `AptInstall{Pkgs, Suite, NoRecommends, Debconf}`, `AptRemove`, `WriteFile`, `RemoveFile`, `Symlink`, `Mkdir`, `Systemctl{Verb, Units, Now}`, `Run{Cmd}`, `UserAddGroup`, and `Custom{Desc, Fn}` for steps that need runtime feedback (battery ladder, mirror bench, SSH apply). Every action has a `Describe()` used by the review screen and `kiln plan`; a `Custom`'s `Desc` must say exactly what it will try. Secrets reach commands only through `Cmd.Stdin` (never argv; never logged).

**The preview can't drift (rule 7).** The review screen and `kiln plan` run the same steps with `sys.Recorder`, which records every change, returns success and changes nothing. `Check` always reads the real system.

## Runner

For each step in catalog order:

1. unticked → `skipped (by you)`;
2. `Applies` says no → `skipped (not applicable: …)`;
3. a dependency failed or was skipped → `skipped (needs …)`;
4. `Check` Done → `unchanged`;
5. else `Plan`, `Apply`, `Check` again → `done`, or `failed` with the step's "what to do next".

A panic or error in a step is recovered and recorded; the run continues (rule 5). Events go to the TUI through an unbounded queue (a stalled terminal can't block the runner) and to `/var/log/kiln.log`.

## Process model (D41)

**One thing at a time.** One step and one action at a time; each waits for its process to exit. "Started" isn't "ready": post-change checks poll until a deadline (sshd listening 15 s, `fail2ban-client ping` 30 s, `docker info` 60 s, `cockpit.socket` 15 s, power manager 30 s). Every command has a deadline and a 10-minute no-output heartbeat. kiln **never kills apt or dpkg**.

**Nothing else changes packages during a run.** Before the first apt action: stop `apt-daily{,-upgrade}.timer`, wait for both services to finish (never stop a running upgrade), then `systemctl mask --runtime` the timers, the services and `packagekit.service`. apt runs with `NEEDRESTART_SUSPEND=1`; after the last step kiln runs `needrestart -r a` once and `needrestart -b` for the summary. Debian's `needrestart.conf` already never restarts dbus, display managers, logind, NetworkManager, wpa_supplicant or docker. At the end (success, failure or Ctrl+C) the masks are removed and the timers started.

**Surviving the terminal.** When applying, kiln re-execs into its own scope (`systemd-run --scope --unit=kiln-run-<id> --collect`) and ignores SIGHUP. EIO/EPIPE on the terminal means "the UI is gone": kiln keeps running and logging. Children start in a new session (`Setsid`) with stdin `/dev/null`, so terminal signals never reach apt/dpkg and prompts fail fast. Ctrl+C in the TUI asks "Stop after the current step?"; SIGINT/SIGTERM stop after the current action, restore the masks and exit.

**Power.** The run holds `systemd-inhibit --what=sleep:idle:handle-lid-switch:handle-power-key:shutdown --mode=block … cat`, reading a pipe only kiln holds (closing it releases the block). That also blocks root's plain `reboot`; `systemctl reboot -i` still works. preflight warns on battery and asks below 30%. No step reboots; the summary's "reboot now" flushes state and log, removes the masks, stops the inhibitor, then runs `systemctl reboot`.

**Interrupted runs.** `state.json` records the current step. On the next start, before anything else: the pending-revert prompt (SSH), then interrupted-dpkg repair (`dpkg --audit` output or an all-digit file in `/var/lib/dpkg/updates/` → `dpkg --configure -a`, `apt-get -f install`), then stale runtime masks. Steps are idempotent, so running again is the resume.

**Locks.** `/run/kiln/kiln.lock` (flock) for the wizard and `kiln mirrors bench --apply|reset`; a second kiln says "already running (pid N)". The battery and thermal helpers and steps share `/run/kiln/<domain>.lock`; a busy helper exits quietly **(persistent, D54)**.

## Files, downloads, commands

| What | Rule |
|---|---|
| Managed write (`sys.WriteFile`) | Temp file `.<name>.kiln-tmp-<rand>` created with its final mode (`O_EXCL`), fsync, rename. First write of an existing file backs it up to `/var/lib/kiln/backup/<absolute path>` (never next to the file). `# Managed by kiln` header where comments are allowed. Identical content = no write. ucf-managed files are only key-edited. |
| Downloads (`sys.Fetch` / `HTTPGet`) | HTTPS into a fresh `mkdtemp` under `/var/tmp` (root, 0700; never `/tmp`, a tmpfs), sha256 checked where the catalog gives one, logged otherwise. Files a user command must read are handed over with `install -o <user> -m 0600`. |
| Piped installers | Only as a pinned `sys.Fetch` download run with `bash <file>` as the user (nvm, uv's installer). Never `bash -c` with interpolation. |
| User commands (`Cmd.As`) | `runuser -u <user> -- env -i HOME USER LOGNAME SHELL=/bin/bash LANG=C.UTF-8 PATH=<home>/.local/bin:/usr/local/go/bin:<home>/go/bin:/usr/local/bin:/usr/bin:/bin <cmd>`, cwd `<home>`. Never relies on `~/.bashrc`. |
| apt | `DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=a APT_LISTCHANGES_FRONTEND=none UCF_FORCE_CONFFOLD=1 LC_ALL=C`; `--force-confdef --force-confold`; `DPkg::Lock::Timeout=<lock wait>`; before update/downloads wait for the apt-daily services, retry "Could not get lock" until the lock wait ends; debconf preseeded; `-t trixie-backports` when asked. The lock wait from the wizard applies to the same run. |
| Directory swaps | Go: extract to `/usr/local/go.kiln-new`, rename `go` → `go.kiln-old`, `go.kiln-new` → `go`, remove `go.kiln-old`. If a run died between the renames, the next run puts `go.kiln-old` back. |

## Risky changes (SSH)

`internal/revert`: validate (`sshd -t`), record `pending` in `state.json` with tar snapshots under `/var/lib/kiln/pending/<step>/` and the undo commands, apply (`reload-or-restart`), poll the check, then commit or undo. A step whose earlier change is still pending keeps its first snapshot. If kiln died mid-change, the next start offers "A change didn't finish last time. Revert it?" **(persistent, D54)**.

## Options and state (persistent, D54)

Values: built-in defaults → this machine's last choices (`state.json`) → wizard edits. Options are validated before the review screen. Per-run options (`go.update_tools`) and secrets are never saved. `state.json` (0600) holds non-default choices, the battery method, the power manager, the run record and `pending`. See [options-reference.md](options-reference.md).

## Safety gate and target user

Changes are applied only on Linux, as root, not in a container, and only when `/etc/kiln/allow-apply` exists (install.sh creates it) or `--i-know-this-is-a-fresh-machine` is passed. Otherwise the wizard shows the plan and why it won't apply.

Target user: `SUDO_USER` (not root), else `PKEXEC_UID`, else the lowest UID ≥ 1000 with a login shell; if none, preflight refuses with the reason. Under plain `su` (no sbin on PATH), kiln prepends `/usr/local/sbin:/usr/sbin:/sbin` before anything runs.
