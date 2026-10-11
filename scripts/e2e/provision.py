#!/usr/bin/env python3
"""Builds the local e2e test environment's media and server state (see setup.sh).

  provision.py media <e2e dir>       generate the test media into <e2e dir>/stage
  provision.py provision <e2e dir>   set up a fresh server on :32597 from the staged media
  provision.py check <e2e dir>       exit 0 if the running server has the expected state

Some UI tests use fixed item ids, so files are moved from stage/ into media/ and scanned in
steps that give the same ids as the original hand-made environment:

  1-3      Test Anime (show, season, episode)
  4, 5, 6  20 Valley, 15 Thunder, 31 Ocean (the fake Bazarr's movies; Test Saga)
  7-303    the other 297 generated movies
  304-307  Test Artist 2, Album 2, Song 2-2, Song 2-3
  308-314  Test Artist 1, Album 1, Song 1-1 .. 1-4, Song 2-1
  315-321  Song 2-4, Test Artist 3, Album 3 and its tracks
  322      00 Long Test
  323-325  Calm Pads, Floating, Floating 1
  326-358  the other five ambient/noise/electronic albums
  359      00 Preview Test (2024)
  360      Test Saga (collection of 4, 5, 6)
  361, 362 the featurette and Trailers/Official Trailer.mkv of 00 Preview Test
After that ids don't matter: the Extended Cut version, the Hi-Res FLAC, Getz/Gilberto, and
the Recorded TV library.
"""
import base64
import hashlib
import hmac
import json
import os
import random
import shutil
import sqlite3
import struct
import subprocess
import sys
import time
import urllib.error
import urllib.request

BASE = "http://127.0.0.1:32597/api/v1"
ADMIN = ("e2eadmin", "e2e-local-test-pass")
TWOFA = ("twofa", "correct horse battery")
REPO = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))


def run(*cmd):
    subprocess.run(cmd, check=True)


def ff(*args):
    run("ffmpeg", "-hide_banner", "-loglevel", "error", "-y", *args)


# ---------------------------------------------------------------------------------------------
# Media


def movie_names():
    """The 300 generated movie folders (same seed and order as the original environment)."""
    random.seed(7)
    words = ("Alpha Bravo Comet Delta Echo Falcon Ghost Harbor Iron Jade Kingdom Lantern Meadow Night Ocean "
             "Phoenix Quartz River Shadow Thunder Umbra Valley Winter Xenon Yonder Zephyr").split()
    made, out = set(), []
    while len(out) < 300:
        t = f"{random.choice(['', 'The ', 'A ', ''])}{random.choice(words)} {random.choice(words)}"
        if random.random() < 0.08:
            t = f"{random.randint(2, 99)} {random.choice(words)}"
        name = f"{t} ({random.randint(1950, 2025)})"
        if name not in made:
            made.add(name)
            out.append(name)
    return out


AMBIENT = [  # artist, album, genre, year, source (TT = track number)
    ("Calm Pads", "Floating", "Ambient", 1998, "aevalsrc=0.2*sin(2*PI*(200+TT*30)*t)+0.15*sin(2*PI*(300+TT*20)*t)*sin(2*PI*0.2*t):s=44100"),
    ("Drone Lake", "Stillness", "Ambient", 2003, "aevalsrc=0.25*sin(2*PI*(150+TT*25)*t)+0.1*sin(2*PI*(450+TT*10)*t):s=44100"),
    ("Static Kids", "White Room", "Noise", 2011, "anoisesrc=color=white:amplitude=0.3:seed=TT"),
    ("Hiss Theory", "Pink Fields", "Noise", 2015, "anoisesrc=color=pink:amplitude=0.4:seed=TT"),
    ("Beat Unit", "Pulse", "Electronic", 2019, "aevalsrc=0.8*sin(2*PI*60*t)*exp(-20*mod(t\\,0.5))+0.3*sin(2*PI*2000*t)*exp(-60*mod(t+0.25\\,0.5))*lt(mod(TT\\,2)\\,2):s=44100"),
    ("Kick Society", "Thump", "Electronic", 2021, "aevalsrc=0.9*sin(2*PI*55*t)*exp(-15*mod(t\\,0.428)):s=44100"),
]


