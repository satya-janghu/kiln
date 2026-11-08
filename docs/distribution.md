# Distribution

## The one-liner

Typed at the console of a freshly installed machine, so it's short (D31). `install.sh` is served from GitHub Pages.

As root (log in as root or use `su -`, not plain `su`):

```bash
wget -qO- https://satya-janghu.github.io/kiln/i | bash
```

With sudo working:

```bash
wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash
```

Type the `https://`. Without it, wget's first request is plain HTTP before GitHub redirects, and that response could be tampered with. `curl -fsSL https://satya-janghu.github.io/kiln/i | bash` works too, but minimal installs usually have wget and not curl.

To run kiln again (for example after a failed item), type the one-liner again.

## install.sh

Small and shellcheck-clean.

1. The whole body sits in `main() { ... }; main "$@"`, so a truncated download runs nothing.
2. `set -euo pipefail`. It requires root; otherwise it prints both one-liners and exits.
3. It checks `/etc/os-release` for `ID=debian` and `VERSION_ID="13"`, and that `dpkg --print-architecture` is `amd64`.
4. It uses `wget` if present, else `curl`, else it says "Install wget first: apt install wget". If HTTPS verification fails, it says to run `apt install ca-certificates`.
5. It downloads `https://github.com/satya-janghu/kiln/releases/latest/download/kiln_linux_amd64.tar.gz` and `.../checksums.txt` into `tmp=$(mktemp -d /var/tmp/kiln.XXXXXX)`, removed by a `trap` on exit. It runs `cd "$tmp" && sha256sum --ignore-missing -c checksums.txt` inside that directory, because checksums.txt holds bare file names. If nothing matched, coreutils prints "no file was verified" and exits non-zero, so a missing archive stops the install.
6. If `{ : </dev/tty; } 2>/dev/null` succeeds, it runs `"$tmp/kiln" "$@" </dev/tty`. The script arrived on stdin, so kiln must read the keyboard from the TTY. Don't test `[ -r /dev/tty ]`: the node is mode 0666, so that is true even with no controlling terminal. It runs kiln as a child, not with `exec`, so the `trap` still removes the directory afterwards.
7. With no TTY, it says "Run this from the machine's console." and exits non-zero.

kiln is never installed to `/usr/local/bin`, and no `/etc/kiln` or allow-apply file is created (D55). After the run, the only trace of kiln is `/var/log/kiln.log` (plus the battery script and timer on laptops that need them, D56).

## Releases (GoReleaser v2)

- `.goreleaser.yaml` with `version: 2`.
- One build: `linux/amd64`, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}"`.
- The archive is always `kiln_linux_amd64.tar.gz`, with no version, so `releases/latest/download/` URLs keep working.
- The checksum file is `checksums.txt`, set with `checksum.name_template` (D14).
- A GitHub Actions workflow on `v*` tags runs `make lint`, `make vuln`, then `goreleaser release --clean`. It then deploys GitHub Pages from `site/`, copying `install.sh` to `site/i` and `site/install.sh`.
- Pages use the Actions source (`actions/upload-pages-artifact` + `actions/deploy-pages`). The repo's `docs/` folder is not a website.
- `make release-check` runs `goreleaser check` locally.
- A tag is cut only after sign-off ([review-process.md](review-process.md)).
