#!/bin/sh
# Install the latest skool-pp-cli and skool-pp-mcp from GitHub releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/zm2231/skool-cli/main/install.sh | sh
# Set SKOOL_INSTALL_DIR to install somewhere other than ~/.local/bin.
set -eu

repo="zm2231/skool-cli"
dir="${SKOOL_INSTALL_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported OS $(uname -s). On Windows, download the zip from https://github.com/$repo/releases/latest" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "Unsupported CPU $(uname -m)" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
[ -n "$tag" ] || { echo "Could not find the latest release of $repo" >&2; exit 1; }
version="${tag#v}"
archive="skool-pp-cli_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL -o "$tmp/$archive" "$base/$archive"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt"

expected=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d' ' -f1)
if command -v sha256sum >/dev/null 2>&1; then
  actual=$(sha256sum "$tmp/$archive" | cut -d' ' -f1)
else
  actual=$(shasum -a 256 "$tmp/$archive" | cut -d' ' -f1)
fi
[ -n "$expected" ] && [ "$expected" = "$actual" ] || { echo "Checksum mismatch for $archive" >&2; exit 1; }

tar -xzf "$tmp/$archive" -C "$tmp"
mkdir -p "$dir"
for bin in skool-pp-cli skool-pp-mcp; do
  install -m 0755 "$tmp/$bin" "$dir/$bin"
  [ "$os" = darwin ] && xattr -d com.apple.quarantine "$dir/$bin" 2>/dev/null || true
done

echo "Installed skool-pp-cli $version to $dir"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "Add $dir to your PATH, for example: echo 'export PATH=\"$dir:\$PATH\"' >> ~/.zshrc" ;;
esac
echo "Next: skool-pp-cli auth login --chrome"
