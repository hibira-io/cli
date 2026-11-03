#!/bin/sh
# Installs the hibira CLI from the GitHub releases of hibira-io/cli.
#
#   curl -fsSL https://hibira.io/install.sh | sh
#
# Environment:
#   HIBIRA_VERSION      version to install, such as v0.1.0 (default: latest)
#   HIBIRA_INSTALL_DIR  where to put the binary (default: /usr/local/bin when
#                       writable, otherwise ~/.local/bin)
#
# Everything runs inside main, so a download cut off halfway runs nothing.

set -eu

REPO="hibira-io/cli"

say() { printf '%s\n' "$*"; }
err() { printf 'hibira install: %s\n' "$*" >&2; exit 1; }
has() { command -v "$1" >/dev/null 2>&1; }

fetch() { # fetch URL [OUTFILE]
	if has curl; then
		if [ -n "${2:-}" ]; then curl -fsSL --retry 3 -o "$2" "$1"; else curl -fsSL --retry 3 "$1"; fi
	elif has wget; then
		if [ -n "${2:-}" ]; then wget -q -O "$2" "$1"; else wget -q -O - "$1"; fi
	else
		err "needs curl or wget"
	fi
}

latest_version() {
	# The /releases/latest page redirects to /releases/tag/vX.Y.Z.
	if has curl; then
		url=$(curl -fsSLI --retry 3 -o /dev/null -w '%{url_effective}' "https://github.com/$REPO/releases/latest")
	else
		url=$(wget -q -S --spider "https://github.com/$REPO/releases/latest" 2>&1 | sed -n 's/^ *[Ll]ocation: *//p' | tail -n 1)
	fi
	tag=${url##*/}
	tag=$(printf '%s' "$tag" | tr -d '\r')
	case "$tag" in
		v[0-9]*) printf '%s' "$tag" ;;
		*) err "could not find the latest release (got '$url')" ;;
	esac
}

detect_os() {
	case "$(uname -s)" in
		Linux) echo linux ;;
		Darwin) echo darwin ;;
		MINGW* | MSYS* | CYGWIN*) err "on Windows, run in PowerShell: irm https://hibira.io/install.ps1 | iex" ;;
		*) err "unsupported OS $(uname -s); build from source with: go install github.com/$REPO/cmd/hibira@latest" ;;
	esac
}

detect_arch() {
	case "$(uname -m)" in
		x86_64 | amd64)
			# A Terminal running under Rosetta reports x86_64 on Apple silicon.
			if [ "$1" = darwin ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || echo 0)" = 1 ]; then
				echo arm64
			else
				echo amd64
			fi
			;;
		arm64 | aarch64) echo arm64 ;;
		*) err "unsupported CPU $(uname -m); build from source with: go install github.com/$REPO/cmd/hibira@latest" ;;
	esac
}

sha256() {
	if has sha256sum; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif has shasum; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	elif has openssl; then
		openssl dgst -sha256 "$1" | sed 's/^.*= //'
	else
		err "needs sha256sum, shasum or openssl to verify the download"
	fi
}

pick_dir() {
	if [ -n "${HIBIRA_INSTALL_DIR:-}" ]; then
		echo "$HIBIRA_INSTALL_DIR"
	elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
		echo /usr/local/bin
	else
		echo "$HOME/.local/bin"
	fi
}

main() {
	os=$(detect_os)
	arch=$(detect_arch "$os")
	version=${HIBIRA_VERSION:-}
	if [ -z "$version" ]; then
		version=$(latest_version)
	fi
	case "$version" in v*) ;; *) version="v$version" ;; esac
	archive="hibira_${version#v}_${os}_${arch}.tar.gz"
	base="https://github.com/$REPO/releases/download/$version"

	tmp=$(mktemp -d 2>/dev/null || mktemp -d -t hibira)
	trap 'rm -rf "$tmp"' EXIT INT TERM

	say "Downloading hibira $version for $os/$arch..."
	fetch "$base/$archive" "$tmp/$archive" || err "download failed: $base/$archive"
	fetch "$base/checksums.txt" "$tmp/checksums.txt" || err "download failed: $base/checksums.txt"

	want=$(awk -v f="$archive" '$2 == f { print $1 }' "$tmp/checksums.txt")
	[ -n "$want" ] || err "$archive is not listed in checksums.txt"
	got=$(sha256 "$tmp/$archive")
	[ "$want" = "$got" ] || err "checksum mismatch for $archive (expected $want, got $got)"

	tar -xzf "$tmp/$archive" -C "$tmp" hibira || err "could not unpack $archive"

	dir=$(pick_dir)
	mkdir -p "$dir" || err "could not create $dir (set HIBIRA_INSTALL_DIR to a folder you can write)"
	if ! { cp "$tmp/hibira" "$dir/hibira.tmp.$$" && chmod 755 "$dir/hibira.tmp.$$" && mv -f "$dir/hibira.tmp.$$" "$dir/hibira"; } 2>/dev/null; then
		rm -f "$dir/hibira.tmp.$$" 2>/dev/null || true
		err "could not write to $dir (re-run with sudo, or set HIBIRA_INSTALL_DIR)"
	fi

	say "Installed $("$dir/hibira" version 2>/dev/null || echo "hibira $version") to $dir/hibira"
	case ":$PATH:" in
		*":$dir:"*) ;;
		*) say "" && say "$dir is not on your PATH. Add it with:" && say "  export PATH=\"$dir:\$PATH\"" ;;
	esac
	say ""
	say "Next: hibira config set --app-id <id> --key <key>   (then run: hibira)"
}

main "$@"
