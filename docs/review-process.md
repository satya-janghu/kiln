# Review process

**There are no tests** (D30): no unit tests, no container tests, no test harnesses. Quality comes from review against primary sources and from runs on the owner's throwaway test box (D46).

## Primary sources

Every package name, path, config key, command flag and default the code relies on is checked against a current primary source:

- packages.debian.org/trixie (and trixie-backports);
- manpages.debian.org/trixie;
- sources.debian.org, for Debian's shipped config and maintainer scripts;
- the trixie release notes;
- kernel.org docs;
- upstream docs (Docker, NVIDIA, GNOME, Go, Charm).

Blogs, forums and memory don't count. Each fact goes in [debian13-notes.md](debian13-notes.md) with its source and date:

- **V**: verified. Code may rely on it.
- **U**: not verified. Code must not rely on it.
- **R**: hardware behavior detected at run time, with both outcomes handled.

Facts from the old tool carry over. New ones (D55's *verify* items, D56's HP battery method) start as **U**.

## Static analysis

Review tooling, not tests. Must be clean before anything is tagged:

- `make lint`: `go vet`, golangci-lint v2, `shellcheck install.sh`;
- `make vuln`: govulncheck;
- `make release-check`.

## Per-step review

Each step is read against its entry in [steps-catalog.md](steps-catalog.md) and the rules in CLAUDE.md and [architecture.md](architecture.md). The review argues:

- every fact it uses is **V** (or **R** with both outcomes handled);
- a second run does no harm;
- each command that can fail leaves the machine usable, and the summary says what to do;
- no `bash -c` with interpolation; secrets never reach argv or the log;
- it fits both GNOME and no desktop, and hides correctly without the hardware.

## Test box

The owner's test box (D46, 10.10.10.150, HP Envy x360) may be reinstalled freely. The owner runs kiln there; it is never run from the Mac.

## Checklist for the rewrite

- [ ] Catalog rewritten to D55 and D56, and approved by the owner.
- [ ] Every step reviewed against the catalog.
- [ ] No **U** facts left that code depends on.
- [ ] `make build lint vuln release-check` clean.
- [ ] Fresh Debian 13 install on the test box, one full run, summary clean.
- [ ] A second run on the same box: nothing breaks, nothing new fails.
- [ ] A GNOME install and an NVIDIA machine, when available.
- [ ] Owner sign-off.
