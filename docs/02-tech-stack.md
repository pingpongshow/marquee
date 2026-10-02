# Tech Stack: Recommendations & Alternatives

_Last updated: 2026-10-01. Status: **Accepted** (2026-10-01); see [decision-log.md](decision-log.md)._

Evaluation criteria (from the owner): **performance, usability, Docker compatibility, maintainability, upgradability.**

## Summary

| Layer | Recommendation | Main alternatives |
|---|---|---|
| Server language | **Go** | C# / .NET, Rust, TypeScript (Node/Bun) |
| Database | **SQLite (WAL) + FTS5** | PostgreSQL |
| Media engine | **FFmpeg / ffprobe** (jellyfin-ffmpeg build) | GStreamer |
| API contract | **OpenAPI 3.1 (REST) + WebSocket events** | GraphQL, gRPC |
| Web client | **React + TypeScript + Vite** | SvelteKit, Vue/Nuxt |
| Apple clients | **Native Swift / SwiftUI** (one shared package, iOS/iPadOS/tvOS targets) | React Native (tvOS fork), Flutter |
| Android (later) | **Kotlin + Jetpack Compose + Media3** | Reuse a cross-platform client |
| Packaging | **Single Docker image** (server binary + embedded web app + FFmpeg) | Separate web container |
| Hardware transcoding | **NVENC (RTX 5090), then Intel QSV, then software** | — |
| Remote access | **Tailscale + `tailscale serve` HTTPS** | Reverse proxy + domain |

## Server: Go (recommended)

**Why:**
- **Performance:** compiled, low memory, and goroutines handle hundreds of concurrent streams and segment requests cheaply. The heavy lifting (encoding) is done by FFmpeg anyway, so the server's job is I/O and orchestration, which Go is very good at.
- **Docker:** compiles to one static binary, giving a tiny image (only FFmpeg adds size). Cross-compiles for amd64/arm64 trivially.
- **Maintainable:** simple language, fast builds, a strong standard library (HTTP, TLS, ACME via `autocert`), and gofmt-enforced style.
- **Upgradable:** strong backwards-compatibility promise, and dependencies are minimal and pinned.

**Key libraries:** `net/http` (Go 1.22+ routing) or `chi`; `oapi-codegen` (server stubs from OpenAPI); `sqlc` (type-safe SQL) + `goose` (migrations); `modernc.org/sqlite` (pure-Go SQLite, no CGO); `fsnotify`; `log/slog`; `golang.org/x/crypto/acme/autocert`; `hashicorp/mdns` or `grandcat/zeroconf`.

**Alternatives:**
- **C# / .NET 9:** what Jellyfin uses. Excellent performance and tooling, and there's a large body of Jellyfin code to learn from. Drawbacks: a heavier runtime and image, and more framework-ceremony. A good second choice.
- **Rust (Axum):** the best raw performance and memory safety. Drawbacks: noticeably slower development and iteration, and a smaller hiring/help pool. Overkill when FFmpeg does the CPU-heavy work.
- **TypeScript (Node/Bun):** one language across server and web. Drawbacks: weaker for CPU-bound work (image resizing, scanning hashes) and process supervision. Also less predictable under load.

## Database: SQLite (recommended)

- Plex itself uses SQLite, which proves it scales to very large home libraries.
- No separate container to run or upgrade. Backups are just a file copy (via the `VACUUM INTO` / backup API).
- FTS5 gives fast full-text search with no extra service.
- **Alternative: PostgreSQL.** Better for heavy concurrent writes and multi-node setups, but it adds a second container and its own upgrade burden. We keep SQL portable (via sqlc) so Postgres can be added later if ever needed.

## Media engine: FFmpeg (no real alternative)

- Use the **jellyfin-ffmpeg** build in the Docker image. It is patched for hardware tone-mapping (OpenCL, VAAPI, QSV, NVENC) and well tested for exactly this use case.
- The server runs FFmpeg as supervised child processes and never links it as a library. That keeps licensing simple and lets us upgrade FFmpeg independently.
- **HLS with fMP4 segments** is the streaming format. It is supported natively by AVPlayer (Apple), by hls.js (web) and by ExoPlayer/Media3 (Android), and it carries HEVC, HDR and Dolby Vision correctly to Apple TV.

## API: OpenAPI REST + WebSocket (recommended)

- `api/openapi.yaml` is the **single source of truth**. We generate:
  - Go server interfaces (`oapi-codegen`)
  - TypeScript client for web (`openapi-typescript` + `openapi-fetch`)
  - Swift client (`swift-openapi-generator`)
  - Kotlin client later (`openapi-generator`)
