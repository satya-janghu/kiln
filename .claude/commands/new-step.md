---
description: Implement one steps-catalog step (no tests; facts verified against primary docs)
argument-hint: <step-id>
---

Implement the catalog step `$ARGUMENTS`. This project has **no tests**. Correctness comes from verified facts and careful reasoning, so don't add `_test.go` files.

1. Read the step's entry in `docs/steps-catalog.md`, the rules in `CLAUDE.md`, and the relevant parts of `docs/architecture.md`, `docs/debian13-notes.md` and `docs/options-reference.md`.
2. List every external fact the step relies on: package names and suites, file paths, config keys, command flags, defaults, sysfs attributes. For each one:
   - if it's **V** in `debian13-notes.md` and was checked less than 30 days ago, use it;
   - otherwise, check it now against a current primary source (packages.debian.org/trixie, manpages.debian.org/trixie, sources.debian.org, release notes, upstream docs), and record the URL and date in `debian13-notes.md`;
   - if it can't be verified, stop and ask. Never write code from memory.
   - If the catalog entry contains "(proposed: Pn)", or relies on a decision with **Status: proposed** in `docs/decisions.md`, stop and ask the owner to decide it first. If implementing reveals a choice the catalog doesn't settle, present options instead of picking one.
3. Look at one or two steps already in `internal/steps/` and follow their structure.
4. Create `internal/steps/<id>/`:
   - `ID`, `Title`, `Category`, `DependsOn` and `Applies` exactly as in the catalog. Not-applicable reasons are plain language and cover hardware and installer choices (GNOME or none, SSH server or none).
   - `Check`: read-only, using only `env.Sys`, `env.Facts` and `env.State`.
   - `Plan`: typed `action.Action`s. Use `Custom` only where runtime feedback is unavoidable, and give it a precise `Describe()`.
   - `Apply`: the default from `step.Base`, unless the catalog needs more. Risky changes use `internal/revert`.
   - Every error says what failed and what to do next.
5. Register the step in catalog order, and add its options to the options schema with defaults (everything on).
6. Run `make build lint vuln` and fix everything.
7. If the step declares packages, run `/check-packages $ARGUMENTS`.
8. Update `docs/steps-catalog.md`, `docs/options-reference.md` and `docs/debian13-notes.md` if behavior, options or facts changed. Set the step's row in `docs/review-process.md` to `implemented`.
9. Report what you built, every fact used with its V/R status, and anything you had to decide. Then suggest running `/review-step $ARGUMENTS`.

Never run the step against a real system from this Mac.
