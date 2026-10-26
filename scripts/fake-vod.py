#!/usr/bin/env python3
"""Fake on-demand sources for developing and testing VOD (LIVE-6), as the Apple and Android UI
tests expect them:

  --m3u-port (default 32621)     /films.m3u: three films as an M3U playlist ("Apple Test Alpha (1985)",
                                 Bravo, Charlie), each a minute long; /android.m3u: the Android
                                 tests' three ("Droid Alpha Film (1985)", Beta (1990), Gamma (1995))
  --xtream-port (default 32622)  an Xtream Codes server, Dispatcharr-style (user "apple", password
                                 "apple-test"): the films "Apple XC Film One" (1994), "Apple XC Film
                                 Two" (2003) and "10 Xenon" (2014, which the e2e Movies library has
                                 too); the series "Apple Test Show" (Drama, 2010; seasons 1-2, five
                                 12-second episodes) and "Apple Other Show" (Comedy, 1985)

The videos are made with FFmpeg into --media (default .e2e/vod-media) the first time, and served
with byte ranges (AVPlayer needs them).

Usage: scripts/fake-vod.py [--m3u-port N] [--xtream-port N] [--media DIR]
"""
import argparse
import http.server
import json
import os
import re
import socketserver
import subprocess
import sys
import threading
import urllib.parse

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
USER, PASSWORD = "apple", "apple-test"
ADDED = "1700000000"

M3U_FILMS = [  # file, title, group
    ("alpha", "Apple Test Alpha (1985)", "Apple Films"),
    ("bravo", "Apple Test Bravo (1999)", "Apple Films"),
    ("charlie", "Apple Test Charlie (2012)", "Apple Films"),
]
ANDROID_FILMS = [  # file, title, group
    ("droid-alpha", "Droid Alpha Film (1985)", "Droid Films"),
    ("droid-beta", "Droid Beta Film (1990)", "Droid Films"),
    ("droid-gamma", "Droid Gamma Film (1995)", "Droid Films"),
]
XC_FILMS = [  # stream id, title, year, category id, genre, minutes
    (9101, "Apple XC Film One", 1994, "1", "Drama", 1),
    (9102, "Apple XC Film Two", 2003, "2", "Comedy", 1),
    (9103, "10 Xenon", 2014, "1", "Drama", 1),
]
XC_FILM_CATEGORIES = [("1", "Apple XC Drama"), ("2", "Apple XC Comedy")]
XC_SERIES = [  # series id, title, year, category id, genre, {season: episode count}
    (9201, "Apple Test Show", 2010, "11", "Drama", {1: 3, 2: 2}),
    (9202, "Apple Other Show", 1985, "12", "Comedy", {1: 1}),
]
XC_SERIES_CATEGORIES = [("11", "Apple XC Drama Series"), ("12", "Apple XC Comedy Series")]
EPISODE_NAMES = ["First", "Second", "Third", "Fourth", "Fifth", "Sixth"]  # through the series


def episode_id(series, season, n):
    return series * 100 + season * 10 + n


def make_media(media):
    """Writes the test videos (H.264 + AAC MP4) unless they're there."""
    os.makedirs(media, exist_ok=True)
    jobs = [(f"{name}.mp4", 60, i) for i, (name, _, _) in enumerate(M3U_FILMS + ANDROID_FILMS)]
    jobs += [(f"{sid}.mp4", 60 * minutes, i + 3) for i, (sid, _, _, _, _, minutes) in enumerate(XC_FILMS)]
    for sid, _, _, _, _, seasons in XC_SERIES:
        for season, count in seasons.items():
            for n in range(1, count + 1):
                jobs.append((f"{episode_id(sid, season, n)}.mp4", 12, season * 10 + n))
    for name, seconds, i in jobs:
        path = os.path.join(media, name)
        if os.path.exists(path):
            continue
        hue = (i * 47) % 360
        subprocess.run(["ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", f"testsrc2=size=640x360:rate=25:duration={seconds},hue=h={hue}",
                        "-f", "lavfi", "-i", f"sine=frequency={220 + 20 * (i % 10)}:duration={seconds}",
                        "-c:v", "libx264", "-pix_fmt", "yuv420p", "-preset", "veryfast", "-c:a", "aac", "-shortest",
                        "-movflags", "+faststart", path], check=True)


