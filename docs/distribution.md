# Distribution

## The one-liner (D58, D60)

**Always curl** (owner's rule, D60). Typed at the console of a freshly installed machine, as root (log in as root, or `su -`):

```bash
apt install -y curl && curl -fsSL --connect-timeout 10 https://tinyurl.com/kilnsetup | bash
```

With sudo already working: `sudo apt install -y curl && curl -fsSL --connect-timeout 10 https://tinyurl.com/kilnsetup | sudo bash`.

- **`apt install -y curl` first:** curl is Priority `optional` in trixie, so a fresh install doesn't have it.
- **`--connect-timeout 10`:** the owner's home network can't reach one of GitHub's four download servers (`185.199.109.133`, seen 2026-11-08 from the Mac and the test box). Without the timeout curl waits about 135 s on it; with it, curl moves to the next server after about 5 s.
- `https://tinyurl.com/kilnsetup` redirects to `https://github.com/satya-janghu/kiln/releases/latest/download/install.sh`, so it always gets the newest release's script. There's no website or GitHub Pages to maintain. Type the `https://`.
- To run kiln again, for example after fixing a failed item, type the same command again.

## install.sh

Small and shellcheck-clean.

1. The whole body sits in `main() { ... }; main "$@"`, so a truncated download runs nothing.
2. `set -euo pipefail`. It must run as root; otherwise it prints the one-liners and exits.
3. It checks `/etc/os-release` for `ID=debian` and `VERSION_ID="13"`, and that `dpkg --print-architecture` is `amd64`.
4. It downloads with curl (`--proto =https --connect-timeout 10 --retry 5 --retry-all-errors`); without curl it says "Run: apt install -y curl". If HTTPS verification fails, it says to run `apt install ca-certificates`.
5. It downloads `kiln_linux_amd64.tar.gz` and `checksums.txt` from `releases/latest/download/` into `mktemp -d /var/tmp/kiln.XXXXXX` (removed by a `trap` on exit), and runs `sha256sum --ignore-missing -c checksums.txt` inside that directory. If nothing matched, sha256sum fails and nothing runs.
6. If `/dev/tty` can be opened, it runs `"$tmp/kiln" "$@" </dev/tty`: the script came on stdin, so kiln reads the keyboard from the terminal. It runs kiln as a child, not with `exec`, so the trap still removes the directory.
7. With no terminal, it says "run this from the machine's console" and exits non-zero.

kiln is never installed and creates no `/etc/kiln` (D55). Afterwards the only trace is `/var/log/kiln.log`, plus the battery script and timer on HP laptops that need them (D56).

## Releases (GoReleaser v2)

- `.goreleaser.yaml` with `version: 2`. One build: `linux/amd64`, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}"`.
- The archive is always `kiln_linux_amd64.tar.gz` (no version) and the checksum file `checksums.txt` (D14), so `releases/latest/download/` URLs keep working.
- `install.sh` is attached to every release (`release.extra_files`), which is what the short URL points at.
- `.github/workflows/release.yml` runs on `v*` tags: tidy check, build, vet, golangci-lint, govulncheck, shellcheck, then `goreleaser release --clean`.
- `make release-check` runs `goreleaser check` locally.
