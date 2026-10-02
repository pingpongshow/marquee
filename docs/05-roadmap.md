# Roadmap

_Last updated: 2026-10-01_

The build order is designed for **minimal rework**: the contract first, then the server, then the clients against a stable API. Each milestone ends with a working, testable increment.

| # | Milestone | Scope | Exit criteria |
|---|---|---|---|
| M0 ✅ | **Foundations** | Monorepo, CI, `openapi.yaml` v1 draft (all P0 endpoints), DB schema + migrations, Dockerfile + compose, config system, logging | `docker compose up` serves a health endpoint and the web shell. Codegen works for Go and TS (Swift in M5). **Done 2026-10-01**: setup/login, settings and library management deployed to the Unraid host. |
| M1 ✅ | **Library core** | Auth/users/devices, libraries, scanner + watcher, ffprobe, naming parser, TMDB movies/TV, anime mapping, music tags + MusicBrainz, personal videos, image cache, search (FTS5) | Real libraries scan and match. The API returns full metadata trees. |
| M2 ✅ | **Plex import** | Importer + wizard API, path mapping, watch state, users, collections, playlists, artwork | Owner's real Plex library imported and reconciled. |
| M3 ✅ | **Playback** | Decision engine, direct play, remux HLS, NVENC + QSV + software transcode, subtitles (VTT, burn-in), sessions/progress, Continue Watching, Up Next, music streaming | Any library file plays in a browser and in an AVPlayer test harness. |
| M4 | **Web client** | Plex-like home/hubs, library grids, detail pages, search, video + music players, user switching, admin settings + dashboard, first-run wizard | Daily-usable on desktop and mobile browsers. |
| M5 | **Apple clients** | Shared MediaKit package, keyframe-accurate direct-stream playlists (AVPlayer HEVC/DV copy), iPhone/iPad app, Apple TV app, Bonjour discovery, Quick Connect, PiP/AirPlay/Now Playing | TestFlight builds used daily on all three devices. |
| M6 | **WAN & hardening** | Tailscale setup + docs, LAN/WAN detection, bandwidth caps, ABR ladders, signed URLs, rate limiting, tone mapping, encoder fallback hardening | Reliable remote playback over cellular. Security checklist passed. |
| M7 | **Plex parity polish** | Intro/credits detection, trickplay, collections/smart playlists, watchlist, lyrics, editions/extras, offline downloads (iOS), stats/history, webhooks, OpenSubtitles search, backups UI | v1.0 success criteria met (see vision doc). |
| M8 | **Android & extras** | Android phone/TV app, Chromecast, CarPlay, MPV player engine on Apple, photos library | — |
| Later | Live TV/DVR, SyncPlay (watch together), TOTP 2FA | — | — |

## Working approach

- Every feature starts as an OpenAPI change, followed by server implementation + tests, and then client work.
- Requirements IDs (e.g. `PLAY-3`) are referenced in commits and PRs for traceability.
- Docs are updated in the same change as the code. Decisions are logged in [decision-log.md](decision-log.md).

## Status

| Milestone | State |
|---|---|
| M0 Foundations | ✅ Done 2026-10-01. Running at `http://10.1.1.10:32500`. |
| M1 Library core | ✅ Done 2026-10-01 (v0.5.0), with the deferrals listed below |
| ↳ M1a scanner | ✅ Filename parser (100% of 6,358 real files), ffprobe, scanner (moves, offline drives, versions, multi-part, sidecars), remembered probe failures, background scans with live progress, items API, library/item pages. |
| ↳ M1b metadata + artwork | ✅ TMDB matching: movies 3,294/3,301 (99.8%), shows 75/75, seasons 247/250, episodes 2,914/2,967. Year-numbered seasons (MythBusters), absolute anime numbering, alternative titles, " - " vs ":" titles. Fix Match, Refresh, Edit with field locks. OMDb ratings (IMDb/RT/Metacritic, optional key). Image cache/resizer. Deezer artist photos and missing album covers. Frame thumbnails for personal videos. |
| ↳ M1d users & ops | ✅ Users and managed profiles, PINs, profile switching, library and content-rating restrictions, remote-streaming permission, devices, account preferences, search (FTS5), file watcher (12,675 folders), activity indicator, update banner, log viewer. |
| M2 Plex import | ✅ Done 2026-10-01 (v0.6): snapshot reader, path mapping, account link/create/merge, watch state, ratings, history, playlists, markers, chosen posters, match reconciliation, re-runnable. Settings → Plex Import wizard. Verified on copies of the real databases. The owner runs the import from the wizard. |
| M3a playback core | ✅ Done 2026-10-01 (v0.7): decision engine (direct play / direct stream / transcode, with reasons), automatic quality (§3a), NVENC → QSV → software with automatic fallback (verified on the RTX 5090 and UHD 770), fixed 6 s fMP4 HLS with seek restarts and throttling, PGS burn-in, WebVTT subtitles, HDR tone mapping, sessions with progress, watched state, play history, Continue Watching/On Deck, Up Next, Skip Intro/Credits (Plex markers), music streaming. Web: full-screen player, music mini-player, Home hubs, watch badges. Tested end to end in a browser. |
| M3b playback polish | ✅ Done 2026-10-01 (v0.7.3): Now Playing dashboard (Settings → Dashboard: streams with stop, server load, recent plays), styled ASS subtitles on web via JASSUB with embedded fonts (verified in a browser), HEVC tagged `hev1` in MP4 repackaged as `hvc1` (fixes The 'Burbs in Safari), automatic client fallback (direct play → direct stream → H.264 transcode) on player errors or a 20 s start stall, client playback errors logged on the server. Moved on: keyframe-accurate direct-stream playlists → M5 (only AVPlayer needs them); multi-rung ABR ladder → M6. |
| M4 web client | 🚧 Started 2026-10-01: profile pictures with drag/zoom framing (USER-11, v0.7.4). |
| ↳ Deferred from M1 | AniList anime titles (TMDB covers the current anime library); MusicBrainz enrichment (artist sort names, genres) → M7; TMDB collections (META-7, P1) → M7; extras/trailers (LIB-8, P1) → M7. |
