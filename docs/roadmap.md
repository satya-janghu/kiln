# Roadmap

There are no tests. Each milestone ends with a clean `make build lint vuln`, an updated catalog and notes, and a review pass (`/review-step` for every step touched; see [review-process.md](review-process.md)).

## M0: Scaffold

- Go module `github.com/satya-janghu/kiln`.
- cobra root command with stub subcommands.
- Makefile with `build`, `lint`, `vuln` and `release-check`.
- `.golangci.yml` (v2).
- CI that runs build, lint and vuln on every push.
- No behavior yet.

## M1: Engine

- `sys.System` (Real + Recorder), the typed actions, `fileio.WriteManaged`, and the `apt` helper.
- The runner: events, skipping dependents, the summary.
- `internal/revert` and `internal/state`.
- The `facts` package, covering hardware and installer choices.
- The options schema and defaults.
- The safety gate.
- `kiln facts` and `kiln plan`.
- Steps: **preflight**, **sudo**, **apt-sources**.

## M2: Wizard

The full Bubble Tea v2 wizard, wired to the runner. This is the only way to apply changes, so it comes before the remaining steps:

- interrupted-change prompt;
- facts screen;
- checklist (toggle an item or category, filter, reset to defaults);
- Huh option forms;
- review tabs;
- run screen (step list, log pane, progress);
- summary with reboot.

It must have a responsive layout and console-safe glyphs.

## M3: Remaining steps

The rest of the catalog, in catalog order, each with Check, Plan and Apply, plus its helpers and subcommands:
- battery ladder;
- mirror bench;
- thermal;
- kernel fallback;
- secrets.

## M4: Distribution

- `install.sh`;
- GitHub Pages short URL;
- GoReleaser v2;
- release workflow.

## M5: Review to sign-off

Repeated `/audit` passes over the whole codebase and docs, checked against current primary documentation, until a full pass finds nothing and both the owner and Claude sign off. Every catalog entry must be marked reviewed in [review-process.md](review-process.md). Then tag `v1.0.0` and use it in production.
