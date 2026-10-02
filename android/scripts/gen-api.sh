#!/bin/sh
# Regenerates the Kotlin API client from api/openapi.yaml (the source of truth).
# Plain "number" maps to Double: the generator's BigDecimal is written as a JSON string.
set -eu
cd "$(dirname "$0")/.."
rm -rf api/src
openapi-generator generate -i ../api/openapi.yaml -g kotlin -o api \
  --global-property models,apis,supportingFiles,modelDocs=false,apiDocs=false,modelTests=false,apiTests=false \
  --type-mappings=number=kotlin.Double \
  --additional-properties=library=jvm-okhttp4,serializationLibrary=kotlinx_serialization,packageName=app.marquee.api,enumPropertyNaming=UPPERCASE,omitGradleWrapper=true,omitGradlePluginVersions=true,sourceFolder=src/main/kotlin \
  > /dev/null
# The generator's own build files and docs aren't used; the api module has its own build.
rm -rf api/build.gradle api/settings.gradle api/gradle api/gradlew api/gradlew.bat api/README.md api/docs api/.openapi-generator-ignore api/.openapi-generator
