#!/usr/bin/env bash
# Builds web/ (the React dashboard) and copies the output into
# internal/webui/dist, where go:embed picks it up for the token-advisor binary.
# Run this before `go build` when you want the real dashboard: git ignores
# the output, and a binary built without it serves a placeholder page.
# Release builds run it through .goreleaser.yaml.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

npm --prefix web ci
npm --prefix web run build

# Keep the tracked .gitkeep, which lets go:embed compile without a build.
find internal/webui/dist -mindepth 1 ! -name .gitkeep -delete
cp -r web/dist/. internal/webui/dist/

echo "copied web/dist -> internal/webui/dist"
