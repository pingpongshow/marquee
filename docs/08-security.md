# Security

_Last updated: 2026-10-02. Status: checklist passed for M6 (v0.9.x)._

Marquee runs on a home server, reachable on the LAN and, for remote clients only, over the owner's existing Tailscale network (WireGuard-encrypted). Nothing is exposed to the public internet.

## Threat model

| Threat | Mitigation |
|---|---|
| Someone on the LAN or tailnet guessing passwords or PINs | Login rate limiting and lockout (5 wrong PINs → 15 min); PIN sign-in only from the home network by default (D34); admins always need a password (D35). |
| A stolen device token | Tokens are random 256-bit values stored only as SHA-256 hashes; every device can be signed out (Settings → Devices, USER-3). |
| Quick Connect code guessing | 6 characters from a 32-symbol alphabet (~10⁹), 10-minute expiry, approval needs a signed-in user, 10 attempts/min per user, token handed out once (D51). |
| Media URLs leaking | Stream URLs are capabilities: a 128-bit session id that stops working when the session ends (D38). Artwork/avatars use the token query parameter (needed for `<img>`/AVPlayer); request logs record paths only, never query strings. |
| Reading files outside the libraries | The API never takes file paths for media; the folder browser is limited to configured roots; backup names must match `^[A-Za-z0-9._-]+\.db$` (tested). |
| Uploaded images as an attack vector | Profile pictures are decoded and re-encoded as JPEG; nothing uploaded is stored or served as-is (D42). Uploads are capped at 10 MB. |
| Clickjacking / content sniffing | `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` on every response. |
| Privilege escalation between users | Every handler checks the session; admin-only endpoints check the admin flag; library and content-rating restrictions are applied in SQL for every listing (tested). Playlists are per user. |
| Remote clients misusing bandwidth | Remote sessions are classified by source address (Tailscale ranges are always remote); per-user and global remote caps, fair upload share (§3a). |
| Vulnerable dependencies | `govulncheck` (Go, toolchain pinned to the patched release) and `npm audit` (web) run before releases. |

## Release checklist

1. `go vet ./...`, `go test ./...` (server), `npx vitest run`, `npm run lint`, `npx tsc --noEmit` (web), `swift test` (MarqueeKit) and the iOS/tvOS UI tests.
2. `govulncheck ./...`: no reachable vulnerabilities. `npm audit --omit=dev`: 0.
3. The image builds from `golang:1.26` (latest patch) and `debian:trixie-slim`.
4. Deploy, check `/api/v1/system/info`, the startup log (migrations, Bonjour interfaces), and a playback on each encoder (`marquee dev-transcode`).

## Results (2026-10-02)

- govulncheck: no reachable vulnerabilities after pinning `toolchain go1.26.8` and upgrading kin-openapi and x/crypto.
- npm audit: 0 vulnerabilities.
- Remote path checked from a tailnet device: classified `remote`, PIN sign-in withheld, security headers present.

## Notes for the owner

- **Devices at home with Tailscale running:** if "Use Tailscale subnets" is on, their traffic to `10.1.1.10` goes through Tailscale and the server (correctly) treats them as remote. Streams still play at full quality on a fast connection, but the PIN profile picker is hidden. Turn that option off on devices that live at home, or leave it; nothing breaks.
- Tailscale on the server is never changed by Marquee.
