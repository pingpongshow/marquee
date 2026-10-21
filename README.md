<div align="center">

# Marquee

**A self-hosted media server for movies, TV, anime, music and live TV, with apps for the web, iPhone, iPad, Apple TV, Android and Android TV.**

[![Release](https://img.shields.io/github/v/release/pingpongshow/marquee?sort=semver)](https://github.com/pingpongshow/marquee/releases)
![Server](https://img.shields.io/badge/server-Go%20%2B%20Docker-00ADD8)
![Apps](https://img.shields.io/badge/apps-Web%20·%20iOS%20·%20tvOS%20·%20Android-6E56CF)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

</div>

Marquee organises the films, shows, music and recordings already on your disks and streams them to your devices, at home and away. It runs on your own hardware, needs no cloud account, and keeps your viewing private: accounts are local, metadata comes from public sources, and the heavy lifting (transcoding, music analysis, natural-language search) happens on your server.

- **One server, every screen:** a web app built into the server, plus native apps for iPhone, iPad, Apple TV, Android phones, tablets and Android TV.
- **Plays anything:** direct play when the device can, otherwise a remux or a hardware-accelerated transcode (NVIDIA, Intel Quick Sync or CPU) sized to the connection.
- **A music player worth using:** gapless playback, loudness levelling, crossfade, synced lyrics, radios and mixes built from how your music actually sounds.
- **Live TV, DVR and on-demand films and shows** from any M3U/XMLTV source or Dispatcharr, with commercials cut from recordings and films on demand saved straight into your library.
- **A household, not just a login:** profiles with optional PINs, kids' profiles with limits, per-person progress, ratings and recommendations.

---

> **New here?** The **[Marquee guide](guide/README.md)** walks through installing, setting up and every setting, including Dispatcharr, Live TV and VOD.

## Contents

- [The guide](guide/README.md)
- [Features](#features)
- [Apps](#apps)
- [Requirements](#requirements)
- [Install the server](#install-the-server)
- [Organising your media](#organising-your-media)
- [Install the apps](#install-the-apps)
- [Remote access](#remote-access)
- [Configuration](#configuration)
- [Updating, backups and data](#updating-backups-and-data)
- [Troubleshooting](#troubleshooting)
- [Building from source](#building-from-source)
- [Repository layout](#repository-layout)
- [Contributing](#contributing)
- [Security](#security)
- [Acknowledgements](#acknowledgements)
- [License](#license)

## Features

### Libraries and metadata
- Movies, TV shows, anime, music and home videos, scanned from your folders and kept current as files change.
- Metadata and artwork from TMDB (with OMDb ratings from IMDb, Rotten Tomatoes and Metacritic), AniList for anime, and MusicBrainz, Deezer, Wikipedia and ListenBrainz for music.
- Collections (automatic film series, manual, and **smart collections** built from filters), extras, multiple versions and editions, and cast and crew pages.
- **Trailers** for any film or show: a local trailer file if you have one, otherwise the official trailer, played in the app.
- Fix Match, metadata editing with locked fields, and custom artwork.
- **Library Health:** duplicates (compare every copy's details and move the one you don't want to a recoverable trash), unmatched items, unplayable files, upgrade candidates, playback errors, missing artwork and missing subtitles.
- **Large files optimizer:** re-encode oversized films and episodes to HEVC (on an NVIDIA GPU end to end), keeping every audio and subtitle track, with an optional size cap per hour of running time.
- Two-way sync of watch state with a Plex server.
- Import watch history, ratings, playlists, markers and accounts from an existing Plex server.

### Playback
- Direct play, direct stream (remux) or transcode, chosen per device and network, with HDR-to-SDR tone mapping.
- Automatic quality for remote viewers, adaptive bitrate, and a fair share of your upload bandwidth.
- Subtitles: text, styled ASS with embedded fonts, and burned-in image subtitles. Each person picks size, colour, background and position, and can adjust subtitle and audio timing (remembered per file). Search OpenSubtitles or use **Bazarr**.
- Skip Intro and Skip Credits (detected automatically), chapters, seek-bar thumbnails, playback speed, an Up Next countdown between episodes and cinema trailers before films.
- Continue Watching and On Deck, with any title removable from the row.
- **Watch together:** shared play, pause and seek across devices.
- Chromecast from iPhone, iPad and Android; AirPlay on Apple devices.
- Offline downloads on iPhone, iPad and Android, optionally converted on the server. Ratings, watched marks and progress made offline sync when you reconnect.

### Music
- **Soundprint:** every track is analysed on your server, powering radios, "sounds like this", **Muse** (describe a playlist in words), **Sound Journey** (a path from one track to another), a DJ, and mood and style pages.
- Daily mixes from your listening, artist bios and popular tracks, smart playlists, favourites, synced lyrics, gapless playback, crossfade, an equaliser, and **Your Year in Music**.
- Loudness levelling on every track (ReplayGain tags, or measured on the server).
- Scrobbling to ListenBrainz and Last.fm.

### Discover and personalise
- **Muse for movies:** "90s sci-fi with time travel" or "acclaimed war films I haven't seen" finds matches in your library.
- "Recommended for You", "Because you watched…", Top Rated, Trending Now and rows from the genres you watch, with films and shows on demand mixed in (marked VOD), plus a holiday row in the 30 days before Halloween, Thanksgiving and Christmas, and "More like this".
- A Home screen each person can reorder, hide and pin rows on.
- Personal ratings, a household community rating with comments, a watchlist and statistics.
- Favourites (the heart) on films, shows, episodes and music, which also steer recommendations.
- Requests through [Seerr](https://github.com/seerr-team/seerr), approved by an admin.

### Live TV and on demand
- A channel guide from any M3U/XMLTV source or Dispatcharr, What's On and favourite channels.
- DVR: one-off and series recordings, commercials cut out with Comskip, quotas per person and a shared or personal view of recordings.
- **VOD Movies and VOD Shows** from Dispatcharr or any Xtream Codes source, matched to TMDB, with filters, Continue Watching and suggestions picked from each person's taste.
- **Save to library** (admins): films and whole series from VOD downloaded, checked and named for your library, using the provider connections nobody is watching on.

### People and administration
- Local accounts with profiles and optional PINs, managed profiles for children (rating and library limits), two-factor sign-in, and Quick Connect for TVs.
- Share with friends through one-time invite links.
- Dashboard, activity, logs, scheduled tasks (each can be switched off), backups and restore, an internet speed test, webhooks and Prometheus metrics.
- Email: admin alerts (failed tasks, downloads and conversions, low disk space and more) and password or PIN resets by email.

## Apps

| Platform | Highlights |
|---|---|
| **Web** | Everything, including settings and administration. Built into the server: open `http://<server>:32500`. |
| **iPhone and iPad** | Downloads, AirPlay and Chromecast, lock-screen controls. |
| **Apple TV** | Top Shelf, Quick Connect sign-in, remote-controllable. |
| **Android phone and tablet** | Downloads, Android Auto, Chromecast. |
| **Android TV** | A D-pad interface with the same features as phones, remote-controllable. |

## Requirements

- **Server:** Linux with Docker and Docker Compose. Any NAS or home server that runs Docker works (Unraid, TrueNAS SCALE, Synology/QNAP with Container Manager, or a plain Linux box).
- **CPU and memory:** 4 cores and 4 GB of RAM are comfortable for direct play and a CPU transcode; add RAM for the transcode scratch space (8 GB by default, held in memory).
- **Hardware transcoding (optional):** an NVIDIA GPU with the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html), or an Intel CPU with Quick Sync (`/dev/dri`). Without either, Marquee transcodes on the CPU, and still direct-plays whatever your devices support.
- **Music analysis (optional):** Soundprint runs on the CPU, or much faster on an NVIDIA GPU. It needs about 1.5 GB of disk for its models.
- **A free [TMDB API key](https://www.themoviedb.org/settings/api)** for movie and TV metadata.
- **Apps:** iOS/iPadOS/tvOS 18 or later; Android 8.0 or later.

## Install the server

Marquee is built and run with Docker Compose from this repository.

**1. Get the code**

```bash
git clone https://github.com/pingpongshow/marquee.git
cd marquee/deploy
cp .env.example .env
```

**2. Point it at your media**

Edit `.env`. At minimum set `VIDEO_DIR` and `MUSIC_DIR` (or leave `MUSIC_DIR` unset if you have no music), `PUID`/`PGID` (the user that owns your media, from `id -u` and `id -g`), and `TZ`.

```ini
PUID=1000
PGID=1000
TZ=Europe/London
CONFIG_DIR=/srv/marquee/config
VIDEO_DIR=/srv/media/video
MUSIC_DIR=/srv/media/music
```

Your video folder is mounted read-only unless you set `VIDEO_MODE=` (empty), which lets Library Health move unwanted duplicates to a trash folder (emptied by the nightly "Empty media trash" task), the Large files optimizer replace files, and VOD downloads go into your library.

**3. Start it**

Pick the line that matches your hardware:

```bash
# CPU only
docker compose up -d --build

# NVIDIA GPU
docker compose -f compose.yaml -f compose.nvidia.yaml up -d --build

# Intel Quick Sync (set RENDER_GID in .env: stat -c %g /dev/dri/renderD128)
docker compose -f compose.yaml -f compose.intel.yaml up -d --build
```

The first build takes a few minutes (it compiles the server and the web app). Remove the `soundprint` service from `compose.yaml` if you don't want music analysis and natural-language search.

**4. Set it up**

Open `http://<server>:32500` and follow the setup:

1. Create the admin account.
2. Add your libraries: point each at a folder inside the container, e.g. `/media/video/Movies`, `/media/video/TV`, `/media/video/Anime`, `/media/music`.
3. Enter your TMDB key under **Settings → Metadata**. Matching and artwork start straight away.
4. Add people under **Settings → Users**: profiles can have a PIN, rating limits and library access.

### On Unraid

Marquee has Unraid templates with an icon, WebUI link and every setting explained:

1. Copy the templates onto the flash drive (from the Unraid terminal):
   ```sh
   cd /boot/config/plugins/dockerMan/templates-user
   wget -O my-Marquee.xml https://raw.githubusercontent.com/pingpongshow/unraid-templates/main/marquee.xml
   wget -O my-Marquee-Soundprint.xml https://raw.githubusercontent.com/pingpongshow/unraid-templates/main/marquee-soundprint.xml
   ```
2. **Docker** tab → **Add Container**, pick **Marquee** under *Template*, set your Video and Music folders and apply. Add **Marquee-Soundprint** the same way for music analysis.
3. Open `http://<server>:32500`.

Images are published to `ghcr.io/pingpongshow/marquee` and `ghcr.io/pingpongshow/marquee-soundprint` with each release, and Unraid shows when an update is available.

## Organising your media

Marquee follows the common naming conventions, so a library that works with other media servers works here.

```text
video/
├── Movies/
│   └── Blade Runner (1982)/
│       ├── Blade Runner (1982).mkv
│       ├── Blade Runner (1982).en.srt          # sidecar subtitles: <name>.<language>.srt
│       ├── poster.jpg                           # optional local artwork
│       └── Trailers/                            # extras: Trailers, Featurettes, Behind The Scenes,
│           └── Official Trailer.mkv             #   Deleted Scenes, Interviews, Scenes, Shorts, Extras
├── TV/
│   └── The Expanse (2015)/
│       └── Season 01/
│           └── The Expanse - S01E01 - Dulcinea.mkv
└── Anime/
    └── Cowboy Bebop/
        └── Season 01/
            └── Cowboy Bebop - S01E01.mkv        # absolute numbering ("- 001") works too
music/
└── Artist/
    └── Album (Year)/
        ├── 01 - Track.flac
        └── cover.jpg
```

- Put the year in film and show folder names; it makes matching reliable.
- Several files for one film (4K and 1080p, a director's cut) become versions of one item.
- Music uses the files' tags first (artist, album artist, album, track, disc, MusicBrainz IDs), so tidy tags matter more than folder names.

## Install the apps

Download the apps from the [latest release](https://github.com/pingpongshow/marquee/releases/latest).

| App | How to install |
|---|---|
| **Android / Android TV** | Download `Marquee-<version>.apk` and open it (allow installing from your browser or file manager). On Android TV, sideload it with a file manager or `adb install`. |
| **iPhone / iPad** | `Marquee-iOS-<version>-unsigned.ipa` is unsigned. Install it with a sideloading tool such as [AltStore](https://altstore.io) or [Sideloadly](https://sideloadly.io), signed with your Apple ID, or build it yourself in Xcode ([see below](#building-from-source)). |
| **Apple TV** | `Marquee-tvOS-<version>-unsigned.ipa`: install with Sideloadly, or build in Xcode. |

The apps find the server on your network automatically, or you can type its address (for example `192.168.1.20:32500`). On a TV, sign in with **Quick Connect**: the TV shows a code that you approve from your phone or the web.

## Remote access

Marquee works over any connection that reaches port 32500.

- **Tailscale (recommended):** install [Tailscale](https://tailscale.com) on the server and your devices, then enter the server's Tailscale address as the **Remote server address** under **Settings → Remote Access**. The apps learn it from the server, use the home address when they can reach it and the Tailscale one otherwise, and remote streams adapt their quality to the connection.
- **Friends:** share the server's machine with them from the Tailscale admin console, then send an invite link from **Settings → Users**. Friends get their own account with the libraries and limits you choose.
- **Reverse proxy:** you can also publish Marquee through a reverse proxy with HTTPS (Caddy, Nginx, Traefik). Set the remote address and your upload speed under **Settings → Remote Access** so remote quality is chosen well (there's a built-in speed test).

## Configuration

Everything is configured in the web app under **Settings**. Optional integrations:

| Integration | Where | What it adds |
|---|---|---|
| TMDB, OMDb, Fanart.tv, OpenSubtitles | Settings → Metadata | Metadata, ratings, artwork, subtitle search |
| Seerr | Settings → Requests | Discover and request titles |
| Bazarr | Settings → Requests | Subtitle status, downloads and search |
| Dispatcharr or M3U/XMLTV | Settings → Live TV | Live TV and DVR |
| Last.fm, ListenBrainz | Settings → Music / your account | Scrobbling |
| Webhooks, Prometheus | Settings → Webhooks / `/api/v1/system/metrics` | Automation and monitoring |

**Environment variables** (all optional; the compose file sets sensible defaults):

| Variable | Default | Purpose |
|---|---|---|
| `MARQUEE_PORT` | `32500` | Web and API port |
| `MARQUEE_CONFIG_DIR` | `/config` | Database, settings, artwork cache, backups and logs |
| `MARQUEE_TRANSCODE_DIR` | `/transcode` | Transcoding scratch space |
| `MARQUEE_RECORDINGS_DIR` | `/recordings` | Live TV recordings |
| `MARQUEE_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `MARQUEE_FFMPEG`, `MARQUEE_FFPROBE` | bundled | Custom FFmpeg binaries |
| `MARQUEE_SOUNDPRINT_URL` | `http://127.0.0.1:32501` | The Soundprint service |
| `MARQUEE_PLEX_DIR` | `/plex` | A read-only Plex data folder to import from |

**Ports**

| Port | Used for |
|---|---|
| `32500/tcp` | Web app and API (the only port you need to expose) |
| `5353/udp` | Bonjour/mDNS, so the apps find the server on your network |
| `32501/tcp` | Soundprint, bound to `127.0.0.1` only |

## Updating, backups and data

**Update** to the latest version:

```bash
cd marquee
git pull
cd deploy
docker compose up -d --build        # add your -f override files as when you installed
docker image prune -f               # optional: remove the old images
```

Database changes are applied automatically on start. A backup is taken first.

**Backups:** Marquee backs up its database every night (keep as many days as you like under **Settings → Scheduled Tasks**), and you can make or restore one there at any time. Backups live in `config/backups/`. To move or rebuild a server, copy the whole `config` folder.

**Where things live**

| Folder | Contents |
|---|---|
| `config/marquee.db` | The database: libraries, people, watch history, ratings, settings |
| `config/backups/` | Database backups |
| `config/logs/` | Logs (also under **Settings → Logs**) |
| `config/subtitles/`, `config/avatars/` | Downloaded subtitles and profile pictures (keep these) |
| `config/` (the rest) | Artwork, thumbnails and other caches; safe to delete, rebuilt as needed |
| `models/` | Soundprint's models |

## Troubleshooting

More problems and fixes are in the guide's [troubleshooting page](guide/troubleshooting.md).

<details>
<summary><b>The apps don't find the server</b></summary>

Automatic discovery uses Bonjour/mDNS, which needs `network_mode: host` (as in the compose file) and the phone or TV on the same network. If your network blocks multicast, type the server's address in the app instead.
</details>

<details>
<summary><b>Videos transcode on the CPU even though I have a GPU</b></summary>

Check that you started with the right override file (`compose.nvidia.yaml` or `compose.intel.yaml`). For NVIDIA, `docker run --rm --runtime=nvidia nvidia/cuda:12.8.0-base-ubuntu24.04 nvidia-smi` must work on the host. For Intel, `RENDER_GID` must match the group of `/dev/dri/renderD128`. The Dashboard shows the encoder each session uses.
</details>

<details>
<summary><b>Libraries are empty or files are skipped</b></summary>

The container runs as `PUID:PGID`; that user needs read access to every media folder. Check the paths inside the container (`/media/video/...`), and look at **Settings → Logs** for files that couldn't be read or probed.
</details>

<details>
<summary><b>Nothing gets matched</b></summary>

Matching needs a TMDB key (**Settings → Metadata**). Use **Fix Match** on anything matched wrongly, and include the year in folder names.
</details>

<details>
<summary><b>Music radios and Muse say analysis isn't running</b></summary>

The Soundprint container must be running (`docker compose ps`). Analysis of a large library takes a while the first time; progress is shown under **Settings → Music**.
</details>

<details>
<summary><b>Casting from the web app</b></summary>

Browsers only allow Chromecast from HTTPS pages, so casting works from the iPhone, iPad and Android apps, or from the web app behind an HTTPS reverse proxy.
</details>

## Building from source

| Part | Requirements | Commands |
|---|---|---|
| Server | Go 1.26, FFmpeg with libass | `cd server && go generate ./... && go test ./... && go run ./cmd/marquee` |
| Web | Node 24 | `cd web && npm ci && npm run gen:api && npm run build` (the output is embedded in the server) |
| iPhone, iPad, Apple TV | Xcode 26, [XcodeGen](https://github.com/yonaskolb/XcodeGen) | `cd apple && scripts/fetch-cast-sdk.sh && scripts/gen-api.sh && xcodegen generate`, then open `Marquee.xcodeproj`, set your team and run |
| Android | JDK 17, Android SDK 35 | `cd android && scripts/gen-api.sh && ./gradlew :app:assembleRelease` |

The API is defined in [`api/openapi.yaml`](api/openapi.yaml); the server stubs and every client are generated from it, so API changes start there.

**End-to-end tests:** `scripts/e2e/setup.sh` builds a local test server with generated media, users and fake Live TV and Bazarr services, and `scripts/e2e/with-server.sh '<command>'` runs a command against it. See [`scripts/e2e/README.md`](scripts/e2e/README.md).

## Repository layout

| Path | Contents |
|---|---|
| `api/openapi.yaml` | The API contract |
| `server/` | Go server (`cmd/marquee`, `internal/…`) |
| `web/` | React web app, built into the server |
| `apple/` | iPhone, iPad and Apple TV apps (SwiftUI, `MarqueeKit` package) |
| `android/` | Android phone, tablet and TV app (Kotlin, Jetpack Compose) |
| `soundprint/` | Music analysis and text-embedding service (Python) |
| `deploy/` | Dockerfile, compose files and example settings |
| `guide/` | The user guide: install, setup and every setting |
| `scripts/` | Development helpers: end-to-end test setup, fake IPTV and Bazarr servers |

## Contributing

Bug reports and pull requests are welcome.

- **Bugs:** open an [issue](https://github.com/pingpongshow/marquee/issues) with what you did, what happened, the app and server versions (**Settings → Dashboard** shows the server's) and anything relevant from **Settings → Logs**.
- **Pull requests:** keep them focused. Change the API in `api/openapi.yaml` first and regenerate the clients; never edit generated code by hand. Run the tests for what you changed (`go test ./...`, `npm test`, and the app's UI tests for the screens you touched).
- By contributing you agree that your contribution is licensed under the AGPL-3.0, like the rest of the project.

## Security

Please don't report security problems in public issues. Use GitHub's [private vulnerability reporting](https://github.com/pingpongshow/marquee/security/advisories/new) instead.

Marquee serves plain HTTP on your home network by default. If you expose it to the internet directly, put it behind an HTTPS reverse proxy; over Tailscale, traffic is already encrypted.

## Acknowledgements

Marquee is built on [FFmpeg](https://ffmpeg.org), [SQLite](https://sqlite.org), [Go](https://go.dev), [React](https://react.dev), SwiftUI, [Jetpack Compose](https://developer.android.com/compose) and [Media3](https://developer.android.com/media/media3), with music analysis by [CLAP](https://huggingface.co/laion/larger_clap_general) and text embeddings by [BGE](https://huggingface.co/BAAI/bge-base-en-v1.5).

Metadata comes from [TMDB](https://www.themoviedb.org), [AniList](https://anilist.co), [MusicBrainz](https://musicbrainz.org), [ListenBrainz](https://listenbrainz.org), [Deezer](https://www.deezer.com), [Wikipedia](https://www.wikipedia.org), [OMDb](https://www.omdbapi.com), [Fanart.tv](https://fanart.tv) and [LRCLIB](https://lrclib.net).

<img src="https://www.themoviedb.org/assets/2/v4/logos/v2/blue_short-8e7b30f73a4020692ccca9c88bafe5dcb6f8a62a4c6bc55cd9ba82bb2cd95f6c.svg" height="14" alt="TMDB"> This product uses the TMDB API but is not endorsed or certified by TMDB.

## License

Marquee is free software under the [GNU Affero General Public License v3.0](LICENSE): you can use, study, modify and self-host it. If you distribute a modified version, or run one as a service for other people, you must publish your changes under the same license. See [NOTICE](NOTICE) for the copyright notice and a linking exception for Google's Cast SDK.