def mp3(path, src, seconds, artist, album, title, track, year, genre):
    ff("-f", "lavfi", "-i", src, "-t", str(seconds), "-c:a", "libmp3lame", "-b:a", "128k",
       "-metadata", f"artist={artist}", "-metadata", f"album_artist={artist}", "-metadata", f"album={album}",
       "-metadata", f"title={title}", "-metadata", f"track={track}", "-metadata", f"date={year}",
       "-metadata", f"genre={genre}", path)


def make_media(e):
    st = os.path.join(e, "stage")
    if os.path.exists(os.path.join(st, ".complete")):
        return
    shutil.rmtree(st, ignore_errors=True)
    os.makedirs(st)

    # Anime: styled ASS subtitles with an embedded font (Test Anime S01E01).
    d = os.path.join(st, "anime/Test Anime/Season 01")
    os.makedirs(d)
    ass = os.path.join(st, "sub.ass")
    with open(ass, "w") as f:
        f.write("""[Script Info]
ScriptType: v4.00+
PlayResX: 1280
PlayResY: 720

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Marquee Test Font,64,&H0000FFFF,&H000000FF,&H00000000,&H00000000,-1,0,0,0,100,100,0,0,1,4,0,2,10,10,40,1
Style: Sign,Marquee Test Font,48,&H00FF00FF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,-12,1,3,0,8,10,10,60,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:20.00,Default,,0,0,0,,Styled yellow dialogue
Dialogue: 0,0:00:01.00,0:00:20.00,Sign,,0,0,0,,{\\pos(640,120)}Rotated magenta sign
""")
    font = next((p for p in ("/System/Library/Fonts/Supplemental/Arial.ttf", "/Library/Fonts/Arial Unicode.ttf",
                             "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf") if os.path.exists(p)), None)
    attach = ["-attach", font, "-metadata:s:t:0", "mimetype=application/x-truetype-font",
              "-metadata:s:t:0", "filename=MarqueeTestFont.ttf"] if font else []
    ff("-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=30", "-f", "lavfi", "-i", "sine=duration=30",
       "-i", ass, *attach, "-map", "0:v", "-map", "1:a", "-map", "2:s", "-c:v", "libx264", "-preset", "ultrafast",
       "-c:a", "aac", "-c:s", "ass", os.path.join(d, "Test Anime - S01E01.mkv"))
    os.remove(ass)

    # Movies: 300 tiny generated ones, plus the long test and the preview test with extras.
    base = os.path.join(st, "base.mp4")
    ff("-f", "lavfi", "-i", "testsrc=size=160x90:rate=24:duration=2", "-f", "lavfi", "-i", "sine=f=440:duration=2",
       "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-shortest", base)
    for name in movie_names():
        d = os.path.join(st, "movies", name)
        os.makedirs(d)
        shutil.copy(base, os.path.join(d, name + ".mp4"))
    os.remove(base)
    d = os.path.join(st, "movies/00 Long Test (2024)")
    os.makedirs(d)
    ff("-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=90", "-f", "lavfi", "-i", "sine=f=330:duration=90",
       "-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-c:a", "aac", "-shortest", os.path.join(d, "00 Long Test (2024).mkv"))
    d = os.path.join(st, "movies/00 Preview Test (2024)")
    os.makedirs(os.path.join(d, "Trailers"))
    ff("-f", "lavfi", "-i", "testsrc=size=1280x720:rate=24:duration=240:decimals=0", "-f", "lavfi", "-i", "sine=frequency=440:duration=240",
       "-c:v", "libx264", "-preset", "ultrafast", "-g", "96", "-c:a", "aac", os.path.join(d, "00 Preview Test (2024).mkv"))
    ff("-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=20", "-f", "lavfi", "-i", "sine=frequency=660:duration=20",
       "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", os.path.join(d, "Trailers/Official Trailer.mkv"))
    ff("-f", "lavfi", "-i", "smptebars=size=1280x720:rate=24:duration=15", "-f", "lavfi", "-i", "sine=frequency=330:duration=15",
       "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", os.path.join(d, "00 Preview Test (2024)-featurette.mkv"))
    ff("-f", "lavfi", "-i", "testsrc=size=640x360:rate=24:duration=150", "-f", "lavfi", "-i", "sine=frequency=500:duration=150",
       "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", os.path.join(d, "00 Preview Test (2024) {edition-Extended Cut}.mkv"))

    # Music: three test albums of 20 s tones, six 40 s albums for Muse and stations, a
    # .lrc for Floating 1 and a 24-bit/96 kHz FLAC.
    for a in (1, 2, 3):
        d = os.path.join(st, f"music/Test Artist {a}/Album {a} (200{a})")
        os.makedirs(d)
        for t in (1, 2, 3, 4):
            mp3(os.path.join(d, f"0{t} - Song {a}-{t}.mp3"), f"sine=f={200 + a * 100 + t * 30}:duration=20", 20,
                f"Test Artist {a}", f"Album {a}", f"Song {a}-{t}", t, f"200{a}", "Jazz" if a == 2 else "Rock")
    ff("-f", "lavfi", "-i", "sine=frequency=440:duration=20", "-ar", "96000", "-sample_fmt", "s32", "-c:a", "flac",
       "-bits_per_raw_sample", "24", os.path.join(st, "music/Test Artist 1/Album 1 (2001)/1-99 Hi-Res Tone.flac"))
    for artist, album, genre, year, src in AMBIENT:
        d = os.path.join(st, f"music/{artist}/{album} ({year})")
        os.makedirs(d)
        for t in (1, 2, 3, 4):
            mp3(os.path.join(d, f"0{t} - {album} {t}.mp3"), src.replace("TT", str(t)), 40, artist, album, f"{album} {t}", t, year, genre)
    with open(os.path.join(st, "music/Calm Pads/Floating (1998)/01 - Floating 1.lrc"), "w") as f:
        f.write("[ar:Calm Pads]\n[00:00.50]Floating on a quiet sea\n[00:04.00]Every wave a slower breath\n"
                "[00:08.00]Drifting where the light goes\n[00:12.00]Into the gentle dark\n[00:16.00](instrumental)\n")

    # A Live TV recording in the Recorded TV library (the fake Bazarr knows it).
    d = os.path.join(st, "recordings/TV/Weather Watch/Season 2")
    os.makedirs(d)
    ff("-f", "lavfi", "-i", "testsrc=size=640x360:rate=25:duration=20", "-f", "lavfi", "-i", "sine=frequency=550:duration=20",
       "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", "-f", "mpegts",
       os.path.join(d, "Weather Watch - S02E10 - Episode 10.ts"))
    open(os.path.join(st, ".complete"), "w").close()


