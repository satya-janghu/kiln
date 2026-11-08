---
description: Verify declared Debian package names against packages.debian.org (no containers)
argument-hint: [step-id]  (omit for all steps)
---

Verify that every package kiln installs exists in Debian 13 trixie amd64, in the suite and component the code assumes.

1. Collect the package names from the code for `$ARGUMENTS` (or every step), and cross-check them against `docs/steps-catalog.md`.
2. For each Debian package, open `https://packages.debian.org/trixie/<pkg>` (or `trixie-backports/<pkg>`). Record:
   - whether it exists;
   - its section (main, contrib, non-free or non-free-firmware);
   - its current version;
   - whether it's transitional or virtual.
3. Check vendor packages in that vendor's repo index:
   - Docker: `https://download.docker.com/linux/debian/dists/trixie/stable/binary-amd64/Packages`
   - GitHub CLI: `https://cli.github.com/packages/dists/stable/main/binary-amd64/Packages`
   - NVIDIA's repos, from their official install docs.
4. Report a table: package, suite, component, status (ok / missing / transitional → new name / wrong component) and version. For each problem, propose the fix and name the catalog item and the `docs/debian13-notes.md` row to update.
5. Don't edit files unless asked.
