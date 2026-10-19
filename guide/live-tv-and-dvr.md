# Live TV and DVR

Watch live channels from an M3U playlist or Dispatcharr, browse the guide, and record programmes and whole series.

Live TV needs one thing from you: a source of channels. That's either an **M3U playlist** with an XMLTV guide (from your IPTV provider, a tuner, or any other service that publishes one) or a **Dispatcharr** server. If you don't have either yet, [VOD and Dispatcharr](vod-and-dispatcharr.md#setting-up-dispatcharr-for-marquee) explains how to set up Dispatcharr in front of your provider. It's the better choice when your provider limits how many streams you can watch at once.

## Add a source

1. Open **Settings → Live TV** and choose **Add source**.
2. Pick the **Type**:
   - **Dispatcharr:** enter Dispatcharr's address, for example `http://192.168.1.20:9191`. Marquee reads the channels from `/output/m3u` and the guide from `/output/epg` on that address, so you don't need to type either.
   - **M3U playlist:** enter the **Playlist URL**. Add the **Guide (XMLTV) URL** too, unless the playlist names its own guide (an `url-tvg` line at the top).
3. Set **Connections allowed** to the number of channels your provider lets you stream at once (see [Connection limits](#connection-limits)). Leave it at 0 if there's no limit.
4. Choose **Save**.

Marquee loads the channels and the next two days of the guide within a few seconds. Each source's card then shows how many channels and guide entries it has, when it last loaded and, with a connection limit set, how many connections are in use. Errors are shown in red on the card (to admins only, because they can include the source's address and login).

Marquee reloads every source every four hours. **Reload channels and guide now** does it straight away.

You can add as many sources as you like. Turn **Enabled** off to set one aside without losing its settings.

### Source settings

| Setting | Default | What it does |
|---|---|---|
| Name | `Dispatcharr` | The source's name in Settings. |
| Type | Dispatcharr | **Dispatcharr** or **M3U playlist**. |
| Dispatcharr address | empty | Dispatcharr only. The server's base address, e.g. `http://192.168.1.20:9191`. |
| Playlist URL | empty | M3U only. The playlist's address. |
| Guide (XMLTV) URL | empty | M3U only, and optional when the playlist names its guide. |
| Connections allowed | `0` | How many channels the provider lets you stream at once. 0 means no limit. Other parts of Settings call this the source's **Maximum streams**. |
| User agent | empty | Only if your provider needs a particular one. |
| Enabled | On | Off hides the source's channels without deleting the source. |

## Watching

**Live TV** in the sidebar has three tabs:

- **Guide:** a grid of channels and programmes for today and tomorrow. Pick a channel to preview it at the top of the page (muted until you unmute it), or open it full screen. Pick a programme for its details and the record buttons.
- **What's On:** what every channel is showing now.
- **Recordings:** your DVR (see [Recordings](#recordings)).

The menu at the top right narrows the channels:

- **All channels.**
- **Favorites:** tap the heart next to a channel to add it.
- **Recently watched.**
- **Hidden channels:** hide a channel from a programme's details in the guide (**Hide … from the guide**). Show it again from this list.
- **Channel groups**, from the source's playlist (the `group-title` of each channel), with how many channels each holds.

Favourites, hidden channels and your chosen filter belong to each person.

In the full-screen player, the arrow keys (or Page Up and Page Down) change channel and Escape goes back to the guide. Marquee streams the channel as it comes, and transcodes it (on the GPU when you have one) when a device can't play it directly.

### Who can watch

Everyone can watch Live TV unless you turn it off for them: in **Settings → Users**, edit the person and turn off **Can watch Live TV**. People without Live TV also don't see VOD.

Channel groups don't limit anyone: everyone who may watch Live TV sees every group.

## Connection limits

Most IPTV providers allow only a set number of streams at once, often one or two. Tell Marquee the number with **Connections allowed** and it shares the connections out instead of letting a stream fail at the provider.

What holds one of a source's connections:

- **Each live channel being watched.** Two people watching the same channel share one connection when no other connection is free.
- **Each recording in progress.** Watching a channel that's being recorded uses the recording's connection, so it costs nothing extra.
- **VOD playback and VOD downloads,** when the VOD source is on the same server as the Live TV source (a Dispatcharr server providing both). Downloads only ever use connections nobody else needs; they give them back the moment someone starts watching or a recording starts. See [VOD downloads](vod-and-dispatcharr.md#connections-who-goes-first).

When every connection is in use:

- The source's other channels show as **unavailable**, dimmed in the guide, with the reason, for example *All connections are in use: recording Gold Rush, watching News One*. Channels already coming in can still be watched.
- A recording always gets a connection. If none is free when it starts, it stops a live stream on another channel; the viewer is told a recording needed the connection. People watching the channel being recorded carry on from the recording.
- VOD downloads from that provider stop and keep what they've downloaded, then carry on about 15 seconds after a connection comes free.

If you have two logins with your provider (two connections), Dispatcharr can combine them: see [Multiple provider logins](vod-and-dispatcharr.md#multiple-provider-logins).

## Recordings

### Where recordings go

Recordings are saved to the **recordings folder**: `/recordings` inside the container unless you choose another. In the compose file it's mapped from `RECORDINGS_DIR` in your `.env`:

```ini
RECORDINGS_DIR=/srv/media/DVR
```

On Unraid, map a host folder such as `/mnt/user/media/DVR` to `/recordings` in the template. The folder must be writable by `PUID:PGID`. If it isn't, **Settings → Live TV → Recording** says so in red and recording stays off.

You can also send new recordings to any other folder the container can write to with **Recordings folder** (type a path or **Browse**). Recordings already made stay where they are and keep working. Inside the folder, Marquee files recordings the way the scanner expects:

```text
DVR/
├── TV/
│   └── Show/
│       └── Season 1/
│           └── Show - S01E02 - Episode name.mkv   # or "Show - 2026-10-02 - Name.mkv" without an episode number
└── Movies/
    └── Film/
        └── Film.mkv
```

Recordings are copies of the channel's stream (no transcode), joined into an MKV when the programme ends. If the source drops part-way, Marquee reconnects and carries on, and a recording cut short by a restart carries on too if its programme hasn't ended.

### Recording a programme or a series

Pick a programme in the guide:

- **Record** records this airing once.
- **Record series** records every airing of the title, on any channel, as they appear in the guide. Marquee follows the guide as it changes: a moved airing is recorded in its new slot.
- **Don't record** cancels a recording that hasn't started.

Recordings start **1 minute early** and end **3 minutes late** by default (**Start early** and **End late** in **Settings → Live TV → Recording**).

### The Recordings tab

**Live TV → Recordings** is the only place recordings appear: they're kept out of your libraries, Home, search and the rest of Marquee. It lists:

- **Recording now,** with how far it's got.
- **Upcoming,** soonest first, with **Cancel**.
- **Series,** with how many airings are scheduled and **Stop series**. Stopping a series keeps what it has recorded.
- **Recorded,** newest first, with size, who recorded it, a progress bar or *Watched*, **Play** or **Resume**, and **Delete**. Failed recordings show why, with **Dismiss**.

**All recordings / Only mine** at the top switches between the household's recordings and your own; Marquee remembers your choice.

A **Recording now** strip also sits at the top of every Live TV tab while anything is recording, with the channel, title, time in and time left.

### Stopping a recording early

Both the Recording now strip and the Recordings tab offer:

- **Stop and keep:** stops now and keeps what was recorded as a normal, playable recording, marked *Stopped early*.
- **Stop and delete:** stops now and throws the partial recording away.

### Sharing and permissions

Recordings are shared with the household: everyone who may watch recordings sees and plays all of them, within their profile's rating limit, with *Recorded by …* on each. Only the person who scheduled a recording or series, or an admin, can cancel, stop or delete it.

In **Settings → Users**, for each person:

| Setting | Default | What it does |
|---|---|---|
| Can record Live TV | On | Schedule recordings and series. Off pauses their series (kept, but nothing new is recorded) until you turn it back on. |
| Can watch recordings | On | See and play the household's recordings (the Recordings tab). |
| Recording quota | Use the default | **Use the default** (the server's quota, never applied to admins), **No limit**, or **A set amount** in GB. |

These only show once a Live TV source is set up and the recordings folder can be written.

### Quotas

A quota caps the space a person's recordings take, counting finished recordings and those in progress, plus the episodes saved for VOD shows they [follow](vod-and-dispatcharr.md#following-a-show) that are still in the library. Their usage under **Live TV → Recordings** and in **Settings → Users** includes those episodes, and a followed show stops saving while the quota is full. Set the household default with **Recording quota per person** (0 = no limit) and override it per person in **Settings → Users**. Admins aren't held to the default, but you can give anyone (yourself included) a set amount.

**When someone is over quota** chooses what happens when one of their recordings is due:

- **Don't record anything new** (the default): the recording isn't made.
- **Remove their oldest watched recordings to make room:** their oldest watched recordings go to the media trash until there's room; if there aren't enough, the new one isn't made.

Either way, a recording in progress always finishes, and a recording someone is watching is never removed. Removed recordings can be restored from the media trash until it's emptied (the nightly **Empty media trash** task).

Each person sees their usage on the Recordings tab, and a warning on the record buttons when they're over. Admins see everyone's in the **Recording space** card under **Settings → Live TV**.

### Commercials

Marquee can find the advert breaks in recordings with [Comskip](https://github.com/erikkaashoek/Comskip), which is built into the Docker image. Choose what happens with **Commercials**:

- **Leave recordings alone** (the default): nothing is checked.
- **Detect only (keep the files as they are):** Comskip finds the breaks and the recording lists them (*found 4 breaks (12:30)*). The file isn't touched.
- **Remove commercials from recordings:** the breaks are found and cut out. Marquee joins the parts that are left into a new file (stream copy where it can, re-encoding only when a cut can't be made cleanly), checks it plays, and replaces the recording. The original goes to the media trash.

Recordings are checked when they finish, and the nightly **Commercials in recordings** task (in the maintenance window) catches older ones. It works on one recording at a time at the lowest CPU and disk priority, and a recording someone is watching waits (*will be removed when nobody's watching*).

On each recording in the Recordings tab, and in its **…** menu, admins also get:

- **Remove commercials now,** whatever the setting.
- **Restore original,** which puts the uncut file back while it's still in the media trash (it's emptied every night). The cut file goes to the trash instead.

Comskip's settings live in [`deploy/comskip.ini`](../deploy/comskip.ini), tuned for US broadcasts. The image copies it to `/usr/local/share/marquee/comskip.ini`. To use your own, mount a file into the container and set `MARQUEE_COMSKIP_INI` to its path. If you build Marquee without Comskip, the **Commercials** setting is disabled and says so.

### Recording settings

| Setting | Default | What it does |
|---|---|---|
| Start early | 1 min | Starts every recording this much early: none, 1, 2, 5 or 10 minutes. |
| End late | 3 min | Ends every recording this much late: none, 1, 3, 5, 10, 15, 30 or 60 minutes. |
| Commercials | Leave recordings alone | **Leave recordings alone**, **Detect only**, or **Remove commercials from recordings**. |
| Recording quota per person | `0` GB | Space each person's recordings (and followed VOD shows' episodes) may take. 0 = no limit. A person's own quota overrides it. |
| When someone is over quota | Don't record anything new | Or **Remove their oldest watched recordings to make room**. |
| Recordings folder | empty (`/recordings`) | Where new recordings go. Empty uses the server's folder. **Use default** goes back to it. |

These settings save as soon as you change them.

## Activity and alerts

Admins see every recording in progress in the **Activity** menu in the top bar, with its time left and a link to Live TV recordings.

**Settings → Notifications** has three alerts for Live TV. All are off until you turn them on:

- **A recording failed.**
- **A Live TV or movie source couldn't be loaded.** Checked whenever the lists refresh.
- **Someone is over their recording quota:** a recording wasn't made, or their oldest watched recordings went to the media trash.

## If something doesn't work

- **No channels, and the source shows "… answered 404":** check the address. For Dispatcharr, enter only the base address (`http://192.168.1.20:9191`), not a playlist link. If Dispatcharr limits which networks may fetch its M3U and EPG, allow the Marquee server's address.
- **Channels but an empty guide:** for M3U, add the **Guide (XMLTV) URL**. For Dispatcharr, give the channels guide data in Dispatcharr (see [EPG](vod-and-dispatcharr.md#4-add-a-guide-epg)).
- **A channel shows as unavailable:** every connection the provider allows is in use. The tooltip says by what.
- **A second stream fails, or shows the same picture as the first:** Dispatcharr is probably in Redirect mode. See [Multiple provider logins](vod-and-dispatcharr.md#multiple-provider-logins).
- **Recording is off:** the recordings folder can't be written. Check the mapping and that `PUID:PGID` owns the folder.

More in [Troubleshooting](troubleshooting.md).

---

[Install](install.md) · [First run](first-run.md) · [Server settings](settings-server.md) · [Media settings](settings-media.md) · **Live TV and DVR** · [VOD and Dispatcharr](vod-and-dispatcharr.md) · [Apps and remote access](apps-and-remote.md) · [Troubleshooting](troubleshooting.md)
