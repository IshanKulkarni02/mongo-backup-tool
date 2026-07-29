#!/usr/bin/env bash
# Packages the built mongobak.app into a distributable .dmg.
# Run `wails build` first. Usage: ./build/scripts/package-dmg.sh
set -euo pipefail

cd "$(dirname "$0")/../.."

APP="build/bin/mongobak.app"
DMG="build/bin/mongobak.dmg"

if [ ! -d "$APP" ]; then
  echo "error: $APP not found — run 'wails build' first" >&2
  exit 1
fi

STAGING="$(mktemp -d)"
trap 'rm -rf "$STAGING"' EXIT

cp -R "$APP" "$STAGING/"
ln -s /Applications "$STAGING/Applications"

rm -f "$DMG"
hdiutil create -volname "mongobak" -srcfolder "$STAGING" -ov -format UDZO "$DMG"

echo "Built $DMG"
