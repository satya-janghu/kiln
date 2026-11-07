# Review process

**There are no tests.** That means no unit tests, no container tests and no hardware test runs. kiln goes into production once the owner and Claude both agree that every part has been checked against current primary documentation and reasoned through for correctness. This file defines what "checked" means and tracks where each step stands.

## What review relies on

1. **Primary sources only.** Every Debian package name, suite, file path, config key, command and flag, default value, and sysfs attribute that the code relies on must be confirmed against a current primary source:
   - packages.debian.org/trixie (and `trixie-backports`);
   - manpages.debian.org/trixie;
   - sources.debian.org (for Debian's shipped config and maintainer scripts);
   - the trixie release notes;
   - kernel.org documentation and the ABI;
   - upstream docs (Docker, firewalld, GNOME, Charm, Go).

   Blogs, forums and memory don't count. Each fact is recorded in [debian13-notes.md](debian13-notes.md) with its URL and the date it was checked. Code may only rely on facts marked **V** there.
2. **Static analysis** (review tooling, not tests). These must be clean:
   - `make lint`: `go vet`, golangci-lint v2 with at least errcheck, staticcheck, gosec, govet, ineffassign, unused and errorlint, plus `shellcheck install.sh`;
   - `make vuln`: govulncheck;
   - `make release-check`.
3. **Reasoned review per step** (`/review-step <id>`), against [steps-catalog.md](steps-catalog.md) and the rules in CLAUDE.md. The review must argue each of these explicitly:
   - **Check is read-only.** List every call it makes.
   - **Idempotency.** Walk through a second run and show it reports `unchanged`.
   - **Failure paths.** For every action that can fail, say what state the machine is left in, and what the summary tells the owner.
   - **Revert.** For risky steps, show that the revert restores the exact previous files and service state.
   - **Not-applicable logic.** Cover every hardware and installer combination: GNOME or none, SSH server or none, laptop or desktop, AMD or Intel or NVIDIA.
   - **No mutation outside `sys.System`,** and no `bash -c` with interpolation.
   - **Secrets never reach** argv, logs or the summary.
4. **Cross-cutting audit** (`/audit`): a full pass over all code and docs. It looks for drift between the catalog and the code, between the catalog and the notes, and between the options reference and the code. It also re-checks every **V** fact older than 30 days, plus concurrency issues, error handling, resource leaks, and TUI behavior at 80x24, at wide sizes, and on the Linux console.
5. **Package list check** (`/check-packages`): every package any step can declare must exist in its stated suite and component. Check it against packages.debian.org, or the vendor repo's index for Docker and the GitHub CLI.

## Safety nets built into the product

Reviews can't catch everything a real machine might do. These features stay in the design for that reason:
- **The review screen** shows the exact plan before anything changes. It comes from the same code path as the real run.
- **Backups:** `.kiln-bak` copies of every file kiln changes.
- **Verify, then revert** for network, SSH and firewall changes, plus an interrupted-change prompt on the next start.
- **A failed step never stops the run,** and the summary says what to do next.
- **A full log** in `/var/log/kiln.log`.

## Sign-off

kiln is ready for production when all of the following hold:

- [ ] Every step below is `reviewed`, with the date and the commit it was reviewed at.
- [ ] One full `/audit` pass on the release commit reports no findings, or only findings the owner accepted in writing in [decisions.md](decisions.md).
- [ ] `make build lint vuln release-check` is clean on the release commit.
- [ ] No **U** facts are left in [debian13-notes.md](debian13-notes.md) that any code path depends on.
- [ ] Every **R** row in debian13-notes.md has its run-time detection, and the handling of both outcomes, checked in the step review.
- [ ] The owner has read the catalog and agrees with it.

## Review status

| Step | Status | Reviewed at (date / commit) | Notes |
|---|---|---|---|
| scaffold (CLI tree, Makefile, lint config, CI) | implemented | | M0. Commands are stubs that name their milestone. |
| preflight | not started | | |
| sudo | not started | | |
| apt-sources | not started | | |
| mirrors | not started | | |
| upgrade | not started | | |
| firmware-packages | not started | | |
| kernel | not started | | |
| network-migration | not started | | |
| multimedia | not started | | |
| repos | not started | | |
| devtools | not started | | |
| system | not started | | |
| power | not started | | |
| thermal | not started | | |
| battery | not started | | |
| firewall | not started | | |
| ssh | not started | | |
| fail2ban | not started | | |
| docker | not started | | |
| cockpit | not started | | |
| remote-desktop | not started | | |
| autoupdates | not started | | |
| node | not started | | |
| python | not started | | |
| go | not started | | |
| firmware-updates | not started | | |
| summary | not started | | |
| wizard (TUI) | not started | | |
| install.sh | not started | | |

Status values: `not started` → `implemented` → `reviewed` (no open findings) → `signed off` (by the owner).