class Handler(http.server.BaseHTTPRequestHandler):
    media = ""

    def log_message(self, fmt, *args):
        sys.stderr.write("%s %s\n" % (self.server.server_address[1], fmt % args))

    def send(self, code, body, kind="application/json"):
        data = body.encode() if isinstance(body, str) else body
        self.send_response(code)
        self.send_header("Content-Type", kind)
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        if self.command != "HEAD":
            self.wfile.write(data)

    def send_media(self, name):
        path = os.path.join(self.media, os.path.basename(name))
        if not os.path.isfile(path):
            return self.send(404, "not found", "text/plain")
        size = os.path.getsize(path)
        start, end = 0, size - 1
        m = re.match(r"bytes=(\d*)-(\d*)$", self.headers.get("Range", ""))
        if m and (m.group(1) or m.group(2)):
            if m.group(1):
                start = int(m.group(1))
                end = min(int(m.group(2)), size - 1) if m.group(2) else size - 1
            else:
                start = max(0, size - int(m.group(2)))
            if start > end:
                self.send_response(416)
                self.send_header("Content-Range", f"bytes */{size}")
                self.end_headers()
                return
            self.send_response(206)
            self.send_header("Content-Range", f"bytes {start}-{end}/{size}")
        else:
            self.send_response(200)
        self.send_header("Content-Type", "video/mp4")
        self.send_header("Accept-Ranges", "bytes")
        self.send_header("Content-Length", str(end - start + 1))
        self.end_headers()
        if self.command == "HEAD":
            return
        with open(path, "rb") as f:
            f.seek(start)
            left = end - start + 1
            while left > 0:
                chunk = f.read(min(1 << 16, left))
                if not chunk:
                    break
                try:
                    self.wfile.write(chunk)
                except (BrokenPipeError, ConnectionResetError):
                    return
                left -= len(chunk)

    do_HEAD = lambda self: self.do_GET()


class M3UHandler(Handler):
    def do_GET(self):
        path = urllib.parse.urlparse(self.path).path
        host = self.headers.get("Host", f"127.0.0.1:{self.server.server_address[1]}")
        if path in ("/films.m3u", "/android.m3u"):
            lines = ["#EXTM3U"]
            for name, title, group in (M3U_FILMS if path == "/films.m3u" else ANDROID_FILMS):
                lines.append(f'#EXTINF:-1 group-title="{group}",{title}')
                lines.append(f"http://{host}/films/{name}.mp4")
            return self.send(200, "\n".join(lines) + "\n", "audio/x-mpegurl")
        if path.startswith("/films/"):
            return self.send_media(path[len("/films/"):])
        self.send(404, "not found", "text/plain")


