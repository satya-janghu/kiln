# Architecture

## Shape

kiln is one static Go binary (`CGO_ENABLED=0`, linux/amd64). Every helper that a systemd unit or udev rule needs is a subcommand of the same binary: `kiln battery apply|watch`, `kiln thermal apply`, `kiln kernel fallback`. That way the machine never ends up with loose shell scripts.

```
cmd/kiln/                 main → internal/cli
internal/cli/             cobra commands (wizard, plan, facts, battery, thermal, kernel, mirrors, secrets)
internal/facts/           Facts struct: hardware + installer choices (desktop, SSH server, blends) + network
internal/state/           /etc/kiln/state.json: last choices, battery method, change-in-progress marker
internal/options/         option schema, defaults (everything on), merging with last choices
internal/sys/             System interface: Real (exec/files/systemd) and Recorder (preview, no side effects)
internal/action/          typed, declarative actions (AptInstall, WriteFile, ServiceEnable, ...)
internal/step/            Step interface, registry, catalog order, Applicability, Status
internal/steps/<id>/      one package per catalog step
internal/runner/          orchestration, dependency skipping, events, summary
internal/revert/          backups, verify-then-revert for risky changes, change-in-progress marker
internal/apt/             apt/dpkg wrapper (non-interactive env, lock wait, debconf), deb822 parse/write
internal/fileio/          managed file writes (atomic, first-write backup, header)
internal/fetch/           HTTPS download with sha256 check; the one audited piped-installer helper
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
- **System settings:** `UserAddGroup`, `Sysctl`, `SysfsWrite`, `FirewallRule`.
- **Downloads:** `Download{URL, SHA256, Dest}`, `PipedInstaller{URL, Pin, AsUser}`.
- **Commands:** `Command{Name, Args, Stdin, AsUser, Env}`. `Stdin` is how secrets are passed (never in argv).
- **`Custom{Desc, Fn}`:** for steps that need runtime feedback, such as the battery ladder, the mirror bench and network migration. Its `Desc` must still say exactly what it will try.

### Why the preview can't drift (rule 7)

The wizard's review screen and `kiln plan` use the same runner as the real run. The only difference is which `sys.System` they pass to `Apply`:

- **The real run uses `sys.Real`.**
- **The preview uses `sys.Recorder`.** It records every call, returns success, and changes nothing. It groups what it recorded into packages, files, services, firewall and commands.
- **`Check` always reads the real system, read-only,** so the preview shows what would actually change on this machine.

## Runner

The runner goes through the steps in catalog order. For each step:

1. If you unticked it → `skipped (by you)`.
2. If `Applies` says no → `skipped (not applicable: <reason>)`.
3. If a dependency failed or was skipped → `skipped (needs <dep>)`.
4. If `Check` reports Done → `unchanged`.
5. Otherwise `Plan`, then `Apply`, then `Check` again to confirm. If confirmed → `done`. If not → `failed`, with the step's "what to do next" text.

A panic or error inside a step is recovered and recorded, and the run continues (rule 5). The runner emits events (`StepStarted`, `Output`, `Progress`, `StepFinished`, `Note`, `RebootNeeded`) to the TUI. Every event also goes to `/var/log/kiln.log` through the redacting logger.

For the whole run, the runner holds a sleep inhibitor: `systemd-inhibit --what=sleep:idle:handle-lid-switch ... sleep infinity`, run as a child process and killed at the end. That way closing the lid can't suspend the machine mid-run.

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
| Exec | `sys` | `exec.Command(name, args...)` only, never `bash -c` with interpolated strings. Commands that run as the target user go through `runuser -u <user> -- ...` with a clean environment (HOME, USER, PATH). |
| Piped installer | `fetch.PipedInstaller` | The only place a downloaded script runs (nvm, and uv's fallback installer). The URL is pinned. The script is downloaded to `/var/tmp/kiln`, its sha256 is logged, and it runs as `bash <file>` as the target user. Never under `set -e` for nvm. |
| apt | `apt` | Sets `DEBIAN_FRONTEND=noninteractive` and `NEEDRESTART_MODE=a`, passes `-o Dpkg::Options::=--force-confdef -o Dpkg::Options::=--force-confold`, waits for the dpkg lock, and preseeds debconf before installing. While waiting it says "waiting for unattended-upgrades", and `-o DPkg::Lock::Timeout` is still to be verified. |
| Managed write | `fileio.WriteManaged` | Writes atomically (temp file in the same directory, fsync, rename). Makes `<file>.kiln-bak` only on the first write of a file that already existed. Adds a `# Managed by kiln` header where the format allows. Does nothing when the content is identical. |
| Download | `fetch.Download` | HTTPS only, sha256 checked, saved to `/var/tmp/kiln`, never `/tmp` (a tmpfs). |

## Risky changes: verify, then revert on failure

Network migration, SSH and firewall changes go through `internal/revert`. You're at the console, so there's no remote session to protect, and the danger is a box that's left half-configured. The pattern is:

1. **Validate offline first:** `sshd -t`, `firewall-cmd --check-config`, and a parsed-back check of NetworkManager profiles.
2. **Record a `pending` entry in `state.json`,** with the backups and how to undo the change.
3. **Apply the change in place,** using reload where possible.
4. **Check it locally.** For SSH, `sshd` is listening on the configured port and `sshd -T` shows the expected values. For the firewall, the expected zones and services are present, and so is SSH if an SSH server is installed. For the network, the link is up, the default route exists, DNS resolves, and HTTPS to `deb.debian.org` works.
5. **If the check passes,** make the change permanent where that's a separate step (`firewall-cmd --runtime-to-permanent`) and clear `pending`. **If it fails,** restore the backups, restart the previous service, clear `pending`, and record `failed` with what to try.
6. **If kiln died mid-change,** the next start finds `pending`. Before anything else, the wizard offers "A network change didn't finish. Revert it?".

## Safety gate

The wizard applies changes only when `GOOS == linux` and either `/etc/kiln/allow-apply` exists or `--i-know-this-is-a-fresh-machine` is passed. Containers are refused. Otherwise it shows the plan and the reason it won't apply.

## Target user

The target user is resolved in this order:

1. `SUDO_USER` (when it isn't root);
2. otherwise the first account with UID ≥ 1000 and a login shell;
3. otherwise the wizard asks.

When started via `su` without `-`, kiln adds `/usr/local/sbin:/usr/sbin:/sbin` to its own PATH.
