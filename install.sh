#!/usr/bin/env bash
# Downloads the latest fad-qa release for this Mac and installs the
# `fad-qa` binary into the current directory. No Go, no repo checkout.
#
#   curl -fsSL https://raw.githubusercontent.com/NabilMouzouna/fadQA/main/install.sh | bash
#
# For Windows, see README.md for the direct curl download command.
set -euo pipefail

REPO="NabilMouzouna/fadQA"

os="$(uname -s)"
arch="$(uname -m)"

if [ "$os" != "Darwin" ]; then
	echo "This installer is for macOS only. For Windows, see the README.md" >&2
	echo "in the repo for a direct curl download command." >&2
	exit 1
fi

case "$arch" in
	arm64) label="macos-arm64" ;;
	x86_64) label="macos-intel" ;;
	*)
		echo "Unsupported architecture: $arch" >&2
		exit 1
		;;
esac

url="https://github.com/${REPO}/releases/latest/download/fad-qa-${label}.zip"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading fad-qa (${label})..."
curl -fsSL "$url" -o "$tmp/fad-qa.zip"

unzip -q "$tmp/fad-qa.zip" -d "$tmp"
mv "$tmp/fad-qa-${label}/fad-qa" ./fad-qa
chmod +x ./fad-qa

# curl downloads aren't quarantined the way browser downloads are, but
# clear the flag defensively in case it ever ends up set anyway.
xattr -d com.apple.quarantine ./fad-qa 2>/dev/null || true

echo
echo "Installed ./fad-qa"
echo "Run it with: ./fad-qa --store https://example.myshopify.com --app realfoot"
