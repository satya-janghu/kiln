# Architecture

## Shape

kiln is one static Go binary (`CGO_ENABLED=0`, linux/amd64). Every helper that a systemd unit or udev rule needs is a subcommand of the same binary: `kiln battery apply|watch`, `kiln thermal apply`. That way the machine never ends up with loose shell scripts.

```
cmd/kiln/                 main → internal/cli
internal/cli/             cobra commands (wizard, plan, facts, battery, thermal, mirrors, secrets)
internal/facts/           Facts struct: hardware + installer choices (desktop, SSH server, blends) + network
internal/state/           /etc/kiln/state.json: last choices, battery method, change-in-progress marker
internal/options/         option schema, defaults (everything on), merging with last choices
internal/sys/             System interface: Real (exec, managed writes, verified downloads) and Recorder (preview)
internal/action/          typed, declarative actions (AptInstall, WriteFile, ServiceEnable, ...)
internal/step/            Step interface, registry, catalog order, Applicability, Status
internal/steps/<id>/      one package per catalog step
internal/runner/          orchestration, dependency skipping, events, summary
internal/revert/          backups, verify-then-revert for risky changes, change-in-progress marker
internal/apt/             apt/dpkg wrapper (non-interactive env, lock wait, debconf), deb822 parse/write
internal/app/             loads logger, System, facts, state, options and steps for every command; scope re-exec
internal/logx/            /var/log/kiln.log (0600) with secrets redacted
internal/secrets/         generation, storage under /etc/kiln/secrets, log redaction
internal/battery/         charge-limit method ladder (shared by the step and `kiln battery`)
internal/thermal/         platform_profile / thinkfan logic (shared by the step and `kiln thermal`)
internal/mirrors/         masterlist parsing, freshness check, throughput bench, mirrorlist writer
internal/tui/             Bubble Tea v2 wizard, Lip Gloss styles, console-safe glyph set
```

## Platform boundary (macOS dev, Linux target)

- Everything must build and lint on darwin. Code that only works on Linux (reading `/sys` and `/proc` live, `runuser`, and so on) lives in `*_linux.go` files, with `*_other.go` stubs that return `ErrUnsupported`.
- The real `sys.System` refuses to change anything unless `runtime.GOOS == "linux"`. This is the last line of defense for rule 1.

## Core interfaces (sketch, not final code)

```go
// internal/step
type Step interface {
    ID() string               // stable kebab-case: "apt-sources"
    Title() string            // plain language, shown in the TUI
    Category() string         // "base", "network", "security", "power", "apps", "dev", "hardware"
    DependsOn() []string      // if any of these failed or was skipped, this step is skipped
    Applies(f *facts.Facts, o *options.Resolved) Applicability  // {OK bool; Reason string}
    Check(ctx context.Context, env *Env) (Status, error)       // READ-ONLY: Done, or NeedsWork plus what differs
    Plan(ctx context.Context, env *Env) ([]action.Action, error)
    Apply(ctx context.Context, env *Env, plan []action.Action) error
}

type Env struct {
    Facts   *facts.Facts
    Options *options.Resolved // this step's options
    Sys     sys.System        // Real or Recorder
    Log     *slog.Logger      // redacting
    Notes   *runner.Notes     // summary notes and reboot reasons
    Secrets *secrets.Store
    State   *state.State
}
```

`step.Base` provides a default `Apply` that runs the plan's actions in order through `env.Sys`. Most steps only write `Check` and `Plan`.

### Actions

Every action is a typed value with `Describe() string` (used for the review screen and `kiln plan`) and `Do(ctx, sys.System) error`. The action types are:

