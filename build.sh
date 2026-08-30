#!/usr/bin/env bash
# Baut die Windows-Binaries (64 Bit). Ohne cgo, daher reicht die Go-Toolchain.
set -euo pipefail

VERSION="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="dist/windows-amd64"
LDFLAGS="-s -w -X main.version=${VERSION}"

rm -rf "$OUT"
mkdir -p "$OUT"

echo "TermiQ ${VERSION} → ${OUT}"

# -H=windowsgui unterdrückt bei der Einrichtung das schwarze Konsolenfenster;
# der Runner behält seine Konsole, damit die Aufgabenplanung Ausgaben mitschreibt.
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "${LDFLAGS} -H=windowsgui" \
  -o "$OUT/termiq-config.exe" ./cmd/termiq-config

GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
  go build -trimpath -ldflags "${LDFLAGS}" \
  -o "$OUT/termiq-run.exe" ./cmd/termiq-run

cp README.md "$OUT/LIESMICH.md"
ls -lh "$OUT"
