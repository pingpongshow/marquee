# VOD and Dispatcharr

Set up Dispatcharr for Marquee, watch your provider's films and shows on demand, save them to your library, and get suggestions from them.

**VOD** (video on demand) is the films and series your IPTV provider offers alongside its live channels, or free catalogues such as Pluto TV's films. Marquee lists them on their own **VOD Movies** and **VOD Shows** pages, matches them to TMDB for posters, cast and ratings, and streams them through the server while someone watches. Nothing is downloaded unless an admin saves a title to the library.

## Setting up Dispatcharr for Marquee

[Dispatcharr](https://github.com/Dispatcharr/Dispatcharr) is a separate, open-source IPTV manager. It sits between your provider and Marquee: it holds your provider logins, builds the channel list and guide, shares out your provider's connections, and offers it all to Marquee as one server. Marquee uses it twice:

- as a **Live TV source** (channels and guide), and
- as a **VOD source** (films and series, through Dispatcharr's Xtream Codes API).

Install Dispatcharr by following its own instructions. The examples here assume it runs at `http://192.168.1.20:9191`. Dispatcharr's screens change between versions, so the names below may differ slightly in yours.

### 1. Add your provider

In Dispatcharr, add your provider under **M3U Accounts**, as an M3U link or an Xtream Codes account (server, username and password). Set the account's maximum number of streams to what your provider allows. If you want the provider's films and series in Marquee, use the Xtream Codes type and turn on VOD for the account, if your version of Dispatcharr asks.

Let Dispatcharr load the account, then add the channels you want under **Channels**.

### 2. Choose channels with groups and profiles

Dispatcharr's channel groups become Marquee's channel groups (the menu at the top of the guide). Channel profiles let you offer different sets of channels to different players. Marquee's Dispatcharr source reads Dispatcharr's full channel list (`/output/m3u`). To give Marquee only one profile's channels, add the source in Marquee as an **M3U playlist** instead, with the profile's playlist link and Dispatcharr's EPG link (`http://192.168.1.20:9191/output/epg`).

### 3. Create a user for Marquee

Under **Users**, create a user for Marquee, for example `marquee`, and give it an **XC password** (Dispatcharr's name for an Xtream Codes password). Give it access to the channel profiles you want Marquee to see. Marquee's VOD source signs in with this username and XC password. Usernames are case-sensitive.

### 4. Add a guide (EPG)

Add your provider's XMLTV guide (or another guide source) under **EPG**, and match it to your channels. Marquee reads the result from Dispatcharr's `/output/epg`. Channels with no guide data show without programmes in Marquee.

### 5. Add Dispatcharr to Marquee

1. **Settings → Live TV → Add source.** Leave the type as **Dispatcharr**, enter `http://192.168.1.20:9191` as the **Dispatcharr address**, and set **Connections allowed** to how many streams your provider allows in total. Save. See [Live TV and DVR](live-tv-and-dvr.md) for the rest.
2. **Settings → VOD.** Turn on **Show VOD**, choose **Add source**, set the **Type** to **Dispatcharr / Xtream Codes**, and enter the **Server URL** (`http://192.168.1.20:9191`), the **Username** (`marquee`) and its XC **Password**. Press **Test**, then **Save**.

Use **exactly the same address** in both places. Marquee works out that the two sources share a provider by comparing their addresses (scheme, host and port), and only then can VOD downloads use the connections live TV leaves free. `http://192.168.1.20:9191` and `http://dispatcharr:9191` count as different servers.

A good **Test** reads something like *Account active · expires 01/12/2027 · 12408 films · 2311 shows · 96 categories*. If it fails:

| Message | What to check |
|---|---|
| The server doesn't know that username | The username, exactly (case matters), and that the address is Dispatcharr's base address. |
| Wrong password, or the user has no XC password set in Dispatcharr | Set an XC password on the user in Dispatcharr and type it again. |
| The server refused the username or password | The user's account in Dispatcharr, and that it may use the Xtream API. |
| The server answered 404 Not Found (or another code) | The address is wrong, or Dispatcharr's Xtream API isn't enabled for this user. |

### Multiple provider logins

Some providers allow one stream per login. If you have two logins, Dispatcharr can combine them so Marquee can play (or record, or download) two things at once:

1. Add each login to Dispatcharr, either as its own M3U account or as an extra profile on the same account (with that login's username and password). Give each one a maximum of **1** stream. Dispatcharr then sends each new stream to whichever login is free.
2. Set Dispatcharr's stream mode to **Proxy**, not **Redirect** (the stream profile used by your channels, or the default one in Dispatcharr's settings). With Redirect, Dispatcharr hands the player the provider's own link and the player connects to the provider directly. Dispatcharr then never sees the stream, so it can't count or balance connections, and a second stream at the same time fails or repeats the first channel. With Proxy, every stream goes through Dispatcharr.
3. In Marquee, set **Connections allowed** on the Live TV source to the total, here **2**.

To check the second login works, play two different channels at once in Marquee (on two devices, or one channel in the guide's preview and another full screen elsewhere). Both should play their own channel. Dispatcharr's **Stats** page shows the active streams and which account or profile each one uses: you should see one on each login.

## VOD sources

Add sources under **Settings → VOD**. There are two kinds:

- **Dispatcharr / Xtream Codes:** your provider's films and series through Dispatcharr (or any other Xtream Codes server). Series appear on **VOD Shows**.
- **M3U playlist:** a playlist of films, such as Pluto TV's free movies. Each film's `group-title` becomes its category. Playlists have films only.

**Test** checks a source as it stands in the form; a saved password is used when you haven't typed a new one. The password is never shown again after saving (*Saved — type to change*).

### Sync and TMDB matching

Marquee reloads each source's lists every **6 hours** by default, counted from the last successful sync. Turn off **Sync automatically** to reload only when you press **Refresh now** (and once at start for a source that has never loaded). Some sources' links, such as Pluto TV's, expire after about a day, so keep automatic sync on for those.

After each sync, Marquee matches new titles to TMDB, using the TMDB id the source gives when it has one, and the title and year otherwise. Each source's card shows how many films and shows it has and when it last loaded, and the line under the sources shows how many films are matched (*matching…* while it works). Matching needs a TMDB key in **Settings → Metadata**; until then titles show the source's own details. Titles TMDB has no confident match for keep the source's details and still play.

### VOD settings

| Setting | Default | What it does |
|---|---|---|
| Show VOD | Off | Off hides VOD Movies and VOD Shows everywhere. Titles and everyone's progress are kept. |
| Sync automatically | On | Reloads the sources' lists on a schedule. Off: only when you press Refresh now. |
| Every | 6 hours | How often to sync: 1, 2, 3, 4, 6, 8, 12, 24 or 48 hours. |
| Name | `Pluto TV movies` | The source's name, also used in the Source filter. |
| Type | M3U playlist | **M3U playlist** or **Dispatcharr / Xtream Codes**. |
| Playlist URL | empty | M3U only. |
| Server URL | empty | Xtream only. Dispatcharr's address, as the Live TV source uses it. |
| Username | empty | Xtream only. A Dispatcharr user with an XC password. |
| Password | empty | Xtream only. The user's XC password. Write-only. |
| User agent | empty | Only if the provider needs a particular one. It's sent when playing too. |
| Enabled | On | Off sets the source aside without deleting it. |

The rest of **Settings → VOD** is [Downloads](#vod-downloads-to-your-library), at the top, and [Saving to library](#saving-to-library-settings), at the bottom.

## Watching VOD

**VOD Movies** and **VOD Shows** sit in the sidebar after Live TV once VOD is on and a source has titles.

Each page has:

- **Recommended for you** at the top (see [Suggestions](#suggestions-from-vod)).
- **Continue Watching:** films and episodes you stopped part-way through. Remove one with **Remove from Continue Watching** in its **…** menu. VOD titles also appear in Home's Continue Watching.
- A **Category** menu (the source's own groups), a search box and **Sort**: Title, Recently added, Release year or Rating.
- **Filters:** watched state (Unwatched, In progress, Watched), genre, decade, minimum rating, **In my library** (yes or no) and source. Active filters show as chips you can clear one by one.
- A poster grid. Titles your library already has get a **green border** (*In your library*).

Your category, sort and filters are remembered for each page.

A film's page shows TMDB's details, cast, director and TMDB score, with **Play** (or resume), **Mark watched**, and the save buttons for admins. A show's page lists its seasons and episodes (fetched from the source when you open it), with watched marks, resume and Up Next between episodes.

Marquee plays VOD as a stream made on the server: the original video when the device can play it, otherwise a transcode. Your progress is kept like any library title, and a title past the watched threshold counts as watched.

### Who can see VOD

Everyone who may watch Live TV sees VOD, within their profile's rating limit. Restricted profiles don't see unrated titles, as in the library. Turn off **Can watch Live TV** in **Settings → Users** to hide both from someone. **Show VOD** off hides VOD from everyone.

## VOD downloads to your library

Admins can save films and episodes from Dispatcharr (Xtream Codes) sources into the library. Marquee downloads them in a queue, checks each file, names it the way the library expects, moves it into place and scans the library. M3U playlist sources can't be saved.

### Set it up

1. In **Settings → VOD → Saving to library**, choose a **Movies folder** and a **Shows folder**. Use the folders of a movies and a TV library so saved titles are scanned. Leaving one empty turns off saving for that kind.
2. Optionally add **More save folders**, for example an **Anime** folder for shows. Give it a name, choose what it **Saves** (Movies and shows, Movies, or Shows) and pick the folder. It then appears beside the save buttons as **Save to Anime**. A warning shows when no library includes a folder.
3. Choose the **Quality** and the other options below, then **Save**.

Saved files follow Radarr and Sonarr's layout, with the quality in the name:

```text
Movies/Blade Runner (1982)/Blade Runner (1982) WEBDL-1080p.mkv
TV/The Expanse (2015)/Season 1/The Expanse - S01E01 - Dulcinea WEBDL-1080p.mkv
```

Names use TMDB's titles so the library matches them. An existing folder for the same film or show is reused, with its spelling.

### Saving titles

On a film's page, **Save to library** queues it. On a show's page, **Save show** and **Save season** queue many episodes at once, and each episode has its own save button.

The buttons compare the VOD copy with your library's:

- **Save to library:** the library doesn't have it.
- **Upgrade (1080p HEVC → replaces 720p H.264):** the VOD copy is better. The new file replaces the library's copy in its current folder, and the old file goes to the media trash once the new one has passed its checks. The library item, with everyone's watch history, stays.
- **In library (same quality)** or **In library (better quality):** nothing to save. Hover for both copies' details and why.

For a show or season, **Save missing (N)** queues only the episodes the library doesn't have, and **Upgrade (M)** also replaces the ones the VOD copies beat.

### The queue

The queue is under **Downloads** at the top of **Settings → VOD** (the **Activity** menu in the top bar links to it too). Each save shows its state (Queued, Downloading, Converting, Verifying, Paused, Saved, Failed, Cancelled or Skipped), size, speed and time left, why it's waiting, and where it's going.

- Each save has **Move to top**, **Pause**, **Resume**, **Retry** and **Cancel**. Pausing keeps what was downloaded; cancelling throws it away.
- **Pause queue** stops everything; saves keep what they downloaded and carry on when you **Resume queue**.
- Downloads resume where they stopped, after a pause, a network failure or a restart.

**Failed** saves have their own list. They're never removed automatically: only **Retry** or **Clear failed** removes them, and they keep their partial download until then, so Retry carries on from where it stopped. With **Retry failed saves** on, Marquee also retries them every 4 hours. Network failures are retried on their own first (five tries over about half an hour).

**Finished** (saved, cancelled and skipped) saves are kept for 5 days, at most the newest 10. **Clear finished** removes them now and leaves the Failed list alone.

### Checks before a file goes into the library

Before a saved file is moved into the library, Marquee checks that it has video, that its length matches what was expected, and that its first and last seconds decode without errors. A file that fails is downloaded again once, then marked failed with the reason. Marquee also checks there's enough disk space before downloading. Upgrades are checked against the length of the library copy they replace.

### Connections: who goes first

Providers allow only so many streams at once, and downloads would use them up. How Marquee shares them depends on whether it knows your limit.

**With a limit known.** When the VOD source shares its address with a Live TV source that has **Connections allowed** set (the [same address](#5-add-dispatcharr-to-marquee)), downloads use only the connections nobody else needs:

- Viewers and recordings always come first. Live channels, recordings and VOD playback from that server all count.
- Downloads use what's left: as many saves at once as there are free connections, up to 4. A large file (200 MB or more) is split over spare connections and downloaded in parts, each resuming on its own.
- When someone starts watching or a recording starts, downloads give back connections straight away (the newest first) and keep their data. They take them back about 15 seconds after the viewer stops, so flicking between channels doesn't stop and start them.

With **Pause while someone is watching** off, downloads only give way to recordings. With **Pause for** set to **Any playback on the server**, downloads pause whenever anything plays, as below.

**Without a limit.** Marquee runs **Saves at once per source** saves at a time and, with **Pause while someone is watching** on, pauses them while someone watches and carries on about 15 seconds after.

### The maintenance window

Turn on **Only during the maintenance window** to download only inside the window set in **Settings → Scheduled Tasks** (03:00 for 4 hours by default). Outside it, downloads stop and keep what they've downloaded.

### Following a show

Turn on **Let people follow series** in **Saving to library** and VOD show pages get a **Follow** button. Following a show saves its new episodes to the library automatically as they appear on the source, through the same queue (with its connection sharing, maintenance window, checks and Failed list). Anyone with VOD access who may record Live TV can follow (admins always can); follows are each person's own.

When following, you choose:

- **Size limit (GB):** saving stops once the episodes this follow saved (those still in the library) reach it. It starts at **Size limit for a new follow**; 0 means no limit of its own.
- **Also save episodes already there:** saves the whole show from the start, not only new episodes.
- **Save to:** the shows folder or another save folder for shows (admins only).

Episodes the library already has, or that were already saved or queued, are skipped. A follow keeps up to 3 episodes in the queue at a time and adds the next as they finish. Marquee looks for new episodes after each VOD sync, whenever a show's episodes are reloaded, and every 30 minutes.

**Saved episodes count towards the follower's recording quota** with their recordings (see [Quotas](live-tv-and-dvr.md#quotas)). A follow stops saving while it's at its size limit (**Full**) or the follower's recording space is full, and it's paused while following is turned off or the person may no longer record. It carries on by itself once there's room again, for example after episodes are deleted. Its status shows on the show page (**Following · 3 episodes · 4.2 GB**, with the reason when it's stopped) and in the **Following** row on **VOD Shows**.

**Unfollow** on the show page stops it. The episodes already saved stay in the library and no longer count towards the quota. Admins see everyone's follows under **Followed series** in **Settings → VOD**, and can unfollow any of them.

### Alerts

In **Settings → Notifications**:

- **VOD download failed** is on by default. It's sent when a save fails after its retries.
- **Saving to the library finished** sends one summary when the queue empties. It's off by default.
- **Someone is over their recording quota** is also sent when a followed show stops saving because its follower's recording space is full.

### Saving to library settings

| Setting | Default | What it does |
|---|---|---|
| Movies folder | not set | Where films are saved. Not set: saving films is off. |
| Shows folder | not set | Where episodes are saved. Not set: saving episodes is off. |
| More save folders | none | Extra folders, each with a **Name**, what it **Saves** (Movies and shows, Movies, or Shows) and a **Folder**. |
| Quality | Original | **Original** keeps the file as it is (an MPEG-TS or other unusual container becomes MKV). **Remux to MKV** copies every stream into MKV. **Transcode** converts with the settings below. |
| Saves at once per source | `1` | 1 to 5. Used when Marquee doesn't know the provider's limit. |
| Resolution | 1080p | Transcode only. A cap (2160p, 1080p, 720p or 480p); smaller videos aren't upscaled. |
| Video codec | HEVC (H.265) | Transcode only. HEVC or H.264, on NVENC, Quick Sync or the CPU. |
| Video quality | Balanced | Transcode only. High, Balanced or Small files (constant quality). |
| Video bitrate (kbps) | empty | Transcode only. A target bitrate instead of the quality setting. |
| Audio | Copy (as it is) | Transcode only. Copy, AAC stereo, or AAC up to 5.1. |
| Scan the library after saving | On | Scans the library whose folder holds the saved file. |
| Skip titles already in the library | On | By TMDB id, or when the file is already in its folder. |
| Pause while someone is watching | On | See [Connections](#connections-who-goes-first). |
| Pause for | Watching from the same source | **Watching from the same source (including its Live TV)**, or **Any playback on the server** (saves bandwidth and GPU). |
| Retry failed saves | On | Every 4 hours, failed saves are tried again from where they stopped. |
| Only during the maintenance window | Off | On: downloads run only inside the maintenance window. |
| Let people follow series | Off | A **Follow** button on VOD show pages: new episodes are saved automatically. See [Following a show](#following-a-show). |
| Size limit for a new follow (GB) | `0` | The size limit a new follow starts with. 0 = no limit (only the recording quota). |

## Suggestions from VOD

Marquee suggests VOD titles from each person's own taste: their favourites (counted most), ratings and what they've watched. Suggestions never come from what other people watch. They leave out titles the person's libraries already have, titles they've started or watched, titles above their rating limit, and anything they've marked **Not interested**.

Suggestions appear in several places:

- **Home:** VOD picks are mixed into Home's suggestion rows (Recommended for You, Top Rated, Trending Now, the genre rows and the holiday row) beside the library's titles, with a **VOD** badge on their posters. Older app versions show them in their own **Watch on VOD** row instead.
- **Also on VOD,** at the bottom of a library film's or show's page: VOD titles like this one.
- **Recommended for you,** at the top of VOD Movies and VOD Shows. **See all** opens the **Recommended** page.
- **Muse:** tick **Include VOD** under the search box in Muse for movies to get VOD titles that fit your description beside the library's.
- **Discover:** a title that isn't in your library but is on VOD shows **Watch now on VOD**, on its tile and in its request dialog, so you can watch it instead of requesting it. Administrators, and people allowed **Can download from VOD** (Settings → Users), also see **Download** beside it when the title can be saved (an Xtream Codes source, with a download folder set). It queues the film, or a show's episodes the library doesn't have, in the download tool's default folder; a film already queued or saved shows that instead.

### The Recommended page

The Recommended page (Movies or Shows) goes deeper, in rows that each take a different angle:

- **Top picks for you.**
- **Because you love …** (or *loved*, *watched*, *Similar to*): one row for each of your most liked titles, from TMDB's recommendations for it.
- **More** of your favourite genres, for example **More Science Fiction**.
- **Critically acclaimed:** TMDB's best rated with plenty of votes, ordered by your taste.
- **Popular on VOD:** well-rated titles with many TMDB votes.

A title appears in only one row. Rows that can't be filled are left out. A new person with no history yet gets Popular on VOD.

### Not interested

Every suggestion has a **…** menu with **Not interested**, which stops that title being suggested to you anywhere, and **Go to film** or **Go to show**.

### Fetch VOD suggestion data

Suggestions need TMDB's recommendations, keywords and vote counts for the VOD catalogue. The **Fetch VOD suggestion data** task in **Settings → Scheduled Tasks** fills them in at four titles a second, best-rated first. It runs in the maintenance window and carries on the next night where it stopped. After a sync matches new titles, it also fetches up to 200 of them straight away. **Run now** fetches everything. A large catalogue can take a few nights the first time.

Each person's suggestions are worked out again when their taste changes, when the catalogue changes, and at least twice a day.

---

[Install](install.md) · [First run](first-run.md) · [Server settings](settings-server.md) · [Media settings](settings-media.md) · [Live TV and DVR](live-tv-and-dvr.md) · **VOD and Dispatcharr** · [Apps and remote access](apps-and-remote.md) · [Troubleshooting](troubleshooting.md)
