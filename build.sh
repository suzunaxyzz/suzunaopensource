#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"

VERSION="${1:-$(grep -oP 'LauncherVersion = "\K[^"]+' main.go || echo dev)}"
OUT="dist"
LDFLAGS_GUI="-H=windowsgui -s -w -X main.LauncherVersion=${VERSION}"
LDFLAGS_CONSOLE="-s -w -X main.LauncherVersion=${VERSION}"

mkdir -p "$OUT"
echo ">> Building Suzuna Launcher v${VERSION}"

GOOS=windows GOARCH=386   go build -trimpath -ldflags "$LDFLAGS_GUI" -o "$OUT/SuzunaLauncher-windows-386.exe"   .
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS_GUI" -o "$OUT/SuzunaLauncher-windows-amd64.exe" .

GOOS=windows GOARCH=386   go build -trimpath -ldflags "$LDFLAGS_CONSOLE" -o "$OUT/SuzunaLauncher-windows-386-debug.exe" .

echo ">> Done:"
ls -lh "$OUT"/*.exe | awk '{print "   " $5 "\t" $NF}'
