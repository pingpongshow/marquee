# Shared settings for the local e2e environment (sourced by setup.sh and with-server.sh).
# Everything lives under <repo>/.e2e, which is git- and docker-ignored.

REPO="$(cd "$(dirname "$0")/../.." && pwd)"
E="$REPO/.e2e"
PORT=32597
IPTV_PORT=32598
BAZARR_PORT=32767
VOD_M3U_PORT=32621
VOD_XTREAM_PORT=32622
BASE="http://127.0.0.1:$PORT/api/v1"

# Starts the server in the background (pid in $E/server.pid) and waits until it answers.
e2e_start_server() {
  if curl -s -m 1 -o /dev/null "$BASE/system/info"; then
    echo "e2e: something is already listening on port $PORT; stop it first" >&2
    return 1
  fi
  mkdir -p "$E/config" "$E/tx" "$E/noplex" "$E/recordings"
  (
    cd "$REPO/server" || exit 1
    MARQUEE_WEB_DIR="$REPO/server/internal/web/dist" \
    MARQUEE_CONFIG_DIR="$E/config" \
    MARQUEE_TRANSCODE_DIR="$E/tx" \
    MARQUEE_PORT=$PORT \
    MARQUEE_FFMPEG="$(command -v ffmpeg)" \
    MARQUEE_FFPROBE="$(command -v ffprobe)" \
    MARQUEE_PLEX_DIR="$E/noplex" \
    MARQUEE_RECORDINGS_DIR="$E/recordings" \
    exec "$E/marquee" >> "$E/server.log" 2>&1
  ) &
  echo $! > "$E/server.pid"
  i=0
  until curl -s -m 1 -o /dev/null "$BASE/system/info"; do
    i=$((i + 1))
    if [ $i -gt 60 ] || ! kill -0 "$(cat "$E/server.pid")" 2>/dev/null; then
      echo "e2e: the server didn't start; see $E/server.log" >&2
      return 1
    fi
    sleep 0.5
  done
}

# Starts the fake IPTV source, the fake Bazarr and the fake VOD sources (pids in $E/fakes.pid).
e2e_start_fakes() {
  python3 "$REPO/scripts/fake-iptv.py" $IPTV_PORT > "$E/fake-iptv.log" 2>&1 &
  echo $! > "$E/fakes.pid"
  python3 "$REPO/scripts/fake-bazarr.py" --port $BAZARR_PORT --movies "$E/media/movies" \
    --tv "$E/recordings/TV" --anime "$E/media/anime" > "$E/fake-bazarr.log" 2>&1 &
  echo $! >> "$E/fakes.pid"
  python3 "$REPO/scripts/fake-vod.py" --m3u-port $VOD_M3U_PORT --xtream-port $VOD_XTREAM_PORT \
    --media "$E/vod-media" > "$E/fake-vod.log" 2>&1 &
  echo $! >> "$E/fakes.pid"
  i=0
  until curl -s -m 1 -o /dev/null "http://127.0.0.1:$BAZARR_PORT/" && curl -s -m 1 -o /dev/null "http://127.0.0.1:$IPTV_PORT/output/m3u"; do
    i=$((i + 1))
    [ $i -gt 40 ] && { echo "e2e: the fake IPTV source or Bazarr didn't start; see $E/fake-*.log" >&2; return 0; }
    sleep 0.25
  done
  # The fake VOD sources make their videos the first time (a few seconds).
  i=0
  until curl -s -m 1 -o /dev/null "http://127.0.0.1:$VOD_M3U_PORT/films.m3u" && curl -s -m 1 -o /dev/null "http://127.0.0.1:$VOD_XTREAM_PORT/player_api.php"; do
    i=$((i + 1))
    [ $i -gt 240 ] && { echo "e2e: the fake VOD sources didn't start; see $E/fake-vod.log" >&2; return 0; }
    sleep 0.25
  done
}

e2e_stop() {
  for f in "$E/server.pid" "$E/fakes.pid"; do
    [ -f "$f" ] || continue
    for p in $(cat "$f"); do kill "$p" 2>/dev/null || true; done
    rm -f "$f"
  done
  # The fake IPTV source runs FFmpeg per stream being watched.
  pkill -f "lavfi -i .*sine=frequency=330" 2>/dev/null || true
  i=0
  while curl -s -m 1 -o /dev/null "$BASE/system/info" && [ $i -lt 20 ]; do i=$((i + 1)); sleep 0.5; done
  return 0
}
