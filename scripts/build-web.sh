#!/usr/bin/env bash
# Builds web/ (the React dashboard) and copies the output into
# internal/webui/dist, where go:embed picks it up for the ccotel binary.
# Run this before `go build`/`go install`/a release build whenever web/
# has changed — internal/webui/dist otherwise still holds the placeholder
# checked into git.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

npm --prefix web ci
npm --prefix web run build

rm -rf internal/webui/dist
mkdir -p internal/webui/dist
cp -r web/dist/. internal/webui/dist/

echo "copied web/dist -> internal/webui/dist"