class XtreamHandler(Handler):
    def do_GET(self):
        url = urllib.parse.urlparse(self.path)
        parts = url.path.strip("/").split("/")
        # Streams: /movie/<user>/<password>/<id>.<ext>, /series/<user>/<password>/<id>.<ext>
        if len(parts) == 4 and parts[0] in ("movie", "series"):
            if parts[1] != USER or parts[2] != PASSWORD:
                return self.send(403, "forbidden", "text/plain")
            return self.send_media(parts[3].split(".")[0] + ".mp4")
        if url.path != "/player_api.php":
            return self.send(404, "not found", "text/plain")
        q = dict(urllib.parse.parse_qsl(url.query))
        if q.get("username") != USER or q.get("password") != PASSWORD:
            return self.send(200, json.dumps({"user_info": {"auth": 0}}))
        action = q.get("action", "")
        self.send(200, json.dumps(self.answer(action, q)))

    def answer(self, action, q):
        if action == "":
            return {"user_info": {"username": USER, "auth": 1, "status": "Active", "exp_date": "1893456000", "max_connections": "2"},
                    "server_info": {"url": "127.0.0.1", "port": str(self.server.server_address[1])}}
        if action == "get_vod_categories":
            return [{"category_id": cid, "category_name": name, "parent_id": 0} for cid, name in XC_FILM_CATEGORIES]
        if action == "get_vod_streams":
            return [{"num": i + 1, "name": title, "stream_type": "movie", "stream_id": sid, "stream_icon": "", "rating": "7.0",
                     "added": ADDED, "category_id": cid, "container_extension": "mp4", "year": str(year), "tmdb": ""}
                    for i, (sid, title, year, cid, _, _) in enumerate(XC_FILMS)]
        if action == "get_vod_info":
            for sid, title, year, cid, genre, minutes in XC_FILMS:
                if str(sid) == q.get("vod_id"):
                    return {"info": {"name": title, "plot": f"{title}, a test film.", "genre": genre, "releasedate": f"{year}-06-01",
                                     "duration_secs": str(60 * minutes), "duration": f"00:{minutes:02d}:00", "cast": "Test Actor",
                                     "director": "Test Director", "tmdb_id": ""},
                            "movie_data": {"stream_id": sid, "name": title, "category_id": cid, "container_extension": "mp4"}}
            return {"info": [], "movie_data": {}}
        if action == "get_series_categories":
            return [{"category_id": cid, "category_name": name, "parent_id": 0} for cid, name in XC_SERIES_CATEGORIES]
        if action == "get_series":
            return [{"num": i + 1, "name": title, "series_id": sid, "cover": "", "plot": f"{title}, a test series.", "cast": "",
                     "director": "", "genre": genre, "releaseDate": f"{year}-01-01", "rating": "7.5", "category_id": cid,
                     "last_modified": ADDED, "tmdb": ""}
                    for i, (sid, title, year, cid, genre, _) in enumerate(XC_SERIES)]
        if action == "get_series_info":
            for sid, title, year, _, _, seasons in XC_SERIES:
                if str(sid) == q.get("series_id"):
                    episodes = {}
                    names = iter(EPISODE_NAMES)
                    for season, count in sorted(seasons.items()):
                        episodes[str(season)] = [{"id": str(episode_id(sid, season, n)), "episode_num": str(n), "season": str(season),
                                                  "title": f"{title} - S{season:02d}E{n:02d} - {next(names)}", "container_extension": "mp4",
                                                  "info": {"duration_secs": "12", "plot": f"Season {season}, episode {n}.",
                                                           "releasedate": f"{year + season - 1}-0{n}-01"}}
                                                 for n in range(1, count + 1)]
                    return {"seasons": [{"season_number": str(s), "name": f"Season {s}", "episode_count": str(c)} for s, c in seasons.items()],
                            "info": {"name": title, "plot": f"{title}, a test series."}, "episodes": episodes}
            return {"seasons": [], "info": [], "episodes": []}
        return {"error": "unknown action"}


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


def main():
    p = argparse.ArgumentParser(description="Fake on-demand sources for VOD tests.")
    p.add_argument("--m3u-port", type=int, default=32621)
    p.add_argument("--xtream-port", type=int, default=32622)
    p.add_argument("--media", default=os.path.join(ROOT, ".e2e", "vod-media"))
    a = p.parse_args()
    make_media(a.media)
    Handler.media = a.media
    servers = [Server(("127.0.0.1", a.m3u_port), M3UHandler), Server(("127.0.0.1", a.xtream_port), XtreamHandler)]
    for s in servers[1:]:
        threading.Thread(target=s.serve_forever, daemon=True).start()
    print(f"fake VOD: films.m3u on {a.m3u_port}, Xtream on {a.xtream_port}, media in {a.media}", flush=True)
    servers[0].serve_forever()


if __name__ == "__main__":
    main()