- **Packages:** `AptUpdate`, `AptInstall{Pkgs, Suite}`, `AptRemove`, `DebconfSet`.
- **Files:** `WriteFile{Path, Content, Mode, Owner, Comment}`, `EditKeys` (key-level edit of an ini-style file, used for GDM's greeter defaults), `Symlink`, `RemoveFile`.
- **Services:** `ServiceEnable`, `ServiceDisable`, `ServiceMask`, `ServiceRestart`, `ServiceReload`.
- **System settings:** `UserAddGroup`, `Sysctl`, `SysfsWrite`.
- **Downloads:** steps call `sys.System.Fetch` (sha256-verified, into the run's private directory) inside `Custom` actions; piped installers are a pinned Fetch followed by `bash <file>` as the user.
- **Commands:** `Command{Name, Args, Stdin, AsUser, Env}`. `Stdin` is how secrets are passed (never in argv).
- **`Custom{Desc, Fn}`:** for steps that need runtime feedback, such as the battery ladder and the mirror bench. Its `Desc` must still say exactly what it will try.

### Why the preview can't drift (rule 7)

The wizard's review screen and `kiln plan` use the same runner as the real run. The only difference is which `sys.System` they pass to `Apply`:

- **The real run uses `sys.Real`.**
- **The preview uses `sys.Recorder`.** It records every call, returns success, and changes nothing. It groups what it recorded into packages, files, services and commands.
- **`Check` always reads the real system, read-only,** so the preview shows what would actually change on this machine.

## Runner

The runner goes through the steps in catalog order. For each step:

1. If you unticked it → `skipped (by you)`.
2. If `Applies` says no → `skipped (not applicable: <reason>)`.
3. If a dependency failed or was skipped → `skipped (needs <dep>)`.
4. If `Check` reports Done → `unchanged`.
5. Otherwise `Plan`, then `Apply`, then `Check` again to confirm. If confirmed → `done`. If not → `failed`, with the step's "what to do next" text.

A panic or error inside a step is recovered and recorded, and the run continues (rule 5). The runner emits events (`StepStarted`, `Output`, `Progress`, `StepFinished`, `Note`, `RebootNeeded`) to the TUI. Every event also goes to `/var/log/kiln.log` through the redacting logger.

For the whole run, the runner holds an inhibitor (`systemd-inhibit … sleep infinity`, a child process killed at the end; see the next section for exactly what it blocks).

## Process model, ordering and interruption safety

kiln changes a live system for 15–40 minutes, and anything can happen in that time: a lid closes, the battery runs out, the terminal window is closed, the desktop session ends, someone presses the power button, or a package update restarts a service kiln depends on. These rules (D41) make sure no command starts before the previous one has finished, nothing else changes packages at the same time, and an interruption at any point leaves a machine that the next run can repair.

### One command at a time

- **Strictly sequential.** The runner executes one step at a time and one action at a time. Every action waits for its process to **exit** and checks the exit code before the next one starts. Nothing runs in the background except the inhibitor and the UI.
- **"Started" isn't "ready".** Commands like `systemctl start`, `systemctl reload` and `apt install` of a service return before that service is actually ready. So every post-change check **polls until a deadline** instead of checking once:
  - sshd listening on its port: up to 15 s. sshd re-execs itself on SIGHUP, so a single check right after `reload` can see a closed port and wrongly trigger a revert.
  - `fail2ban-client ping`: up to 30 s;
  - `docker info`: up to 60 s;
  - NetworkManager activation, using `nmcli --wait`: up to 90 s;
  - `cockpit.socket` active: up to 15 s.

  A deadline that runs out counts as a failed check, which triggers the step's revert or `failed`.
- **Deadlines on every command.** Each command gets a context deadline sized to it, for example 2 min for `apt update` and 60 min for a large `apt install`. It also gets a no-output watchdog (10 min) that logs a "still running: <command>" heartbeat to the log pane, so a hang is visible. kiln **never kills apt or dpkg** when a deadline passes. It reports the hang and waits for you to decide, because killing dpkg mid-unpack is worse than waiting.

### Nothing else changes packages during a run

Before the first apt action, and only for the run's duration, kiln quiets every other package manager:
- **Automatic updates:**
  1. `systemctl stop apt-daily.timer apt-daily-upgrade.timer`.
  2. Wait until `apt-daily.service` and `apt-daily-upgrade.service` (unattended-upgrades) have **finished**. kiln never stops a running upgrade.
  3. `systemctl mask --runtime` both timers and both services.

  Masking alone doesn't stop a running unit (systemctl(1)), hence steps 1 and 2. A runtime mask lives in `/run/systemd/system/` and disappears at reboot, so even a crash can't leave automatic updates disabled.
- **PackageKit / GNOME Software:** the same treatment for `packagekit.service` (D-Bus activated), so GNOME Software can't start a refresh or an offline update in the middle of the run.
- **Service restarts after upgrades:** kiln's apt runs set `NEEDRESTART_SUSPEND=1`, so needrestart doesn't restart services (ssh, cockpit, fail2ban…) in the middle of the run while later steps are still configuring them.
  - needrestart 3.11 skips its apt hook, but keeps its `/run/needrestart/unpacked` flag.
  - After the last step, kiln runs `needrestart -r a` **once**: one combined restart.
  - It then runs `needrestart -b` (batch mode, which never restarts anything) to collect what's still pending: services, a newer kernel, microcode. That goes in the summary.
  - Debian's own `needrestart.conf` already never auto-restarts `dbus`, the display managers (`gdm`…), `systemd-logind`, `NetworkManager`, `wpa_supplicant`, `docker`, `getty@`, `user@` or the apt-daily units. kiln relies on that list rather than its own, so the desktop session and the network are never restarted. Those services appear in the summary as "restart at next reboot".
- **Restoring:** at the end of the run (success, failure or Ctrl+C), the masks are removed and the timers started again. Timers with `Persistent=true` may fire straight away for a missed slot; that's expected, because kiln has finished by then.

### Surviving the terminal or the session

kiln may run inside GNOME Terminal. Closing that window, logging out, or a display-manager restart would normally kill it mid-dpkg. So:
- **Own scope.** When it starts applying, kiln re-executes itself inside its own transient scope, `systemd-run --scope --unit=kiln-run-<time> --collect`, as root, so it lands in the system slice. The desktop session ending doesn't take it down.
- **Terminal loss.** kiln ignores `SIGHUP` and handles `EIO`/`EPIPE` on the terminal as "the UI is gone". It stops drawing and carries on with the run, logging to `/var/log/kiln.log`. The next `sudo kiln` shows the result.
- **Children are detached.** Every child command starts in a **new session** (Go `SysProcAttr.Setsid`), so the terminal's `SIGINT`/`SIGHUP` never reach apt or dpkg directly. Its stdin is `/dev/null`, so any program that tries to prompt fails fast instead of hanging. In the TUI, Ctrl+C is a key press, which asks "Stop after the current step?".
- **SIGTERM and SIGINT to kiln itself** (for example from a forced shutdown): kiln stops starting new actions, lets the current child finish, records the interruption, restores the masks, and exits.
- **Detaching is derived, not quoted.** That `Setsid` keeps the terminal's signals away from children follows from setsid(2): "the new session has no controlling terminal". Children's stdout/stderr are pipes to kiln, never the terminal, so a terminal hangup can't produce EIO in them.

### Power, sleep and reboot

- **Inhibitor.** The run holds `systemd-inhibit --what=sleep:idle:handle-lid-switch:handle-power-key:shutdown --mode=block --who=kiln --why="installing packages; rebooting now could break the system"`.
  - That blocks the lid, the power key, idle, and every reboot or poweroff that goes through logind, **including root's** `systemctl reboot` and `reboot`. In systemd 257 a `block` lock refuses every caller.
  - The deliberate escapes still work: `systemctl reboot -i` (`--check-inhibitors=no`), `--force`, and holding the power button.
  - Blocking root's reboot too is intended: the risk is an accidental reboot halfway through dpkg.
- **Battery.** preflight warns when the machine is on battery, and asks for confirmation if the charge is below 30%. A battery that dies mid-dpkg is the most likely interruption on a laptop.
- **No step reboots the machine.** fwupd is run so it never reboots (see step 24). The summary's "reboot now" works like this:
  1. flush and fsync `state.json`, the summary and the log;
  2. remove the runtime masks;
  3. **stop the inhibitor**, which would otherwise block kiln's own reboot;
  4. only then run `systemctl reboot`.

### Interrupted runs and the next start

- **Run record.** `state.json` has a `run` record: `{started, step, action, finished}`. It's written, atomically, before each step starts and when it ends.
- **Next start finds it unfinished.** The wizard says "The last run stopped during <step> (<reason if known>)". It repairs first, then offers to run again. Every step is idempotent, so re-running is the resume.
- **Repair before anything else**, in this order, before the internet check:
  1. **The `pending` revert prompt** for risky changes, so a half-applied change is undone before anything else runs.
  2. **Interrupted dpkg.** If `dpkg --audit` prints anything (it always exits 0, so kiln checks the output), or `/var/lib/dpkg/updates/` contains an all-digit file name (apt's "dpkg was interrupted" test), run `dpkg --configure -a` then `apt-get -f install -y`, both through the apt helper.
  3. **Stale runtime masks** from a crash are removed, unless another kiln is running. A reboot clears them anyway.
- **Single instance.** kiln takes an exclusive `flock` on `/run/kiln/kiln.lock` for the whole wizard run. A second `kiln` shows "kiln is already running (pid N)", and offers to show the log, read-only.
- **Helpers lock too.** The helpers that systemd and udev start (`kiln battery apply|watch`, `kiln thermal apply`) take `/run/kiln/<domain>.lock`. The battery step and thermal step take the same locks, so a udev-triggered battery apply can never write thresholds at the same moment as the step does. If the main run holds the battery lock, the helper exits quietly; the step applies the same values anyway.

### Files and downloads

- **Mode from the start.** Temp files are created with their **final mode**, using an `O_EXCL` create: 0600 for secrets and the state file, otherwise the target's mode. The managed writer never creates a file and fixes the mode afterwards, so a secret is never readable even for an instant.
- **Download directory.** Downloads go to a fresh `mkdtemp` directory under `/var/tmp` (`/var/tmp/kiln.XXXXXX`, root, 0700), never a fixed name. `/var/tmp` is world-writable, so a fixed path could be pre-created or symlinked by another user. Files a user-run command must read (installer scripts) are handed over with `install -o <user> -m 0600` into the user's cache directory, not shared through `/var/tmp`.
- **Swaps are atomic.** A directory that's replaced is swapped, never deleted first. For example, Go is extracted into `/usr/local/go.kiln-new`, then `/usr/local/go` is renamed to `/usr/local/go.kiln-old`, `go.kiln-new` is renamed to `go`, and `go.kiln-old` is removed. An interruption leaves either the old or the new Go, never none. A leftover `.kiln-new` or `.kiln-old` is cleaned up on the next run.

## Options and state

- **Where values come from, lowest priority to highest:**
  1. built-in defaults (everything on);
  2. this machine's last choices from `/etc/kiln/state.json`;
  3. what you change in the wizard.
- **Validation:** options are checked once, before the review screen. See [options-reference.md](options-reference.md).
- **`state.json` (0600, root) holds:**
  - the choices from the last run;
  - what the battery ladder accepted;
  - which power manager is in use;
  - a `pending` record while a risky change is in progress.
- **Helpers read it too:** `kiln battery apply` re-applies exactly what the step chose.

## Helpers every step uses

| Helper | Package | Rule |
|---|---|---|
| Exec | `sys` | `exec.Command(name, args...)` only, never `bash -c` with interpolated strings. **User-run rule:** every "as the user" command runs as `runuser -u <user> -- env -i HOME=<home> USER=<user> LOGNAME=<user> SHELL=/bin/bash LANG=C.UTF-8 PATH=<home>/.local/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin <cmd>`, with cwd set to `<home>`. The clean environment keeps root's `XDG_*` and other variables out, which would otherwise move nvm or uv directories. Commands never rely on `~/.bashrc`, because Debian's skel `.bashrc` returns early in non-interactive shells. argv uses absolute paths, never `~`. |
| Piped installer | `sys.Fetch` + `Run` | The only way a downloaded script runs (nvm, and uv's fallback installer). The URL is pinned. The script is downloaded to the run's private directory, its sha256 is logged, and it runs as `bash <file>` as the target user. Never under `set -e` for nvm. |
| apt | `apt` | **Environment:** `DEBIAN_FRONTEND=noninteractive`, `NEEDRESTART_MODE=a` (honoured by needrestart 3.11, and it overrides the config), `APT_LISTCHANGES_FRONTEND=none`, `UCF_FORCE_CONFFOLD=1` (confdef/confold don't cover ucf-managed files), and `LC_ALL=C` (output is parsed). **Options:** `-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold -o DPkg::Lock::Timeout=<lock wait>`. **Locks:** the timeout covers only the dpkg `lock-frontend`/`lock`. `/var/lib/apt/lists/lock` (update) and `/var/cache/apt/archives/lock` (downloads) fail at once, so before `update` or a download the helper waits until `apt-daily.service` and `apt-daily-upgrade.service` are inactive, showing "waiting for automatic updates to finish". It retries on "Could not get lock" until the lock wait runs out. **Debconf:** preseeded before installing. **Suites:** `-t trixie-backports` when a step asks for backports. |
| Managed write | `sys.System.WriteFile` | Writes atomically: a temp file `.<name>.kiln-tmp-<rand>` in the same directory (the leading dot and the `.` in the name mean sudoers.d, apt and run-parts ignore it during the instant it exists), then fsync and rename. On the first write of a file that already existed, it backs it up to `/var/lib/kiln/backup/<absolute path>`, preserving mode, owner and timestamps. Backups are never kept next to the file, because apt warns about stray files in `sources.list.d/`. Adds a `# Managed by kiln` header where the format allows. Does nothing when the content is identical. |
| Download | `sys.System.Fetch` / `HTTPGet` | HTTPS, sha256 checked, saved to the run's private `mkdtemp` directory under `/var/tmp` (never `/tmp`, a tmpfs); `HTTPGet` for small read-only documents (key lists, JSON indexes, Release files). |

## Risky changes: verify, then revert on failure

SSH changes go through `internal/revert`. You're at the console, so there's no remote session to protect, and the danger is a box that's left half-configured. The pattern is:

1. **Validate offline first:** `sshd -t` and a parsed-back check of NetworkManager profiles.
2. **Record a `pending` entry in `state.json`,** with the backups (paths under `/var/lib/kiln/backup/`) and how to undo the change.
3. **Apply the change in place,** using reload where possible.
4. **Check it locally.** For SSH, `sshd` is listening on the configured port and `sshd -T` shows the expected values.
5. **If the check passes,** clear `pending`. **If it fails,** restore the backups, restart the previous service, clear `pending`, and record `failed` with what to try.
6. **If kiln died mid-change,** the next start finds `pending`. Before anything else, the wizard offers "A change didn't finish last time. Revert it?".

## Safety gate

The wizard applies changes only when `GOOS == linux` and either `/etc/kiln/allow-apply` exists or `--i-know-this-is-a-fresh-machine` is passed. Containers are refused. Otherwise it shows the plan and the reason it won't apply.

## Target user

The target user is resolved in this order:

1. `SUDO_USER` (when it isn't root);
2. otherwise the first account with UID ≥ 1000 and a login shell;
3. otherwise the wizard asks.

When started via `su` without `-`, kiln adds `/usr/local/sbin:/usr/sbin:/sbin` to its own PATH.
