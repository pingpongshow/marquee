#!/usr/bin/env python3
"""A fake IPTV source for developing and testing Live TV (LIVE-1..4).

Serves, Dispatcharr-style:
  /output/m3u  an M3U playlist of four test channels
  /output/epg  an XMLTV guide: half-hour shows from 6 hours ago to 2 days ahead
  /stream/<n>  an endless MPEG-TS test pattern made by FFmpeg as it's watched

Channel 3 is interlaced MPEG-2 with MP2 audio, like cable TV, so it has to be transcoded;
the others are H.264 + AAC and can be copied.

Usage: scripts/fake-iptv.py [port]   (default 32598)
"""
import datetime as dt
import http.server
import socketserver
import subprocess
import sys
from xml.sax.saxutils import escape

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 32598
CHANNELS = [
    # number, id, name, group, source, codec
    ("1", "news", "Marquee News", "News", "testsrc2=size=1280x720:rate=30", "h264"),
    ("2", "movies", "Movie Channel", "Movies", "smptehdbars=size=1280x720:rate=30", "h264"),
    ("3", "cable", "Cable Classic", "Entertainment", "testsrc=size=720x480:rate=30", "mpeg2"),
    ("4.1", "kids", "Kids Zone", "Kids", "rgbtestsrc=size=1280x720:rate=30", "h264"),
]
SHOWS = ["Morning Report", "Weather Watch", "The Big Story", "Late Edition", "Classic Film", "Cartoon Hour", "Science Today", "Sports Desk"]


def playlist(host):
    lines = [f'#EXTM3U x-tvg-url="http://{host}/output/epg"']
    for num, cid, name, group, _, _ in CHANNELS:
        lines.append(f'#EXTINF:-1 tvg-id="{cid}" tvg-name="{name}" tvg-logo="http://{host}/logo/{cid}.svg" tvg-chno="{num}" group-title="{group}",{name}')
        lines.append(f"http://{host}/stream/{cid}")
    return "\n".join(lines) + "\n"


def guide():
    now = dt.datetime.now(dt.timezone.utc).replace(minute=0, second=0, microsecond=0)
    out = ['<?xml version="1.0" encoding="UTF-8"?>', "<tv>"]
    for _, cid, name, _, _, _ in CHANNELS:
        out.append(f'<channel id="{cid}"><display-name>{escape(name)}</display-name></channel>')
    fmt = lambda t: t.strftime("%Y%m%d%H%M%S +0000")
    for i, (_, cid, _, _, _, _) in enumerate(CHANNELS):
        t = now - dt.timedelta(hours=6)
        k = i
        while t < now + dt.timedelta(days=2):
            length = dt.timedelta(minutes=30 if (k + i) % 3 else 60)
            title = SHOWS[k % len(SHOWS)]
            out.append(f'<programme start="{fmt(t)}" stop="{fmt(t + length)}" channel="{cid}"><title>{escape(title)}</title>'
                       f'<sub-title>Episode {k % 12 + 1}</sub-title><desc>{escape(title)} on the fake IPTV source.</desc>'
                       f'<category>{"News" if cid == "news" else "Series"}</category>'
                       f'<episode-num system="xmltv_ns">{k % 4}.{k % 12}.0/1</episode-num></programme>')
            t += length
            k += 1
    out.append("</tv>")
    return "\n".join(out)


def logo(cid):
    colours = {"news": "#d33", "movies": "#36c", "cable": "#a3a", "kids": "#3a3"}
    return (f'<svg xmlns="http://www.w3.org/2000/svg" width="200" height="120"><rect width="200" height="120" rx="16" fill="{colours.get(cid, "#888")}"/>'
            f'<text x="100" y="74" font-size="40" text-anchor="middle" fill="white" font-family="sans-serif">{cid.upper()[:5]}</text></svg>')


class Handler(http.server.BaseHTTPRequestHandler):
    def log_message(self, *a):
        pass

    def send(self, body, ctype):
        b = body.encode()
        self.send_response(200)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        host = self.headers.get("Host", f"127.0.0.1:{PORT}")
        if self.path == "/output/m3u":
            return self.send(playlist(host), "audio/x-mpegurl")
        if self.path == "/output/epg":
            return self.send(guide(), "application/xml")
        if self.path.startswith("/logo/"):
            return self.send(logo(self.path[6:].split(".")[0]), "image/svg+xml")
        if self.path.startswith("/stream/"):
            ch = next((c for c in CHANNELS if c[1] == self.path[8:]), None)
            if not ch:
                self.send_error(404)
                return
            _, _, _, _, src, codec = ch
            video = ["-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-pix_fmt", "yuv420p"] if codec == "h264" else \
                ["-vf", "setfield=tff", "-c:v", "mpeg2video", "-flags", "+ilme+ildct", "-alternate_scan", "1", "-b:v", "4M"]
            audio = ["-c:a", "aac"] if codec == "h264" else ["-c:a", "mp2"]
            cmd = ["ffmpeg", "-v", "error", "-re", "-f", "lavfi", "-i", src, "-f", "lavfi", "-i", "sine=frequency=330",
                   *video, *audio, "-f", "mpegts", "pipe:1"]
            self.send_response(200)
            self.send_header("Content-Type", "video/mp2t")
            self.end_headers()
            p = subprocess.Popen(cmd, stdout=subprocess.PIPE)
            try:
                while True:
                    chunk = p.stdout.read(188 * 64)
                    if not chunk:
                        break
                    self.wfile.write(chunk)
            except (BrokenPipeError, ConnectionResetError):
                pass
            finally:
                p.kill()
            return
        self.send_error(404)


class Server(socketserver.ThreadingMixIn, http.server.HTTPServer):
    daemon_threads = True
    allow_reuse_address = True


if __name__ == "__main__":
    print(f"fake IPTV on :{PORT}", flush=True)
    Server(("0.0.0.0", PORT), Handler).serve_forever()
