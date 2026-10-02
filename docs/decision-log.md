# Decision Log

Record of significant decisions. Status: **Proposed**, then **Accepted**, and later possibly **Superseded**.

| # | Date | Decision | Status | Rationale |
|---|---|---|---|---|
| D1 | 2026-10-01 | Self-hosted, no cloud account or relay | Accepted | Owner's core motivation: escape forced Plex changes. |
| D2 | 2026-10-01 | v1 media types: movies, TV, anime, personal videos, music. Photos later. | Accepted | Owner requirement. |
| D3 | 2026-10-01 | v1 clients: Web + iPhone/iPad + Apple TV. Android planned, built later. | Accepted | Owner requirement. |
| D4 | 2026-10-01 | Plex-like UI/UX | Accepted | Owner requirement. |
| D5 | 2026-10-01 | Plex library import is P0 | Accepted | Owner requirement. |
| D6 | 2026-10-01 | Server in Go | Accepted | See 02-tech-stack.md |
| D7 | 2026-10-01 | SQLite (WAL, FTS5) | Accepted | See 02-tech-stack.md |
| D8 | 2026-10-01 | OpenAPI-first REST + WebSocket, with generated clients | Accepted | Minimizes rework across 3+ clients. |
| D9 | 2026-10-01 | Web in React + TS + Vite, embedded in server binary | Accepted | See 02-tech-stack.md |
| D10 | 2026-10-01 | Apple clients native SwiftUI + AVPlayer, with a pluggable player engine (MPV later) | Accepted | Best playback quality on Apple TV. |
| D11 | 2026-10-01 | Single multi-arch Docker image with jellyfin-ffmpeg | Accepted | Simplest deploy/upgrade. |
| D12 | 2026-10-01 | HLS fMP4 as the streaming format | Accepted | Native on all target players, and carries HEVC/HDR/DV. |
| D13 | 2026-10-01 | Production host: Unraid at 10.1.1.10, app under `/mnt/docker/<app>` | Accepted | Owner's server. See 07-deployment-environment.md |
| D14 | 2026-10-01 | Remote access via the host's existing Tailscale, **unmodified** (no `tailscale serve`). Remote URL `http://dingdong.tailb5b08.ts.net:32500`. Built-in ACME demoted to P2. | Accepted | Owner: Tailscale is already running and must not be altered. WireGuard already encrypts. |
| D15 | 2026-10-01 | Encoder order: NVENC, then QSV, then software, with automatic fallback | Accepted | RTX 5090 + UHD 770 available. GPU is shared with AI containers. |
| D16 | 2026-10-01 | Image targets linux/amd64 only (arm64 optional) | Accepted | Production is x86_64. |
| D17 | 2026-10-01 | Project name: **Marquee** | Accepted | Owner choice. |
| D18 | 2026-10-01 | LAN clients connect directly. Tailscale is used only by remote clients. Network class is decided server-side by source IP. | Accepted | Owner requirement. |
| D19 | 2026-10-01 | Automatic per-session quality selection (min of client setting, user cap, measured bandwidth, upload fair-share, source) with an ABR ladder for remote | Accepted | Owner requirement: remote Tailscale clients have limited bandwidth. |
| D20 | 2026-10-01 | Plex-style organized settings area with library management in the web dashboard | Accepted | Owner requirement. |
| D21 | 2026-10-01 | Web toolchain pinned to TypeScript 5.9 | Accepted | openapi-typescript and typescript-eslint don't support TS 6/7 yet. Revisit when they do. |
| D22 | 2026-10-01 | Swift API codegen set up in M5 (not M0) | Accepted | No Apple client code until M5. The OpenAPI spec is already the contract. |
| D23 | 2026-10-01 | Settings stored as one JSON document over code defaults. Secrets are write-only in the API. | Accepted | New settings get defaults automatically on upgrade. API keys are never returned to clients. |
| D24 | 2026-10-01 | Plex import is order-independent: it merges into existing libraries by remapped file path. Plex wins for manual edits and fixed matches. | Accepted | The owner can add libraries now without waiting for M2. |
| D25 | 2026-10-01 | OMDb is an optional ratings source (IMDb, Rotten Tomatoes, Metacritic). Lookups are budgeted per day and refreshed every 30 days. | Accepted | Owner asked. TMDB lacks critic scores. A free key's 1,000/day covers the library over a few days. |
| D26 | 2026-10-01 | Deezer public API for artist photos and missing album covers (exact name matches only) | Accepted | No key needed. Music tags are already good. MusicBrainz enrichment deferred to M7. |
| D27 | 2026-10-01 | The server queues scans itself (library added, folders changed, never-scanned at startup, file watcher after 30 s quiet). It doesn't depend on clients. | Accepted | A stale browser tab left new libraries unscanned. |
| D28 | 2026-10-01 | File watcher triggers an incremental whole-library rescan rather than per-folder partial scans | Accepted | Unchanged files are skipped, so rescans take under a second for 16k tracks. This avoids partial-scan edge cases. |
| D29 | 2026-10-01 | MatcherVersion: when matching improves, previously failed items are retried on upgrade | Accepted | Upgrades fix old misses without waiting for the weekly retry. |
| D30 | 2026-10-01 | Content-rating restrictions use one scale across US movie and TV ratings. Unrated movies/TV are hidden when a limit is set. Music and home videos are never rating-restricted. | Accepted | Matches Plex's behaviour for managed users. |
| D31 | 2026-10-01 | **Supersedes D24's conflict rule.** Plex matches are applied only when Plex was edited, when Marquee is unmatched and Plex fits the file name, or when Plex fits the name strictly better. Otherwise Marquee's is kept and reported. | Accepted | Testing on the real library showed Plex wrong in about 20 of 37 disagreements and right in 8. |
| D32 | 2026-10-01 | Plex accounts can be merged into one Marquee user during import | Accepted | The owner's household has one person with three Plex accounts. |
| D33 | 2026-10-01 | Matching always searches with the year from the file/folder name (scan key), never one written by an earlier match. Shows without a year are disambiguated by how well TMDB seasons/episode counts fit the files. | Accepted | One Piece (2023 series) had been matched to the 1999 anime. |
| D34 | 2026-10-01 | PIN sign-in is allowed on the home network only by default (configurable: off/local/everywhere), with a 5-attempt PIN lockout separate from passwords | Accepted | Owner wants Plex-style PIN sign-in. A 4-digit PIN is weak over the internet. |
| D35 | 2026-10-01 | Passwords are required only for administrators. PIN and password are optional per user, and profiles with neither open with a tap where PIN sign-in is allowed. | Accepted | Owner requirement (Plex Home behaviour). The PIN sign-in network setting (home-only by default) limits where credential-less profiles can be used. |
| D36 | 2026-10-01 | HLS uses server-generated VOD playlists of fixed 6 s fMP4 segments. Transcodes force keyframes on the boundaries and restart FFmpeg with `-ss -copyts` on seeks. | Accepted | Seekable from the first request, with simple restarts. Direct stream (copy) boundaries are approximate until keyframe-accurate playlists (M3b). |
| D37 | 2026-10-01 | Text subtitles are delivered as WebVTT sidecars. Image subtitles (PGS/VobSub) are burned in. ASS is converted to WebVTT on web for now. | Accepted | Avoids transcoding for text subtitles. Styled ASS via JASSUB is planned. |
| D38 | 2026-10-01 | Media URLs use the playback session id as a capability (no auth header needed), valid only while the session lives | Accepted | `<video>`/AVPlayer can't reliably send headers. Satisfies WAN-5's short-lived stream URLs. |
| D39 | 2026-10-01 | **Supersedes D37's ASS rule.** ASS/SSA is sent as-is to clients that can render it (web with JASSUB: WebAssembly + Worker + OffscreenCanvas), with embedded fonts extracted on demand. Other clients get WebVTT. | Accepted | Anime signs and styling are lost in WebVTT. JASSUB is libass in WebAssembly, so it renders like MPV. |
| D40 | 2026-10-01 | HEVC tagged `hev1` in MP4 is direct-streamed (copied) and retagged `hvc1` instead of direct-played | Accepted | Safari and AVPlayer won't play `hev1`. Copying keeps quality and costs almost nothing. |
| D41 | 2026-10-01 | The web player steps down by itself when playback fails or stalls for 20 s: full profile → no direct play → H.264-only transcode. Each failure is reported to the server log. | Accepted | Browsers misreport what they can decode. Plex does the same silently. |
