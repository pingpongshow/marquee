# Architecture

_Last updated: 2026-10-01. Status: **Draft**._

## System overview

```
                ┌──────────────────────── Docker container ────────────────────────┐
 Web browser ──►│  HTTP(S) server (Go)                                             │
 iOS/iPadOS  ──►│   ├─ /api/v1/*        REST (OpenAPI)                             │
 tvOS        ──►│   ├─ /api/v1/events   WebSocket                                  │
 Android     ──►│   ├─ /stream/*        Direct play (range) / HLS playlists+segments│
                │   ├─ /images/*        Resized, cached artwork                    │
                │   └─ /               Embedded web app                            │
                │                                                                  │
                │  Core services                                                   │
                │   ├─ Library scanner + fs watcher                                │
                │   ├─ Metadata agents (TMDB, AniList/AniDB, MusicBrainz, …)       │
                │   ├─ Playback decision engine                                    │
                │   ├─ Transcode manager ──► FFmpeg child processes                │
                │   ├─ Session/progress tracker                                    │
                │   ├─ Task scheduler (scans, backups, trickplay, intro detect)    │
                │   ├─ Auth/users                                                  │
                │   └─ Plex importer                                               │
                │                                                                  │
                │  SQLite (WAL)  /config/db    Image cache /config/cache           │
                └──────────────────────────────────────────────────────────────────┘
                    │ /media (ro)        │ /transcode (tmpfs)      │ /plex (ro, import)
```

## Repository layout (monorepo)

```
/api/openapi.yaml          # API contract — source of truth
/server                    # Go module
  /cmd/server              # main
  /internal/{api,auth,library,scanner,metadata,playback,transcode,
             session,tasks,images,plex_import,db,config,events}
  /migrations              # goose SQL migrations
/web                       # React + Vite app (built into server via go:embed)
/apple                     # Xcode workspace
  /Packages/MediaKit       # shared Swift package: API client, models, player, view models
  /iOS                     # iPhone + iPad target
  /tvOS                    # Apple TV target
/android                   # later
/deploy                    # Dockerfile, compose.yaml, reverse-proxy examples
/docs                      # this documentation
```

## Core data model (sketch)

- **library** (id, name, type: movie|show|anime|music|other|photo, settings JSON)
- **library_path** (library_id, path)
- **item**: a polymorphic metadata node with `type` (movie, show, season, episode, artist, album, track, video, collection, photo, …), `parent_id`, `grandparent_id`, `index`, title fields, summary, year, dates, ratings, `guid` set, `locked_fields`, timestamps
- **external_id** (item_id, provider, value): tmdb, tvdb, imdb, anidb, anilist, musicbrainz, plex
- **media_version** (item_id, edition/label): one per version of an item
- **media_file** (version_id, path, size, mtime, container, duration, bitrate, `fingerprint` for rename detection)
- **stream** (file_id, index, kind: video|audio|subtitle, codec, language, title, flags, HDR/DV info, external path)
- **chapter / marker** (file_id, kind: chapter|intro|credits, start, end)
- **person / credit** (item_id, person_id, role, character, order)
- **tag** (genre, studio, collection, label, country, mood) + item_tag
- **artwork** (item_id, kind: poster|backdrop|logo|thumb|banner, source, local cache path, selected)
- **user** (id, name, password_hash, pin, is_admin, is_managed, restrictions JSON, preferences JSON, avatar_version). The profile picture is `<config>/avatars/<id>.jpg`.
- **device / session_token** (user_id, device name, platform, token hash, last_seen)
- **user_item_state** (user_id, item_id, view_offset_ms, play_count, last_viewed_at, rating, is_watchlisted)
- **playlist / playlist_item**
- **play_history** (user, item, device, start/stop, method, bandwidth): powers stats
- **task_run, setting, webhook**

The Plex model maps closely onto this (Plex also uses a polymorphic `metadata_items` tree), which keeps import straightforward.

## Playback flow

1. The client calls `POST /api/v1/playback/decide` with item/version, its **device profile** (supported containers, codecs, profiles/levels, HDR, max resolution, subtitle formats), its quality setting, its measured bandwidth (from a short probe against `GET /api/v1/playback/bandwidth-probe`), and the selected audio/subtitle tracks. The server classifies the request as local or remote by source IP, then computes the target bitrate per requirements §3a (client setting, user cap, measured bandwidth × 0.7, fair share of the upload budget, source bitrate).
2. The decision engine returns one of:
   - **Direct Play:** a signed URL to the raw file (range requests).
   - **Direct Stream:** an HLS master playlist. FFmpeg remuxes with `-c copy`, and transcodes only audio if needed.
   - **Transcode:** an HLS master playlist with one or more bitrate rungs. Encoder order is NVENC, then QSV, then software, with automatic fallback if a hardware job fails (the GPU is shared with AI workloads). Tone mapping and subtitle burn-in are applied as required.
   The response includes the reason (for example "audio codec DTS not supported"), shown in the dashboard and the client's "playback info" panel.
