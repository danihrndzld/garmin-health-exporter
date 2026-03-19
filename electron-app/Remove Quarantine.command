#!/bin/bash
#
# Remove Quarantine.command
# Double-click this after moving Garmin Data Exporter.app to /Applications.
# It removes the macOS quarantine flag so the app opens without a security warning.
#

APP="/Applications/Garmin Data Exporter.app"

if [ ! -d "$APP" ]; then
  echo ""
  echo "  ERROR: '$APP' not found."
  echo "  Move the app to /Applications first, then run this script."
  echo ""
  read -p "  Press Enter to close..."
  exit 1
fi

echo ""
echo "  Removing quarantine from: $APP"
xattr -rd com.apple.quarantine "$APP"

if [ $? -eq 0 ]; then
  echo "  Done. You can now open Garmin Data Exporter normally."
else
  echo "  Something went wrong. Try running in Terminal:"
  echo "  xattr -rd com.apple.quarantine \"$APP\""
fi

echo ""
read -p "  Press Enter to close..."
