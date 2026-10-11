# Project guide for Claude

- This is **Marquee**, a self-hosted media server: a Go server in Docker plus web, iOS/iPadOS/tvOS and Android clients. See `README.md`.
- `docs/` held private planning notes and was removed from the repository (2026-10-11). Don't recreate it or commit planning documents, requirement lists or decision logs; it is git-ignored.
- API changes start in `api/openapi.yaml`, and clients are generated from it. Never hand-edit generated code.
- Reference requirement IDs (e.g. `PLAY-3`) in commit messages.
