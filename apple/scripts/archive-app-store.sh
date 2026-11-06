#!/bin/sh
# Archives the iPhone/iPad and Apple TV apps for the App Store. Only these builds set
# MARQUEE_APP_STORE, which leaves out Live TV, VOD and Discover at compile time; Xcode builds,
# the GitHub release files and the Mac app keep them. The apps' version comes from project.yml.
#
#   apple/scripts/archive-app-store.sh             signed archives, checked
#   apple/scripts/archive-app-store.sh --upload    and sends them to App Store Connect
#   apple/scripts/archive-app-store.sh --unsigned  no signing, to check what's in a build
#
# They're written to apple/build/app-store, or $MARQUEE_APP_STORE_DIR.
set -eu

MODE="${1:-}"
case "$MODE" in
  ""|--upload|--unsigned) ;;
  *) echo "usage: $0 [--upload|--unsigned]" >&2; exit 2 ;;
esac

TEAM="64L6B86X7S"
APPLE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${MARQUEE_APP_STORE_DIR:-$APPLE_DIR/build/app-store}"

cd "$APPLE_DIR"
xcodegen generate >/dev/null
rm -rf "$OUT"
mkdir -p "$OUT"

# archive <scheme> <platform>
archive() {
  ARCHIVE="$OUT/$1.xcarchive"
  echo "Archiving $1..."
  if [ "$MODE" = "--unsigned" ]; then
    set -- "$1" "$2" CODE_SIGNING_ALLOWED=NO
  else
    set -- "$1" "$2" -allowProvisioningUpdates "DEVELOPMENT_TEAM=$TEAM"
  fi
  SCHEME="$1" PLATFORM="$2"
  shift 2
  xcodebuild archive -quiet -project Marquee.xcodeproj -scheme "$SCHEME" -configuration Release \
    -destination "generic/platform=$PLATFORM" -archivePath "$ARCHIVE" \
    'SWIFT_ACTIVE_COMPILATION_CONDITIONS=$(inherited) MARQUEE_APP_STORE' "$@"

  # What's left out must really be gone: the words on its buttons aren't in the app.
  BINARY="$ARCHIVE/Products/Applications/Marquee.app/Marquee"
  [ -f "$BINARY" ] || { echo "error: no app at $BINARY" >&2; exit 1; }
  for words in "VOD Movies" "Live TV" "Watch now on VOD" "Guide, channels and recordings"; do
    if strings -a "$BINARY" | grep -qF "$words"; then
      echo "error: \"$words\" is still in $BINARY" >&2
      exit 1
    fi
  done
  echo "$SCHEME: no Live TV, VOD or Discover."

  if [ "$MODE" = "--upload" ]; then
    xcodebuild -exportArchive -quiet -allowProvisioningUpdates -archivePath "$ARCHIVE" \
      -exportPath "$OUT/$SCHEME-upload" -exportOptionsPlist "$OUT/upload.plist"
    echo "$SCHEME: uploaded."
  fi
}

cat > "$OUT/upload.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>method</key><string>app-store-connect</string>
  <key>destination</key><string>upload</string>
  <key>teamID</key><string>$TEAM</string>
  <key>signingStyle</key><string>automatic</string>
</dict>
</plist>
EOF

archive Marquee "iOS"
archive MarqueeTV "tvOS"
