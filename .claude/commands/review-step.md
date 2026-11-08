---
description: Review one step against the catalog, current docs and the rules (report only, no edits)
argument-hint: <step-id>
---

Review the implementation of catalog item `$ARGUMENTS`. **Don't edit any files. Report only.** There are no tests, so this review is the main quality gate: be thorough and adversarial.

Compare the code with the item in `docs/steps-catalog.md`, the rules in `CLAUDE.md` and D54–D56 in `docs/decisions.md`. Cover each of these:

- **Catalog conformance:** what it installs, writes and runs, when it's shown, and how it confirms it worked. List every divergence, in either direction.
- **Facts:** every package name, path, key, flag and default must be **V** in `docs/debian13-notes.md`. Re-open the primary source for anything that looks off. A **U** fact the code depends on is a blocker.
- **Fresh install, run once:** does it work on a fresh Debian 13 server install and on a GNOME install? Does a second run finish without errors or damage? Don't ask for re-run machinery.
- **Failure paths:** for each command that can fail, what state is the machine left in, and does the summary tell the owner what to do? Are the steps that need it skipped?
- **Hardware:** AMD or Intel, NVIDIA (Turing+, Maxwell–Ada, older), laptop or desktop, Secure Boot on or off.
- **Rules:** no `bash -c` with interpolation; apt non-interactive with confold and lock waiting; atomic file writes; downloads to `/var/tmp`; user commands via `runuser` with a clean environment; secrets never in argv or the log.
- **Simplicity (D54):** flag anything that saves state, runs after kiln exits (except D56's battery timer and D61's Docker prune timer), adds an option or question, or handles edge cases a fresh install never has.
- **Unapproved decisions:** nothing relying on **Status: proposed**. Behaviour the catalog doesn't specify is a finding.
- **Code quality:** error wrapping, context cancellation and timeouts, resource cleanup, unchecked errors, dead code; `make lint` clean.

Output findings sorted by severity (blocker / should-fix / nit), each with `file:line`, the evidence and a one-line fix. End with a verdict: `reviewed` or `changes needed`.
