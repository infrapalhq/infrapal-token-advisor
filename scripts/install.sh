#!/bin/sh
# Installs the latest token-advisor release, checking its checksum.
#
#   curl -fsSL https://raw.githubusercontent.com/infrapalhq/infrapal-token-advisor/main/scripts/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/infrapalhq/infrapal-token-advisor/main/scripts/install.sh | sh
#
# INSTALL_DIR               where to put it (default: ~/.local/bin)
# DESTDIR                   a staging directory to put INSTALL_DIR under, for
#                           packaging (default: none, install in place)
# TOKEN_ADVISOR_VERSION     a version such as 1.2.3 (default: the latest)
# TOKEN_ADVISOR_BASE_URL    where to fetch from, for testing a local build
set -eu

repo=infrapalhq/infrapal-token-advisor
install_dir=${INSTALL_DIR:-$HOME/.local/bin}
dest=${DESTDIR:-}$install_dir

fail() {
	echo "install.sh: $1" >&2
	exit 1
}

# fetch FILE URL downloads URL to FILE. final_url URL prints where URL
# redirects to. Either curl or wget will do.
if command -v curl >/dev/null 2>&1; then
	fetch() { curl -fsSL -o "$1" "$2"; }
	final_url() { curl -fsSLI -o /dev/null -w '%{url_effective}' "$1"; }
elif command -v wget >/dev/null 2>&1; then
	fetch() { wget -q -O "$1" "$2"; }
	final_url() {
		wget -q -S --spider "$1" 2>&1 |
			awk 'tolower($1) == "location:" { url = $2 } END { if (url == "") exit 1; print url }' |
			tr -d '\r'
	}
else
	fail "curl or wget is required"
fi

case $(uname -s) in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) fail "unsupported system: $(uname -s)" ;;
esac

case $(uname -m) in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) fail "unsupported architecture: $(uname -m)" ;;
esac

version=${TOKEN_ADVISOR_VERSION:-}
if [ -z "$version" ]; then
	# The /latest page redirects to the newest tag, with no API rate limit.
	url=$(final_url "https://github.com/$repo/releases/latest") ||
		fail "couldn't find the latest release"
	# With no release yet, it redirects to the releases list instead.
	case $url in
	*/releases/tag/v*) ;;
	*) fail "no release found" ;;
	esac
	version=${url##*/}
	version=${version#v}
fi

base=${TOKEN_ADVISOR_BASE_URL:-https://github.com/$repo/releases/download/v$version}
archive=infrapal-token-advisor_${version}_${os}_${arch}.tar.gz

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "downloading token-advisor $version ($os/$arch)"
fetch "$tmp/$archive" "$base/$archive" || fail "couldn't download $base/$archive"
fetch "$tmp/checksums.txt" "$base/checksums.txt" || fail "couldn't download checksums.txt"

want=$(awk -v name="$archive" '$2 == name { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || fail "no checksum for $archive"
if command -v sha256sum >/dev/null 2>&1; then
	got=$(sha256sum "$tmp/$archive" | awk '{ print $1 }')
elif command -v shasum >/dev/null 2>&1; then
	got=$(shasum -a 256 "$tmp/$archive" | awk '{ print $1 }')
else
	fail "sha256sum or shasum is required to check the download"
fi
[ "$got" = "$want" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" token-advisor
mkdir -p "$dest"
# mv within a directory is atomic, so a running binary is never half written.
cp "$tmp/token-advisor" "$dest/.token-advisor.new"
chmod 755 "$dest/.token-advisor.new"
mv "$dest/.token-advisor.new" "$dest/token-advisor"

if [ -n "${DESTDIR:-}" ]; then
	echo "staged $dest/token-advisor, to be installed as $install_dir/token-advisor"
	exit 0
fi
echo "installed $install_dir/token-advisor"
case ":$PATH:" in
*":$install_dir:"*) ;;
*) echo "add $install_dir to your PATH to run it by name" ;;
esac
echo "next: run \`token-advisor init\` to start collecting"
