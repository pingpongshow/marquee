#!/usr/bin/env python3
"""A fake Bazarr for developing and testing Marquee's Bazarr subtitles (META-12) and the
library health missingSubtitles check (ADM-11).

Serves the parts of Bazarr 1.x's API Marquee uses, for a few movies and episodes of the
local e2e library:
  GET   /api/system/status
  GET   /api/movies, /api/series, /api/episodes            (radarrid[], seriesid[], episodeid[])
  GET   /api/movies/wanted, /api/episodes/wanted
  PATCH /api/movies/subtitles, /api/episodes/subtitles      download the best match
  GET   /api/providers/movies, /api/providers/episodes      manual search
  POST  /api/providers/movies, /api/providers/episodes      download a search result

Like the real one it sees the media under its own mounts (/movies, /tv, /anime) and finds
Marquee's files by path tail (folder + file name). A download waits a moment, then writes a
small "<video>.<lang>.srt" next to the real file, so Marquee should attach it to the item.

Auth is the X-API-KEY header (default "fake-bazarr-key").

Usage: scripts/fake-bazarr.py [--port 32767] [--key KEY]
           [--movies DIR] [--tv DIR] [--anime DIR]   (where /movies, /tv and /anime really are)
"""
import argparse
import http.server
import json
import os
import socketserver
import threading
import time
import urllib.parse

ap = argparse.ArgumentParser()
ap.add_argument("--port", type=int, default=32767)
ap.add_argument("--key", default="fake-bazarr-key")
ap.add_argument("--movies", help="local folder Bazarr sees as /movies")
ap.add_argument("--tv", help="local folder Bazarr sees as /tv")
ap.add_argument("--anime", help="local folder Bazarr sees as /anime")
ap.add_argument("--delay", type=float, default=2.0, help="seconds a download takes")
args = ap.parse_args()

EN = {"name": "English", "code2": "en", "code3": "eng", "forced": False, "hi": False}
FR = {"name": "French", "code2": "fr", "code3": "fra", "forced": False, "hi": False}
DE = {"name": "German", "code2": "de", "code3": "deu", "forced": False, "hi": False}

MOVIES = [
    {"radarrId": 1, "title": "20 Valley", "year": "1984", "path": "/movies/20 Valley (1984)/20 Valley (1984).mp4",
     "imdbId": "tt9000001", "monitored": True, "profileId": 1, "audio_language": [EN],
     "subtitles": [dict(EN, path=None, file_size=0, embedded_track_id=None)], "missing_subtitles": [FR]},
    {"radarrId": 2, "title": "15 Thunder", "year": "1970", "path": "/movies/15 Thunder (1970)/15 Thunder (1970).mp4",
     "imdbId": "tt9000002", "monitored": True, "profileId": 1, "audio_language": [EN],
     "subtitles": [], "missing_subtitles": [EN, dict(EN, forced=True)]},
    {"radarrId": 3, "title": "31 Ocean", "year": "1997", "path": "/movies/31 Ocean (1997)/31 Ocean (1997).mp4",
     "imdbId": "tt9000003", "monitored": True, "profileId": 1, "audio_language": [EN],
     "subtitles": [], "missing_subtitles": []},
    # Not in the e2e library: Marquee must ignore it.
    {"radarrId": 4, "title": "Elsewhere", "year": "2001", "path": "/movies/Elsewhere (2001)/Elsewhere (2001).mkv",
     "imdbId": "tt9000004", "monitored": True, "profileId": 1, "audio_language": [EN],
     "subtitles": [], "missing_subtitles": [EN]},
]
SERIES = [
    {"sonarrSeriesId": 1, "title": "Test Anime", "path": "/anime/Test Anime", "tvdbId": 900001, "imdbId": "",
     "episodeFileCount": 1, "episodeMissingCount": 0, "profileId": 1, "monitored": True},
    {"sonarrSeriesId": 2, "title": "Weather Watch", "path": "/tv/Weather Watch", "tvdbId": 900002, "imdbId": "",
     "episodeFileCount": 1, "episodeMissingCount": 0, "profileId": 1, "monitored": True},
]
EPISODES = [
    {"sonarrSeriesId": 1, "sonarrEpisodeId": 11, "season": 1, "episode": 1, "title": "Episode 1",
     "path": "/anime/Test Anime/Season 01/Test Anime - S01E01.mkv", "subtitles": [], "missing_subtitles": [EN, DE]},
    {"sonarrSeriesId": 2, "sonarrEpisodeId": 21, "season": 2, "episode": 10, "title": "Episode 10",
     "path": "/tv/Weather Watch/Season 2/Weather Watch - S02E10 - Episode 10.ts", "subtitles": [], "missing_subtitles": []},
]
CANDIDATES = [
    {"provider": "opensubtitlescom", "subtitle": "fake-handle-best", "language": "en", "release_info": ["Fake.Release.1080p.WEB"],
     "score": 96, "score_without_hash": 90, "orig_score": 350, "hearing_impaired": "False", "forced": "False",
     "uploader": "fakeuser", "original_format": "False", "matches": ["title", "year"], "dont_matches": [], "url": None},
    {"provider": "podnapisi", "subtitle": "fake-handle-ok", "language": "en", "release_info": ["Other.Release.720p"],
     "score": 71, "score_without_hash": 71, "orig_score": 250, "hearing_impaired": "True", "forced": "False",
     "uploader": "", "original_format": "False", "matches": ["title"], "dont_matches": ["release_group"], "url": None},
]
LOCK = threading.Lock()
SRT = "1\n00:00:00,500 --> 00:00:02,500\nSubtitles from the fake Bazarr\n\n2\n00:00:03,000 --> 00:00:05,000\nSecond line\n"


