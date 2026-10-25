#!/bin/sh
# Runs a command with the local e2e server (port 32597), the fake IPTV source (32598), the
# fake Bazarr (32767) and the fake VOD sources (films.m3u on 32621, Xtream on 32622) up, then
# stops them. Run scripts/e2e/setup.sh once first.
#
#   scripts/e2e/with-server.sh '<command>'
. "$(dirname "$0")/env.sh"

if [ ! -x "$E/marquee" ] || [ ! -f "$E/config/marquee.db" ]; then
  echo "e2e: no environment in $E; run scripts/e2e/setup.sh first" >&2
  exit 1
fi
trap 'e2e_stop' EXIT INT TERM
e2e_start_server || exit 1
e2e_start_fakes
sh -c "$1"
