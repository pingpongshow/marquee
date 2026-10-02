# Deployment Environment

_Last updated: 2026-10-01. Facts gathered by read-only inspection of the production host._

## Production host: `root@10.1.1.10`

| Item | Value |
|---|---|
| OS | Unraid OS 7.3 (kernel 6.18, x86_64) |
| CPU | Intel Core i7-13700K (16C/24T), iGPU UHD 770 → **Quick Sync (QSV)** |
| GPU | **NVIDIA GeForce RTX 5090** (32 GB, driver 595.84) → **NVENC/NVDEC** (H.264, HEVC, AV1 encode) |
| RAM | 125 GB |
| Docker | 29.5, Compose v2.40. Runtimes: `nvidia`, `runc` (default `runc`) |
| Render nodes | `/dev/dri/renderD128`, `/dev/dri/renderD129` (Intel + NVIDIA; identify which is which at M0) |
| Container data | `/mnt/docker/<app>` (1.8 TB, ~790 GB free) |
| Media | `/mnt/data` (27 TB, 26 TB free) |
| Remote access | Tailscale 1.102 on the host |
| Unraid user/group | PUID `99` (nobody) / PGID `100` (users) |

### Shared GPU

The RTX 5090 is also used by other GPU containers (`llama` llama.cpp-cuda, and ComfyUI/Ollama app data exists). NVENC/NVDEC are dedicated engines, so they don't compete with CUDA compute. VRAM and CUDA tone-mapping can still contend, though, so the transcoder must **fall back automatically**: NVENC first, then QSV, then software.

## Media layout

| Host path | Contents | Plex mount | Planned mount in our container |
|---|---|---|---|
| `/mnt/data/Video/Movies` | ~3,370 movie folders, `Title (Year)/` | `/movies` | `/media/video/Movies` (ro) |
| `/mnt/data/Video/Shows` | ~75 shows, `Show (Year)/` (year sometimes omitted) | `/tv` | `/media/video/Shows` (ro) |
| `/mnt/data/Video/Anime` | 1 show now, growing | `/anime` | `/media/video/Anime` (ro) |
| `/mnt/data/Music` | ~16,100 tracks, 617 GB | `/music` | `/media/music` (ro) |
| `/mnt/data/Photos/library` | Photos (Immich also runs on this host) | `/photos` | not mounted yet (photos are P2) |
| `/mnt/docker/plex` | Plex config + DB (`com.plexapp.plugins.library.db`, 341 MB) | `/config` | `/plex` (**ro**, import only) |

`/mnt/data/Video` also contains two folders Plex does not mount (`Hentai`, `XXX`). They are excluded from planning unless the owner wants them added as restricted libraries.

### Music folder quirks the scanner must handle
- Artist folders are often comma-joined collaborator lists (e.g. `ArtistA,ArtistB,ArtistC`), and some names are truncated. **Embedded tags are authoritative**; folder names are only a fallback.
- Loose tracks exist at the library root.
- Temp files from download tools (e.g. `*_staging.flac`) must be ignored via configurable ignore patterns.

## Deployment plan

- App directory: `/mnt/docker/<appname>/` containing `compose.yaml`, `.env`, `config/` (DB, cache, logs) and the source checkout or build context.
- Image is built from the repo (on the host or by CI), tagged `<appname>:<version>`.
- Container runs with `runtime: nvidia`, `NVIDIA_VISIBLE_DEVICES=all`, `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility`, plus `/dev/dri` for QSV, and `PUID=99 PGID=100`.
- Transcode directory: `tmpfs` (RAM is plentiful), capped at roughly 16 GB.
- Network: bridge with a published port (e.g. `32500`) to avoid colliding with Plex on `32400`. Plex keeps running during the transition.
- Development happens on the owner's Mac (M2 Max, Docker Desktop, Go 1.26, Node 25, Xcode 26.2). Production builds target `linux/amd64`.

## Library profile (dev-scan, 2026-10-01)

| Library | Files | Items | Scan time |
|---|---|---|---|
| Movies | 3,318 | 3,301 movies (14 with 2 versions) | 14 s |
| Shows | 2,984 | 75 shows, 250 seasons, 2,966 episodes | 7 s |
| Anime | 52 | 1 show, 52 episodes | <1 s |
| Music | 16,087 | 2,161 artists, 4,696 albums (3,179 single-track), 688 lyric files | 24 s |

Playback-relevant mix (movies): HEVC 1,817 · H.264 1,431 · AV1 41 · MPEG-4/2/VC-1 25. **Dolby Vision 26, HDR10 11, HLG 1.**
Audio: AAC 2,127 · EAC3 525 · AC3 405 · DTS 159 · TrueHD 15. Subtitles: **PGS 3,710** (image, needs burn-in on web) · SRT 3,224 embedded + 2,180 external · **ASS 579** · VobSub 159.
Damaged files found (ffprobe can't read them): `Friend Request (2016)…mkv`, `Taken 3. 2014 EXTENDED…mp4`, `The.Godfather.Part.II.1974…mp4`.

## Remote access: Tailscale (do not modify)

- Tailscale is already installed and running on the host. **Marquee must not alter or reconfigure it** (no `tailscale serve`, Funnel or ACL changes).
- Tailnet name: `dingdong.tailb5b08.ts.net` (Tailscale IP `100.70.212.119`). Marquee listens on all interfaces, so remote clients use **`http://dingdong.tailb5b08.ts.net:32500`**. Traffic is already encrypted end-to-end by Tailscale (WireGuard), so plain HTTP is safe on this path.
- Verified 2026-10-01: requests via the Tailscale IP are classified `remote`, and LAN requests `local`.
- Local clients use `http://10.1.1.10:32500` and never Tailscale.
- Apple apps need an ATS exception allowing HTTP to `*.ts.net` (and `NSAllowsLocalNetworking` for the LAN).
- Remote devices run the Tailscale app. Remote family members get node sharing (managed by the owner in Tailscale's admin console, not by Marquee).

## Running deployment (M0, 2026-10-01)

| Item | Value |
|---|---|
| Directory | `/mnt/docker/marquee/` (`compose.yaml`, `.env`, `config/`, `src/`) |
| Image | `marquee:0.1.0` (374 MB), built on the host from `src/` |
| Container | `marquee`, `runtime: nvidia`, `/dev/dri` with group 18 (`video`), runs as 99:100 |
| Verified | Healthcheck OK. NVENC HEVC + AV1 and QSV H.264 test encodes succeed inside the container. |
| `/dev/dri` | `renderD128` = Intel i915 (QSV). `renderD129` = NVIDIA. |
| Timezone | America/Los_Angeles |

Update procedure: rsync the repo to `/mnt/docker/marquee/src`, then `cd /mnt/docker/marquee && docker compose build && docker compose up -d`.