def local(path):
    for mount, real in (("/movies/", args.movies), ("/tv/", args.tv), ("/anime/", args.anime)):
        if path.startswith(mount) and real:
            return os.path.join(real, path[len(mount):])
    return None


def save(entry, lang, forced, hi):
    """Write the subtitle next to the video and move the language from missing to have."""
    base = os.path.splitext(entry["path"])[0] + "." + lang + (".forced" if forced else ".hi" if hi else "") + ".srt"
    real = local(base)
    if real and os.path.isdir(os.path.dirname(real)):
        with open(real, "w") as f:
            f.write(SRT)
        print("wrote", real, flush=True)
    else:
        print("not writing (no local folder for)", base, flush=True)
    name = {"en": "English", "fr": "French", "de": "German"}.get(lang, lang)
    entry["subtitles"].append({"name": name, "code2": lang, "code3": "", "path": base, "forced": forced, "hi": hi, "file_size": len(SRT)})
    entry["missing_subtitles"] = [m for m in entry["missing_subtitles"] if not (m["code2"] == lang and m["forced"] == forced)]


def flag(v):
    return str(v).lower() == "true"


class Handler(http.server.BaseHTTPRequestHandler):
    def send(self, code, body=None):
        data = b"" if body is None else json.dumps(body).encode()
        self.send_response(code)
        if body is not None:
            self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def params(self):
        q = urllib.parse.parse_qs(urllib.parse.urlparse(self.path).query)
        n = int(self.headers.get("Content-Length") or 0)
        if n:  # form fields work too, as in Bazarr
            for k, v in urllib.parse.parse_qs(self.rfile.read(n).decode()).items():
                q.setdefault(k, v)
        return q

    def handle_one(self, method):
        if self.headers.get("X-API-KEY") != args.key:
            return self.send(401, {"message": "Unauthorized"})
        path = urllib.parse.urlparse(self.path).path
        q = self.params()
        one = lambda k, d="": q.get(k, [d])[0]
        ids = lambda k: {int(x) for x in q.get(k, [])}
        page = lambda rows: self.send(200, {"data": rows, "total": len(rows)})
        with LOCK:
            if method == "GET" and path == "/api/system/status":
                return self.send(200, {"data": {"bazarr_version": "1.6.0-fake", "python_version": "3"}})
            if method == "GET" and path == "/api/movies":
                want = ids("radarrid[]")
                return page([m for m in MOVIES if not want or m["radarrId"] in want])
            if method == "GET" and path == "/api/series":
                return page(SERIES)
            if method == "GET" and path == "/api/episodes":
                s, e = ids("seriesid[]"), ids("episodeid[]")
                return page([x for x in EPISODES if x["sonarrSeriesId"] in s or x["sonarrEpisodeId"] in e])
            if method == "GET" and path == "/api/movies/wanted":
                return page([{"radarrId": m["radarrId"], "title": m["title"], "missing_subtitles": m["missing_subtitles"]}
                             for m in MOVIES if m["missing_subtitles"]])
            if method == "GET" and path == "/api/episodes/wanted":
                return page([{"sonarrSeriesId": e["sonarrSeriesId"], "sonarrEpisodeId": e["sonarrEpisodeId"],
                              "episodeTitle": e["title"], "missing_subtitles": e["missing_subtitles"]}
                             for e in EPISODES if e["missing_subtitles"]])
            if method == "GET" and path in ("/api/providers/movies", "/api/providers/episodes"):
                pass  # searched outside the lock below
            elif method in ("PATCH", "POST") and path in ("/api/movies/subtitles", "/api/providers/movies",
                                                          "/api/episodes/subtitles", "/api/providers/episodes"):
                pass
            else:
                return self.send(404, {"message": "not found"})
        # Slow parts run without the lock, like Bazarr's providers.
        time.sleep(args.delay)
        if method == "GET":
            return page(CANDIDATES)
        with LOCK:
            if "movies" in path:
                entry = next((m for m in MOVIES if m["radarrId"] == int(one("radarrid", "0"))), None)
            else:
                entry = next((e for e in EPISODES if e["sonarrEpisodeId"] == int(one("episodeid", "0"))), None)
            if entry is None:
                return self.send(404, {"message": "Not found"})
            if "providers" in path:
                save(entry, "en", flag(one("forced")), flag(one("hi")))
            else:
                save(entry, one("language", "en"), flag(one("forced")), flag(one("hi")))
        return self.send(204)

    def do_GET(self):
        self.handle_one("GET")

    def do_PATCH(self):
        self.handle_one("PATCH")

    def do_POST(self):
        self.handle_one("POST")

    def log_message(self, fmt, *a):
        print(self.command, self.path, "→", a[1] if len(a) > 1 else "", flush=True)


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


if __name__ == "__main__":
    print(f"fake Bazarr on http://127.0.0.1:{args.port} (key {args.key!r})", flush=True)
    Server(("127.0.0.1", args.port), Handler).serve_forever()
