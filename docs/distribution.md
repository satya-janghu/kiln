# Distribution

## The one-liner

You type this at the console of a freshly installed machine, so it's kept short. `install.sh` is served from GitHub Pages.

With sudo already working:

```bash
wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash
```

As root (if you set a root password during install there is no sudo yet; log in as root or use `su -`, not plain `su`):

```bash
wget -qO- https://satya-janghu.github.io/kiln/i | bash
```

Type the `https://` part. Without it, wget's first request goes over plain HTTP before GitHub redirects, and that response could be tampered with on the network. `curl -fsSL https://satya-janghu.github.io/kiln/i | sudo bash` works too. Minimal installs usually have wget but not curl.

After the first run, the binary is already installed, so later runs are just `sudo kiln`.

## install.sh (small, auditable, shellcheck-clean)

1. The whole body sits inside `main() { ... }; main "$@"`, so a truncated download runs nothing.
2. It uses `set -euo pipefail`. It requires root (`id -u` = 0). Otherwise it prints both one-liners and exits.
3. It checks `/etc/os-release` for `ID=debian` and `VERSION_ID="13"`, and checks that `dpkg --print-architecture` is `amd64`.
4. It picks a downloader: `wget` if present, else `curl`, else it fails with "Install wget first: apt install wget". wget and `ca-certificates` are both Priority standard, so they're absent if the installer's "standard system utilities" task was unticked. If HTTPS verification fails, the message says to run `apt install ca-certificates`.
5. It downloads `https://github.com/satya-janghu/kiln/releases/latest/download/kiln_linux_amd64.tar.gz` and `.../checksums.txt` into `tmp=$(mktemp -d /var/tmp/kiln.XXXXXX)` (removed by a `trap` on exit). Then it runs `cd "$tmp" && sha256sum --ignore-missing -c checksums.txt`. It must run inside that directory, because checksums.txt holds bare file names. If nothing matched, coreutils 9.7 prints "no file was verified" and exits non-zero, so a missing or renamed archive also stops the install.
6. It installs to `/usr/local/bin/kiln` (0755) and creates `/etc/kiln/` (0755) and `/etc/kiln/allow-apply`.
7. If `{ : </dev/tty; } 2>/dev/null` succeeds, it runs `exec /usr/local/bin/kiln "$@" </dev/tty`. The script itself arrived on stdin, so the wizard has to read the keyboard from the TTY instead. Don't test `[ -r /dev/tty ]`: the device node is mode 0666, so that test is true even when there is no controlling terminal; only actually opening it tells.
8. If there's no TTY, it prints "kiln was installed. Run `sudo kiln` from a terminal." and exits 0.

## Releases (GoReleaser v2)

- `.goreleaser.yaml` with `version: 2`.
- One build: `linux/amd64`, `CGO_ENABLED=0`, `-trimpath`, `-ldflags "-s -w -X main.version={{.Version}} -X main.commit={{.Commit}}"`.
- The archive name is fixed as `kiln_linux_amd64.tar.gz`, with no version in it, so `releases/latest/download/` URLs keep working.
- The checksum file is named `checksums.txt` via `checksum.name_template`. GoReleaser's default name includes the version.
- A GitHub Actions workflow runs on `v*` tags: `make lint`, `make vuln`, then `goreleaser release --clean`. After that it deploys GitHub Pages from `site/`, where the workflow copies `install.sh` to `site/i` and `site/install.sh`.
- Pages are deployed with the Actions source (`actions/upload-pages-artifact` + `actions/deploy-pages`), not "deploy from /docs". The repo's `docs/` folder is design documentation, not a website.
- Check the config locally with `make release-check` (`goreleaser check`).
- A tag is only cut after review sign-off (see [review-process.md](review-process.md)).
