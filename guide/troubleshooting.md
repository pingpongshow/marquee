# Troubleshooting

Common problems, what causes them and how to fix them, plus where to find the logs.

## Contents

- [Where to look first](#where-to-look-first)
- [The server](#the-server)
- [Apps and signing in](#apps-and-signing-in)
- [Playback and transcoding](#playback-and-transcoding)
- [Libraries and metadata](#libraries-and-metadata)
- [Music](#music)
- [Live TV and DVR](#live-tv-and-dvr)
- [VOD downloads](#vod-downloads)
- [Library Health and Large files](#library-health-and-large-files)
- [Reporting a bug](#reporting-a-bug)

## Where to look first

**Settings → Logs** shows the most recent 5,000 entries since the server started, refreshing every few seconds. Set **Minimum level** to **Warnings** or **Errors** to find problems quickly.

The same log is written to `config/logs/marquee.log` (rotated at 10 MB, with five older files kept), which survives restarts. You can also follow it from Docker:

```bash
docker logs -f marquee
docker logs -f marquee-soundprint
```

For more detail, set `MARQUEE_LOG_LEVEL=debug` (in `.env`, or the template's variable on Unraid) and restart the container. Set it back to `info` afterwards.

**Settings → Dashboard** shows what's playing, how each stream is delivered (direct play, remux or transcode) and which encoder each transcode uses.

## The server

<details>
<summary><b>The container won't start or keeps restarting</b></summary>

Look at `docker logs marquee`. Common causes:

- **`could not select device driver "nvidia"`** or **unknown runtime "nvidia"**: the container asks for the NVIDIA runtime but the host doesn't have it. On Unraid, remove `--runtime=nvidia` from *Extra Parameters* if you have no NVIDIA GPU, or install the **Nvidia Driver** plugin. With compose, leave out `compose.nvidia.yaml`, or install the NVIDIA Container Toolkit.
- **`create /config: permission denied`**: the `config` folder isn't writable by `PUID:PGID`. Fix its owner (see [Folders and permissions](install.md#folders-and-permissions)).
- **`invalid MARQUEE_PORT`** or **`invalid MARQUEE_LOG_LEVEL`**: check those variables.
- **Port already in use**: something else on the server listens on 32500. Stop it, or set `MARQUEE_PORT` to another port.
</details>

<details>
<summary><b>The web page doesn't open</b></summary>

- Check the container is running and healthy: `docker ps` shows `(healthy)` after about 20 seconds.
- Use the server's own address and port 32500, e.g. `http://192.168.1.20:32500`. Marquee uses host networking, so there's no port mapping to check.
- Use `http://`, not `https://`, at home: Marquee serves plain HTTP. HTTPS comes from `tailscale serve`, at its `https://….ts.net` address.
- A firewall on the server (ufw, firewalld) must allow 32500/tcp.
</details>

## Apps and signing in

<details>
<summary><b>The apps don't find the server</b></summary>

Automatic discovery uses Bonjour/mDNS. It needs:

- `network_mode: host` (as in the compose file; Unraid's template uses host networking too),
- **Advertise on the local network (Bonjour)** on, under **Settings → Network**,
- your home network's range in **LAN subnets** on the same page: Marquee only advertises on network interfaces with an address inside those subnets (never Docker or Tailscale interfaces),
- the phone or TV on the same network as the server, not a guest network or a VLAN that blocks multicast,
- 5353/udp allowed by any firewall on the server.

The log line `bonjour: advertising` shows which interfaces it uses. If your network blocks multicast, type the server's address in the app instead, e.g. `192.168.1.20:32500`.
</details>

<details>
<summary><b>The app can't connect away from home</b></summary>

- Set **Remote server address** under **Settings → Remote Access**, with **Allow remote streaming** on, then open the app once at home so it learns the address. Or type the Tailscale address into the app while you're away.
- Check Tailscale is connected on the phone, and that the address opens in the phone's browser.
- If the person has **Allow streaming away from home** off in **Settings → Users**, they can only use Marquee at home.
</details>

<details>
<summary><b>The profile picker doesn't appear, or PINs don't work</b></summary>

With **PIN sign-in** set to **On the home network only** (the default), the *Who's watching?* picker only appears on the home network. Away from home, sign in with a password or Quick Connect. If the picker is missing at home too, the server thinks you're remote: check **LAN subnets** in **Settings → Network** (the top of that page says whether your browser counts as Local or Remote).

Five wrong PINs lock a profile for 15 minutes. An account with two-factor sign-in can't sign in with only a PIN from outside the home.
</details>

<details>
<summary><b>Quick Connect codes are refused</b></summary>

Enter the 6-character code exactly as the TV shows it, while it's still on screen, in **Account → Link a device** (or `http://<server>:32500/link`). The device signs in as whoever approves it, so approve it from the right account.
</details>

## Playback and transcoding

<details>
<summary><b>Videos transcode on the CPU even though I have a GPU</b></summary>

1. **Check what Marquee found.** At start the log has a line like `hardware encoders nvenc=true qsv=false`. If your GPU shows `false`, the container can't see it.
2. **NVIDIA:** start with `compose.nvidia.yaml`, or keep `--runtime=nvidia` in Unraid's *Extra Parameters*. The NVIDIA Container Toolkit (or Unraid's Nvidia Driver plugin) must be installed, and this must work on the host:
   ```bash
   docker run --rm --runtime=nvidia nvidia/cuda:12.8.0-base-ubuntu24.04 nvidia-smi
   ```
   Check `NVIDIA_VISIBLE_DEVICES` is `all` or your GPU's UUID, and `NVIDIA_DRIVER_CAPABILITIES` includes `video`.
3. **Intel:** pass `/dev/dri` into the container (`compose.intel.yaml`, or the **Intel Quick Sync** device on Unraid), and add the group that owns the render device: `RENDER_GID` in `.env`, or `--group-add=<id>` on Unraid.
   ```bash
   stat -c %g /dev/dri/renderD128
   ```
4. **Check the encoder order** in **Settings → Transcoder → Hardware encoders**. NVIDIA NVENC or Intel Quick Sync should come before Software (CPU), and must not have been removed from the list.
5. **Busy GPU:** consumer NVIDIA drivers limit how many encodes run at once. Beyond **NVIDIA encode sessions** (default 8), new streams use Quick Sync or the CPU instead of failing.

The Dashboard shows the encoder each session uses.
</details>

<details>
<summary><b>Playback stutters or buffers</b></summary>

- On the Dashboard, check whether the stream is transcoding and on which encoder. A CPU transcode of 4K HDR is slow; set up hardware transcoding.
- Away from home, set **Internet upload speed** under **Settings → Remote Access** (use **Test internet speed**) so Marquee doesn't send more than your connection can carry.
- The transcode scratch space is in RAM (8 GB by default). If the server is short of memory, make it smaller with `TRANSCODE_SIZE`, or the tmpfs size on Unraid.
- If only the picture stutters at original quality, the sound is fine and a lower quality plays smoothly, the file's frames may be timed out of order. Look for it in **Settings → Library Health → Frame timing errors** and press **Repair** (MP4 and MOV files).
</details>

<details>
<summary><b>Remote viewers get full quality and buffer, or local viewers get low quality</b></summary>

Marquee decides local or remote by the device's address: **LAN subnets** in **Settings → Network** are local, and everything else (including Tailscale) is remote.

- If a home device counts as remote, add its subnet to **LAN subnets**.
</details>

<details>
<summary><b>Casting from the web app doesn't work</b></summary>

Browsers only allow Chromecast from HTTPS pages, so casting works from the iPhone, iPad and Android apps, or from the web app over `tailscale serve` (HTTPS).
</details>

## Libraries and metadata

<details>
<summary><b>Libraries are empty or files are skipped</b></summary>

- The container runs as `PUID:PGID`; that user needs read access to every media folder. Check with `ls -n`.
- Libraries use paths inside the container (`/media/video/...`, `/media/music`), not paths on the host.
- Files matching the ignore patterns (`*sample*`, `*.part`, hidden files and others) are skipped. See **Ignore files matching** in **Settings → Libraries**.
- Each library shows the result of its last scan. **Settings → Logs** lists files that couldn't be read or probed.
</details>

<details>
<summary><b>Nothing gets matched</b></summary>

Matching needs a TMDB key (**Settings → Metadata**). Use **Fix Match** on anything matched wrongly, and include the year in folder names. Library Health lists everything still unmatched.
</details>

<details>
<summary><b>New files don't appear</b></summary>

File watching (**Scan automatically when files change** in **Settings → Libraries**) picks up new files within seconds on local disks. Network shares (SMB, NFS) and some FUSE file systems don't send change events, so the library's **Full rescan every (hours)** catches them instead. Scan a library now with its refresh button.
</details>

## Music

<details>
<summary><b>Music radios and Muse say analysis isn't running</b></summary>

The Soundprint container must be running (`docker compose ps`), with the same music folder mounted at `/media/music`. On first start it downloads about 1.5 GB of models into `models`, which needs to be writable. Analysis of a large library takes a while the first time; progress is shown under **Settings → Music**.
</details>

## Live TV and DVR

<details>
<summary><b>A Live TV source fails with "answered 404"</b></summary>

The error names the address Marquee tried.

- **Dispatcharr:** enter only Dispatcharr's own address in **Dispatcharr address**, e.g. `http://192.168.1.20:9191`. Marquee adds `/output/m3u` and `/output/epg` itself, so an address that already ends in a path gives a 404.
- **M3U playlist:** open the **Playlist URL** and **Guide (XMLTV) URL** in a browser to check they work. Some providers need a particular **User agent**.
</details>

<details>
<summary><b>The guide loads but channels won't play (Dispatcharr)</b></summary>

Check the channel plays in Dispatcharr itself first. If it plays there but not in Marquee, look at how Dispatcharr hands out streams. In **Proxy** mode, Marquee always streams from Dispatcharr. In **Redirect** mode, Dispatcharr sends Marquee straight to the provider's own address, and some providers refuse that connection (a 404 or 403 in the log) or count it against your connection limit. Switching the channels' stream profile to Proxy in Dispatcharr usually fixes it.

Also check **Connections allowed** matches what your provider allows: at the limit, other channels show as unavailable.
</details>

<details>
<summary><b>"The recordings folder can't be written, so recording is off"</b></summary>

The folder in **Settings → Live TV → Recordings folder** (or `/recordings` by default) must be mounted read/write and writable by `PUID:PGID`. If you chose your own folder, make sure it's mounted into the container. See [A separate DVR folder on Unraid](install.md#a-separate-dvr-folder-on-unraid).
</details>

<details>
<summary><b>Recordings don't appear in the library</b></summary>

If Marquee says no library includes the recordings folder, add it to the **Recorded TV** library in **Settings → Libraries**. Check too that the person has **Can watch recordings** on in **Settings → Users**, and that the recording's rating is within their limit.
</details>

## VOD downloads

<details>
<summary><b>A VOD source says "the server doesn't know that username"</b></summary>

Dispatcharr answers 404 for a username it doesn't have. Usernames are case-sensitive, so type it exactly as in Dispatcharr, and check the address. "Wrong password, or the user has no XC password set in Dispatcharr" means the user needs an XC password in Dispatcharr. See [VOD and Dispatcharr](vod-and-dispatcharr.md).
</details>

<details>
<summary><b>A download says "Verification failed"</b></summary>

Each saved film or episode is checked before it goes into your library: it must have video, its length must match what the source said (within 2%, or a minute for a runtime that isn't the file's own), and its first and last seconds must decode cleanly. A file that fails is downloaded again once by itself; if it fails twice, it's marked failed with the reason.

| Reason | What it usually means |
|---|---|
| *it is 1:12:05 long; expected 1:58:30* | The source sent a cut-short file. Try again later, or another source. |
| *the end doesn't decode* / *the start doesn't decode* | The download was damaged or truncated. |
| *the file is … bytes; the source said …* | The connection dropped before the end. |
| *it has no video* | The source sent audio only or an error page. |

With **Retry failed saves** on (the default, **Settings → VOD**), failed saves are queued again every few hours. You can also choose **Retry** on the item in the **Downloads** page.

Other failures: *not enough disk space* (free some space in the save folder) and *couldn't write the file* (the save folder must be writable; see [Folders and permissions](install.md#folders-and-permissions)).
</details>

## Library Health and Large files

<details>
<summary><b>"Marquee can't write to this folder; the media mount is read-only"</b></summary>

Deleting a file from Library Health moves it to the media trash, inside the library folder, so the video folder must be writable: set `VIDEO_MODE=` (empty) in `.env` and run `docker compose up -d`, or set the **Video** path to *Read/Write* on Unraid. **Allow deleting media files** must also be on in **Settings → Libraries**. If the mount is writable, check the folder's owner matches `PUID:PGID`.
</details>

<details>
<summary><b>Large files: "Only X% smaller … the original was kept"</b></summary>

A re-encoded file replaces the original only if it's at least the **Keep the new file when it's at least … smaller** amount smaller (20% by default). Otherwise it's thrown away, the original kept, and the file marked skipped. This is normal for files that are already efficiently encoded, such as HEVC at a modest bitrate. "It came out bigger" means the same, more so.

To get more out of it, pick a smaller preset (lower resolution, a lower quality setting, or a **Maximum size per hour (GB)** with AAC audio, since copied lossless audio takes 1.5–2 GB an hour), or lower the threshold. Files optimized before can be queued again: use the **Optimized before** filter, or **Re-optimize** in the history. See [Media settings](settings-media.md).
</details>

<details>
<summary><b>Large files: "The re-encoded file didn't check out, so the original was kept"</b></summary>

The new file failed its checks, and nothing was changed. The reason follows the message. Check the encoder works (see [GPU not used](#playback-and-transcoding)) and look in **Settings → Logs** around that time.
</details>

## Reporting a bug

Open an [issue on GitHub](https://github.com/pingpongshow/marquee/issues) with:

- what you did, what you expected and what happened,
- the app and its version, and the server version (shown on **Settings → Dashboard**),
- how you run Marquee (compose or Unraid, and which GPU),
- the relevant lines from **Settings → Logs** or `config/logs/marquee.log`.

Check logs for anything private before posting: addresses, usernames and file names. Please don't report security problems in public issues: use GitHub's [private vulnerability reporting](https://github.com/pingpongshow/marquee/security/advisories/new) instead.

---

[Guide contents](README.md) · [Install the server](install.md) · [Apps and remote access](apps-and-remote.md) · [Live TV and DVR](live-tv-and-dvr.md) · [VOD and Dispatcharr](vod-and-dispatcharr.md)
