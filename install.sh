#!/usr/bin/env bash
# install.sh: download, verify and install kiln, then start the wizard.
#
#   wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash
#
# Spec: docs/distribution.md. Everything runs from main(), called on the last
# line, so a truncated download runs nothing.

RELEASE_URL="https://github.com/satya-janghu/kiln/releases/latest/download"
ARCHIVE="kiln_linux_amd64.tar.gz"
SUMS="checksums.txt"
BIN="/usr/local/bin/kiln"
CONF_DIR="/etc/kiln"

# Set in main; global (not local) so the EXIT trap can still see it.
tmp=""
downloader=""

die() {
	printf 'kiln install: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [ -n "$tmp" ]; then
		rm -rf -- "$tmp"
	fi
	tmp=""
}

# fetch URL DEST: download with wget or curl, with a plain-language error.
fetch() {
	local url="$1" dest="$2" rc=0
	if [ "$downloader" = wget ]; then
		wget -q -O "$dest" "$url" </dev/null || rc=$?
		case "$rc" in
		0) return 0 ;;
		5) die "the secure (HTTPS) connection to GitHub could not be verified. Install the CA certificates with: apt install ca-certificates, then run the installer again." ;;
		8) die "GitHub has no file at $url. There may be no published release yet; check https://github.com/satya-janghu/kiln/releases." ;;
		*) die "downloading $url failed (wget exit code $rc). Check the network connection and try again." ;;
		esac
	else
		curl -fsSL --proto '=https' -o "$dest" "$url" </dev/null || rc=$?
		case "$rc" in
		0) return 0 ;;
		60 | 77) die "the secure (HTTPS) connection to GitHub could not be verified. Install the CA certificates with: apt install ca-certificates, then run the installer again." ;;
		22) die "GitHub has no file at $url. There may be no published release yet; check https://github.com/satya-janghu/kiln/releases." ;;
		*) die "downloading $url failed (curl exit code $rc). Check the network connection and try again." ;;
		esac
	fi
}

main() {
	set -euo pipefail

	if [ "$(id -u)" -ne 0 ]; then
		cat >&2 <<'EOF'
kiln must be installed as root.

If sudo works on this machine, run:
  wget -qO- https://satya-janghu.github.io/kiln/i | sudo bash

If there is no sudo yet (you set a root password during install), log in as
root, or run `su -` (not plain `su`), and then run:
  wget -qO- https://satya-janghu.github.io/kiln/i | bash
EOF
		exit 1
	fi

	[ -r /etc/os-release ] || die "cannot read /etc/os-release, so this system cannot be identified. kiln runs only on Debian 13 (trixie)."
	local os_id os_version arch
	# shellcheck source=/dev/null
	os_id=$(. /etc/os-release && printf '%s' "${ID-}")
	# shellcheck source=/dev/null
	os_version=$(. /etc/os-release && printf '%s' "${VERSION_ID-}")
	if [ "$os_id" != debian ] || [ "$os_version" != 13 ]; then
		die "kiln runs only on Debian 13 (trixie). This system reports ID=${os_id:-unknown} VERSION_ID=${os_version:-unknown}."
	fi

	command -v dpkg >/dev/null 2>&1 || die "dpkg was not found, so the architecture cannot be checked. kiln runs only on Debian 13 amd64."
	arch=$(dpkg --print-architecture)
	[ "$arch" = amd64 ] || die "kiln runs only on amd64 (64-bit PC) machines. This system is $arch."

	if command -v wget >/dev/null 2>&1; then
		downloader=wget
	elif command -v curl >/dev/null 2>&1; then
		downloader=curl
	else
		die "neither wget nor curl is installed. Install wget first: apt install wget"
	fi

	tmp=$(mktemp -d /var/tmp/kiln.XXXXXX) || die "could not create a temporary directory in /var/tmp."
	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM

	printf 'Downloading kiln...\n'
	fetch "$RELEASE_URL/$ARCHIVE" "$tmp/$ARCHIVE"
	fetch "$RELEASE_URL/$SUMS" "$tmp/$SUMS"

	printf 'Verifying the download...\n'
	# checksums.txt holds bare file names, so this must run inside $tmp.
	# If nothing matched, sha256sum fails with "no file was verified".
	(cd "$tmp" && sha256sum --ignore-missing -c "$SUMS") ||
		die "the downloaded file does not match the published checksum. Nothing was installed. Try again; if it keeps failing, report it at https://github.com/satya-janghu/kiln/issues."

	mkdir "$tmp/x"
	tar -xzf "$tmp/$ARCHIVE" -C "$tmp/x" --no-same-owner kiln ||
		die "the downloaded archive could not be unpacked. Nothing was installed."

	# Install next to the target and rename, so a running kiln is not overwritten in place.
	install -d -m 0755 "$(dirname "$BIN")"
	install -m 0755 -o root -g root "$tmp/x/kiln" "$BIN.new"
	mv -f "$BIN.new" "$BIN"
	install -d -m 0755 -o root -g root "$CONF_DIR"
	touch "$CONF_DIR/allow-apply"
	printf 'Installed kiln to %s.\n' "$BIN"

	# exec replaces this shell, so the EXIT trap would never run: clean up now.
	cleanup
	trap - EXIT HUP INT TERM

	# This script arrived on stdin, so the wizard must read the keyboard from the
	# terminal. Opening /dev/tty is the only reliable test (the node is 0666).
	if { : </dev/tty; } 2>/dev/null; then
		exec "$BIN" "$@" </dev/tty
	fi
	printf '%s\n' "kiln was installed. Run \`sudo kiln\` from a terminal."
	exit 0
}

main "$@"
