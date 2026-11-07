# Roadmap

There are no tests. Each milestone ends with a clean `make build lint vuln`, an updated catalog and notes, and a review pass (`/review-step` for every step touched; see [review-process.md](review-process.md)).

## Where things stand (end of 2026-11-07)

**Done:**
- M0–M4 code exists, and all 23 steps are implemented.
- Every Debian fact is verified: no **U** rows are left in debian13-notes.md.
- Field-tested on the HP Envy x360 (10.10.10.150, a server install on Wi-Fi). Two consecutive runs are clean, and the second changes nothing.
- All six per-step review batches ran, and their forced fixes are committed (D53).
- Owner decisions D50–D53 are applied.

**Stopped at:** the owner reset the direction (D54). kiln must be a one-time tool that leaves nothing running behind, with fewer questions. Nothing has been implemented for D54 yet.

**Next session, in order:**
1. Ask the owner: approach A (one-shot, strip persistence) or B (rewrite much smaller). See D54.
2. Implement that approach, plus the D54 answers marked "still applies":
   - write `Port 22` when another sshd file sets a Port;
   - the startup internet check: HTTPS to deb.debian.org and github.com, 5 s each, saying which failed (today's code: HTTP GET to deb.debian.org, HTTPS to github.com/robots.txt, 60 s);
   - mirror test: skip cloud-only mirrors and cap at 10 in-country;
   - thinkfan independent of the power step;
   - make the fail2ban catalog text match the code.
3. Update the catalog, architecture and options reference to match. Then re-run the review for the steps that changed, and run kiln on the test box again (reinstalling Debian there is fine).
4. Found in the doc pass, not fixed yet: NVIDIA cards older than Maxwell (Kepler) that aren't in the open-module table are classed "pre-Turing" and offered Debian's 550 driver, which doesn't support them (`internal/steps/multimedia/nvidia.go` classify). They should be left on nouveau with a note.
5. Still open whichever approach is chosen:
   - interruption drills;
   - a GNOME machine and the NVIDIA machines (RTX 2070 Super, GTX 970);
   - `make vuln` (install govulncheck on the Mac);
   - publishing (GitHub repo, release, Pages).

## M0: Scaffold

- Go module `github.com/satya-janghu/kiln`.
- cobra root command with stub subcommands.
- Makefile with `build`, `lint`, `vuln` and `release-check`.
- `.golangci.yml` (v2).
- CI that runs build, lint and vuln on every push.
- No behavior yet.

## M1: Engine

- `sys.System` (Real + Recorder), the typed actions, `sys.System.WriteFile (managed write)`, and the `apt` helper.
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
- secrets.

## M4: Distribution

- `install.sh`;
- GitHub Pages short URL;
- GoReleaser v2;
- release workflow.

## M5: Review to sign-off

Repeated `/audit` passes over the whole codebase and docs, checked against current primary documentation, until a full pass finds nothing and both the owner and Claude sign off. Every catalog entry must be marked reviewed in [review-process.md](review-process.md). Then tag `v1.0.0` and use it in production.
