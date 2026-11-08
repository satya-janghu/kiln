---
description: Implement one steps-catalog item (no tests; facts verified against primary docs)
argument-hint: <step-id>
---

Implement the catalog item `$ARGUMENTS`. This project has **no tests**, so don't add `_test.go` files. Correctness comes from verified facts and careful reasoning.

1. Read the item in `docs/steps-catalog.md`, the rules in `CLAUDE.md`, D54–D56 in `docs/decisions.md`, and `docs/architecture.md`.
2. List every external fact the item relies on: package names and suites, file paths, config keys, command flags, defaults, sysfs attributes. For each:
   - if it's **V** in `docs/debian13-notes.md`, use it;
   - otherwise verify it now against a primary source (packages.debian.org/trixie, manpages.debian.org/trixie, sources.debian.org, release notes, upstream docs) and record it there with the URL;
   - if it can't be verified, stop and ask. Never write code from memory.
3. Stop and ask if the item relies on anything with **Status: proposed**, or if implementing it reveals a choice the catalog doesn't settle. Present options; don't pick one.
4. Keep it simple: plain commands with `exec.Command` arguments, atomic file writes, no options, no saved state, nothing that runs after kiln exits (D56's battery timer is the only exception). Every error says what failed and what to do next.
5. Run `make build lint vuln` and fix everything.
6. If the item installs packages, run `/check-packages $ARGUMENTS`.
7. Update `docs/steps-catalog.md` and `docs/debian13-notes.md` if behaviour or facts changed.
8. Report what you built and every fact used with its status. Then suggest `/review-step $ARGUMENTS`.

Never run kiln against a real system from this Mac.
