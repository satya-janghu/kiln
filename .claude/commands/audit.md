---
description: Full cross-cutting review of code and docs against current primary documentation (report only)
---

Run a full audit of the kiln repository. **Don't edit files. Report only.** This is the repeated pass that leads to production sign-off (see `docs/review-process.md`). Use subagents to cover areas in parallel where that helps, then merge and de-duplicate their findings.

Cover:

1. **Docs vs code drift:**
   - `docs/steps-catalog.md` vs `internal/steps/*`: every action, applicability condition and Check;
   - `docs/options-reference.md` vs the options schema and defaults;
   - `docs/architecture.md` vs the package layout and interfaces;
   - `docs/tui-design.md` vs `internal/tui`.
2. **Facts:**
   - every **V** row in `docs/debian13-notes.md` older than 30 days: re-open its source and confirm it still holds (newer point releases, backports versions);
   - every **U** row: is any code relying on it? That's a blocker.
3. **Engineering rules 1–12** in `CLAUDE.md`, checked across the whole codebase:
   - grep for `os.WriteFile`, `exec.Command` outside `sys`, `bash -c`, `/tmp/`, and secrets passed in argv;
   - check that every step is idempotent;
   - check that a failing step can't abort the run.
4. **Safety:** the apply gate and the darwin refusal, the `pending` revert path, the sleep inhibitor, behavior when run over SSH (a warning only), and behavior when the network drops mid-run.
5. **Code quality:** error handling and wrapping, context cancellation and timeouts on every network call and long-running command, goroutine and channel leaks in the runner/TUI event flow, data races, resource cleanup, and `make lint` / `make vuln` results.
6. **TUI:** layout at 80x24, 100x30 and 240x67, the console glyph set under `TERM=linux`, and every screen's keys against `docs/tui-design.md`. Reason it through from the code.
7. **install.sh and release config:** shellcheck-clean, `main "$@"` wrapper, checksum verification, `/dev/tty` handling, and the GoReleaser archive and checksum names matching install.sh.
8. **Dependencies:** current versions of Go and every module in `go.mod` vs `docs/decisions.md` D1, and any security advisories.

Output findings grouped by area and sorted by severity (blocker / should-fix / nit), each with `file:line`, evidence and a proposed fix. End with: number of findings by severity, which review-status rows in `docs/review-process.md` could move to `reviewed`, and whether this pass would count as the sign-off pass.
