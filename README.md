<div align="center">

# Marquee

**A self-hosted media server for movies, TV, anime, music and live TV, with apps for the web, iPhone, iPad, Apple TV, Android and Android TV.**

[![Release](https://img.shields.io/github/v/release/pingpongshow/marquee?sort=semver)](https://github.com/pingpongshow/marquee/releases)
![Server](https://img.shields.io/badge/server-Go%20%2B%20Docker-00ADD8)
![Apps](https://img.shields.io/badge/apps-Web%20·%20iOS%20·%20tvOS%20·%20Android-6E56CF)
[![License: AGPL v3](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)

</div>

Marquee runs on your own hardware, streams to your devices at home and away, and keeps everything private: accounts are local, metadata is fetched from public sources, and the GPU features (transcoding, music analysis, natural-language search) run on your server.

---

## Contents

- [Features](#features)
- [Apps](#apps)
- [Install the server](#install-the-server)
- [Install the apps](#install-the-apps)
- [Remote access](#remote-access)
- [Configuration](#configuration)
- [Building from source](#building-from-source)
- [Repository layout](#repository-layout)
- [Documentation](#documentation)
- [License](#license)

## Features

### Libraries and metadata
- Movies, TV shows, anime, music and home videos, scanned from your folders and kept current as files change.
- Metadata and artwork from TMDB (with OMDb, IMDb, Rotten Tomatoes and Metacritic ratings), AniList for anime, and MusicBrainz, Deezer, Wikipedia and ListenBrainz for music.
- Collections (automatic film series, manual, and **smart collections** built from filters), extras and trailers, multiple versions and editions, and cast and crew pages.
- Fix Match, metadata editing with locked fields, and custom artwork.
- **Library Health:** duplicates (compare every copy's file details and move the one you don't want to a recoverable trash), unmatched items, missing or unplayable files, upgrade candidates, playback errors, missing artwork and missing subtitles.
- Import watch history, ratings, playlists, markers and accounts from an existing Plex server.

### Playback
- Direct play, direct stream (remux) or transcode, chosen per device and network: NVIDIA NVENC, Intel Quick Sync or the CPU, with HDR-to-SDR tone mapping.
- Automatic quality for remote viewers, with adaptive bitrate and a fair share of upload bandwidth.
- Subtitles: text, styled ASS with embedded fonts, and burned-in image subtitles. Each person chooses size, colour, background and position, and can adjust subtitle and audio timing (remembered per file). Search OpenSubtitles or use **Bazarr**.
- Skip Intro / Skip Credits (detected automatically), chapters, seek-bar thumbnails, playback speed, Up Next, and cinema trailers before movies.
- Watch together: shared play, pause and seek across devices.
- **Remote control:** send what you're watching to another open Marquee app ("Play on…") and control it from your phone.
- Chromecast from iPhone, iPad and Android; AirPlay on Apple devices.
- Offline downloads on iPhone, iPad and Android, optionally converted on the server.

### Music
- **Soundprint:** every track is analysed on the server's GPU, powering radios, "sounds like this", **Muse** (describe a playlist in words), **Sound Journey** (a path from one track to another), the DJ, and mood and style pages.
- Daily mixes from your listening, artist bios and popular tracks, smart playlists, synced lyrics, ReplayGain/loudness levelling, crossfade and gapless playback, an equaliser, a car mode, and **Your Year in Music**.
- Scrobbling to ListenBrainz and Last.fm.

### Discover and personalise
- **Muse for movies:** "90s sci-fi with time travel" or "acclaimed war films I haven't seen" finds matches in your library.
- "Recommended for You" and "Because you watched…" rows, and "More like this".
- A Home screen each person can reorder, hide and pin rows on.
- Watchlist, ratings, statistics, and requests through Seerr (approved by an admin).

### Live TV
- A channel guide from any M3U/XMLTV source or Dispatcharr, What's On, favourites, and per-profile channel groups.
- DVR: one-off and series recordings, which become normal library items.

### People and administration
- Local accounts with profiles and optional PINs, managed profiles for children (rating and library limits), two-factor sign-in, and Quick Connect for TVs.
- Share with friends through one-time invite links.
- Dashboard, activity, logs, scheduled maintenance tasks, backups and restore, webhooks and Prometheus metrics.

## Apps

| Platform | Highlights |
|---|---|
| **Web** | Everything, including settings and administration. Built into the server. |
| **iPhone and iPad** | Downloads, AirPlay and Chromecast, car mode, remote control. |
| **Apple TV** | Top Shelf, Quick Connect sign-in, remote-controllable. |
| **Android phone and tablet** | Downloads, Android Auto, Chromecast, car mode, remote control. |
| **Android TV** | A D-pad interface with the same features as phones, remote-controllable. |

## Install the server

Marquee runs as a Docker container (plus an optional companion container for the GPU music and search features).

**Requirements**
- Linux with Docker and Docker Compose (tested on Unraid).
- Optional: an NVIDIA GPU with the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/) for hardware transcoding and the Soundprint/Muse features, and/or an Intel CPU with Quick Sync (`/dev/dri`).
- A free [TMDB API key](https://www.themoviedb.org/settings/api) for movie and TV metadata.

**1. Get the source**

```bash
git clone https://github.com/pingpongshow/marquee.git
cd marquee
```

**2. Create a folder for Marquee and a compose file**

```bash
mkdir -p /opt/marquee && cp deploy/compose.yaml deploy/.env.example /opt/marquee/
cd /opt/marquee && mv .env.example .env
ln -s /path/to/marquee src   # the compose file builds from ./src
```

Edit `compose.yaml` to point at your media:

```yaml
    volumes:
      - ./config:/config
      - /path/to/Video:/media/video        # read-write only if you want Library Health to trash duplicates
      - /path/to/Music:/media/music:ro
```

- **Without an NVIDIA GPU:** remove the `runtime: nvidia` lines (and the `soundprint` service if you don't want the music analysis).
- **Without Intel Quick Sync:** remove the `devices` and `group_add` entries.

Set `PUID`/`PGID` (the user that owns your media), `TZ` and `MARQUEE_VERSION` in `.env`.

**3. Start it**

```bash
docker compose up -d --build
```

Open `http://<server>:32500` and follow the setup: create the admin account, add libraries (Movies, Shows, Anime, Music, Videos) and enter your TMDB key under **Settings → Metadata**.

## Install the apps

Download the apps from the [latest release](https://github.com/pingpongshow/marquee/releases/latest).

| App | How to install |
|---|---|
| **Android / Android TV** | Download `Marquee-<version>.apk` and open it (allow installing from your browser or file manager). On Android TV, sideload it with a file manager or `adb install`. |
| **iPhone / iPad** | `Marquee-iOS-<version>-unsigned.ipa` is unsigned. Install it with a sideloading tool such as [AltStore](https://altstore.io) or [Sideloadly](https://sideloadly.io) (signed with your Apple ID), or build it yourself in Xcode (see below). |
| **Apple TV** | `Marquee-tvOS-<version>-unsigned.ipa`: install with Sideloadly, or build in Xcode. |

The apps find the server on your network automatically (Bonjour/NSD), or you can type its address.

## Remote access

Marquee works over any connection that reaches port 32500. The simplest private option is [Tailscale](https://tailscale.com): install it on the server and your devices, and add the server's Tailscale address in the apps as a second address. They use the home address when they can reach it, and the Tailscale one otherwise. Remote streams adapt their quality to the connection.

To give friends access, share the server's machine with them from the Tailscale admin console and send them an invite link from **Settings → Users**.

## Configuration

Everything is configured in the web app under **Settings**. Optional integrations:

| Integration | Where | What it adds |
|---|---|---|
| TMDB, OMDb, Fanart.tv, OpenSubtitles | Settings → Metadata | Metadata, ratings, artwork, subtitle search |
| Seerr | Settings → Requests | Discover and request titles |
| Bazarr | Settings → Requests | Subtitle status, downloads and search from the player |
| Dispatcharr or M3U/XMLTV | Settings → Live TV | Live TV and DVR |
| Last.fm, ListenBrainz | Settings → Music / Account | Scrobbling |
| Webhooks, Prometheus | Settings → Webhooks / `/api/v1/system/metrics` | Automation and monitoring |

Environment variables (all optional): `MARQUEE_PORT` (default 32500), `MARQUEE_CONFIG_DIR` (`/config`), `MARQUEE_TRANSCODE_DIR`, `MARQUEE_RECORDINGS_DIR`, `MARQUEE_LOG_LEVEL`, `MARQUEE_FFMPEG`/`MARQUEE_FFPROBE`, `MARQUEE_SOUNDPRINT_URL` (default `http://127.0.0.1:32501`), `MARQUEE_PLEX_DIR` (a read-only Plex data folder to import from).

## Building from source

| Part | Requirements | Commands |
|---|---|---|
| Server | Go 1.26, FFmpeg with libass | `cd server && go generate ./... && go test ./... && go run ./cmd/marquee` |
| Web | Node 24 | `cd web && npm ci && npm run gen:api && npm run build` (output is embedded in the server) |
| iPhone, iPad, Apple TV (iOS/tvOS 18+) | Xcode 26, [XcodeGen](https://github.com/yonaskolb/XcodeGen) | `cd apple && scripts/fetch-cast-sdk.sh && scripts/gen-api.sh && xcodegen generate`, then open `Marquee.xcodeproj`, set your team and run |
| Android | JDK 17, Android SDK 35 | `cd android && scripts/gen-api.sh && ./gradlew :app:assembleRelease` |

The API is defined in [`api/openapi.yaml`](api/openapi.yaml); the server stubs and every client are generated from it.

## Repository layout

| Path | Contents |
|---|---|
| `api/openapi.yaml` | The API contract |
| `server/` | Go server (`cmd/marquee`, `internal/…`) |
| `web/` | React web app, built into the server |
| `apple/` | iPhone, iPad and Apple TV apps (SwiftUI, `MarqueeKit` package) |
| `android/` | Android phone, tablet and TV app (Kotlin, Jetpack Compose) |
| `soundprint/` | Music analysis and text-embedding companion service (Python) |
| `deploy/` | Dockerfile and compose file |
| `scripts/` | Development helpers (fake IPTV and Bazarr servers for testing) |
| `docs/` | Requirements, architecture, roadmap and decision log |

## Documentation

- [Requirements](docs/01-requirements.md)
- [Architecture](docs/03-architecture.md)
- [Roadmap](docs/05-roadmap.md)
- [Decision log](docs/decision-log.md)
- [Security](docs/08-security.md)

## License

Marquee is free software under the [GNU Affero General Public License v3.0](LICENSE): you can use, study, modify and self-host it. If you distribute a modified version, or run one as a service for other people, you must publish your changes under the same license. See [NOTICE](NOTICE) for the copyright notice and a linking exception for Google's Cast SDK.
