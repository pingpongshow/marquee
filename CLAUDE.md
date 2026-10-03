# Project guide for Claude

- This is **Marquee**, a self-hosted media server: a Go server in Docker plus web, iOS/iPadOS/tvOS and Android clients. See `README.md` and `docs/`.
- `docs/` is the source of truth for scope and design. Follow `docs/01-requirements.md` and `docs/05-roadmap.md`. Check `docs/decision-log.md` before changing stack or architecture, and log new decisions there.
- Keep the docs current. When work changes a requirement, the architecture or a decision, update the relevant doc in the same change, and refresh its "Last updated" date.
- API changes start in `api/openapi.yaml`, and clients are generated from it. Never hand-edit generated code.
- Reference requirement IDs (e.g. `PLAY-3`) in commit messages.
