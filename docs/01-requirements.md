# Requirements

_Last updated: 2026-10-02_

Priority key:
- **P0:** required for the first usable release.
- **P1:** required for "on par with Plex" (v1.0).
- **P2:** later.

## 1. Libraries & media types

| ID | Requirement | Pri |
|---|---|---|
| LIB-1 | Library types: Movies, TV Shows, Anime (TV variant), Music, Other/Personal Videos. Photos are reserved in the schema. | P0 |
| LIB-2 | Multiple folders per library, and multiple libraries per type. | P0 |
| LIB-3 | Scanner parses Plex-style naming (`Movie (Year)/Movie (Year).mkv`, `Show/Season 01/Show - S01E02.mkv`, `{tmdb-123}` / `{imdb-tt…}` hints). | P0 |
| LIB-4 | Real-time file watching plus scheduled full scans. Must detect adds, removes, renames and moves without losing watch state. | P0 |
| LIB-5 | Media probe on import with ffprobe: container, codecs, resolution, HDR/DV, bitrate, audio and subtitle tracks, and chapters. | P0 |
| LIB-6 | External subtitles (`.srt`, `.ass`, `.ssa`, `.vtt`, `.sup`) linked by filename and language tag. | P0 |
| LIB-7 | Multiple versions/editions of the same item (4K + 1080p, Director's Cut). | P1 |
| LIB-8 | Local extras: trailers, featurettes, behind-the-scenes and theme music. | P1 |
| LIB-9 | Handles offline drives gracefully: items go "unavailable" rather than being deleted. A trash/empty step is required to remove them. | P0 |
| LIB-10 | Photos library (EXIF, albums, timeline). Immich already serves photos on the host, so this may stay out of scope. | P2 |
| LIB-11 | Configurable ignore patterns (e.g. `*_staging.*`, `.partial`, sample files) and tolerance of loose files at the library root. | P0 |
| LIB-12 | Music artist and album come from embedded tags (including multi-artist/`ARTISTS` tags). Folder names are only a fallback, because real folders are comma-joined collaborator lists. | P0 |

## 2. Metadata

| ID | Requirement | Pri |
|---|---|---|
| META-1 | Movies and TV from TMDB (primary). TVDB is optional as an alternate ordering source. | P0 |
| META-2 | Anime: AniList/AniDB enrichment and the anime-lists mapping (AniDB↔TVDB↔TMDB). Per-show episode ordering: aired, absolute or DVD. | P0 |
| META-3 | Music: embedded tags first, then MusicBrainz, Cover Art Archive and Fanart.tv for artist art. ✅ Tags, Deezer art (D26) and MusicBrainz genres, release types and dates (D77). | P0 |
| META-4 | Personal videos: folder and filename based. Date comes from file metadata. Thumbnails are extracted from a video frame. | P0 |
| META-5 | Fix Match / Unmatch / manual search, edit any field, lock edited fields, and custom posters/backgrounds. | P0 |
| META-6 | Cast and crew with photos, plus person pages that list their filmography in your library. | P1 |
| META-7 | Collections: manual and automatic (TMDB collections). Smart collections are filter-based. | P1 |
| META-8 | Ratings (critic/audience), genres, studios, content ratings and tags. | P0 |
| META-9 | Lyrics: embedded, `.lrc` sidecar and LRCLIB, with timed lyrics. | P1 |
| META-10 | Local artwork (`poster.jpg`, `fanart.jpg`, `folder.jpg`) takes precedence. | P0 |
| META-11 | Metadata language preference, with per-library overrides. | P1 |

## 3. Playback & streaming (server)

| ID | Requirement | Pri |
|---|---|---|
| PLAY-1 | **Playback decision engine:** compare the client's device profile and bandwidth with the media, then choose Direct Play, Direct Stream (remux) or Transcode. | P0 |
| PLAY-2 | Direct Play over HTTP with range requests. | P0 |
| PLAY-3 | Direct Stream: remux to HLS (fMP4) with stream copy. Used for MKV to Apple devices, for example. | P0 |
| PLAY-4 | Transcode to HLS. Video: H.264/HEVC (AV1 later). Audio: AAC, AC3, EAC3 or Opus. Seeking restarts the encoder at the requested position. | P0 |
| PLAY-5 | Hardware acceleration: NVIDIA NVENC/NVDEC first (production has an RTX 5090), then Intel QSV, then automatic software fallback. AMD VAAPI is P2. | P0 |
| PLAY-6 | HDR→SDR tone mapping when transcoding for SDR clients. No 4K/HDR/DV in the library yet, but it must be supported later (schema and decision engine model HDR/DV from day one). | P1 |
| PLAY-7 | Subtitles: pass through text subtitles as WebVTT. Deliver ASS/SSA styled (with embedded fonts) to clients that can render it. Burn in image subtitles (PGS/VobSub). | P0 |
| PLAY-8 | Audio and subtitle track selection, with per-user language preferences and "remember per show". | P0 |
| PLAY-9 | **Automatic quality selection.** The server picks the best bitrate per session from the network class (local or remote), measured client bandwidth, server settings, user limits and client settings. See §3a. | P0 |
| PLAY-15 | Adaptive bitrate during playback (implemented, D53): multi-rung HLS ladder for remote sessions. The player switches rungs as throughput changes, and the server re-evaluates the session ceiling when bandwidth or the number of concurrent remote streams changes. | P0 |
| PLAY-16 | Encoder capacity awareness: track active NVENC sessions (the consumer driver limit is about 8 concurrent encodes), overflow to QSV and then CPU, and queue or downgrade rather than fail. | P0 |
| PLAY-10 | Music streaming: direct play, or transcode to AAC/Opus at a selectable bitrate. Supports gapless playback and ReplayGain. | P0 |
| PLAY-11 | Concurrent transcode limit, transcode throttling (stay N seconds ahead) and cleanup of the temporary transcode directory. | P0 |
| PLAY-12 | Chapters, plus intro and credits markers with "Skip Intro" / "Skip Credits". | P1 |
| PLAY-13 | Trickplay: seek-bar thumbnail previews. | P1 |
| PLAY-14 | Pre-generated "optimized versions" for remote or mobile use. | P2 |

## 3a. Automatic quality & network awareness

**Network classification** (per request, server-side):
- **Local:** source IP is in the configured LAN subnets (default `10.1.1.0/24`, editable in Settings → Network). Local clients connect directly to `http://10.1.1.10:32500`. **Tailscale is never used on the LAN.**
- **Remote:** everything else, including Tailscale (`100.64.0.0/10`) and any `*.ts.net` hostname. Only remote clients use Tailscale.
- Clients try the LAN address first and fall back to the Tailscale address. They re-check when the network changes (Wi-Fi to cellular, leaving home).

**Quality selection.** Target bitrate = the minimum of:
1. **Client setting:** "Local quality" / "Remote quality". The default is *Original* for local and *Auto* for remote, or a fixed cap such as 1080p 8 Mbps or 720p 4 Mbps.
2. **Per-user remote limit**, set by the admin.
3. **Measured bandwidth × headroom (≈0.7).** The client runs a short bandwidth probe at playback start and reports ongoing HLS throughput.
4. **Fair share of the server's internet upload budget.** The admin enters the upload speed in Settings → Remote Access. It is divided across active remote streams, with a minimum per stream.
5. **Source bitrate.** Never transcode up. If everything allows the original and the client can decode it, use Direct Play or Direct Stream.

**Server settings** (Settings → Transcoder / Remote Access):
- Internet upload speed, total remote bandwidth limit, per-stream remote limit.
- Default remote quality ladder (e.g. 1080p 12 Mbps, 1080p 8, 720p 4, 480p 1.5, 360p 0.75).
- Prefer HEVC for remote clients that support it (≈40% less bandwidth at the same quality). AV1 is offered later if the client supports it.
- Transcoder quality/speed preset, hardware encoder order, max simultaneous transcodes.
- Local: never transcode for bandwidth (only for codec/container/subtitle compatibility).

**Transparency:** every session shows the decision and its reason in the dashboard and the player's info panel (e.g. "Transcode 4.1 Mbps HEVC: remote, measured 6 Mbps").

## 3b. Music (Plexamp-class)

The owner wants the best music experience possible: everything Plexamp and Plex's Sonic features do, on web, Apple and Android. Analysis runs on the server (the RTX 5090 is available), so no audio leaves the house.

| ID | Requirement | Pri |
|---|---|---|
| MUSIC-1 | **Sonic analysis:** every track gets an audio embedding plus features (BPM, key, energy, danceability, mood, loudness), computed in the background on the server with GPU acceleration. New tracks are analysed as they are added. | P1 |
| MUSIC-2 | **Sonically similar:** similar tracks, albums and artists by sound (not just tags), shown on track, album and artist pages. | P1 |
| MUSIC-3 | **Radios:** track, album, artist, genre, mood, decade ("Time Travel") and library radio. Endless, and steered by sonic similarity, ratings, skips and play history. | P1 |
| MUSIC-4 | **Sonic Adventure:** a playlist that travels smoothly from one track to another through sonically adjacent tracks. | P1 |
| MUSIC-5 | **Muse (natural-language playlists):** "rainy Sunday jazz with a late-night feel" builds a playlist from your library. Text-to-audio embeddings run locally; an optional LLM (admin-supplied API key) interprets longer prompts. | P1 |
| MUSIC-6 | **Guest DJ / smart play:** optional modes that weave related tracks, deeper cuts or the artist's other albums into whatever is playing. | P1 |
| MUSIC-7 | **Mixes for you:** daily mixes per user (by taste clusters), "Rediscover" (loved but not played lately), "Deep cuts", "Recently added mix", mood mixes and anniversary albums on Home. | P1 |
| MUSIC-8 | **Smart playlists:** rule-based (genre, artist, year, rating, play count, last played, added date, mood, BPM, key…) with limits and sort, kept up to date automatically. | P1 |
| MUSIC-9 | **Playback quality:** gapless playback, loudness levelling (ReplayGain tags or EBU R128 analysis, track/album mode), optional crossfade and "sweet fades" that respect gapless albums, and a 10-band EQ on clients that support it. | P1 |
| MUSIC-10 | **Lyrics:** embedded, `.lrc` sidecars and LRCLIB, with synced, scrolling lyrics in the player (also META-9). | P1 |
| MUSIC-11 | **Ratings and taste:** 1–5 star ratings in half stars (every client can set halves, not only show them) / love ratings on tracks, albums and artists. Skips and plays feed recommendations. Per-user listening stats ("Year in music"). | P1 |
| MUSIC-12 | **Scrobbling:** optional Last.fm and ListenBrainz scrobbling per user. | P2 |
| MUSIC-13 | **Plexamp-style player:** full-screen now playing with artwork-coloured backgrounds, queue editing (drag to reorder, play next, add to queue), shuffle/repeat, sleep timer, waveform or visualizer, and a mini-player. | P1 |
| MUSIC-14 | **Apple and Android:** offline downloads with smart sync (e.g. "keep my top 500 and today's mix"), CarPlay / Android Auto, lock-screen and widget controls, and Siri / Assistant "play … on Marquee" where the platforms allow. | P1 |
| MUSIC-15 | **Discovery:** artist bios and photos, similar artists in your library, popular tracks (via Last.fm/ListenBrainz data) for an artist, and "Recently played" / "Most played" hubs. | P1 |
| MUSIC-16 | **Muse (natural-language playlists) under Marquee's own name:** the feature Plex calls "Sonic Sage" is **Muse** across the server, API (`/music/muse`), web, Apple and Android apps and the docs (Q17). | P1 |
| MUSIC-17 | **Mixes for you from listening history:** curated mixes like Plexamp's, from what each person plays, skips and rates (taste clusters, "discovery" of rarely played library tracks near their taste, rediscover, decade and mood mixes), refreshed daily. Builds on MUSIC-7. | P1 |
| MUSIC-18 | **Browse and play by mood and style:** Plexamp-style mood, style and genre pages with stations and playlists for each (moods from sonic analysis and tags, styles from tags/MusicBrainz), on every client. | P1 |
| MUSIC-19 | **Playlist downloads:** download whole playlists (including smart playlists and mixes) for offline playback on Apple and Android, kept in sync as they change. | P1 |

## 3b-2. Watch together

| ID | Requirement | Pri |
|---|---|---|
| SYNC-1 | **Watch together (SyncPlay):** people on the server watch the same title in step on any client: shared play, pause and seek, everyone waits while someone loads, and others can join a group in progress. | P1 |

## 3c. Live TV

Plex-style Live TV on every client (see the owner's reference screenshot: Guide / What's On tabs, a live preview with "Now On", a time-scrolling guide grid with channel logos, favourites and a now line).

| ID | Requirement | Pri |
|---|---|---|
| LIVE-1 | **Sources:** M3U playlists with XMLTV guides (admin-added), free ad-supported channels (Pluto TV and similar, where their terms allow), and an optional **Dispatcharr** integration (its channels, groups, logos and EPG) when the admin sets it up. | P1 |
| LIVE-2 | **Guide:** a grid of channels × time with now/next, a "now" line, day picker, jump back/forward, categories and favourites; plus a "What's On" view of what's airing now. | P1 |
| LIVE-3 | **Watching:** tune a channel with a muted live preview in the guide, full screen, channel up/down, and the server remuxing or transcoding streams for each client like any other playback (LAN and Tailscale). | P1 |
| LIVE-4 | **Per-user:** favourite channels, hidden channels, recently watched, and access rules (e.g. kids' profiles). ✅ D78. | P1 |
| LIVE-5 | **DVR:** record programmes and series to a library. ✅ D76. | P2 |

## 3d. Requests (Seerr)

| ID | Requirement | Pri |
|---|---|---|
| REQ-1 | **Seerr integration:** the admin connects the Seerr instance on the server (URL and API key). Users search for movies and shows that aren't in the library and request them from any Marquee client; request status shows in the app. Marquee users map to Seerr users so quotas and approvals apply. | P1 |
| REQ-2 | **Discover:** trending, popular and upcoming titles from Seerr, marked "In library", "Requested" or "Request", in a Discover tab. | P2 |

## 4. Users, auth & state

| ID | Requirement | Pri |
|---|---|---|
| USER-1 | Local accounts. The first-run setup creates the admin. No cloud account is required. | P0 |
| USER-2 | Managed users (Plex Home equivalent): PIN switching, content rating limits and library restrictions. | P0 |
| USER-10 | **PIN sign-in (Plex-style):** a "Who's watching?" profile picker on the sign-in screen. Passwords are required only for administrators. PIN and password are optional for everyone else. Profiles with a PIN sign in with it, profiles with only a password use it, and profiles with neither open with one tap (Plex Home style). The admin chooses where it's allowed: home network only (default), everywhere, or off. Five wrong PINs lock the profile for 15 minutes. | P0 |
| USER-11 | **Profile pictures (optional):** users upload a picture for themselves, and admins for anyone. The user drags and zooms it inside the round frame used for profile bubbles, which show it on the sign-in picker, the profile switcher, the account menu and the user list. Users without one show their initial. | P1 |
| USER-3 | Per-device sessions and tokens that can be listed and revoked. | P0 |
| USER-4 | TV-friendly login: Apple TV shows a code and you approve it from your phone or web ("Quick Connect"). Implemented (D51). | P1 |
| USER-5 | Per-user watch state: resume offset, played/unplayed, play count, last watched, and ratings. | P0 |
| USER-6 | Continue Watching / On Deck, Recently Added and Up Next (next episode). | P0 |
| USER-7 | Playlists for video and music, plus smart playlists (smart playlists: MUSIC-8, M6.5). | P1 |
| USER-8 | Watchlist. | P1 |
| USER-9 | TOTP two-factor auth for admin/WAN logins. ✅ Any password account can turn it on (D75). | P2 |

## 5. Remote access (WAN)

| ID | Requirement | Pri |
|---|---|---|
| WAN-1 | Built-in HTTPS with automatic Let's Encrypt certificates when a domain is configured. Not needed while Tailscale is used. | P2 |
| WAN-2 | **Tailscale is the remote path**, using the existing host install unchanged. Remote clients connect to `http://<host>.<tailnet>.ts.net:32500`, encrypted by WireGuard. Reverse-proxy docs are P2. | P0 |
| WAN-3 | Clients store multiple server addresses (LAN `http://10.1.1.10:32500` and Tailscale `http://dingdong.tailb5b08.ts.net:32500`). LAN is preferred and Tailscale is used only when the LAN is unreachable. Re-evaluated on network change. | P0 |
| WAN-4 | Per-user and global remote bandwidth caps, plus separate default quality for local and remote (see §3a). | P0 |
| WAN-5 | Login rate-limiting and lockout, short-lived signed stream URLs, and no filesystem paths exposed via the API. | P0 |
| WAN-6 | Remote access self-test in the admin UI ("Is my server reachable from outside?"). | P2 |

## 6. Clients

### Common to all clients (P0 unless marked)
- Plex-like layout: a sidebar or top navigation of libraries, and a Home screen with horizontal "hubs" (Continue Watching, Recently Added per library, On Deck, Collections).
- Library grid with sort, filter, an A–Z jump bar, and poster/list/detail views.
- Detail pages for movies, shows, seasons, episodes, artists and albums. They include a hero backdrop, metadata, cast and related items.
- Global search across all libraries (instant results).
- Video player: track and subtitle pickers, quality selector, skip intro/credits, Up Next autoplay, trickplay thumbnails (P1), and resume.
- Music player: queue, shuffle/repeat, gapless playback, lyrics (P1) and a mini-player.
- User switching with PIN.
- Server discovery on the LAN (Bonjour/mDNS) and manual server URL entry.

### Web (P0)
- Desktop and mobile responsive. Keyboard shortcuts.
- Admin/settings area: libraries, users, transcoder, remote access, scheduled tasks, logs, and a dashboard showing now playing, bandwidth and active transcodes.
- Media Session API for OS media keys.
- Cast to Chromecast (P2).

### iOS / iPadOS (P0)
- Native look and feel. Picture-in-Picture, AirPlay, background audio, lock-screen controls and Now Playing info.
- Offline downloads with progress sync when back online (P1).
- CarPlay audio (P2).

### tvOS (P0)
- Focus-engine navigation, top-shelf extension (P1), and Siri Remote scrubbing.
- 4K HDR10 / Dolby Vision / Atmos direct play where the device supports it.
- Match frame rate and dynamic range.

### Android / Android TV (P1, built 2026-10-02)
- Same API and feature set as the Apple apps. One app for phones, tablets and Android TV (D66).

## 7. Server administration

| ID | Requirement | Pri |
|---|---|---|
| ADM-1 | Web-based first-run setup wizard: create admin, name the server, add libraries, optional Plex import. | P0 |
| ADM-8 | **Settings area organized like Plex**, with a left-hand settings nav grouped into sections (see §7a), search across settings, inline help text, and safe defaults. Changes apply live where possible, with a clear "restart required" notice otherwise. | P0 |
| ADM-9 | **Library management in the web dashboard:** add/edit/delete libraries, a server-side folder browser for picking paths, per-library agent/language/ordering, scan, refresh metadata, empty trash and scan status/progress. | P0 |
| ADM-10 | **Dashboard:** now playing (user, device, local/remote, decision, bitrate, encoder), bandwidth graphs (local vs remote), active transcodes and GPU/CPU encoder usage, recently added, alerts. | P0 |
| ADM-2 | Scheduled tasks: scans, metadata refresh, DB optimize/backup, trickplay/intro generation and transcode cleanup. | P0 |
| ADM-3 | Automatic DB backups with retention, and restore. | P0 |
| ADM-4 | Activity/history log and playback statistics (Tautulli-like). | P1 |
| ADM-5 | Webhooks / notifications on play, stop and library add. | P1 |
| ADM-6 | Structured logs viewable in the UI and downloadable. | P0 |
| ADM-7 | Prometheus metrics endpoint. | P2 |

### 7a. Settings structure (web dashboard)

| Section | Contents |
|---|---|
| **Server → General** | Server name, language, version/update info, privacy (no telemetry), restart |
| **Server → Network** | LAN subnets, listen port, LAN address shown to clients, discovery (Bonjour) on/off |
| **Server → Remote Access** | Tailscale URL, internet upload speed, total and per-stream remote limits, connectivity check |
| **Server → Transcoder** | Hardware encoder order and status (NVENC/QSV/CPU detected), max transcodes, quality preset, HEVC for remote, tone mapping, transcode dir, throttling |
| **Server → Libraries** | Library list. Add library wizard (type, name, folders via folder browser, language, agent, episode ordering, advanced: ignore patterns, scanner options, include in Home/search, visibility). Edit, scan, refresh, empty trash |
| **Server → Scheduled Tasks** | Maintenance window, backups (schedule, retention, restore), scans, metadata refresh, trickplay/intro detection toggles |
| **Server → Metadata** | Provider API keys (TMDB, Fanart.tv, OpenSubtitles), provider order per media type, artwork preferences |
| **Server → Plex Import** | Import wizard, path mapping, re-run sync, last import report |
| **Users** | Users and managed profiles, library access, content-rating limits, remote quality limit, PINs, invite/Quick Connect |
| **Devices** | Signed-in devices per user, last seen, revoke |
| **Activity** | Dashboard, history, logs (viewer + download), alerts |
| **Account (per user)** | Profile, password/PIN, playback languages, subtitle style, local/remote quality defaults |

## 8. Plex import

See [04-plex-import.md](04-plex-import.md). Summary (P0): import libraries and paths, matches (GUIDs), edited metadata, custom artwork, collections, playlists, users, and per-user watch state and resume points. The import is repeatable and supports path remapping for Docker.

## 9. Non-functional requirements

| Area | Target |
|---|---|
| Performance | Home screen API under 150 ms on a 50k-item library. Search responds in under 100 ms. First frame on LAN direct play in under 1.5 s. Initial scan above 500 files/min (without metadata fetch). |
| Scale | 100k+ video items and 500k+ tracks on SQLite. **10–25 users**, ~10 concurrent streams with at least 6 simultaneous remote transcodes. |
| Resource use | Idle RAM under 200 MB. Image cache and transcode dir are size-bounded. |
| Reliability | DB in WAL mode, automatic backups, and graceful shutdown that finishes writes and kills FFmpeg children. |
| Security | HTTPS for WAN, bcrypt/argon2 password hashing, scoped tokens, no path traversal, dependency scanning in CI. |
| Upgradability | Versioned DB migrations run automatically on startup with a pre-migration backup. API is versioned (`/api/v1`). Clients tolerate unknown fields. |
| Deployment | Multi-arch Docker image (amd64 and arm64). Volumes: `/config`, `/media` (read-only OK), `/transcode`. GPU passthrough documented. |
| Maintainability | OpenAPI spec as the source of truth. Typed generated clients. Unit and integration tests. Architecture decision log. |
| Accessibility | Keyboard and screen-reader navigable web UI, and Dynamic Type / VoiceOver on Apple platforms. |
| Privacy | No telemetry. Outbound calls go only to metadata providers the admin enabled. |
