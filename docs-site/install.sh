#!/bin/sh
# tizen-cert installer for macOS and Linux.
# Usage: curl -fsSL https://tizen-cert-cli.vercel.app/install.sh | sh
# Options via env: INSTALL_DIR=/some/bin VERSION=v1.0.0
set -eu

REPO="gifflet/tizen-cert-cli"
VERSION="${VERSION:-latest}"

case "$(uname -s)" in
  Darwin) os="darwin" ;;
  Linux)  os="linux" ;;
  *) echo "error: unsupported OS $(uname -s) - download manually from https://github.com/$REPO/releases" >&2; exit 1 ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "error: unsupported architecture $(uname -m)" >&2; exit 1 ;;
esac

asset="tizen-cert-${os}-${arch}"
if [ "$VERSION" = "latest" ]; then
  url="https://github.com/$REPO/releases/latest/download/$asset"
else
  url="https://github.com/$REPO/releases/download/$VERSION/$asset"
fi

# Pick the install dir: an explicit INSTALL_DIR wins; otherwise use
# /usr/local/bin when writable without sudo, else ~/.local/bin.
if [ -n "${INSTALL_DIR:-}" ]; then
  dir="$INSTALL_DIR"
elif [ -w /usr/local/bin ]; then
  dir="/usr/local/bin"
else
  dir="$HOME/.local/bin"
fi
mkdir -p "$dir"

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT

echo "Downloading $asset ($VERSION)..."
curl -fSL --progress-bar -o "$tmp" "$url"
chmod +x "$tmp"
mv "$tmp" "$dir/tizen-cert"
trap - EXIT

echo "Installed: $dir/tizen-cert ($("$dir/tizen-cert" --version))"

case ":$PATH:" in
  *":$dir:"*) ;;
  *)
    echo ""
    echo "note: $dir is not on your PATH. Add this to your shell profile:"
    echo "  export PATH=\"\$PATH:$dir\""
    ;;
esac
