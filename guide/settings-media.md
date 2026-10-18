# Media settings

Everything about your media itself: libraries and scanning, metadata providers, transcoding, music, AI, cinema trailers, requests, Plex, Library Health and the Large files optimizer.

Everything here is under **Settings** in the web app and needs an administrator account. Most sections have a save bar at the bottom: nothing changes until you press **Save**.

## Contents

- [Libraries](#libraries)
- [Metadata](#metadata)
- [Transcoder](#transcoder)
- [Music](#music)
- [AI](#ai)
- [Cinema Trailers](#cinema-trailers)
- [Requests](#requests)
- [Plex](#plex)
- [Library Health](#library-health)
- [Large files optimizer](#large-files-optimizer)

## Libraries

**Settings → Libraries** lists your libraries, with each one's folders, item count and last scan. While a library scans you see its progress (finding files, scanning, getting metadata); the square button stops a scan.

- **Scan all** scans every library now.
- The arrows button next to a library scans just that one.
- The pencil edits it; the bin deletes it. Deleting a library removes its metadata, artwork and watch history from Marquee. Your media files are not touched.
- When a library has titles whose files are gone, it says how many under its last scan, with **Remove missing titles**. Confirming removes them now, with their watch history, instead of waiting for **Remove missing titles after (days)** (below). Only folders that can be reached count: if every folder of the library is offline, nothing is removed.

### Adding a library

Press **Add library**. The dialog has three steps.

**Type**

| Setting | Default | What it does |
|---|---|---|
| Type | Movies | **Movies** (`Movie (Year)/Movie (Year).mkv`), **TV Shows** (`Show/Season 01/Show - S01E01.mkv`), **Anime** (TV layout with anime metadata and episode numbering), **Music** (organised by embedded tags) or **Other Videos** (home and personal videos, organised by folder). Can't be changed later. |
| Library name | The type's name | For example "Movies" or "Home Videos". |
| Language | English (US) | Language for this library's titles, summaries and artwork. |

**Folders**

Browse to one or more folders inside the container, for example `/media/video/Movies`. A library can have several folders; their contents are combined. The folder picker can only browse the folders listed under **Folders the folder picker can browse** (below).

**Advanced** (optional)

| Setting | Default | What it does |
|---|---|---|
| Episode ordering | Aired (by season) | TV Shows and Anime only. **Aired (by season)**, **Absolute** (episodes numbered 1…n, common for anime) or **DVD**. |
| Show on Home | On | Includes this library's rows (Recently Added, Continue Watching) on the Home screen. |
| Include in search | On | Includes this library in search results. |
| Full rescan every (hours) | `24` | A safety net: file watching picks up new files instantly. 0 = never. |
| Ignore files matching | (empty) | Extra patterns for this library, added to the server-wide ones. Example: `*_staging.*`. |

The first scan starts as soon as you add the library.

### Scanning

These apply to all libraries.

| Setting | Default | What it does |
|---|---|---|
| Scan automatically when files change | On | Watches library folders and picks up new, renamed and deleted files within seconds. |
| Scan all libraries when the server starts | Off | Runs a full scan of everything at every start. |
| Find intros and credits | On | Compares the audio of episodes in each season, in the maintenance window, so Skip Intro and Skip Credits work on new episodes. Markers imported from Plex are kept. |
| Make seek previews | On | Thumbnails shown while seeking through videos, made in the maintenance window. Uses roughly 3–7 MB per movie; a large library takes a few nights. |
| Mark as watched after (%) | `90` | 50 to 100. Progress past this point counts as watched. |
| Ignore files matching | `*_staging.*`, `*.part`, `*.partial`, `*.tmp`, `*sample*`, `.*` | Temporary and partial downloads, samples and hidden files are skipped. Patterns use `*` and `?` wildcards. |
| Folders the folder picker can browse | `/media` | Paths inside the Marquee container. Add any other folder you mount, for example `/recordings`. |

**Recommended:** keep file watching on. If your media is on a network share that doesn't report changes, keep **Full rescan every (hours)** at 24 or lower.

### Media files

| Setting | Default | What it does |
|---|---|---|
| Remove missing titles after (days) | `7` | How long a title whose files are gone stays greyed out before a scan removes it, with its watch history. 0 = never: missing titles stay until you use **Remove missing titles**. See [Missing files](#missing-files). |
| Allow deleting media files | Off | Lets administrators delete media files from Library Health. Deleted files move to a `.marquee-trash` folder in their library folder and are removed for good by the nightly **Empty media trash** task. |

Deleting needs write access: the video folder is mounted read-only unless you set `VIDEO_MODE=` (empty) in `.env`. See [Install](install.md).

### Missing files

When a scan can't find a file that used to be there, for example because you deleted it or moved a show folder from your TV library to your Anime library, the title stays in the library greyed out as unavailable. If the file comes back (or turns up in another folder of the same library), the title is available again with its watch history.

After **Remove missing titles after (days)** the next scan removes the title for good, along with its watch history, and any season or show left empty. Titles are only removed from library folders that can be read and aren't empty, so a drive that's offline or not mounted (which often leaves an empty folder behind) never loses its titles.

## Metadata

**Settings → Metadata** holds the API keys for online metadata. Movies, TV and anime use TMDB, with AniList for anime (no key needed). Music uses MusicBrainz (no key needed).

Keys are write-only: once saved they show as **Configured**, and you can **Replace** or **Remove** them.

| Setting | Default | What it does |
|---|---|---|
| TMDB API key | (empty) | **Required** for movie and TV metadata. Free at themoviedb.org → Settings → API. |
| Fanart.tv API key | (empty) | Optional. Adds logos, clear art and artist images. |
| OpenSubtitles API key | (empty) | Optional. Turns on **Find subtitles** in the player. Get one at opensubtitles.com → API consumers. |
| OpenSubtitles username | (empty) | Shown once an OpenSubtitles key is set. Optional; signing in allows more downloads a day. |
| OpenSubtitles password | (empty) | Stored on the server and only sent to OpenSubtitles. |
| OMDb API key | (empty) | Optional. Adds IMDb, Rotten Tomatoes and Metacritic scores for movies and shows. Free keys (omdbapi.com) allow 1,000 lookups a day; a large library fills in over a few days. |
| Daily lookup limit | `950` | Keep it below your OMDb key's daily allowance. Ratings refresh every 30 days. |
| Default episode numbering | Seasonal | Anime only. **Seasonal** matches TVDB-style seasons; **Absolute** numbers episodes 1…n. Each show can override it. |

Matching and artwork start as soon as you save a TMDB key.

## Transcoder

**Settings → Transcoder** controls how Marquee converts video when a device can't play a file as it is.

### Hardware encoders

Encoders are tried in this order. If one fails or is at capacity, the next is used automatically. Use the arrows to reorder, the bin to remove one, and the **+** buttons to add one back.

| Encoder | In the default order | What it is |
|---|---|---|
| NVIDIA NVENC | 1st | GPU encoder. Fastest, handles many streams at once. |
| Intel Quick Sync | 2nd | Intel integrated graphics. Used when the NVIDIA GPU is busy or unavailable. |
| VA-API | Not added | Generic Linux hardware encoding (AMD or Intel). |
| Software (CPU) | 3rd | Always available. Slowest; the last resort. |

An encoder your server doesn't have is simply skipped, so the default order works on any hardware.

### Quality

| Setting | Default | What it does |
|---|---|---|
| Encoder preset | Balanced | **Prefer speed**, **Balanced** or **Prefer quality**. Quality uses more GPU or CPU time per stream. |
| Maximum simultaneous transcodes | `12` | Further streams wait or fall back to lower quality. |
| NVIDIA encode sessions | `8` | NVENC jobs that may run at once. Beyond this, new streams use Quick Sync or the CPU instead of failing. 0 = no limit. |
| Use HEVC for remote streams | On | About 40 % less bandwidth at the same quality on devices that support it (all recent Apple devices). |
| HDR tone mapping | On | Converts HDR to SDR when a device can't display HDR, so colours aren't washed out. |
| Transcode ahead (segments) | `10` | How far ahead of the viewer the transcoder works before pausing. Each segment is 6 seconds. |

**Recommended:** set **NVIDIA encode sessions** to what your card and driver allow. Consumer NVIDIA drivers limit how many encodes can run at once; if streams fail when several people watch, lower it. The Large files optimizer uses this limit too, and always leaves one session free for playback.

### Remote quality ladder

The steps offered to remote devices. Marquee starts at the highest step the connection supports and moves between steps as bandwidth changes. Each step has a **Label**, a **Max height** and a **Video kbps**; use **+ Add step** and the bin to change the list.

| Label | Max height | Video kbps |
|---|---|---|
| 1080p 12 Mbps | 1080 | 12000 |
| 1080p 8 Mbps | 1080 | 8000 |
| 720p 4 Mbps | 720 | 4000 |
| 480p 1.5 Mbps | 480 | 1500 |
| 360p 750 kbps | 360 | 750 |

How much bandwidth remote streams may use overall is set under **Settings → Remote Access**. See [Apps and remote access](apps-and-remote.md).

## Music

**Settings → Music** covers Soundprint analysis, artist details, lyrics, volume levelling and Last.fm.

### Soundprint analysis

Marquee listens to every track on your server to power radios, "sounds like" suggestions, Sound Journey, Muse and daily mixes. Audio never leaves the server. The card shows the model, whether it's running on the GPU or the CPU, and how many tracks are analysed.

| Setting | Default | What it does |
|---|---|---|
| Analyse music | On | New tracks are analysed within the hour, and right after a music scan. |

**Analyse new tracks now** starts a run straight away. If the card says the analysis service isn't running, start the `soundprint` container (it's in the compose file).

### Artist and album details

| Setting | Default | What it does |
|---|---|---|
| Look up artists and albums online | On | Asks MusicBrainz about each artist and album, about one a second, and Wikipedia for artist bios: styles, solo artist or band, country, active years, album types and labels. Only names and MusicBrainz IDs are sent. The first run takes a few hours for a large library; later runs cover new music. |

**Look up new artists and albums now** starts a run straight away.

### Lyrics

Lyrics come from the files themselves and from `.lrc` files next to them.

| Setting | Default | What it does |
|---|---|---|
| Look up lyrics online | Off | When a track has none, asks LRCLIB (a free, open lyrics database). Only the artist, title, album and length are sent. |

### Volume levelling

Volume levelling is always on. Tracks with ReplayGain tags use them; tracks without have their loudness measured once, in the maintenance window, a few at a time at low priority. **Measure now** starts measuring straight away.

### Last.fm and ListenBrainz

Last.fm needs an API account for your server, which you set up once here. Each person then connects their own Last.fm account under **Account → Last.fm**.

1. Create an API account at last.fm/api/account/create. The callback URL can be left empty.
2. Enter its details here and press **Save**.

| Setting | Default | What it does |
|---|---|---|
| API key | (empty) | Your Last.fm API account's key. |
| Shared secret | (empty) | Your Last.fm API account's shared secret. |

Once set up, the fields show dots; enter new values to replace them, or press **Turn off**.

ListenBrainz needs nothing here: each person pastes their own ListenBrainz user token under **Account → ListenBrainz**. Tracks played halfway or more are sent as listens.

## AI

**Settings → AI** connects an optional language model running in [Ollama](https://ollama.com) on your network. It makes Muse understand prompts better, re-ranks results, explains recommendations and finds songs by what their lyrics are about. Everything stays on your network. With it off, everything works as before.

| Setting | Default | What it does |
|---|---|---|
| Use AI (Ollama) | Off | Turns the AI features on. |
| Ollama address | `http://127.0.0.1:11434` | Where Ollama listens. **Test connection** checks it and lists the installed models. |
| Chat model | `qwen3:8b` | Reads prompts, re-ranks and writes reasons. Pick from the installed models, or choose **Other…** to type a name. |
| Embedding model | `mxbai-embed-large` | Indexes movies, shows and lyrics. Changing it re-indexes in the background. |
| Unload when idle (minutes) | `5` | Ollama frees the GPU after this long without requests. 0 unloads right after each one. |

**What AI does** (these take effect while **Use AI** is on):

| Setting | Default | What it does |
|---|---|---|
| Understand Muse prompts | On | Reads music and movie prompts into genres, years, people, "like …" titles and moods. Without it, simple keyword matching is used. |
| Re-rank movie Muse results | On | Puts the best matches for the prompt first, with a short reason each. |
| Lyrics themes for music Muse | On | Finds songs by what they're about ("songs about the sea"). Lyrics come from the files, `.lrc` files and lyrics already looked up. **Rebuild** re-indexes every track's lyrics. |
| Reasons on recommendations | On | A one-line "why this" for the top recommendations. |

**Learning**

| Setting | Default | What it does |
|---|---|---|
| Learn from skips and ratings | On | Tracks skipped in the first 30 seconds, films abandoned early and low ratings count against similar picks in radios, mixes, Muse and recommendations; full plays and high ratings count for them. Skips fade over 30 days. Works without AI. |

The **Status** card shows whether Ollama is reachable, which models are loaded (and how much GPU memory they use), how many movies and shows are indexed, and the last error. If a chosen model isn't installed, install it with `ollama pull <model>` or pick another.

**Recommended:** install the two default models (`ollama pull qwen3:8b` and `ollama pull mxbai-embed-large`), press **Test connection**, then turn on **Use AI**. If Ollama shares a GPU with transcoding, keep **Unload when idle** short.

## Cinema Trailers

**Settings → Cinema Trailers** plays trailers of other movies in your library before a movie, like at the cinema, when the movie is started from the beginning. Each person can turn this off under **Account → Play trailers before movies**.

| Setting | Default | What it does |
|---|---|---|
| Trailers before a movie | Off | Off, or 1 to 5 trailers. |
| Pre-roll video | None | Played after the trailers, just before the movie: a cinema intro or your own bumper. Find it by name, or enter its item ID (the number in its page's address) and press **Use ID**. The cross clears it. |

The trailers are the trailer files in your library (the `Trailers` folder next to a film), one per film, picked mostly from films the person hasn't watched and that were added recently, within their rating limit. If you have no local trailers, nothing plays before the film except the pre-roll.

## Requests

**Settings → Requests** connects Seerr (requests for titles you don't have) and Bazarr (subtitles), and is where you approve requests.

API keys are entered only here, in your own server's settings. They're stored on your server, never shown again, and only sent to the service they belong to.

### Seerr

People search for movies and shows that aren't in your library and request them from any Marquee app. Each request waits for an administrator's approval, then goes to [Seerr](https://github.com/seerr-team/seerr), which hands it to Radarr or Sonarr.

| Setting | Default | What it does |
|---|---|---|
| Address | (empty) | Your Seerr server, for example `http://192.168.1.20:5055`. Leave empty to turn requests off. |
| API key | (empty) | From Seerr → Settings → General → API Key. |

**Test connection** checks the address and key. Who may request is set per person in **Settings → Users** (**Can request movies and shows**, off by default).

### Bazarr

Downloads subtitles for movies and episodes through [Bazarr](https://www.bazarr.media), from the player's **Find subtitles** and from Library Health's **Missing subtitles** check.

| Setting | Default | What it does |
|---|---|---|
| Bazarr address | (empty) | For example `http://192.168.1.20:6767`. Leave empty to turn Bazarr off. |
| Bazarr API key | (empty) | From Bazarr → Settings → General → API Key. |

### Approving requests

**Waiting for approval** lists pending requests. **Approve** sends one to Seerr; **Decline** asks for an optional reason, which the person sees with their request. **Recent requests** shows the last few decided in the past 10 days; finished requests are removed after 10 days.

## Plex

**Settings → Plex** has two parts: two-way sync with a running Plex server, and a one-time import from Plex's database. You can use either or both.

### Two-way sync

Keeps watched status, plays, resume positions and ratings the same in Marquee and Plex. When something changed on one side, it's copied to the other; when it changed on both, the newer change wins. Plex is changed only through its own API: its database, libraries and files are never touched, and nothing in Plex is deleted.

**Connection and what to sync**

| Setting | Default | What it does |
|---|---|---|
| Plex server address | (empty) | As Marquee reaches it, with the port, for example `http://192.168.1.10:32400`. |
| Plex token | (empty) | The Plex server owner's token. Stored on the server, never shown again. Instead of pasting one, press **Sign in with Plex** and approve Marquee in the plex.tv tab that opens (or enter the code shown at plex.tv/link). |
| Watched status and plays | On | Watched or unwatched, play counts and when last played. |
| Ratings | On | Your star ratings. |
| Resume positions | On | Movies and episodes. |
| Play history | On | Plays in Plex appear in Marquee's history. Plays in Marquee reach Plex when the sync runs, so Plex shows the sync time, not when you played them. |
| Automatic sync | Off | Off until you turn it on. **Sync now** works either way. |
| Every | 30 minutes | How often automatic sync runs: 5, 15 or 30 minutes, or 1, 2, 6, 12 or 24 hours. |
| Folders | Suggested automatically | Items pair up through their files: each Plex folder is mapped to the Marquee folder with the same files. The Plex import saves its folders here; **Add folder** adds one by hand. |

Press **Save** after changing any of these.

**People:** each Plex account syncs with the Marquee user you choose, or **Don't sync**. Plex accounts appear here after the Plex import, a preview or a sync. Plex Home users sync through the owner's sign-in; friends you share your Plex server with can only sync if Plex lets the owner act for them.

**Preview** lists what would change each way without changing anything. **Sync now** runs a sync. The last sync's summary is shown below, with the time of the next automatic sync.

**Recommended:** run **Preview** first, check the folder mappings and people, then **Sync now** once before turning on **Automatic sync**. Turn on the *Plex sync is failing* alert in [Notifications](settings-server.md#alerts) so you hear if the token stops working.

### Import from Plex

A one-time import of watch status, resume points, ratings, play history, playlists, intro and credits markers, the posters you chose and Plex's matches. Plex itself is not changed. The folders and people you choose are kept for two-way sync.

The import reads Plex's database, so mount your Plex data folder (the one containing `Library`) read-only at `/plex` in the Marquee container (or set `MARQUEE_PLEX_DIR`). Until you do, this part says **Plex database not found**. Two-way sync doesn't need it.

1. Press **Check Plex library**. Marquee reads a copy of the Plex database and shows what it found before anything is imported.
2. **Folders:** check that each Plex folder is mapped to the Marquee folder holding the same files. The percentage shows how many files were found. Fix any that are wrong and press **Re-check with these folders**.
3. **People:** for each Plex account choose **Link to user** (an existing Marquee user), **Create user** (with a new username), **Merge into…** (another Plex account's choice) or **Skip**.
4. **What to import:** all on by default.

| Setting | Default | What it does |
|---|---|---|
| Watched status, resume points and ratings | On | Per person. |
| Play history | On | Each play, with its date. |
| Playlists | On | Items not in your Marquee library are left out. |
| Intro and credits markers | On | Used for Skip Intro and Skip Credits. |
| Posters you chose in Plex | On | Copies the artwork you picked. |
| Plex's matches where they fit better | On | Takes Plex's match where Marquee's looks wrong. |

5. Press **Start import**. You can leave the page; it carries on in the background. The report shows what was paired, imported and skipped, and any matches where Marquee kept its own (check those with **Fix match**).

Running the import again updates everything without duplicating it. New users created by the import sign in by choosing their profile on the home network; add a PIN or password in **Settings → Users** if you like.

## Library Health

**Settings → Library Health** lists things worth fixing in your libraries. Each check shows how many it found; open one to see the titles.

| Check | Severity | What it finds |
|---|---|---|
| Unplayable files | Problem | Files that couldn't be read: no audio or video, no duration, or ffprobe failed. |
| Playback errors | Problem | Titles that failed to play in the last 30 days. |
| Duplicates | Warning | The same film or show more than once in a library, or a title with two files of the same resolution and edition. |
| Unmatched | Warning | Movies, shows, artists and albums without online metadata. |
| Missing subtitles | Warning | Movies and episodes still missing subtitles their Bazarr language profile asks for. Needs Bazarr; otherwise shown as unavailable. |
| Upgrade candidates | Info | Movies and episodes whose best file is SD or 720p, or has a low bitrate for its resolution (under 10 Mb/s for 4K or 3 Mb/s for 1080p in H.264, half that for HEVC, AV1 and VP9). |
| Missing artwork | Info | Movies, shows and albums without a poster. |

Each title in a check has **Open**, **Refresh metadata** and **Ignore** (which hides it from that check; **Undo** brings it back). Movies and shows also have **Fix match**, and **Missing subtitles** has **Download with Bazarr**.

### Duplicates and the media trash

For duplicates with more than one file, Marquee shows the copies side by side (resolution, video codec, HDR, bitrate, audio, subtitles, duration, container, size and when added), with the better value in each row highlighted.

With **Allow deleting media files** on (Settings → Libraries), each copy has **Delete…**. Confirming **Move to trash** moves the file to the `.marquee-trash` folder in its library folder.

The media trash is emptied every night by the **Empty media trash** task (Settings → Scheduled Tasks, in the maintenance window). Until then you can restore a file by moving it back out of `.marquee-trash` by hand. The **Tools** section of Library Health shows when the trash was last emptied and what that freed. To keep deleted files longer, turn that task off for a while.

The media trash also holds originals kept by the Large files optimizer, recordings removed for the DVR quota and originals from commercial removal.

## Large files optimizer

**Library Health → Large files** finds movies and episodes that are bigger than they need to be and re-encodes them smaller, on the GPU where possible. The overview shows how much space it has saved.

Each file is converted next to the original, checked, and swapped in only if it's enough smaller. The library item keeps its ID, watch history and metadata. Playback always comes first.

### What a conversion keeps

- **Every audio track** (all languages, commentary), copied or converted to AAC as the preset says.
- **Every subtitle.** Text subtitles stay as they are in MKV (MP4's become SRT); picture subtitles are copied; closed captions become SRT.
- **Fonts and attachments** in MKV files.
- **The container:** MKV stays MKV and MP4 stays MP4, unless an MP4 has picture subtitles, which MP4 can't hold. Other containers (AVI, TS and so on) become MKV.
- **HDR**, when the preset keeps it and the codec is HEVC or AV1, as 10-bit HDR10. H.264, or a preset set to convert, tone-maps to SDR.
- **Resolution:** reduced to the preset's maximum, never upscaled.

Dolby Vision profile 5 files can't be converted faithfully and are refused.

### How a file is checked and swapped

Before anything replaces the original, the new file must:

- be readable by FFprobe;
- be as long as the original, within 1 %;
- have the same number of audio and subtitle streams the plan expects;
- be no bigger than the preset's resolution;
- decode cleanly for a few seconds at the start, middle and end.

If it passes and is at least the **smallest saving** smaller, it takes the original's place. Otherwise it's thrown away, the original is left as it was, and the job is marked **Skipped** (too little saving) or **Failed**.

### How NVIDIA full-GPU conversion works

Files are encoded on the NVIDIA GPU when there is one, then Quick Sync, then the CPU, always at the lowest priority.

On NVIDIA, a conversion can stay entirely on the GPU: decoding (NVDEC), resizing and colour conversion, and encoding (NVENC), with no frames copied back to the CPU. That's the fastest path and leaves the CPU free. It's used when:

- the encoder is NVENC;
- no tone mapping is needed (tone mapping runs on the CPU);
- the source is H.264, HEVC, AV1, VP9, MPEG-2 or VC-1, in 4:2:0 at 8 or 10 bits (the formats NVDEC handles).

Otherwise the file is still decoded on the GPU where possible, with the filters on the CPU.

The optimizer shares NVENC with playback. It only uses NVENC while that leaves a session free under **NVIDIA encode sessions** (Settings → Transcoder), and gives its session up when playback needs it.

### Files tab

Find files to convert, see the estimate, and queue them.

| Filter | Default | What it does |
|---|---|---|
| Library | All video libraries | Limit to one library. |
| Type | Movies and episodes | Or **Movies** or **Episodes** only. |
| At least (GB) | `10` | Only files at least this big. Empty = any size. |
| Bitrate at least (Mb/s) | (empty) | Only files at or above this bitrate. |
| Resolution | Any | 4K (2160p), 1080p, 720p or SD. |
| HDR | HDR and SDR | Or **Only HDR** or **Only SDR**. |
| Optimized before | Any | **Not optimized or queued yet** (what's still to do: leaves out optimized files and files already in the queue), or **Already optimized (re-optimize)** to queue optimized files again, for example with a smaller preset. |
| Preset | The default preset | Used for the size estimates and for queueing. |
| Sort | Biggest first | Or **Most to save first**, **Highest bitrate first** or **Title**. |
| Codecs | None (all) | Tap codecs (H.264, HEVC, MPEG-2, VC-1, MPEG-4, AV1, VP9) to show only those. Older codecs (MPEG-2, VC-1, H.264) shrink the most. |

The bar above the list shows how many files match, their total size and about how big they'd be after. Tick files (or the box in the header for the whole page), or press **Select all … matching** for every match across all pages, then **Queue … files**.

The list shows each file's size, resolution and codec (with HDR), bitrate, length and estimated size after, with the saving as a percentage.

### Queue tab

Shows waiting and running conversions, with progress, the encoder (NVIDIA GPU, Quick Sync or CPU), speed, frames per second and time left, and why a job is paused.

- **Pause queue** / **Resume queue** holds or releases the whole queue.
- **Clear finished** removes finished jobs from the list.
- Each job has **Cancel** (queued or running) or **Retry** (failed, cancelled or skipped).

Job states: Queued, Optimizing, Checking, Optimized, Failed, Cancelled, Skipped and Restored.

### History tab

Lists finished conversions with their size before and after, and the total saved.

- **Re-optimize with** chooses a preset, then **Re-optimize** on an optimized file queues it again with that preset.
- **Restore original** puts the original back while it's still in the media trash (needs **Keep originals in the media trash**). The optimized file goes to the trash instead.
- **Delete kept originals** deletes all the originals still in the media trash, after a confirmation. They can't be restored afterwards.

### Settings tab: queue

| Setting | Default | What it does |
|---|---|---|
| Files at once | `1` | 1 to 4. Each one uses a GPU encoder session or CPU time. |
| Keep the new file when it's at least … smaller | `20%` | 5 %, 10 %, 15 %, 20 %, 25 %, 30 %, 40 % or 50 %. Otherwise the new file is thrown away and the original kept. |
| Default preset | 1080p HEVC (balanced) | The preset picked first in the Files and History tabs. |
| Pause while anyone is watching | On | Encoding stops where it is while anything plays, and carries on about 15 seconds after. Playback comes first on the GPU either way. |
| Only during the maintenance window | Off | Queued files only convert inside the maintenance window, set in Settings → Scheduled Tasks. |
| Keep originals in the media trash | Off | **On:** each original goes to the media trash once its new file is in place, and can be restored until the trash is emptied (every night). **Off:** the original is deleted once the new file is in place, freeing the space at once; saving with it off also deletes originals kept earlier. Either way the original is only touched after the new file has been checked. |

Press **Save** at the bottom of the tab to apply changes, or **Discard**.

**Recommended:** turn on **Keep originals in the media trash** for your first few conversions, watch a couple of results, then turn it off once you're happy. Keep **Files at once** at 1 unless you have a powerful GPU and nobody watching.

### Built-in presets

| Preset | Resolution | Codec | Video | Audio | HDR |
|---|---|---|---|---|---|
| 1080p HEVC (balanced) | 1080p | HEVC | Balanced | Copy every track | Kept |
| 1080p HEVC (small) | 1080p | HEVC | Small | AAC up to 5.1 | Kept |
| 720p HEVC | 720p | HEVC | Balanced | AAC up to 5.1 | Converted to SDR |
| Same resolution, HEVC | Kept | HEVC | Balanced | Copy every track | Kept |

Built-in presets can't be edited or deleted.

### Custom presets

Press **New preset** to make your own, or the pencil to edit one. Press **Done**, then **Save** at the bottom of the tab. You can have up to 30.

| Setting | Default | What it does |
|---|---|---|
| Name | (empty) | Required and unique, for example "Kids' shows 720p". |
| Resolution | 1080p | **Keep**, **4K (2160p)**, **1080p**, **720p** or **480p**. Never upscaled. |
| Codec | HEVC (H.265) | **HEVC (H.265)**, **H.264** (plays everywhere, bigger) or, when your server has an AV1 encoder, **AV1** (smallest, slow without a recent GPU). |
| Video | Balanced | **High quality**, **Balanced** or **Small** (constant quality), or **Target bitrate…** for a fixed bitrate. |
| Video bitrate (Mb/s) | (empty) | Shown with **Target bitrate…**. 0.3 to 100 Mb/s. |
| Maximum size per hour (GB) | (empty: no limit) | Optional, 0.25 to 100. Caps each file's size per hour of running time: at 5, a 2-hour film is at most 10 GB and a 45-minute episode 3.75 GB. Each file's video bitrate is capped so it fits with its audio and subtitles; files that need less stay smaller. |
| Audio | Copy every track | **Copy every track**, **AAC stereo** or **AAC up to 5.1** (stereo stays stereo). Every track is kept either way. |
| HDR | Keep HDR (10-bit) | Or **Convert to SDR**. HDR is kept only in HEVC or AV1. |

About the quality levels: **Balanced** is a good default for films. **High quality** keeps more detail and saves less; **Small** saves more and softens fine detail. On NVIDIA and Quick Sync they use constant-quality levels 20, 24 and 28; on the CPU, HEVC uses 20, 23 and 26.

About **Maximum size per hour**: the size covers everything in the file, so pair it with AAC audio. Copied lossless tracks (TrueHD, DTS-HD MA) take 1.5–2 GB an hour on their own and leave little room for the picture. The video is never squeezed below 1 Mb/s, so a file whose audio nearly fills the limit can come out a little over it. On Quick Sync the cap becomes a fixed target bitrate, since its constant quality can't be capped.

### Worked example: films of about 5 GB

Say you want 1080p films to end up around 5 GB, without touching ones that are already small enough.

1. Open **Library Health → Large files → Settings** and press **New preset**.
2. Fill it in:

   | Setting | Value |
   |---|---|
   | Name | `Films ~5 GB` |
   | Resolution | 1080p |
   | Codec | HEVC (H.265) |
   | Video | Balanced |
   | Maximum size per hour (GB) | `2.5` |
   | Audio | AAC up to 5.1 |
   | HDR | Keep HDR (10-bit) |

3. Press **Done**, set **Default preset** to *Films ~5 GB* if you like, and press **Save**.
4. Go to **Files**. Set **Type** to Movies, keep **At least (GB)** at 10 (or lower it to 6), choose the preset, and sort by **Most to save first**.
5. Check the estimate in the bar above the list, tick the files you want (or **Select all … matching**) and press **Queue … files**.

What you get: 2.5 GB per hour is about 5.4 Mb/s for the whole file. A 5.1 AAC track takes about 0.4 Mb/s, so the picture gets up to about 5 Mb/s, which is typical for 1080p HEVC at Balanced quality anyway. A 2-hour film comes out at no more than 5 GB, a 90-minute one at no more than about 3.75 GB, and an easy-to-compress film comes out smaller still. Each extra audio language takes another 0.4 Mb/s from the picture.

Files that wouldn't shrink by at least the smallest saving (20 % by default) are skipped and left alone. They stay in the Files list under *Not optimized or queued yet* (marked Skipped), so you can queue them again with a smaller preset. Files that were optimized and that you want smaller still: filter **Optimized before** to *Already optimized (re-optimize)*, or use **Re-optimize** in History.

---

[Install](install.md) · [First run](first-run.md) · [Server settings](settings-server.md) · [Media settings](settings-media.md) · [Live TV and DVR](live-tv-and-dvr.md) · [VOD and Dispatcharr](vod-and-dispatcharr.md) · [Apps and remote access](apps-and-remote.md) · [Troubleshooting](troubleshooting.md)
