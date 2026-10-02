# Marquee

A self-hosted, Plex-like media server and client ecosystem: a Docker server plus web, iPhone/iPad and Apple TV apps (Android later). It streams video and music over LAN and WAN and can import an existing Plex library.

## Documentation

| Doc | Contents |
|---|---|
| [Vision & Goals](docs/00-vision-and-goals.md) | Why, principles, v1.0 success criteria, non-goals |
| [Requirements](docs/01-requirements.md) | Prioritized functional and non-functional requirements (P0/P1/P2) |
| [Tech Stack](docs/02-tech-stack.md) | Recommendations, alternatives and trade-offs |
| [Architecture](docs/03-architecture.md) | Components, data model, playback flow, remote access, upgrade strategy |
| [Plex Import](docs/04-plex-import.md) | How the existing Plex library is migrated |
| [Roadmap](docs/05-roadmap.md) | Milestones M0–M8 |
| [Decision Log](docs/decision-log.md) | Accepted and proposed decisions |
| [Open Questions](docs/06-open-questions.md) | Items awaiting owner input |
| [Deployment Environment](docs/07-deployment-environment.md) | Production host, media layout, Docker and Tailscale plan |

## Status

M0–M7 are done and deployed (v0.19), with music (M6.5) at Plexamp level. M8 (Android, CarPlay and extras) has started. See [Roadmap](docs/05-roadmap.md).

Highlights:

- **Libraries:** movies, shows, anime, music and personal videos. TMDB/OMDb metadata, collections, extras and versions. The Plex import brings over watch history, playlists, ratings and markers.
- **Playback:** direct play, direct stream or transcode on NVENC, Quick Sync or the CPU, with HDR tone mapping, adaptive bitrate over Tailscale, styled subtitles, OpenSubtitles search, Skip Intro/Credits (imported or detected) and seek previews.
- **Music:** GPU sonic analysis, similar music, radios, Sonic Sage prompts, Sonic Adventure, daily mixes, Guest DJ, smart playlists, synced lyrics, loudness levelling, crossfade and gapless playback.
- **Apps:** web, iPhone/iPad (offline downloads, CarPlay) and Apple TV (Top Shelf), with profiles, PINs, Quick Connect, watchlist, statistics and webhooks.
## Repository layout

| Path | Contents |
|---|---|
| `api/openapi.yaml` | API contract (source of truth) |
| `server/` | Go server (`cmd/marquee`, `internal/…`) |
| `web/` | React web client, built into the server binary |
| `apple/` | iPhone/iPad and Apple TV apps (XcodeGen `project.yml`, `MarqueeKit` Swift package) |
| `sonic/` | Sonic analysis sidecar (Python, CLAP on the GPU) |
| `deploy/` | Dockerfiles, compose file, env example |
| `docs/` | Plan, requirements, architecture and decisions |

## Development

```bash
cd server && go generate ./... && go test ./...      # regenerate API stubs, run tests
cd web && npm run gen:api && npm run build             # regenerate TS types, build into server
cd apple && scripts/gen-api.sh && xcodegen generate    # regenerate the Swift client and Xcode project
MARQUEE_CONFIG_DIR=/tmp/mq MARQUEE_TRANSCODE_DIR=/tmp/mq-tx go run ./server/cmd/marquee
cd web && npm run dev                                  # hot-reload UI on :5173, proxies /api to :32500
```

## Deploy (Unraid host)

```bash
rsync -a --delete --exclude-from=.dockerignore --exclude .git --exclude apple ./ root@10.1.1.10:/mnt/docker/marquee/src/
ssh root@10.1.1.10 'cd /mnt/docker/marquee && cp src/deploy/compose.yaml compose.yaml && docker compose build && docker compose up -d'
```
