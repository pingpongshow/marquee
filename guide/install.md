# Install the server

Run Marquee in Docker from the published images, build it yourself, or install it on Unraid from the templates.

## Contents

- [What you need](#what-you-need)
- [Option A: the published images](#option-a-the-published-images)
- [Option B: build from source](#option-b-build-from-source)
- [On Unraid](#on-unraid)
- [Folders and permissions](#folders-and-permissions)
- [Environment variables and ports](#environment-variables-and-ports)
- [Updating](#updating)
- [Backups and where your data lives](#backups-and-where-your-data-lives)
- [Moving to a new server](#moving-to-a-new-server)

## What you need

| | |
|---|---|
| **Server** | Linux with Docker and Docker Compose (Unraid, TrueNAS SCALE, Synology or QNAP with Container Manager, or a plain Linux box). The published images are for 64-bit Intel and AMD processors (`linux/amd64`). |
| **CPU and memory** | 4 cores and 4 GB of RAM are comfortable for direct play and a CPU transcode. Add RAM for the transcode scratch space, which is held in memory (8 GB by default). |
| **Hardware transcoding** (optional) | An NVIDIA GPU with the [NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html), or an Intel CPU with Quick Sync (`/dev/dri`). Without either, Marquee transcodes on the CPU and still direct-plays whatever your devices support. |
| **Music analysis** (optional) | Soundprint runs on the CPU, or much faster on an NVIDIA GPU. Its models take about 1.5 GB of disk, downloaded on first start. |
| **Disk for Marquee itself** | A few GB for the `config` folder (database, artwork, seek previews), growing with your library. Seek previews take roughly 3–7 MB per film. Put `config` on an SSD if you can. |
| **TMDB API key** | Free from [themoviedb.org → Settings → API](https://www.themoviedb.org/settings/api). Needed for film and TV metadata. You enter it after installing. |

Marquee doesn't need a cloud account or a licence. It serves plain HTTP on port 32500.

## Option A: the published images

Each release is published to the GitHub Container Registry:

- `ghcr.io/pingpongshow/marquee:latest`: the server, web app, FFmpeg and Comskip.
- `ghcr.io/pingpongshow/marquee-soundprint:latest`: music analysis and natural-language search (Muse, radios, "sounds like").

Releases are also tagged by version (for example `:1.4.0`) if you'd rather pin one.

**1. Make a folder and a settings file**

```bash
mkdir -p /srv/marquee && cd /srv/marquee
```

Create `.env` next to where your `compose.yaml` will go:

```ini
# The user and group that own your media folders (id -u / id -g).
PUID=1000
PGID=1000
TZ=Europe/London

CONFIG_DIR=./config
VIDEO_DIR=/mnt/user/media/video
MUSIC_DIR=/mnt/user/media/music
RECORDINGS_DIR=./recordings
MODELS_DIR=./models

# Read-only by default. Set to empty (VIDEO_MODE=) to allow writing; see "Folders and permissions".
VIDEO_MODE=:ro
# RAM for the transcode scratch space.
TRANSCODE_SIZE=8g
MARQUEE_LOG_LEVEL=info

# Optional: a Plex Media Server data folder to import from.
# PLEX_DIR=/path/to/Plex Media Server
# Intel Quick Sync only: the group that owns /dev/dri/renderD128 (stat -c %g /dev/dri/renderD128).
# RENDER_GID=109
```

**2. Create `compose.yaml`**

```yaml
name: marquee
services:
  marquee:
    image: ghcr.io/pingpongshow/marquee:latest
    container_name: marquee
    restart: unless-stopped
    user: "${PUID:-1000}:${PGID:-1000}"
    # Host networking lets the apps find the server on your network (Bonjour/mDNS).
    network_mode: host
    environment:
      TZ: ${TZ:-UTC}
      MARQUEE_LOG_LEVEL: ${MARQUEE_LOG_LEVEL:-info}
    volumes:
      - ${CONFIG_DIR:-./config}:/config
      - ${VIDEO_DIR:?set VIDEO_DIR in .env}:/media/video${VIDEO_MODE:-:ro}
      - ${MUSIC_DIR:-./empty-music}:/media/music:ro
      - ${RECORDINGS_DIR:-./recordings}:/recordings
      - ${PLEX_DIR:-./empty-plex}:/plex:ro
    tmpfs:
      - /transcode:size=${TRANSCODE_SIZE:-8g},uid=${PUID:-1000},gid=${PGID:-1000},mode=0750

  # Remove this service if you don't want music analysis and natural-language search.
  soundprint:
    image: ghcr.io/pingpongshow/marquee-soundprint:latest
    container_name: marquee-soundprint
    restart: unless-stopped
    user: "${PUID:-1000}:${PGID:-1000}"
    network_mode: host
    volumes:
      - ${MODELS_DIR:-./models}:/models
      - ${MUSIC_DIR:-./empty-music}:/media/music:ro
```

**3. Add hardware transcoding (optional)**

For an NVIDIA GPU, create `compose.nvidia.yaml`:

```yaml
services:
  marquee:
    runtime: nvidia
    environment:
      NVIDIA_VISIBLE_DEVICES: all
      NVIDIA_DRIVER_CAPABILITIES: compute,video,utility
  soundprint:
    runtime: nvidia
    environment:
      NVIDIA_VISIBLE_DEVICES: all
      NVIDIA_DRIVER_CAPABILITIES: compute,utility
```

For Intel Quick Sync (or other VA-API hardware), create `compose.intel.yaml` and set `RENDER_GID` in `.env`:

```yaml
services:
  marquee:
    devices:
      - /dev/dri:/dev/dri
    group_add:
      - "${RENDER_GID:-109}"
```

**4. Start it**

```bash
# CPU only
docker compose up -d

# NVIDIA GPU
docker compose -f compose.yaml -f compose.nvidia.yaml up -d

# Intel Quick Sync
docker compose -f compose.yaml -f compose.intel.yaml up -d
```

Open `http://<server>:32500` and carry on with [First run](first-run.md).

## Option B: build from source

The `deploy/` folder in the repository has the same compose files with a `build:` section, so Docker builds the images on your server.

```bash
git clone https://github.com/pingpongshow/marquee.git
cd marquee/deploy
cp .env.example .env
```

Edit `.env` as in Option A (at minimum `VIDEO_DIR`, `MUSIC_DIR`, `PUID`, `PGID` and `TZ`), then start it with the line that matches your hardware:

```bash
# CPU only
docker compose up -d --build

# NVIDIA GPU
docker compose -f compose.yaml -f compose.nvidia.yaml up -d --build

# Intel Quick Sync (set RENDER_GID in .env)
docker compose -f compose.yaml -f compose.intel.yaml up -d --build
```

The first build takes a few minutes: it compiles the web app, the server and Comskip. Remove the `soundprint` service from `compose.yaml` if you don't want music analysis.

## On Unraid

Marquee has Unraid templates with an icon, a WebUI link and every field explained. They install the published images.

1. Copy the templates onto the flash drive (from the Unraid terminal):
   ```bash
   cd /boot/config/plugins/dockerMan/templates-user
   wget -O my-Marquee.xml https://raw.githubusercontent.com/pingpongshow/unraid-templates/main/marquee.xml
   wget -O my-Marquee-Soundprint.xml https://raw.githubusercontent.com/pingpongshow/unraid-templates/main/marquee-soundprint.xml
   ```
2. **Docker** tab → **Add Container**, pick **Marquee** under *Template*, fill in the fields below and apply.
3. Add **Marquee-Soundprint** the same way if you want music analysis.
4. Open `http://<server>:32500` (or use **WebUI** from the container's menu).

**Marquee template**

| Field | Inside the container | Default | Notes |
|---|---|---|---|
| WebUI port | `32500` | `32500` | Host networking, so this is the port on the server itself. |
| Config | `/config` | `/mnt/user/appdata/marquee/config` | Database, settings, artwork, backups and logs. Read/write. |
| Video | `/media/video` | (empty) | Your films, shows, anime and home videos. Read-only by default; switch to read/write for the features in [Folders and permissions](#folders-and-permissions). |
| Music | `/media/music` | (empty) | Your music, read-only. Optional. |
| Recordings | `/recordings` | `/mnt/user/appdata/marquee/recordings` | Live TV recordings. Read/write. Shown under *Show more settings*. |
| Plex data (optional) | `/plex` | (empty) | A Plex data folder to import from, read-only. |
| Intel Quick Sync | `/dev/dri` | (empty) | Set to `/dev/dri` for Intel hardware transcoding. You also need `--group-add=<render group id>` in *Extra Parameters*. |
| NVIDIA_VISIBLE_DEVICES | | `all` | Which NVIDIA GPUs Marquee may use: `all`, or a GPU's UUID from `nvidia-smi -L`. |
| TZ | | `UTC` | Your time zone, e.g. `Europe/London`. Recording schedules use it. |
| MARQUEE_LOG_LEVEL | | `info` | `debug`, `info`, `warn` or `error`. |

The template's *Extra Parameters* (switch on **Advanced View**) are:

```text
--runtime=nvidia --user 99:100 --tmpfs /transcode:size=8g,uid=99,gid=100,mode=0750
```

- `--runtime=nvidia` gives the container the NVIDIA GPU. It needs the **Nvidia Driver** plugin. **Remove it if you have no NVIDIA GPU**, or the container won't start. The image already sets `NVIDIA_DRIVER_CAPABILITIES=compute,video,utility`.
- `--user 99:100` runs Marquee as Unraid's usual `nobody:users`, which owns shares by default.
- `--tmpfs /transcode:size=8g,…` is the transcode scratch space in RAM. Make `size` smaller if your server is short of memory.

**Marquee-Soundprint template**

| Field | Inside the container | Default | Notes |
|---|---|---|---|
| Models | `/models` | `/mnt/user/appdata/marquee/models` | The analysis models, about 1.5 GB. |
| Music | `/media/music` | (empty) | The same music folder as Marquee, read-only. |
| NVIDIA_VISIBLE_DEVICES | | `all` | Which GPUs Soundprint may use. |
| NVIDIA_DRIVER_CAPABILITIES | | `compute,utility` | |
| Unload when idle (minutes) | `IDLE_UNLOAD_MINUTES` | `5` | Frees the models from the GPU after this many idle minutes. |

Its *Extra Parameters* are `--runtime=nvidia --user 99:100`; remove `--runtime=nvidia` without an NVIDIA GPU, as above.

### A separate DVR folder on Unraid

Recordings go to `/recordings` (the **Recordings** path) unless you choose another folder. To keep recordings on a share of their own, for example next to your other media:

1. Edit the container and **Add another Path, Port, Variable, Label or Device** → **Path**: *Container Path* `/DVR`, *Host Path* `/mnt/user/media/DVR`, *Access Mode* Read/Write.
2. In Marquee, go to **Settings → Live TV → Recording** and set **Recordings folder** to `/DVR` (use **Browse** to pick it).
3. Add the new folder to the **Recorded TV** library in **Settings → Libraries** if Marquee says no library includes it.

Recordings already made stay where they are and keep working. **Use default** goes back to `/recordings`. See [Live TV and DVR](live-tv-and-dvr.md).

## Folders and permissions

Marquee sees your media at fixed paths inside the container. You use these paths when you add libraries.

| Inside the container | What goes there | Access |
|---|---|---|
| `/config` | Database, settings, artwork cache, backups, logs | Read/write |
| `/media/video` | Films, shows, anime, home videos (one subfolder each, e.g. `/media/video/Movies`) | Read-only by default |
| `/media/music` | Music | Read-only |
| `/recordings` | Live TV recordings | Read/write |
| `/transcode` | Transcode scratch space (in RAM) | Read/write |
| `/plex` | A Plex data folder to import from (optional) | Read-only |
| `/models` (Soundprint) | Soundprint's models | Read/write |

**PUID and PGID.** The containers run as the user and group you give them (`PUID:PGID` in compose, `--user` on Unraid). That user needs to read every media folder and write to `config`, `recordings` and `models`. Find the right numbers with `id -u` and `id -g` as the user that owns your media, or `ls -n` on a media folder.

```bash
ls -n /mnt/user/media/video
sudo chown -R 1000:1000 /srv/marquee/config /srv/marquee/recordings /srv/marquee/models
```

**Read-only or writable video?** Marquee only reads your video folder unless a feature needs to change it. Leave it read-only (`VIDEO_MODE=:ro`, or *Read Only* on Unraid) if you don't use any of these:

| Feature | Why it writes |
|---|---|
| Library Health: deleting files (duplicates, unwanted copies) | Files move to a `.marquee-trash` folder in their library folder, recoverable until the nightly **Empty media trash** task. Also needs **Allow deleting media files** (off by default) under **Settings → Libraries**. |
| Library Health → Large files | Writes the re-encoded file next to the original, then moves the original to the media trash. |
| VOD: saving films and shows to your library | Downloads go into the **Movies folder**, **Shows folder** and other save folders you choose under **Settings → VOD**. Those must be writable. |
| DVR into a library folder | Only if you point **Recordings folder** at a folder under `/media/video`. |

To make video writable, set `VIDEO_MODE=` (empty) in `.env` and run `docker compose up -d` again, or set the **Video** path to *Read/Write* on Unraid. Music is always mounted read-only; nothing in Marquee writes to it.

Downloaded subtitles (OpenSubtitles) are kept in `config`, not next to your files, so they don't need write access.

## Environment variables and ports

You rarely need to change these: the image and compose file set them. Everything else is configured in the web app under **Settings**.

**Marquee**

| Variable | Default | Purpose |
|---|---|---|
| `MARQUEE_PORT` | `32500` | Web and API port. |
| `MARQUEE_CONFIG_DIR` | `/config` | Database, settings, artwork cache, backups and logs. |
| `MARQUEE_TRANSCODE_DIR` | `/transcode` | Transcode scratch space. |
| `MARQUEE_RECORDINGS_DIR` | `/recordings` | The default Live TV recordings folder (**Settings → Live TV → Recordings folder** can override it). |
| `MARQUEE_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error`. |
| `MARQUEE_FFMPEG` | `/usr/lib/jellyfin-ffmpeg/ffmpeg` | The FFmpeg binary. |
| `MARQUEE_FFPROBE` | `/usr/lib/jellyfin-ffmpeg/ffprobe` | The FFprobe binary. |
| `MARQUEE_SOUNDPRINT_URL` | `http://127.0.0.1:32501` | Where the Soundprint service listens. |
| `MARQUEE_PLEX_DIR` | `/plex` | A read-only Plex data folder to import from. |
| `MARQUEE_COMSKIP` | `comskip` | The Comskip binary, for finding adverts in recordings. |
| `MARQUEE_COMSKIP_INI` | `/usr/local/share/marquee/comskip.ini` | Comskip's settings file. |
| `MARQUEE_WEB_DIR` | (empty) | Development only: serve the web app from this folder instead of the built-in copy. |
| `TZ` | `UTC` | Time zone, used for the guide, recordings and the maintenance window. |
| `NVIDIA_VISIBLE_DEVICES`, `NVIDIA_DRIVER_CAPABILITIES` | `all`, `compute,video,utility` | Which NVIDIA GPUs the container gets and what it may use them for. |

**Soundprint**

| Variable | Default | Purpose |
|---|---|---|
| `IDLE_UNLOAD_MINUTES` | `5` | Frees the models from memory after this many idle minutes. |

**Compose `.env` settings** (these only feed the compose file)

| Setting | Default | Purpose |
|---|---|---|
| `PUID`, `PGID` | `1000` | The user and group the containers run as. |
| `TZ` | `UTC` | Time zone. |
| `CONFIG_DIR` | `./config` | Host folder for `/config`. |
| `VIDEO_DIR` | (required) | Host folder for `/media/video`. |
| `VIDEO_MODE` | `:ro` | Empty makes video writable. |
| `MUSIC_DIR` | (empty) | Host folder for `/media/music`. |
| `RECORDINGS_DIR` | `./recordings` | Host folder for `/recordings`. |
| `PLEX_DIR` | (empty) | Host folder for `/plex`. |
| `MODELS_DIR` | `./models` | Host folder for Soundprint's `/models`. |
| `TRANSCODE_SIZE` | `8g` | RAM for the transcode scratch space. |
| `RENDER_GID` | `109` | Intel only: the group that owns `/dev/dri/renderD128`. |
| `MARQUEE_VERSION` | `latest` | Option B: the tag given to the images you build. |

**Ports**

| Port | Used for |
|---|---|
| `32500/tcp` | Web app and API. The only port you need to open. |
| `5353/udp` | Bonjour/mDNS, so the apps find the server on your network. |
| `32501/tcp` | Soundprint, bound to `127.0.0.1` only. |

Both containers use host networking, so there are no port mappings: Marquee listens on the server's own port 32500. Bonjour only works with host networking.

## Updating

Database changes are applied automatically when the new version starts, and a backup is taken first.

**Published images (Option A)**

```bash
cd /srv/marquee
docker compose pull
docker compose up -d                # add your -f override files as when you installed
docker image prune -f               # optional: remove the old images
```

**Unraid:** the **Docker** tab shows *update ready* when a new release is out. Use **Check for Updates**, then **Apply Update** on each Marquee container.

**Built from source (Option B)**

```bash
cd marquee
git pull
cd deploy
docker compose up -d --build        # add your -f override files as when you installed
docker image prune -f
```

The server's version is shown on **Settings → Dashboard**.

## Backups and where your data lives

Marquee backs up its database every night in the maintenance window. Under **Settings → Scheduled Tasks → Database backups** you can:

- set **Keep nightly backups for** (default 7 days, up to 365),
- **Back up now**,
- download, restore or delete any backup.

Backups cover libraries, users, watch history and settings. Media files aren't included. Backups are kept in `config/backups/`, on the same disk as the database, so copy them somewhere else now and then.

| Folder | Contents |
|---|---|
| `config/marquee.db` | The database: libraries, people, watch history, ratings, settings. |
| `config/backups/` | Database backups. |
| `config/logs/` | Logs (`marquee.log`, rotated at 10 MB, five kept). Also under **Settings → Logs**. |
| `config/subtitles/` | Subtitles downloaded from OpenSubtitles. |
| `config/avatars/` | Profile pictures. |
| `config/plex-import/` | Working files for a Plex import. |
| `config/cache/` | Artwork, seek previews, intro fingerprints and other caches. Safe to delete; rebuilt as needed (seek previews and intro detection take a few nights). |
| `models/` | Soundprint's models; downloaded again if deleted. |
| `recordings/` | Your Live TV recordings. |

The simplest full backup is a copy of the whole `config` folder taken while Marquee is stopped:

```bash
docker compose stop marquee
tar -czf marquee-config-$(date +%F).tar.gz -C /srv/marquee config
docker compose start marquee
```

## Moving to a new server

1. Stop Marquee on the old server.
2. Copy the whole `config` folder (and `recordings`, if you use Live TV) to the new server.
3. Install Marquee on the new server with the same container paths: `/media/video`, `/media/music`, `/recordings`. Libraries store paths inside the container, so if your media now lives somewhere else on the host, change the host side of the mount, not the container side.
4. Make sure `PUID:PGID` can read and write the copied folders, then start it.

If the server's address changes, update **Local server address** under **Settings → Network**, and the apps' saved address if they don't find the server by themselves.

---

[Guide contents](README.md) · Next: [First run](first-run.md) · [Troubleshooting](troubleshooting.md)
