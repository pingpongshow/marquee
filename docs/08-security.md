# Security

_Last updated: 2026-10-02. Status: checklist passed for M7 (v0.19.x)._

Marquee runs on a home server, reachable on the LAN and, for remote clients only, over the owner's existing Tailscale network (WireGuard-encrypted). Nothing is exposed to the public internet.

## Threat model

| Threat | Mitigation |
|---|---|
| Someone on the LAN or tailnet guessing passwords or PINs | Login rate limiting and lockout (5 wrong PINs → 15 min); PIN sign-in only from the home network by default (D34); admins always need a password (D35). Two-factor accounts (D75): 5 wrong codes per account → 15 min, each code and recovery code works once even under concurrent sign-ins, and away from home a PIN alone doesn't open them. |
| A stolen device token | Tokens are random 256-bit values stored only as SHA-256 hashes; every device can be signed out (Settings → Devices, USER-3). |
| Quick Connect code guessing | 6 characters from a 32-symbol alphabet (~10⁹), 10-minute expiry, approval needs a signed-in user, 10 attempts/min per user, token handed out once (D51). |
| Media URLs leaking | Stream URLs are capabilities: a 128-bit session id that stops working when the session ends (D38). Artwork, people photos and avatars use a per-user image key (`?key=`, D85) that only opens those images, so URLs handed to notifications, Android Auto, CarPlay or Cast devices can't be used to sign in; profiles only get art of items they may see. Request logs record paths only, never query strings. |
| Reading or writing files outside the libraries | The API never takes file paths for media; the folder browser is limited to configured roots; backup names must match `^[A-Za-z0-9._-]+\.db$` (tested); downloaded subtitles must name a plain language code (a path in the language was a write-anywhere bug, fixed 2026-10-02); DVR deletes only touch the recordings folder. |
| Uploaded images as an attack vector | Profile pictures are decoded and re-encoded as JPEG; nothing uploaded is stored or served as-is (D42). Uploads are capped at 10 MB. |
| Clickjacking / content sniffing | `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin` on every response. |
| Privilege escalation between users | Every handler checks the session; admin-only endpoints check the admin flag; library and content-rating restrictions are applied in SQL for every listing (tested). Playlists are per user. |
| Invite links (D86) | One-time 192-bit random tokens stored hashed, expiring (7 days by default, 90 at most); lookups and accepts are rate limited per address (10 failures → 15 min); only the first acceptance succeeds even when two race. New accounts are friends: password only, restrictions from the invite, never on the profile picker or switcher, no PIN sign-in and no switching into household profiles (tested). |
| Remote control (D87) | Players and controllers must be signed in; people see and control only their own devices (admins everyone's, friends only their own); a "play" command is checked against the target profile's library and rating limits; commands expire after 60 s and queues and device counts are bounded. |
| Bazarr (D87) | Admin-only URL and write-only API key (never returned or logged); requests only for movies and episodes the asking person may see. |
| Remote clients misusing bandwidth | Remote sessions are classified by source address (Tailscale ranges are always remote); per-user and global remote caps, fair upload share (§3a). `X-Forwarded-For` is honoured only from loopback (`tailscale serve` on the host), not from Docker bridge addresses, so another container can't claim to be on the home network. |
| Webhooks reaching internal services (SSRF) | Only admins can add webhook URLs or send tests; URLs must be http(s); payloads are signed (HMAC-SHA256) so receivers can reject forgeries; deliveries time out after 10 s and a full queue drops events. |
| Downloads leaking files | Originals are served only for items the user can see, from the same file lookup as playback (no paths from the client); converted files belong to the user who asked for them; accounts without remote access can't download away from home. |
| Third-party credentials | The OpenSubtitles API key and password are write-only in the API (only "is set" is returned) and are sent only to OpenSubtitles. |
| Tokens on devices | Device tokens live in the Keychain (Apple) and app-private storage (Android). Artwork URLs (Top Shelf, notifications, Android Auto, CarPlay) carry the image key, not the token (D85). Android's music service accepts only the app itself and trusted controllers (system UI, Android Auto, Assistant). |
| Vulnerable dependencies | `govulncheck` (Go, toolchain pinned to the patched release) and `npm audit` (web) run before releases. |

## Release checklist

1. `go vet ./...`, `go test ./...` (server), `npx vitest run`, `npm run lint`, `npx tsc --noEmit` (web), `swift test` (MarqueeKit) and the iOS/tvOS UI tests.
2. `govulncheck ./...`: no reachable vulnerabilities. `npm audit --omit=dev`: 0.
3. The image builds from `golang:1.26` (latest patch) and `debian:trixie-slim`.
4. Deploy, check `/api/v1/system/info`, the startup log (migrations, Bonjour interfaces), and a playback on each encoder (`marquee dev-transcode`).

## Results (2026-10-02)

- Bug sweep (v0.33): reviews of the server and every client. Fixed: subtitle-language path traversal, two-factor code guessing and PIN bypass away from home, image URLs carrying sign-in tokens (image keys), music service open to any app on Android, X-Forwarded-For trusted from Docker bridges, artwork of hidden libraries, transcoders restarting after a session ended or twice at once, Seerr approvals that could run twice.

- govulncheck: no reachable vulnerabilities after pinning `toolchain go1.26.8` and upgrading kin-openapi and x/crypto.
- npm audit: 0 vulnerabilities.
- Remote path checked from a tailnet device: classified `remote`, PIN sign-in withheld, security headers present.
- M7 re-check (v0.19): govulncheck reports nothing reachable (only GO-2026-5932, the deprecated `x/crypto/openpgp` package, which Marquee doesn't import; no fix exists). npm audit: 0. Fixed during review: the raw download route now applies the same remote-access rule as playback.

## Notes for the owner

- **Devices at home with Tailscale running:** if "Use Tailscale subnets" is on, their traffic to `10.1.1.10` goes through Tailscale and the server (correctly) treats them as remote. Streams still play at full quality on a fast connection, but the PIN profile picker is hidden. Turn that option off on devices that live at home, or leave it; nothing breaks.
- Tailscale on the server is never changed by Marquee.
- **Sharing with friends outside the home:** a friend needs an address they can reach. With Tailscale, share the server's node with them from the Tailscale admin console (Machines → Share), then send an invite link that uses the Tailscale address. Their traffic counts as remote, so remote caps and the invite's remote setting apply.