3. For remote transcodes, the master playlist contains a ladder of rungs at or below the ceiling. The player's ABR picks a rung, and progress reports carry observed throughput so the server can lower or raise the ceiling (e.g. when another remote stream starts and the upload fair-share shrinks).
4. The transcode manager starts FFmpeg lazily when the first segment is requested. When the client seeks outside the generated range, it restarts at that position. It throttles ahead of the playhead and kills idle jobs.
5. The client reports progress every ~10 s and on pause/stop/seek (`POST /api/v1/sessions/{id}/progress`). The server updates `user_item_state`, emits WebSocket events and writes `play_history`.
6. Watched threshold: 90% (configurable). Credits markers count as "end" when present.

Stream URLs carry **short-lived signed tokens** in the query string, because `<video>` and AVPlayer can't always send auth headers.

## Media URLs (implemented M3a)

Media isn't served through the generated JSON API. `POST /api/v1/playback/sessions` returns a URL under `/api/v1/stream/{sessionId}/`:

| Path | Purpose |
|---|---|
| `file` | Direct play: the original file with byte-range support |
| `master.m3u8`, `index.m3u8` | HLS (direct stream or transcode). A VOD playlist of fixed 6 s segments generated by the server. |
| `init.mp4`, `{n}.m4s` | fMP4 segments. FFmpeg (re)starts at the requested segment with `-ss n×6 -copyts` and forced keyframes every 6 s, so restarts line up with the playlist. |
| `subtitles/{streamId}.vtt` / `.ass` | Text subtitle as WebVTT, or as ASS for clients that render it (cached per file) |
| `fonts.json`, `fonts/{index}` | Font attachments embedded in the file, for ASS rendering (extracted on demand and cached) |
| `audio` | Progressive AAC for music that can't be played directly |

The session id is a random 128-bit capability that stops working when the session ends (410 Gone). Sessions are reaped after 3 minutes without progress reports.

## Remote access design

- Clients store a **server record** with a stable server ID and a list of candidate URLs (LAN IP, mDNS host, WAN domain). They race the candidates on connect and prefer LAN.
- The server advertises itself via mDNS (`_mediaserver._tcp`) and answers a discovery endpoint `GET /api/v1/system/info` (unauthenticated, minimal).
- LAN clients always connect directly to `http://10.1.1.10:32500` (no Tailscale and no TLS). Apple clients enable local-network HTTP via ATS `NSAllowsLocalNetworking`.
- WAN path (decided): **Tailscale, for remote clients only**, using the host's existing Tailscale unchanged. Remote clients connect to `http://<host>.<tailnet>.ts.net:32500`; WireGuard provides encryption. Built-in ACME and reverse-proxy support remain optional (P2).
- The server classifies each request as LAN or WAN (via configured LAN subnets, e.g. `10.1.1.0/24`. Tailscale `100.64.0.0/10` counts as WAN for quality caps by default but is configurable) to apply remote quality caps.

## Background tasks

| Task | Default schedule |
|---|---|
| Library scan | fs watcher (real time) + nightly full scan |
| Metadata refresh | weekly for recently added items, monthly for everything else |
| DB backup | daily, keep 7 |
| DB optimize (`VACUUM`/`ANALYZE`) | weekly |
| Trickplay generation | nightly, low priority (P1) |
| Intro/credits detection | nightly, low priority (audio fingerprint per season) (P1) |
| Transcode dir cleanup | hourly + on startup |

## Upgrade & compatibility strategy

- **DB:** forward-only goose migrations, numbered. On startup: back up, then migrate, then serve. Never edit a released migration.
- **API:** `/api/v1` is stable. Additive changes only (new fields/endpoints). Breaking changes go to `/api/v2` with v1 kept for at least one major release. `GET /api/v1/system/info` returns the server version and API version, and clients warn when they're too old.
- **Clients:** generated from the spec. Decoders ignore unknown fields and enum values fall back to "unknown".
- **Image tags:** `latest`, `X`, `X.Y` and `X.Y.Z`, so owners can pin a version and are never force-updated.
