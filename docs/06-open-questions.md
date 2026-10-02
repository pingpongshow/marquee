# Open Questions

_Last updated: 2026-10-01_

## Open

| # | Question | Why it matters | Default if unanswered |
|---|---|---|---|
| Q6 | Apple Developer Program membership ($99/yr)? | Required for TestFlight and installing on Apple TV long-term. | Assume yes before M5. |
| Q9 | Open source (and which license) or private? | Licensing of dependencies, repo hosting. | Private GitHub repo. |
| Q10 | Anime default: seasonal (TVDB-style) or absolute episode numbering? | Anime agent defaults. | Seasonal, with a per-show override. |
| Q12 | Should `Video/Hentai` and `Video/XXX` (not in Plex today) become libraries, e.g. hidden from managed users? | Library and restriction setup. | Excluded. |
| Q13 | Photos: build our own (P2), or leave photos to the Immich instance already on the host? | Scope. | Leave to Immich. Revisit after v1.0. |

## Answered

| # | Answer (2026-10-01) |
|---|---|
| Q1 | Long-term host: Unraid server `root@10.1.1.10` (i7-13700K + RTX 5090). Docker builds go in `/mnt/docker`. See [07-deployment-environment.md](07-deployment-environment.md). |
| Q2 | ~6,600 video files (12 TB) and ~16,100 tracks (617 GB). Some anime now, more later. No 4K/DV yet, but they must be supported later. |
| Q3 | Plex runs in Docker on the same host. Media is in `/mnt/data/Video` and `/mnt/data/Music`. |
| Q4 | Remote access via Tailscale, for remote clients only. LAN clients connect directly. |
| Q5 | At least 10 users, possibly more. |
| Q8 | **Marquee** |
| Q11 | Tech stack approved. |
| Q7 | TMDB key added 2026-10-01. OMDb optional (owner asked whether to add it; it has been added as an optional ratings source). |
