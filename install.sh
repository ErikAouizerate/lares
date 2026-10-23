#!/bin/sh
# Install lares from a GitHub release: no Go, no repository clone.
#
#   curl -fsSL https://github.com/ErikAouizerate/lares/releases/latest/download/install.sh | sh
#
# PREFIX sets where the binary goes (default ~/.local/bin), LARES_VERSION picks
# a version other than the latest one, LARES_REPO another repository.
set -eu

REPO=${LARES_REPO:-ErikAouizerate/lares}
PREFIX=${PREFIX:-$HOME/.local}
VERSION=${LARES_VERSION:-}

say() { printf 'lares: %s\n' "$*"; }
die() { printf 'lares: %s\n' "$*" >&2; exit 1; }

# fetch downloads a URL with whichever client the machine has.
fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1" -o "$2"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$2" "$1"
    else
        die "neither curl nor wget is installed"
    fi
}

# latest_version reads the tag of the latest release out of the redirect GitHub
# answers with, so that no API token and no JSON parsing is needed.
latest_version() {
    url="https://github.com/$REPO/releases/latest"

    if command -v curl >/dev/null 2>&1; then
        effective=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "$url") || die "cannot reach $url"
    else
        effective=$(wget -q -S --max-redirect=0 "$url" 2>&1 | awk '/^  Location: /{print $2}' | tr -d '\r') || die "cannot reach $url"
    fi

    printf '%s\n' "${effective##*/}"
}

[ "$(uname -s)" = Linux ] || die "lares runs on Linux only (this is $(uname -s))"

case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    aarch64|arm64) arch=arm64 ;;
    *) die "no lares binary for $(uname -m)" ;;
esac

[ -n "$VERSION" ] || VERSION=$(latest_version)
[ "$VERSION" != latest ] && [ -n "$VERSION" ] || die "no release found in $REPO"

archive="lares_${VERSION}_linux_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$VERSION"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "downloading $VERSION for linux/$arch"
fetch "$base/$archive" "$tmp/$archive" || die "cannot download $base/$archive"

# The checksums are published next to the archives. A release without them is
# installed without the check rather than refused.
if fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS" 2>/dev/null; then
    grep " $archive\$" "$tmp/SHA256SUMS" >"$tmp/check" || die "$archive is not listed in $base/SHA256SUMS"
    ( cd "$tmp" && sha256sum -c check >/dev/null 2>&1 ) || die "the checksum of $archive does not match $base/SHA256SUMS"
    say "checksum verified"
fi

tar -xzf "$tmp/$archive" -C "$tmp"
install -d -m 0755 "$PREFIX/bin"
install -m 0755 "$tmp/lares" "$PREFIX/bin/lares"
say "installed $PREFIX/bin/lares"

case ":$PATH:" in
    *":$PREFIX/bin:"*) ;;
    *) say "note: $PREFIX/bin is not in PATH, add it to use lares" ;;
esac

# The binary knows how to install its coi profile, which is what declares the
# sandbox: doing it here saves a step on a fresh machine.
"$PREFIX/bin/lares" install

cat <<'EOF'

next steps:
  lares build     build the image the sandbox is made from
  lares init      create the container, once
  lares mount     let the sandbox see a repository, from inside it
  lares pi        pi in that repository
EOF
