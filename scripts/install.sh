#!/bin/sh
set -eu

REPO="${RUNGATED_REPO:-amareshhebbar/rungated}"
VERSION="${RUNGATED_VERSION:-latest}"
DEST="${RUNGATED_INSTALL_DIR:-}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported OS: $os (download the Windows build from the releases page)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported CPU: $arch" >&2; exit 1 ;;
esac

if [ -n "${RUNGATED_BASE_URL:-}" ]; then
  base="$RUNGATED_BASE_URL"
elif [ "$VERSION" = latest ]; then
  base="https://github.com/$REPO/releases/latest/download"
else
  case "$VERSION" in v*) ;; *) VERSION="v$VERSION" ;; esac
  base="https://github.com/$REPO/releases/download/$VERSION"
fi

bin="rungated-$os-$arch"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fetch() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$1" -O "$2"
  else
    echo "need curl or wget" >&2
    exit 1
  fi
}

echo "downloading $bin"
fetch "$base/$bin" "$tmp/rungated"
fetch "$base/SHA256SUMS" "$tmp/SHA256SUMS"

want=$(grep " $bin\$" "$tmp/SHA256SUMS" | awk '{print $1}')
if [ -z "$want" ]; then
  echo "no checksum for $bin in SHA256SUMS" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  got=$(sha256sum "$tmp/rungated" | awk '{print $1}')
else
  got=$(shasum -a 256 "$tmp/rungated" | awk '{print $1}')
fi
if [ "$want" != "$got" ]; then
  echo "checksum mismatch for $bin" >&2
  exit 1
fi
chmod +x "$tmp/rungated"

if [ -z "$DEST" ]; then
  if [ -w /usr/local/bin ]; then
    DEST=/usr/local/bin
  else
    DEST="$HOME/.local/bin"
  fi
fi
mkdir -p "$DEST"
mv "$tmp/rungated" "$DEST/rungated"

echo "installed $("$DEST/rungated" version) to $DEST/rungated"
case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "add $DEST to your PATH: export PATH=\"$DEST:\$PATH\"" ;;
esac
echo
echo "next:"
echo "  rungated init --id my-agent"
echo "  RUNGATED_API_KEY=change-me rungated serve"
