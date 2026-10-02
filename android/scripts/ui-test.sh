#!/bin/sh
# Runs the UI tests on the connected emulator or device and pulls the screenshots.
# Usage: scripts/ui-test.sh [server] [test class or method] [out dir]
#   server defaults to the development server on the host (10.0.2.2:32597).
set -e
cd "$(dirname "$0")/.."
ADB="${ANDROID_HOME:-/opt/homebrew/share/android-commandlinetools}/platform-tools/adb"
SERVER="${1:-10.0.2.2:32597}"
CLASS="${2:-app.marquee.MarqueeUiTest}"
OUT="${3:-build/ui-shots}"
./gradlew -q :app:installDebug :app:installDebugAndroidTest
"$ADB" shell rm -rf /sdcard/Android/data/app.marquee/files/shots
"$ADB" shell am instrument -w -e server "$SERVER" -e class "$CLASS" app.marquee.test/androidx.test.runner.AndroidJUnitRunner | tee build/ui-test.log
rm -rf "$OUT" && mkdir -p "$OUT"
"$ADB" pull /sdcard/Android/data/app.marquee/files/shots/. "$OUT" >/dev/null 2>&1 || true
grep -q "^OK (" build/ui-test.log
