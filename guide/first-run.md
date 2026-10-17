# First run

Create the admin account, add your libraries, turn on metadata and add the people you share with.

## Contents

- [Create the admin account](#create-the-admin-account)
- [Add your libraries](#add-your-libraries)
- [Name your files](#name-your-files)
- [Add your TMDB key](#add-your-tmdb-key)
- [Add people](#add-people)
- [Your first hour](#your-first-hour)

## Create the admin account

Open `http://<server>:32500` in a browser, for example `http://192.168.1.20:32500`. A new server shows **Welcome to Marquee**:

| Field | What to enter |
|---|---|
| **Server name** | Shown to every app that connects. Default `Marquee`; you can change it later under **Settings → General**. |
| **Admin username** | Your own username. |
| **Password** | At least 8 characters. |
| **Confirm password** | The same again. |

Choose **Create server**. You're signed in and taken to **Settings → Libraries**.

The Home screen shows a **Finish setting up Marquee** checklist (libraries, TMDB key, Plex import, people, remote access) until you've done each step or choose **Hide setup checklist**.

## Add your libraries

A library is one kind of media from one or more folders. In **Settings → Libraries**, choose **Add library**. The dialog has three steps.

**1. Type**

| Type | For | Expected layout |
|---|---|---|
| **Movies** | Films | `Movie (Year)/Movie (Year).mkv` |
| **TV Shows** | Series | `Show/Season 01/Show - S01E01.mkv` |
| **Anime** | Anime series and films | The TV layout, with AniList metadata and anime episode numbering |
| **Music** | Albums and tracks | Organised by the files' embedded tags |
| **Other Videos** | Home and personal videos | Organised by folder, no online metadata |

Also on this step:

| Setting | Default | What it does |
|---|---|---|
| **Library name** | The type's name (Movies, TV Shows, Anime, Music, Home Videos) | Shown in the sidebar and on Home. |
| **Language** | English (US) | The language for titles, summaries and artwork. |

**2. Folders**

Choose **Browse for folder** and pick one or more folders. These are paths inside the container, so they start with `/media`: for example `/media/video/Movies`, `/media/video/TV`, `/media/video/Anime` or `/media/music`. A library can have several folders, for example films on two disks.

You can choose **Add library** here, or **Advanced** for more options.

**3. Advanced**

| Setting | Default | What it does |
|---|---|---|
| **Episode ordering** (TV Shows and Anime) | Aired (by season) | **Aired (by season)**, **Absolute** (episodes numbered 1…n, common for anime) or **DVD**. |
| **Show on Home** | On | Include this library's rows (Recently Added, Continue Watching) on the Home screen. |
| **Include in search** | On | Show this library's titles in search results. |
| **Full rescan every (hours)** | 24 | A safety net: new files are picked up within seconds by file watching. 0 = never. |
| **Ignore files matching** | (none) | Patterns added to the server-wide ignore list, e.g. `*_staging.*`. |

Marquee starts scanning straight away. Progress shows on the library's row, and the **Scanning** options under the library list (file watching, intro detection, seek previews, the watched threshold) apply to every library. See [Media settings](settings-media.md) for those.

To change a library later, use its pencil (edit) button to change its name, folders and advanced options. The refresh button scans it now. The library type can't be changed; delete the library and add it again instead.

A **Recorded TV** library appears when you set up Live TV recordings. It's marked *Live TV recordings, shown only under Live TV*.

## Name your files

Marquee follows the common naming conventions, so a library that works with other media servers works here.

```text
video/
├── Movies/
│   └── Blade Runner (1982)/
│       ├── Blade Runner (1982).mkv
│       ├── Blade Runner (1982).en.srt          # sidecar subtitles: <name>.<language>.srt
│       ├── poster.jpg                           # optional local artwork
│       └── Trailers/                            # extras: Trailers, Featurettes, Behind The Scenes,
│           └── Official Trailer.mkv             #   Deleted Scenes, Interviews, Scenes, Shorts, Extras
├── TV/
│   └── The Expanse (2015)/
│       └── Season 01/
│           └── The Expanse - S01E01 - Dulcinea.mkv
└── Anime/
    └── Cowboy Bebop/
        └── Season 01/
            └── Cowboy Bebop - S01E01.mkv        # absolute numbering ("- 001") works too
music/
└── Artist/
    └── Album (Year)/
        ├── 01 - Track.flac
        └── cover.jpg
```

- Put the year in film and show folder names; it makes matching reliable.
- Several files for one film (4K and 1080p, a director's cut) become versions of one item.
- Music uses the files' tags first (artist, album artist, album, track, disc, MusicBrainz IDs), so tidy tags matter more than folder names.
- Temporary and partial downloads are skipped by the server-wide ignore patterns: `*_staging.*`, `*.part`, `*.partial`, `*.tmp`, `*sample*` and hidden files (`.*`).

If something is matched wrongly, open it and use **Fix Match**.

## Add your TMDB key

Films, shows and anime need a TMDB key for posters, summaries, cast and ratings. Get one free at [themoviedb.org → Settings → API](https://www.themoviedb.org/settings/api), then paste it into **TMDB API key** under **Settings → Metadata** and save. Matching and artwork start straight away.

Music needs no key: it uses MusicBrainz and other public sources. The other keys on that page (Fanart.tv, OpenSubtitles, OMDb) are optional; see [Media settings](settings-media.md).

## Add people

Everyone in your household gets their own watch history, ratings, recommendations and preferences. Go to **Settings → Users** and choose **Add user**.

First pick the kind of account:

- **Person:** signs in by choosing their profile, with an optional PIN or password.
- **Managed profile:** for children or shared TVs. Opened by switching profile, optionally with a PIN. It has no password of its own.

| Field | Notes |
|---|---|
| **Username** | Required. |
| **Display name** | Optional. Shown on the profile picker. |
| **Email** | Optional. For reset links if they forget their password or PIN. |
| **Password** (people only) | Required for administrators (at least 8 characters). Otherwise optional, but needed to sign in from outside the home network while PIN sign-in is home-only. |
| **PIN** | Optional, 4 digits. Asked when choosing this profile. |
| **Administrator** (people only) | Can change settings, libraries and users. |

For anyone who isn't an administrator, you also choose what they can do:

| Setting | Default | What it does |
|---|---|---|
| **Access to all libraries** | On | Includes libraries added later. Turn off to tick the libraries they may see. |
| **Maximum content rating** | No limit | Films and shows rated higher, and unrated ones, are hidden. Music isn't affected. |
| **Can request movies and shows** | Off | Shown when Seerr is connected. Every request still needs an admin's approval. |
| **Can watch Live TV** | On | Shown when Live TV is set up. |
| **Can record Live TV**, **Can watch recordings**, **Recording quota** | | Shown when recording is available. See [Live TV and DVR](live-tv-and-dvr.md). |
| **Allow streaming away from home** | On | Turn off to limit this person to the home network. |
| **Remote quality limit** | Server default (automatic) | The highest quality they get away from home. |

**How people sign in.** The **Sign-in** card on the same page sets **PIN sign-in**:

| Option | What it means |
|---|---|
| **On the home network only (recommended)** (default) | At home, apps show a *Who's watching?* picker and people enter their 4-digit PIN. Away from home, they use their password. |
| **Everywhere, including remote** | The picker and PINs work away from home too. A password is safer than a 4-digit PIN over the internet. |
| **Off: always use a password** | No profile picker. |

Five wrong PINs lock that profile for 15 minutes. People without a PIN use their password; managed profiles without a PIN open with one tap.

Friends outside your household join through an invite link instead; see [Apps and remote access](apps-and-remote.md#share-with-friends).

## Your first hour

Once your libraries are scanning, work through these in order. Each links to the page that explains it.

1. **Check the server basics.** Name the server and set the default metadata language in **Settings → General**. Check **Settings → Network** lists your home network's subnet, so your devices count as local and get full quality. → [Server settings](settings-server.md)
2. **Check hardware transcoding.** In **Settings → Transcoder**, make sure NVIDIA NVENC or Intel Quick Sync is first in the encoder list if you have one. Play something that needs a transcode and look at **Settings → Dashboard**: it shows the encoder each session uses. → [Media settings](settings-media.md)
3. **Turn on optional metadata.** OMDb for IMDb, Rotten Tomatoes and Metacritic ratings; Fanart.tv for logos; OpenSubtitles for subtitle search. → [Media settings](settings-media.md)
4. **Start music analysis.** If Soundprint is running, **Settings → Music** shows its progress. A large library takes a while the first time. → [Media settings](settings-media.md)
5. **Import from Plex** if you're moving over: watch history, ratings, playlists and markers, under **Settings → Plex**. → [Media settings](settings-media.md)
6. **Add the household.** People, managed profiles for children, and PINs, in **Settings → Users**. → [Server settings](settings-server.md)
7. **Set up email** under **Settings → Notifications**, so people can reset a forgotten password or PIN and you get alerts (low disk space, failed tasks). → [Server settings](settings-server.md)
8. **Install the apps** on your phones, tablets and TVs, and set up remote access with Tailscale. → [Apps and remote access](apps-and-remote.md)
9. **Add Live TV** from Dispatcharr or an M3U/XMLTV source, and choose where recordings go. → [Live TV and DVR](live-tv-and-dvr.md)
10. **Add films and shows on demand** from Dispatcharr or an M3U playlist, and save the ones you want to your library. → [VOD and Dispatcharr](vod-and-dispatcharr.md)
11. **Check your backups.** A database backup is taken every night; see **Settings → Scheduled Tasks**, and copy `config/backups/` somewhere safe. → [Install the server](install.md#backups-and-where-your-data-lives)

---

[Guide contents](README.md) · Previous: [Install the server](install.md) · Next: [Apps and remote access](apps-and-remote.md)
