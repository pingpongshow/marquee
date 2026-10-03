#!/bin/sh
# Adds the fixtures the UI tests need to a local test library, then rescans it:
# an album with very long title and artist names and real cover art (Now Playing layout and
# artwork loading). Safe to run again.
#
#   scripts/make-ui-test-fixtures.sh <music library folder> <server> <admin token> <music library id>
set -eu
music="$1"; server="$2"; token="$3"; library="$4"
artist="Stan Getz,João Gilberto,Astrud Gilberto,Antonio Carlos Jobim"
album="$music/$artist/Getz-Gilberto (1964)"
track="$album/05 - Corcovado (Quiet Nights Of Quiet Stars).mp3"
mkdir -p "$album"
if [ ! -f "$track" ]; then
  ffmpeg -loglevel error -f lavfi -i "sine=frequency=330:duration=40" -f lavfi -i "sine=frequency=495:duration=40" \
    -filter_complex "amix=inputs=2,volume=0.3" -c:a libmp3lame -b:a 128k \
    -metadata title="Corcovado (Quiet Nights Of Quiet Stars)" -metadata artist="$artist" \
    -metadata album_artist="$artist" -metadata album="Getz/Gilberto" -metadata date=1964 \
    -metadata track=5 -metadata genre="Bossa Nova" "$track"
fi
if [ ! -f "$album/cover.jpg" ]; then
  ffmpeg -loglevel error -f lavfi -i "color=c=0xd9c9a0:s=600x600" \
    -vf "drawbox=x=60:y=60:w=480:h=480:color=0x1f6f8b@1:t=fill,drawbox=x=160:y=160:w=280:h=280:color=0xe0a030@1:t=fill" \
    -frames:v 1 "$album/cover.jpg"
fi
curl -fsS -X POST -H "Authorization: Bearer $token" "http://$server/api/v1/libraries/$library/scan" >/dev/null
echo "fixtures ready in $music"
