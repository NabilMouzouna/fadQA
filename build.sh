#!/usr/bin/env bash
# Builds fad-qa for macOS and Windows (Intel + Apple Silicon / ARM64) and
# packages each as a zip containing just the binary and the README — no Go
# toolchain or repo checkout required by whoever receives it. Output goes
# to dist/, which is gitignored.
set -euo pipefail
cd "$(dirname "$0")"

DIST=dist
rm -rf "$DIST"
mkdir -p "$DIST"

build() {
	local goos=$1 goarch=$2 ext=$3 label=$4
	local pkgdir="$DIST/fad-qa-${label}"

	echo "Building ${label}..."
	mkdir -p "$pkgdir"
	GOOS="$goos" GOARCH="$goarch" go build -o "$pkgdir/fad-qa${ext}" .
	cp README.md "$pkgdir/"

	(cd "$DIST" && zip -rq "fad-qa-${label}.zip" "fad-qa-${label}")
	rm -rf "$pkgdir"
}

build darwin arm64 "" "macos-arm64"
build darwin amd64 "" "macos-intel"
build windows amd64 ".exe" "windows-amd64"
build windows arm64 ".exe" "windows-arm64"

echo
echo "Done. Packages in $DIST/:"
ls -1 "$DIST"