- This is the main tool for "minimal reiteration": every client stays in lock-step with the server at compile time.
- A WebSocket channel carries live events: scan progress, now playing, remote control and library updates.
- **Alternatives:** GraphQL (flexible queries, but caching, codegen for Swift, and media-range endpoints are awkward); gRPC (great for typed RPC, but poor browser support and not suited to media streaming).

## Web client: React + TypeScript + Vite (recommended)

- The largest ecosystem, the most mature video tooling (`hls.js`, `jassub` for anime ASS subtitles), and virtualized grids for huge libraries (`@tanstack/react-virtual`).
- Data: TanStack Query (caching), TanStack Router. Styling: Tailwind + Radix UI primitives (accessible menus and dialogs).
- The build output is embedded into the Go binary (`go:embed`), so one container serves everything.
- **Alternatives:** SvelteKit (smaller bundles and very pleasant to write, but a smaller ecosystem for media players); Vue/Nuxt (similar trade-off).

## Apple clients: native SwiftUI (recommended)

- **AVPlayer is the best player on Apple hardware.** It gives HDR10/Dolby Vision, Atmos passthrough, frame-rate matching on tvOS, PiP, AirPlay and lock-screen controls for free. Plex-quality playback on Apple TV effectively requires native.
- One Xcode project with a shared Swift package (API client, models, playback logic, view models) and thin iOS/iPadOS and tvOS UI targets. That gives roughly 70–80% code sharing.
- **Player engine abstraction:** v1 uses AVPlayer, and the server remuxes or transcodes anything AVPlayer can't play. An **MPV (MPVKit) engine** can be plugged in later for direct play of MKV and native ASS rendering (great for anime) without UI changes. Swiftfin and Infuse take the same approach.
- **Alternatives:**
  - React Native: shares code with web, but tvOS support relies on a community fork, the focus engine is fiddly, and video quality depends on native modules anyway.
  - Flutter: no official tvOS support, and video is again a native plugin.

## Android (later): Kotlin + Compose + Media3

- Media3/ExoPlayer is the standard player there, and Compose for TV handles Android TV.
- The OpenAPI-generated Kotlin client keeps it in sync with the server. Planning now just means keeping the API platform-neutral, which the OpenAPI approach guarantees.
- Kotlin Multiplatform isn't worth it, because the Apple clients are Swift and there is no other Kotlin code to share with. Plain Kotlin is fine.

## Deployment: Docker

- One image: `server` binary, embedded web app, jellyfin-ffmpeg, multi-arch (amd64 and arm64).
- Volumes: `/config` (DB, images, logs), `/media` (libraries, read-only), `/transcode` (tmpfs or fast disk), and an optional read-only mount of the Plex data dir for import.
- GPU passthrough: `/dev/dri` (Intel/AMD) or the NVIDIA Container Toolkit.
- `compose.yaml` provided. Upgrades: `docker compose pull && docker compose up -d`. Migrations run automatically after a backup.

### Target host: Linux (Unraid) with NVIDIA RTX 5090 + Intel i7-13700K

See [07-deployment-environment.md](07-deployment-environment.md). This hardware **confirms the stack above and changes nothing fundamental**. It adds these specifics:

- **Primary encoder: NVENC** (RTX 5090, Blackwell). It does H.264, HEVC 10-bit and AV1 encode, plus NVDEC decode, which comfortably handles many simultaneous 4K→1080p transcodes. The container uses `runtime: nvidia`.
- **Secondary encoder: Intel QSV** (UHD 770), via `/dev/dri`.
- **Fallback: software (libx264/libx265).**
- The decision engine chooses an encoder per job (NVENC, then QSV, then CPU) and falls back automatically on failure, because the GPU is shared with AI containers.
- **Tone mapping** (for future HDR/Dolby Vision): CUDA/OpenCL or libplacebo/Vulkan on NVIDIA. This is the reason for using jellyfin-ffmpeg.
- **Image:** `linux/amd64` is the only required architecture. Base image `debian:bookworm-slim` + `jellyfin-ffmpeg7`.
- **Development:** the server and web app can be developed on the Mac (software transcoding in Docker there is fine for testing). Hardware-transcode work is tested on the Unraid host.
- **Remote access is via Tailscale** (already on the host). It needs no port forwarding and gives HTTPS through `tailscale serve`, so built-in ACME/TLS drops to P2.

## Tooling

- Monorepo, GitHub Actions CI (lint, test, build multi-arch image, Xcode build), Renovate/Dependabot.
- Go: `golangci-lint`, `go test`. Web: ESLint, Vitest, Playwright for E2E. Apple: XCTest/Swift Testing.
- Semantic versioning, with a changelog per release.
