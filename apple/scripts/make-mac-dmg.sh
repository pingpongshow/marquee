#!/bin/sh
# Builds the Mac app for distribution outside the App Store: archives MarqueeMac (universal, Developer
# ID, hardened runtime), wraps it in Marquee-macOS-<version>.dmg, notarises it and staples the ticket.
#
#   apple/scripts/make-mac-dmg.sh <version>
#
# Notarising uses the keychain profile `marquee-notary` (xcrun notarytool store-credentials). Without
# it the signed DMG is still built, and this says so plainly with what to run. It's written to
# apple/build/mac, or $MARQUEE_DMG_DIR.
set -eu

VERSION="${1:-}"
if [ -z "$VERSION" ]; then
  echo "usage: $0 <version>" >&2
  exit 2
fi

IDENTITY="Developer ID Application: Stephen Packer (64L6B86X7S)"
TEAM="64L6B86X7S"
PROFILE="marquee-notary"
APPLE_DIR="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${MARQUEE_DMG_DIR:-$APPLE_DIR/build/mac}"
DMG="$OUT/Marquee-macOS-$VERSION.dmg"
ARCHIVE="$OUT/MarqueeMac.xcarchive"
STAGE="$OUT/dmg"

cd "$APPLE_DIR"
xcodegen generate >/dev/null

# The app's own version comes from project.yml, as for the other apps: the name of the DMG should
# agree with it.
BUILT_VERSION="$(xcodebuild -project Marquee.xcodeproj -scheme MarqueeMac -showBuildSettings 2>/dev/null | awk -F' = ' '/ MARKETING_VERSION = /{print $2; exit}')"
if [ "$BUILT_VERSION" != "$VERSION" ]; then
  echo "note: project.yml's MARKETING_VERSION is $BUILT_VERSION; the DMG is named $VERSION." >&2
fi

mkdir -p "$OUT"
rm -rf "$ARCHIVE" "$STAGE" "$DMG"

echo "== Archiving (arm64 and x86_64)"
xcodebuild archive \
  -project Marquee.xcodeproj \
  -scheme MarqueeMac \
  -configuration Release \
  -destination 'generic/platform=macOS' \
  -archivePath "$ARCHIVE" \
  -derivedDataPath "$OUT/DerivedData" \
  ARCHS="arm64 x86_64" ONLY_ACTIVE_ARCH=NO \
  DEVELOPMENT_TEAM="$TEAM" \
  CODE_SIGN_STYLE=Manual \
  CODE_SIGN_IDENTITY="$IDENTITY" \
  PROVISIONING_PROFILE_SPECIFIER= \
  ENABLE_HARDENED_RUNTIME=YES \
  CODE_SIGN_INJECT_BASE_ENTITLEMENTS=NO \
  OTHER_CODE_SIGN_FLAGS="--timestamp" \
  -quiet

APP="$ARCHIVE/Products/Applications/Marquee.app"
[ -d "$APP" ] || { echo "error: no app in the archive" >&2; exit 1; }

echo "== Checking the signature"
codesign --verify --deep --strict --verbose=1 "$APP"
codesign -dv "$APP" 2>&1 | grep -E 'Authority=Developer ID Application|flags=|Timestamp=' || true
codesign -d --entitlements - "$APP" 2>/dev/null || true
lipo -info "$APP/Contents/MacOS/Marquee"

echo "== Making the disk image"
mkdir -p "$STAGE"
ditto "$APP" "$STAGE/Marquee.app"
ln -s /Applications "$STAGE/Applications"
hdiutil create -quiet -volname "Marquee" -srcfolder "$STAGE" -fs HFS+ -format UDZO -ov "$DMG"
rm -rf "$STAGE"
codesign --force --sign "$IDENTITY" --timestamp "$DMG"
codesign --verify --verbose=1 "$DMG"

if PROFILE_CHECK="$(xcrun notarytool history --keychain-profile "$PROFILE" 2>&1)"; then
  echo "== Notarising (this waits for Apple)"
  LOG="$OUT/notarytool.log"
  xcrun notarytool submit "$DMG" --keychain-profile "$PROFILE" --wait | tee "$LOG"
  if grep -q 'status: Accepted' "$LOG"; then
    xcrun stapler staple "$DMG"
    xcrun stapler validate "$DMG"
    spctl -a -t open --context context:primary-signature -v "$DMG" || true
    echo "== Done, notarised and stapled: $DMG"
  else
    echo "error: Apple didn't accept it. See: xcrun notarytool log <id> --keychain-profile $PROFILE" >&2
    exit 1
  fi
else
  echo
  # The check also fails with the profile there: no network, or Apple's service down.
  case "$PROFILE_CHECK" in
    *"No Keychain password item found"*)
      echo "== NOTARISING WAS SKIPPED: the keychain profile '$PROFILE' isn't there."
      ;;
    *)
      echo "== NOTARISING WAS SKIPPED: couldn't check the keychain profile '$PROFILE' with Apple."
      echo "   notarytool said: $PROFILE_CHECK"
      ;;
  esac
  echo "   The DMG is built and signed, but Gatekeeper will warn on other Macs until it's notarised:"
  echo "   $DMG"
  echo "   To finish, run:"
  case "$PROFILE_CHECK" in
    *"No Keychain password item found"*)
      echo "     xcrun notarytool store-credentials $PROFILE --apple-id <your Apple ID> --team-id $TEAM"
      ;;
  esac
  echo "     xcrun notarytool submit \"$DMG\" --keychain-profile $PROFILE --wait"
  echo "     xcrun stapler staple \"$DMG\""
fi
