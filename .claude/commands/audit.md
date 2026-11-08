---
description: Full cross-cutting review of code and docs against current primary documentation (report only)
---

Run a full audit of the kiln repository. **Don't edit files. Report only.** This is the repeated pass that leads to sign-off (see `docs/review-process.md`). Use subagents for areas in parallel where that helps, then merge and de-duplicate their findings.

Cover:

1. **Docs vs code:** every item in `docs/steps-catalog.md` against its code; `docs/architecture.md` against the package layout; `docs/tui-design.md` against the TUI.
2. **Facts:** re-check **V** rows in `docs/debian13-notes.md` older than 30 days; any code relying on a **U** row is a blocker.
3. **Rules in `CLAUDE.md`** across the whole codebase:
   - grep for `bash -c`, `/tmp/`, secrets in argv, and non-atomic writes;
   - check that a failing step can't stop the run.
4. **Simplicity (D54):** anything that saves state, keeps running after kiln exits (except D56's battery timer), asks extra questions, or exists only for re-runs.
5. **Safety:** the refusal on non-Linux, non-root or non-Debian-13; SSH checked before reload; network loss mid-run.
6. **Code quality:**
   - error handling;
   - timeouts on network calls and long commands;
   - goroutine and channel leaks between the runner and the TUI;
   - races;
   - `make lint` and `make vuln`.
7. **TUI:** layout at 80x25 and on a large console, the glyph set under `TERM=linux`, and every screen's keys against `docs/tui-design.md`.
8. **install.sh and release config:** shellcheck-clean, checksum verification, `/dev/tty` handling, and GoReleaser names matching install.sh.
9. **Dependencies:** current Go and module versions against `docs/decisions.md` D1, and security advisories.

Output findings grouped by area, sorted by severity (blocker / should-fix / nit), each with `file:line`, evidence and a proposed fix. End with counts by severity and whether this pass counts as sign-off.
