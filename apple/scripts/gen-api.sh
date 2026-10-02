#!/bin/sh
# Regenerates the Swift API client from api/openapi.yaml (the source of truth).
set -eu
cd "$(dirname "$0")/.."
swift run --package-path Tools swift-openapi-generator generate \
  --config scripts/openapi-generator-config.yaml \
  --output-directory MarqueeKit/Sources/MarqueeAPI/Generated \
  ../api/openapi.yaml
