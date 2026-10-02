# Vision & Goals

_Last updated: 2026-10-01_

## Why this exists

Recent Plex updates have degraded the experience: UI redesigns, features moved behind accounts or the cloud, and streaming-service clutter. This project is a **self-hosted media server and client ecosystem** that:

- Matches Plex's core feature set and **feels like Plex** (layout, navigation, browsing patterns).
- Is **fully self-hosted**: no required cloud account, no phone-home, and no forced updates. The owner controls when to upgrade.
- Streams video and audio reliably on the **local network and over the internet (WAN)**.
- Can **import an existing Plex library**, including metadata, matches, watch history and resume points, so switching costs nothing.

## Guiding principles

1. **Contract-first, build once.** The API spec is written before the clients. Each client is generated from the same spec, which keeps rework low.
2. **Your server, your rules.** No telemetry, ads or "Discover" feeds. Features are never removed by a remote update.
3. **Plex-familiar UX.** Plex users should find their way around instantly. We copy the information architecture, not the branding or assets.
4. **Direct play first.** Transcode only when needed, and remux (copy streams into a new container) instead of re-encoding whenever possible.
5. **Polished, not just functional.** Smooth scrolling, fast search, correct metadata, proper subtitle rendering (including anime ASS styles), and clean error states.
6. **Single Docker deployment.** One container image runs the server and the web app. Upgrades are just pulling a new image tag.
7. **Upgradable and maintainable.** Versioned API, versioned database migrations, automated tests, and documented architecture decisions.

## Success criteria for "v1.0"

- [ ] Import an existing Plex library with no manual fixes for at least 95% of items.
- [ ] Movies, TV, anime, personal videos and music libraries scan, match and display with artwork.
- [ ] Web, iPhone, iPad and Apple TV clients can browse, search and play everything in the library.
- [ ] 4K HDR direct play on Apple TV, and automatic transcoding for clients or bandwidth that can't handle it.
- [ ] Remote (WAN) playback with adaptive quality and secure HTTPS access.
- [ ] Multiple users with separate watch state, managed (kid) profiles and PIN switching.
- [ ] Resume, Continue Watching (On Deck), Recently Added, collections and playlists work across all clients.

## Non-goals (for now)

- Streaming-service aggregation, "Discover" or social features.
- A central cloud account or relay service run by us.
- Live TV / DVR (it may come later; see roadmap).
- Photos, which are deferred to a later phase but planned for in the data model.
