#!/bin/sh
# Fetches Google's Cast iOS sender SDK (Chromecast, D82) into Vendor/, from Google's own
# download (developers.google.com/cast/docs/ios_sender), checked against a pinned SHA-256.
# Vendor/ isn't committed; run this once before building the iPhone/iPad app.
set -e
cd "$(dirname "$0")/.."
VERSION=4.8.6
SHA256=55f6c21291a1315c68063f07e7d76225564bff70f2fd38caad135c71d66eb310
[ -d Vendor/GoogleCast.xcframework ] && [ "$(cat Vendor/.cast-version 2>/dev/null)" = "$VERSION" ] && exit 0
mkdir -p Vendor
tmp=$(mktemp -d)
curl -sSL -o "$tmp/cast.zip" "https://dl.google.com/dl/chromecast/sdk/ios/GoogleCastSDK-ios-${VERSION}_dynamic.zip"
echo "$SHA256  $tmp/cast.zip" | shasum -a 256 -c - >/dev/null
unzip -q "$tmp/cast.zip" -d "$tmp"
rm -rf Vendor/GoogleCast.xcframework
mv "$tmp"/GoogleCastSDK-ios-*/GoogleCast.xcframework Vendor/
echo "$VERSION" > Vendor/.cast-version
rm -rf "$tmp"
echo "Google Cast SDK $VERSION in Vendor/"
