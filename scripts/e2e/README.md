# Local end-to-end test environment

The Apple and Android UI tests run against a local Marquee server with synthetic test media.
These scripts build that environment in `.e2e/` at the repo root, which git and Docker ignore.

```sh
scripts/e2e/setup.sh                 # build it (about a minute); keeps a working one
scripts/e2e/setup.sh --clean         # throw it away and build it again
scripts/e2e/with-server.sh '<cmd>'   # run <cmd> with the server and the fakes up
```

`setup.sh` needs Go, FFmpeg and Python 3. It:
- generates the media with FFmpeg,
- builds the server to `.e2e/marquee`,
- runs it on port 32597 with every `MARQUEE_*` path under `.e2e/`,
- completes first-run setup, then creates the users, libraries, Live TV source and Bazarr settings.

`with-server.sh` starts the server, `scripts/fake-iptv.py` (port 32598) and `scripts/fake-bazarr.py`
(port 32767), runs the command, then stops them all. Logs go to `.e2e/*.log`.

## What's in it

- **Users:**
  - `e2eadmin` / `e2e-local-test-pass` (admin, id 1)
  - `kiddo`, a managed profile without a PIN that may request titles (id 2)
  - `twofa` / `correct horse battery`, with two-factor sign-in on (the secret is in `.e2e/twofa.secret`)
- **Tokens:** the admin's in `.e2e/token` and Kiddo's in `.e2e/kid.token`.
- **Libraries:**
  - Anime: Test Anime, with styled ASS subtitles and an embedded font
  - Movies: 300 generated titles, plus `00 Long Test` and `00 Preview Test` (featurette, trailer, Extended Cut), and the Test Saga collection
  - Music: Test Artist 1-3, six 40-second albums (Calm Pads/Floating has an `.lrc`), a 24-bit/96 kHz FLAC, and Getz/Gilberto with cover art
  - Recorded TV: one Weather Watch episode
- **Integrations:** the fake IPTV source, the fake Bazarr and a dummy OpenSubtitles key. No TMDB
  key, so nothing is matched online.
- **Not set up:** Seerr, which is only connected in production, and the Soundprint sidecar.

Some tests use fixed item ids, so `provision.py` adds files in steps that always give the same
ids. Its docstring lists them, for example 4-6 (20 Valley, 15 Thunder, 31 Ocean), 322
(00 Long Test), 324 (Floating), 359 (00 Preview Test) and 362 (its trailer).

## Running the UI tests

```sh
# iOS (the scheme passes MARQUEE_TEST_ADMIN_TOKEN to the tests); tvOS is the same with -scheme MarqueeTV
scripts/e2e/with-server.sh 'cd apple && MARQUEE_TEST_ADMIN_TOKEN=$(cat ../.e2e/token) xcodebuild test \
  -project Marquee.xcodeproj -scheme Marquee -destination id=<simulator> \
  -only-testing:MarqueeUITests/MarqueeUITests/testBrowseAndPlay'
# Android, on an emulator
scripts/e2e/with-server.sh 'cd android && MARQUEE_TEST_ADMIN_TOKEN=$(cat ../.e2e/token) \
  scripts/ui-test.sh 10.0.2.2:32597 app.marquee.MarqueeUiTest#tapToSeek'
```

Tests that change shared state, such as ratings, watch history or Home layouts, leave it
changed. Run `setup.sh --clean` to get back to a known state.
