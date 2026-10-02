# Plex Library Import

_Last updated: 2026-10-01. Status: **Implemented** (M2, v0.6)._

Goal: move from Plex with **no loss of watch history, matches, or customizations**, and without re-doing any manual metadata work.

## Source

Two import paths are supported. Using both gives the best result.

1. **Database import (primary).** Read the Plex SQLite DB directly, read-only, from a copy:
   - macOS: `~/Library/Application Support/Plex Media Server/Plug-in Support/Databases/com.plexapp.plugins.library.db`
   - Linux/Docker: `<plex config>/Library/Application Support/Plex Media Server/Plug-in Support/Databases/com.plexapp.plugins.library.db`
   - Artwork: `.../Plex Media Server/Metadata/` and `.../Media/` (custom posters, thumbnails)
   - The importer always works on a **snapshot copy** and never touches Plex's live DB. Plex can keep running.
   - Note: some Plex FTS tables use a custom tokenizer that standard SQLite can't open. We read only the regular tables, so this isn't a problem.
2. **API import (supplement).** Use a Plex server URL and token to fetch per-user watch state for Plex Home/shared users, which may be stored per account, and to verify results.

## Production specifics

- Plex runs as the `plex` container (linuxserver image) on the same host. Its DB is at `/mnt/docker/plex/Library/Application Support/Plex Media Server/Plug-in Support/Databases/com.plexapp.plugins.library.db` (341 MB).
- Mount `/mnt/docker/plex` read-only at `/plex` in our container. The importer copies the DB to a snapshot using the SQLite backup API before reading.
- Default path mapping (Plex container path to our container path):

| Plex | Ours |
|---|---|
| `/movies` | `/media/video/Movies` |
| `/tv` | `/media/video/Shows` |
| `/anime` | `/media/video/Anime` |
| `/music` | `/media/music` |
| `/photos` | _skipped (photos P2)_ |

## Order independence (decided 2026-10-01)

The import must work whether it runs **before or after** the owner adds libraries in Marquee:

- **Libraries already exist:** each Plex section is mapped to the Marquee library whose folders cover the remapped Plex paths. No duplicate libraries are created. If no library matches, one is created.
- **Item pairing is by file path** (remapped), never by title. A Plex item attaches to the Marquee item that owns the same file.
- **Conflict rule (revised after testing on the real library):** "Plex always wins" would have replaced about 20 correct matches with wrong ones (*Gauche the Cellist* → *The Cell*, *The Americanization of Emily* → *The Addams Family*, *One Piece* (2023 series) → the 1999 anime). The importer now scores each candidate against the title and year in the file or folder name (`naming.MatchScore`):
  1. Items edited or locked in Plex: Plex wins.
  2. Marquee unmatched and Plex's title fits the name (score ≥ 3): Plex's match is applied.
  3. Both matched and disagree: Plex's match is applied only if it fits the name strictly better. Otherwise Marquee's match is kept and listed in the report for review with Fix Match.
- Watch state, resume points and history are always imported (newest wins on re-runs).
- Re-runs are idempotent, so the owner can keep using Plex during the transition and sync again.

## Accounts

Each Plex account with activity can be **linked** to an existing Marquee user, **created** as a new user, **merged** into another Plex account in the same import (one person with several Plex accounts), or **skipped**. The Plex owner account links to the Marquee admin by default. Imported users start with no password or PIN, so they sign in by tapping their profile wherever PIN sign-in is allowed (home network by default). An admin or the user can add a PIN or password at any time.

## Results on the owner's library (2026-10-01 test on database copies)

| | |
|---|---|
| Files paired | 22,441 of 22,447 (the rest: 3 damaged files, 1 temp file, 2 removed tracks) |
| Owner watch state | 7,351 watched, 421 resume points, 677 ratings |
| Play history | 4,860 entries (8 accounts) |
| Playlists | 36 of 48 (the other 12 contain only music no longer in the library) |
| Markers | 414 intro/credits |
| Chosen posters | 43 of 43 found |
| Matches | 3,320 agreed · 7 taken from Plex (all correct) · 31 kept Marquee's (listed for review) |
| Re-run | No duplicates (verified) |

## What gets imported

| Data | Plex source | Notes |
|---|---|---|
| Libraries & folders | `library_sections`, `section_locations` | Path remapping table (e.g. `/Volumes/Media` → `/media`) |
| Items & hierarchy | `metadata_items` (type, parent_id, index, titles, summary, dates, ratings) | Movie=1, Show=2, Season=3, Episode=4, Artist=8, Album=9, Track=10, Clip/Other, Collection=18 |
| Matches | `metadata_items.guid` + `taggings`/`tags` with GUID tags (`tmdb://`, `tvdb://`, `imdb://`) | Legacy agents (`com.plexapp.agents.*`, HAMA for anime) are mapped to provider IDs |
| User edits & locks | `metadata_items.user_fields` (locked fields) | Locked fields stay locked |
| Files | `media_items`, `media_parts`, `media_streams` | Matched to our scan by path + size; then re-probed |
| Custom artwork | `user_thumb_url`, `user_art_url` → bundle files | Copied into our artwork store |
| Collections | collection items + `taggings` | Manual collections preserved |
| Playlists | `play_queue_generators` / playlist items | Per owner account |
| Users | `accounts` | Created as local users, and the owner chooses passwords/PINs |
| Watch state | `metadata_item_settings` (view_offset, view_count, last_viewed_at, rating per account) | Keyed by item GUID, so it survives path differences |
| Watch history | `metadata_item_views` | Imported into `play_history` for stats |
| Intro/credits markers | `taggings` with marker tags | Saves re-running detection |
| Labels, genres, etc. | `tags` / `taggings` | Mapped to our tag types |

## Process (wizard in web admin)

1. Point to the Plex data directory, mounted read-only into the container at `/plex`, or upload the DB file. Optionally add a Plex URL and token.
2. **Preview:** show the libraries found, item counts, users and a path-mapping editor. Validate that remapped paths exist.
3. **Dry run:** produce a report of what will be matched and what won't (missing files, unknown agents).
4. **Import:** create libraries, import items with provider IDs (skipping re-matching), copy artwork, users and watch state. Then run a normal scan to reconcile files and probe streams.
5. **Report:** show successes, warnings and unmatched items with "Fix Match" links.
6. **Re-runnable:** importing again updates watch state (newest wins) without duplicating anything. This lets you run both servers side by side during the transition.

## Validation

- Build a test fixture from an anonymized copy of the owner's Plex DB.
- Success target: at least 95% of items imported with correct matches, and 100% of watch state preserved for matched items.
