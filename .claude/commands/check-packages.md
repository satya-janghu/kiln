---
description: Verify declared Debian package names against packages.debian.org (no containers)
argument-hint: [step-id]  (omit for all steps)
---

Verify that every package kiln can declare exists in Debian 13 trixie amd64, in the suite and component the code assumes.

1. Get the list with `go run ./cmd/kiln catalog packages --all` (or `--step $ARGUMENTS`). It prints each package with its suite (`trixie` or `trixie-backports`), its component, and its repo (debian, docker, github-cli, extrepo:<name>). This only prints; it changes nothing. If that hidden command doesn't exist yet, collect the names from `docs/steps-catalog.md` and say so.
2. For each Debian package, open `https://packages.debian.org/trixie/<pkg>` (or `trixie-backports/<pkg>`). Record whether it exists, its section (main/contrib/non-free/non-free-firmware), its current version, and whether it's a transitional or virtual package. Check vendor packages in that vendor's repo index (Docker: `https://download.docker.com/linux/debian/dists/trixie/stable/binary-amd64/Packages`; GitHub CLI: `https://cli.github.com/packages/dists/stable/main/binary-amd64/Packages`).
3. Report a table of package, suite, component, status (ok / missing / transitional → new name / wrong component) and version. For each problem, propose the fix and name the catalog entry and `docs/debian13-notes.md` row to update.
4. Don't edit files unless asked. Do update the "checked on" dates in `docs/debian13-notes.md` if the owner says so.