# ---------------------------------------------------------------------------------------------
# API


class API:
    def __init__(self, token=None):
        self.token = token

    def call(self, method, path, body=None, token=None, raw=None, ctype="application/json"):
        data = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        headers = {"Content-Type": ctype}
        tok = token or self.token
        if tok:
            headers["Authorization"] = "Bearer " + tok
        req = urllib.request.Request(BASE + path, method=method, data=data, headers=headers)
        try:
            with urllib.request.urlopen(req, timeout=60) as r:
                d = r.read()
                return json.loads(d) if d else None
        except urllib.error.HTTPError as e:
            raise SystemExit(f"{method} {path}: {e.code} {e.read()[:300]!r}")


def totp(secret, t=None):
    key = base64.b32decode(secret + "=" * (-len(secret) % 8))
    h = hmac.new(key, struct.pack(">Q", int((t or time.time()) // 30)), hashlib.sha1).digest()
    o = h[-1] & 15
    return "%06d" % ((struct.unpack(">I", h[o:o + 4])[0] & 0x7FFFFFFF) % 1000000)


class Env:
    def __init__(self, e):
        self.e = e
        self.stage = os.path.join(e, "stage")
        self.media = os.path.join(e, "media")
        self.api = API()
        self.libs = {}

    def db(self, sql):
        con = sqlite3.connect(f"file:{os.path.join(self.e, 'config/marquee.db')}?mode=ro", uri=True, timeout=10)
        try:
            return con.execute(sql).fetchall()
        finally:
            con.close()

    def max_id(self):
        return self.db("SELECT COALESCE(MAX(id), 0) FROM items")[0][0]

    def move(self, rel, dest_root=None):
        """Moves a staged file or folder into the live media (or recordings) folder."""
        src = os.path.join(self.stage, rel)
        dst = os.path.join(dest_root or self.media, rel)
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        os.rename(src, dst)

    def idle(self):
        return all(l["scanStatus"] == "idle" for l in self.api.call("GET", "/libraries"))

    def scan(self, lib, expect_max, timeout=300):
        """Scans a library and waits until it's idle with items up to expect_max."""
        self.api.call("POST", f"/libraries/{self.libs[lib]}/scan")
        self.wait(expect_max, timeout)

    def wait(self, expect_max, timeout=300):
        end = time.time() + timeout
        while time.time() < end:
            time.sleep(1)
            m = self.max_id()
            if m > expect_max:
                raise SystemExit(f"item ids ran past {expect_max} (now {m}); the ids won't match the tests")
            if m == expect_max and self.idle():
                time.sleep(1)  # let a watcher-triggered scan settle
                if self.idle() and self.max_id() == expect_max:
                    return
        raise SystemExit(f"timed out waiting for item {expect_max} (at {self.max_id()})")

    def add_library(self, name, kind, path, expect_max):
        os.makedirs(path, exist_ok=True)
        lib = self.api.call("POST", "/libraries", {"name": name, "type": kind, "paths": [path]})
        self.libs[name] = lib["id"]
        self.wait(expect_max)
        return lib["id"]

    def title(self, item_id):
        r = self.db(f"SELECT title FROM items WHERE id = {int(item_id)}")
        return r[0][0] if r else None

    def expect(self, item_id, title):
        got = self.title(item_id)
        if got != title:
            raise SystemExit(f"item {item_id} is {got!r}, expected {title!r}")


def provision(e):
    env = Env(e)
    a = env.api
    dev = lambda cid: {"clientId": cid, "name": "e2e setup", "platform": "web"}

    # First-run setup and users.
    r = a.call("POST", "/setup", {"serverName": "E2E", "username": ADMIN[0], "password": ADMIN[1], "device": dev("e2e-setup")})
    a.token = r["token"]
    with open(os.path.join(e, "token"), "w") as f:
        f.write(a.token + "\n")
    kid = a.call("POST", "/users", {"username": "kiddo", "displayName": "Kiddo", "isManaged": True})
    restr = kid.get("restrictions") or {}
    restr["canRequest"] = True
    a.call("PATCH", f"/users/{kid['id']}", {"restrictions": restr})
    a.call("POST", "/users", {"username": TWOFA[0], "displayName": "Two FA", "password": TWOFA[1]})
    tok = a.call("POST", "/auth/login", {"username": TWOFA[0], "password": TWOFA[1], "device": dev("e2e-totp")})["token"]
    secret = a.call("POST", "/auth/totp/setup", token=tok)["secret"]
    a.call("POST", "/auth/totp/enable", {"code": totp(secret)}, token=tok)
    with open(os.path.join(e, "twofa.secret"), "w") as f:
        f.write(secret + "\n")
    print("users: e2eadmin, kiddo, twofa", flush=True)

    # Anime (1-3).
    env.move("anime/Test Anime")
    env.add_library("Anime", "anime", os.path.join(env.media, "anime"), 3)

    # Movies (4-303): the three Bazarr movies first, one at a time.
    names = movie_names()
    env.move("movies/20 Valley (1984)")
    env.add_library("Movies", "movies", os.path.join(env.media, "movies"), 4)
    env.move("movies/15 Thunder (1970)")
    env.scan("Movies", 5)
    env.move("movies/31 Ocean (1997)")
    env.scan("Movies", 6)
    for n in names:
        if n not in ("20 Valley (1984)", "15 Thunder (1970)", "31 Ocean (1997)"):
            env.move(f"movies/{n}")
    env.scan("Movies", 303)
    for i, t in ((4, "20 Valley"), (5, "15 Thunder"), (6, "31 Ocean")):
        env.expect(i, t)
    print("movies: 300", flush=True)

    # Music (304-321).
    A1, A2, A3 = "music/Test Artist 1/Album 1 (2001)", "music/Test Artist 2/Album 2 (2002)", "music/Test Artist 3/Album 3 (2003)"
    env.move(f"{A2}/02 - Song 2-2.mp3")
    env.add_library("Music", "music", os.path.join(env.media, "music"), 306)
    env.move(f"{A2}/03 - Song 2-3.mp3")
    env.scan("Music", 307)
    for t, want in ((1, 310), (2, 311), (3, 312), (4, 313)):
        env.move(f"{A1}/0{t} - Song 1-{t}.mp3")
        env.scan("Music", want)
    env.move(f"{A2}/01 - Song 2-1.mp3")
    env.scan("Music", 314)
    env.move(f"{A2}/04 - Song 2-4.mp3")
    env.move("music/Test Artist 3")
    env.scan("Music", 321)
    for i, t in ((308, "Test Artist 1"), (309, "Album 1"), (310, "Song 1-1"), (311, "Song 1-2"), (314, "Song 2-1")):
        env.expect(i, t)

    # 00 Long Test (322).
    env.move("movies/00 Long Test (2024)")
    env.scan("Movies", 322)
    env.expect(322, "00 Long Test")

    # Ambient, noise and electronic albums (323-358), Calm Pads/Floating first.
    FL = "music/Calm Pads/Floating (1998)"
    env.move(f"{FL}/01 - Floating 1.mp3")
    env.move(f"{FL}/01 - Floating 1.lrc")
    env.scan("Music", 325)
    for t in (2, 3, 4):
        env.move(f"{FL}/0{t} - Floating {t}.mp3")
    for artist, album, _, year, _ in AMBIENT[1:]:
        env.move(f"music/{artist}")
    env.scan("Music", 358, timeout=600)
    env.expect(323, "Calm Pads")
    env.expect(324, "Floating")
    print("music: 6 + 3 albums", flush=True)

    # 00 Preview Test (359), Test Saga (360), its featurette (361) and trailer (362).
    PT = "movies/00 Preview Test (2024)"
    env.move(f"{PT}/00 Preview Test (2024).mkv")
    env.scan("Movies", 359)
    env.expect(359, "00 Preview Test")
    col = a.call("POST", f"/libraries/{env.libs['Movies']}/collections", {"title": "Test Saga", "itemIds": [4, 5, 6]})
    if col["id"] != 360:
        raise SystemExit(f"Test Saga is item {col['id']}, expected 360")
    env.move(f"{PT}/00 Preview Test (2024)-featurette.mkv")
    env.scan("Movies", 361)
    env.move(f"{PT}/Trailers")
    env.scan("Movies", 362)
    env.expect(362, "Official Trailer")
    env.move(f"{PT}/00 Preview Test (2024) {{edition-Extended Cut}}.mkv")
    a.call("POST", f"/libraries/{env.libs['Movies']}/scan")
    for _ in range(120):
        time.sleep(1)
        if len(a.call("GET", "/items/359").get("versions", [])) == 2 and env.idle():
            break
    else:
        raise SystemExit("the Extended Cut version didn't show up on item 359")
    print("preview test: extras, trailer, two versions", flush=True)

    # Hi-Res FLAC and Getz/Gilberto (long names, cover art).
    env.move(f"{A1}/1-99 Hi-Res Tone.flac")
    env.scan("Music", 364)
    run(os.path.join(REPO, "apple/scripts/make-ui-test-fixtures.sh"), os.path.join(env.media, "music"),
        "127.0.0.1:32597", a.token, str(env.libs["Music"]))
    env.wait(367)  # artist, album, Corcovado

    # Recorded TV (what the DVR would make on its first recording).
    env.move("recordings/TV", dest_root=e)
    lib = a.call("POST", "/libraries", {"name": "Recorded TV", "type": "shows", "paths": [os.path.join(e, "recordings/TV")]})
    env.libs["Recorded TV"] = lib["id"]

    # Integrations: OpenSubtitles (dummy key), the fake IPTV source and the fake Bazarr.
    a.call("PATCH", "/settings", {"metadata": {"openSubtitlesApiKey": "test-key"}})
    a.call("PATCH", "/settings", {"integrations": {
        "liveTvSources": [{"name": "Test IPTV", "kind": "dispatcharr", "url": "http://127.0.0.1:32598", "enabled": True}],
        "bazarrUrl": "http://127.0.0.1:32767", "bazarrApiKey": "fake-bazarr-key"}})

    # Kiddo's token and "Road Trip" playlist (Floating 1-4).
    kt = a.call("POST", "/auth/pin", {"userId": kid["id"], "device": {"clientId": "e2e-kid", "name": "e2e setup", "platform": "web"}})["token"]
    with open(os.path.join(e, "kid.token"), "w") as f:
        f.write(kt + "\n")
    tracks =[i["id"] for i in a.call("GET", "/items/324/children", token=kt)["items"]]
    p = a.call("POST", "/playlists", {"title": "Road Trip", "kind": "audio"}, token=kt)
    a.call("POST", f"/playlists/{p['id']}/items", {"itemIds": tracks}, token=kt)

    # Let scans, the guide refresh and trickplay finish.
    end = time.time() + 300
    while not env.idle() and time.time() < end:
        time.sleep(2)
    a.call("POST", "/tasks/trickplay/run")
    for _ in range(300):
        time.sleep(2)
        t = [x for x in a.call("GET", "/tasks") if x["id"] == "trickplay"]
        if not t or not t[0].get("running"):
            break
    for _ in range(30):
        st = a.call("GET", "/livetv/status")
        if st.get("channels"):
            break
        time.sleep(1)
    print("libraries:", ", ".join(f"{l['name']} ({l['itemCount']})" for l in a.call("GET", "/libraries")), flush=True)
    print("live tv channels:", a.call("GET", "/livetv/status").get("channels"), flush=True)


def check(e):
    """Exit 0 when the running server looks like a finished e2e environment."""
    try:
        token = open(os.path.join(e, "token")).read().strip()
        open(os.path.join(e, "kid.token")).read()
        a = API(token)
        ok = (a.call("GET", "/items/359")["title"] == "00 Preview Test"
              and a.call("GET", "/items/6")["title"] == "31 Ocean"
              and a.call("GET", "/items/324")["title"] == "Floating"
              and {l["name"] for l in a.call("GET", "/libraries")} >= {"Anime", "Movies", "Music", "Recorded TV"})
    except (SystemExit, OSError, KeyError, ValueError):
        ok = False
    sys.exit(0 if ok else 1)


if __name__ == "__main__":
    cmd, e = sys.argv[1], os.path.abspath(sys.argv[2])
    {"media": make_media, "provision": provision, "check": check}[cmd](e)
