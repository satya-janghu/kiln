---
description: Review one step against the catalog, current docs and the engineering rules (report only, no edits)
argument-hint: <step-id>
---

Review the implementation of step `$ARGUMENTS` in `internal/steps/$ARGUMENTS/`. **Don't edit any files. Report only.** There are no tests, so this review is the main quality gate: be thorough and adversarial.

Compare the code against the step's entry in `docs/steps-catalog.md`, the rules in `CLAUDE.md`, `docs/architecture.md` and `docs/review-process.md`. Cover each of these explicitly:

- **Catalog conformance:** purpose, applicability, options, actions, Check logic and gotchas. List every divergence, in either direction.
- **Facts:** every package name, path, key, flag and default the code uses must appear in `docs/debian13-notes.md` as **V** (checked less than 30 days ago) or be handled as **R**. Re-open the primary source for anything that looks off and quote the relevant line. Flag any **U** fact the code depends on as a blocker.
- **Check is read-only:** list every call it makes.
- **Idempotency:** walk through a second run on an already-configured machine. Does every path report `unchanged`?
- **Failure paths:** for each action that can fail, what state is the machine left in, and what does the summary tell the owner? Are dependent steps skipped?
- **Revert** (risky steps): does the revert restore the exact previous files and service state? What happens if kiln dies between "apply" and "check" (the `pending` record)?
- **Applicability matrix:** GNOME or none, SSH server or none, laptop or desktop, AMD, Intel or NVIDIA, Secure Boot on or off, backports on or off.
- **Shared path:** all changes go through `action`s and `sys.System`. No direct `os.WriteFile`, no direct `exec.Command`, no `bash -c` with interpolation. Piped installers use only `a pinned `sys.Fetch` download run with `bash <file>` as the user`.
- **Files:** every write goes through `sys.System.WriteFile (managed write)`.
- **apt:** non-interactive environment, confold/confdef, preseeded debconf, lock wait.
- **Secrets:** never in argv, logs, the summary or `state.json`.
- **Unapproved decisions:** the code must implement nothing marked "(proposed: Pn)" or relying on a **Status: proposed** decision. Any behavior the catalog doesn't specify counts as a finding, not as something to accept.
- **Code quality:** error wrapping, context cancellation, resource cleanup, races, shadowed errors, unchecked errors, dead code. Is `make lint` clean?

Output findings sorted by severity (blocker / should-fix / nit), each with `file:line`, the evidence (quote the doc or code), and a one-line fix. End with a verdict: `reviewed` (no blockers or should-fix findings) or `changes needed`.
