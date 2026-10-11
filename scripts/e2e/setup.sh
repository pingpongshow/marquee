#!/bin/sh
# Builds the local end-to-end test environment in <repo>/.e2e: synthetic test media, the
# server binary, and a configured server (users, libraries, Live TV, Bazarr) with the item ids
# the UI tests expect. See scripts/e2e/README.md.
#
#   scripts/e2e/setup.sh           keep an existing, working environment (rebuilds the binary)
#   scripts/e2e/setup.sh --clean   delete .e2e and build it again from scratch
set -eu
. "$(dirname "$0")/env.sh"

for tool in go ffmpeg ffprobe python3 curl; do
  command -v $tool >/dev/null || { echo "e2e: $tool is needed" >&2; exit 1; }
done
if curl -s -m 1 -o /dev/null "$BASE/system/info"; then
  echo "e2e: a server is already running on port $PORT; stop it first" >&2
  exit 1
fi
[ -f "$REPO/server/internal/web/dist/index.html" ] || \
  echo "e2e: warning: the web client isn't built (cd web && npm run build); the API still works" >&2

mkdir -p "$E"
echo "e2e: building the server"
(cd "$REPO/server" && go build -o "$E/marquee.new" ./cmd/marquee)
mv "$E/marquee.new" "$E/marquee"

trap 'e2e_stop' EXIT INT TERM

if [ "${1:-}" != "--clean" ] && [ -f "$E/.complete" ]; then
  e2e_start_server
  if python3 "$REPO/scripts/e2e/provision.py" check "$E"; then
    echo "e2e: $E is already set up"
    exit 0
  fi
  echo "e2e: the existing environment doesn't check out; rebuilding it"
  e2e_stop
fi

# Fresh start: everything except the binary just built.
find "$E" -mindepth 1 -maxdepth 1 ! -name marquee -exec rm -rf {} +
echo "e2e: generating test media"
python3 "$REPO/scripts/e2e/provision.py" media "$E"
mkdir -p "$E/media"
e2e_start_server
e2e_start_fakes
echo "e2e: configuring the server"
python3 "$REPO/scripts/e2e/provision.py" provision "$E"
touch "$E/.complete"
echo "e2e: ready. Admin token in $E/token, Kiddo's in $E/kid.token."
echo "e2e: run things against it with scripts/e2e/with-server.sh '<command>'"
