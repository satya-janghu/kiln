#!/usr/bin/env bash
# install.sh: download kiln, check it, and run it once (docs/distribution.md).
#
#   wget -qO- https://tinyurl.com/kilnsetup | bash
#
# Everything runs from main(), called on the last line, so a truncated
# download runs nothing. kiln runs from a temporary directory that is removed
# afterwards; it is never installed (D55).

RELEASE_URL="https://github.com/satya-janghu/kiln/releases/latest/download"
ARCHIVE="kiln_linux_amd64.tar.gz"
SUMS="checksums.txt"

# Global so the EXIT trap can see it.
tmp=""
downloader=""

die() {
	printf 'kiln: %s\n' "$*" >&2
	exit 1
}

cleanup() {
	if [ -n "$tmp" ]; then
		rm -rf -- "$tmp"
	fi
}

# fetch URL DEST: download with wget or curl, with a plain-language error.
fetch() {
	local url="$1" dest="$2" rc=0
	if [ "$downloader" = wget ]; then
		# Short connect timeouts with retries: if one of GitHub's servers is
		# unreachable, move on to the next instead of waiting minutes.
		wget -q --timeout=10 --tries=5 -O "$dest" "$url" </dev/null || rc=$?
		case "$rc" in
		0) return 0 ;;
		5) die "the secure (HTTPS) connection to GitHub could not be verified. Run: apt install ca-certificates, then try again." ;;
		8) die "GitHub has no file at $url. Check https://github.com/satya-janghu/kiln/releases." ;;
		*) die "downloading $url failed (wget exit code $rc). Check the network connection and try again." ;;
		esac
	else
		curl -fsSL --proto '=https' --connect-timeout 10 --retry 5 --retry-all-errors -o "$dest" "$url" </dev/null || rc=$?
		case "$rc" in
		0) return 0 ;;
		60 | 77) die "the secure (HTTPS) connection to GitHub could not be verified. Run: apt install ca-certificates, then try again." ;;
		22) die "GitHub has no file at $url. Check https://github.com/satya-janghu/kiln/releases." ;;
		*) die "downloading $url failed (curl exit code $rc). Check the network connection and try again." ;;
		esac
	fi
}

main() {
	set -euo pipefail

	if [ "$(id -u)" -ne 0 ]; then
		cat >&2 <<'EOF'
kiln must run as root. Log in as root (or run `su -`), then run:
  wget -qO- https://tinyurl.com/kilnsetup | bash
If sudo already works, you can instead run:
  wget -qO- https://tinyurl.com/kilnsetup | sudo bash
EOF
		exit 1
	fi

	[ -r /etc/os-release ] || die "cannot read /etc/os-release. kiln runs only on Debian 13 (trixie)."
	local os_id os_version arch
	# shellcheck source=/dev/null
	os_id=$(. /etc/os-release && printf '%s' "${ID-}")
	# shellcheck source=/dev/null
	os_version=$(. /etc/os-release && printf '%s' "${VERSION_ID-}")
	if [ "$os_id" != debian ] || [ "$os_version" != 13 ]; then
		die "kiln runs only on Debian 13 (trixie). This system reports ID=${os_id:-unknown} VERSION_ID=${os_version:-unknown}."
	fi
	arch=$(dpkg --print-architecture 2>/dev/null || true)
	[ "$arch" = amd64 ] || die "kiln runs only on amd64 (64-bit PC) machines. This one is ${arch:-unknown}."

	if command -v wget >/dev/null 2>&1; then
		downloader=wget
	elif command -v curl >/dev/null 2>&1; then
		downloader=curl
	else
		die "neither wget nor curl is installed. Run: apt install wget, then try again."
	fi

	tmp=$(mktemp -d /var/tmp/kiln.XXXXXX) || die "could not create a temporary directory in /var/tmp."
	trap cleanup EXIT
	trap 'exit 1' HUP INT TERM

	printf 'Downloading kiln...\n'
	fetch "$RELEASE_URL/$ARCHIVE" "$tmp/$ARCHIVE"
	fetch "$RELEASE_URL/$SUMS" "$tmp/$SUMS"

	# checksums.txt holds bare file names, so this must run inside $tmp.
	# If nothing matched, sha256sum fails with "no file was verified".
	(cd "$tmp" && sha256sum --quiet --ignore-missing -c "$SUMS") ||
		die "the download doesn't match its published checksum. Nothing was changed. Try again."

	tar -xzf "$tmp/$ARCHIVE" -C "$tmp" --no-same-owner kiln ||
		die "the downloaded archive could not be unpacked. Nothing was changed."
	chmod 0755 "$tmp/kiln"

	# The script arrived on stdin, so kiln must read the keyboard from the
	# terminal. Opening /dev/tty is the only reliable test (the node is 0666).
	if ! { : </dev/tty; } 2>/dev/null; then
		die "run this from the machine's console (or an SSH session with a terminal)."
	fi
	# A child, not exec, so the trap still removes $tmp afterwards.
	"$tmp/kiln" "$@" </dev/tty
}

main "$@"
