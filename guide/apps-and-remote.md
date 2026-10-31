# Apps and remote access

Install the apps, sign in, stream away from home and set your own preferences.

## Contents

- [Install the apps](#install-the-apps)
- [Connect and sign in](#connect-and-sign-in)
- [Profiles and switching](#profiles-and-switching)
- [Remote access](#remote-access)
- [Share with friends](#share-with-friends)
- [Casting and AirPlay](#casting-and-airplay)
- [Offline downloads](#offline-downloads)
- [Your account and preferences](#your-account-and-preferences)

## Install the apps

The web app is built into the server: open `http://<server>:32500` in any browser. The native apps are on the [latest release](https://github.com/pingpongshow/marquee/releases/latest) page.

| App | File | Needs |
|---|---|---|
| Android phone, tablet and Android TV | `Marquee-<version>.apk` | Android 8.0 or later |
| iPhone and iPad | `Marquee-iOS-<version>-unsigned.ipa` | iOS/iPadOS 18 or later |
| Apple TV | `Marquee-tvOS-<version>-unsigned.ipa` | tvOS 18 or later |

### Android phones and tablets

Download the APK on the device and open it. Android asks you to allow installing apps from your browser or file manager the first time. Updates install the same way, over the top of the old version.

### Android TV

Either copy the APK to the TV and open it with a file manager, or install it from a computer with `adb`:

1. On the TV, turn on **Developer options** (Settings → Device Preferences → About → press **Build** seven times), then **USB debugging** or **Network debugging**.
2. Find the TV's address under its network settings, then from your computer:

```bash
adb connect 192.168.1.30:5555
adb install -r Marquee-1.4.0.apk
```

Accept the prompt on the TV the first time. `-r` replaces an installed copy when you update.

### iPhone and iPad

The IPA is unsigned, so you sign it with your own Apple ID when you install it:

- **[AltStore](https://altstore.io)** or **[Sideloadly](https://sideloadly.io)**: install the tool on your computer, connect the device, drop in the IPA and sign in with your Apple ID.
- **Xcode:** build it yourself (see [Building from source](../README.md#building-from-source)), set your team and run it on the device.

With a free Apple ID, sideloaded apps stop opening after 7 days until you re-sign them (AltStore can refresh them automatically). A paid developer account lasts a year.

### Apple TV

Install `Marquee-tvOS-<version>-unsigned.ipa` with **Sideloadly**, or build the tvOS target in Xcode:

1. Put the Apple TV and your Mac on the same network. On the Apple TV, open **Settings → Remotes and Devices → Remote App and Devices**.
2. In Xcode, **Window → Devices and Simulators**, and pair the Apple TV when it appears.
3. Select the tvOS scheme and the Apple TV as the destination, set your team, and run.

## Connect and sign in

**Finding the server.** On the same network, the apps find Marquee by themselves (Bonjour) and list it under *Choose your server*. Pick it. If nothing appears, type the address instead, for example `192.168.1.20:32500` (the port is added if you leave it out).

Discovery needs **Advertise on the local network (Bonjour)** on under **Settings → Network**, host networking in Docker, and the device on the same network as the server.

**Signing in.** What you see depends on **PIN sign-in** in **Settings → Users** (see [First run](first-run.md#add-people)):

- **Who's watching?** Tap your profile and enter your 4-digit PIN, if you have one.
- **Username and password**, from any network. If you've turned on two-factor sign-in, you're also asked for an **Authentication code**.
- **Quick Connect**, for TVs and anything awkward to type on. Choose **Use Quick Connect** and the device shows a 6-character code. On your phone or computer, open **Account → Link a device** in Marquee (or go to `http://<server>:32500/link`), enter the code and choose **Link**. The device signs in as you. The iPhone and iPad apps can approve codes too.

**Forgotten password or PIN.** Choose **Forgot password?** or **Forgot PIN?** and enter your username or email. If your account has an email address and the server has email set up (**Settings → Notifications**), a link arrives that works for 30 minutes. Otherwise ask an administrator to reset it in **Settings → Users**.

**Signed-in devices.** Administrators can see every signed-in app and browser in **Settings → Devices** and sign any of them out.

## Profiles and switching

On a shared device, switch to another household profile without signing out:

- **Web:** your avatar → **Switch profile**.
- **iPhone, iPad and Apple TV:** **Switch Profile** in the app's settings.
- **Android and Android TV:** **Switch profile** in the app's settings, which takes you back to *Who's watching?*.

Profiles with a PIN ask for it. Locked profiles (five wrong PINs) open again after 15 minutes. Friends don't appear on the profile picker and can't switch profiles.

## Remote access

Use Marquee away from home through Tailscale only (see below). Devices on your home network get full quality; everything else, including Tailscale, is treated as remote and gets a quality that fits the connection.

### Tailscale

[Tailscale](https://tailscale.com) connects your devices to the server privately and encrypted, with nothing opened on your router.

1. Install Tailscale on the server (on Unraid, the Tailscale plugin) and on each phone, tablet or laptop you'll use away from home.
2. Optional but recommended: give Marquee an HTTPS address with `tailscale serve`:
   ```bash
   tailscale serve --bg 32500
   ```
   It prints an address like `https://media.example-tailnet.ts.net`.
3. In **Settings → Remote Access**, turn on **Allow remote streaming** and set **Remote server address** to that address (or to `http://<tailscale ip>:32500` if you skipped step 2).
4. Open the apps once at home. They learn the remote address from the server and switch between the home and Tailscale addresses by themselves. You can also type the Tailscale address into an app while you're away.

### Don't expose Marquee to the internet

Tailscale is the only supported way to reach Marquee away from home. Don't forward port 32500 (or any port) on your router, don't put the server in your router's DMZ, and don't publish Marquee through a public reverse proxy or tunnel. Marquee serves plain HTTP and holds your accounts and viewing history, and anything reachable from the internet is scanned and attacked within minutes. Port forwarding and DMZ are not safe alternatives.

### Settings → Network

| Setting | Default | What it does |
|---|---|---|
| **LAN subnets** | `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | Devices on these networks connect directly and always get full quality. CIDR notation, e.g. `192.168.1.0/24`. Tailscale addresses (`100.64.0.0/10`) are always remote. |
| **Local server address** | (empty) | The address local apps use, e.g. `http://192.168.1.20:32500`. |
| **Advertise on the local network (Bonjour)** | On | Lets the apps find this server automatically. It advertises only on network interfaces inside your LAN subnets (not Docker or Tailscale). |

The top of the page says whether the browser you're using is connected as **Local** or **Remote**, which is a quick way to check your subnets.

### Settings → Remote Access

**Tailscale**

| Setting | Default | What it does |
|---|---|---|
| **Allow remote streaming** | On | When on, apps are given the **Remote server address** so they can reach the server away from home. To keep one person at home, turn off **Allow streaming away from home** for them in **Settings → Users**. |
| **Remote server address** | (empty) | The HTTPS address from `tailscale serve`, e.g. `https://media.example-tailnet.ts.net`. |

**Bandwidth**

Remote quality is chosen automatically: the lowest of the app's quality setting, the person's limit, the measured connection speed, and this server's fair share of upload bandwidth.

| Setting | Default | What it does |
|---|---|---|
| **Internet upload speed** | (empty: unknown) | Your server's upload speed, in Mbps. Shared fairly between remote streams. |
| **Test internet speed** | | Measures the server's own connection against Cloudflare's speed test (about 20 seconds), showing download, upload and latency. **Use as upload speed** saves 90% of the measured upload, leaving headroom for everything else on your connection. |
| **Total remote limit** | Unlimited | A cap across all remote streams, in Mbps. |
| **Per-stream remote limit** | Unlimited | A cap for each remote stream, in Mbps. |
| **Minimum per-stream bandwidth** | 1.5 Mbps | The floor when many remote streams share the upload. Below this, new streams start at the lowest quality. |

The steps remote devices move between are in **Settings → Transcoder → Remote quality ladder**; see [Media settings](settings-media.md).

## Share with friends

Friends outside your household get their own account with the libraries and limits you choose.

1. Make sure they can reach the server: share it with them from the Tailscale admin console (**⋯ → Share**); they install Tailscale and accept.
2. Open the web app **at that address** (the invite link uses the address in your browser), go to **Settings → Users → Friends** and choose **Invite a friend**.
3. Fill in **Who it's for** (a note for you, e.g. Mum), **Expires after (days)** (default 7, between 1 and 90), and their access: libraries, maximum content rating, remote streaming and quality.
4. Choose **Create invite link** and send them the link. It works once.

Your friend opens the link, picks a username and password, and joins. Friends sign in with their own password and aren't shown on the profile picker. Remove a friend from the **Friends** list at any time.

## Casting and AirPlay

- **Chromecast** from the iPhone, iPad and Android apps. Browsers only allow casting from HTTPS pages, so the web app can cast only over `tailscale serve` (HTTPS).
- **AirPlay** on iPhone and iPad, to an Apple TV or AirPlay 2 speakers (HomePod, Sonos and others) for music.

## Offline downloads

The iPhone, iPad and Android apps can download films, episodes, whole seasons and music to watch and listen offline. Choose a quality when you download:

| Quality | What you get |
|---|---|
| **Original** | The file as it is on the server. |
| **High (1080p)**, **Medium (720p)**, **Low (480p)** | Converted on the server first, smaller and playable everywhere. |

Music always downloads as the original file. Downloads are listed under **Downloads** in the app. When you're offline the app says so and plays your downloads without the server; ratings, watched marks and progress made offline sync when you reconnect.

## Your account and preferences

Each person sets their own preferences in the web app under avatar → **Account**. They follow you to every app you sign in to.

| Card | Settings |
|---|---|
| **Profile** | Profile picture and **Display name**. |
| **Email** | **Email address**, for a link to reset your password or PIN if you forget it. Administrators also get alerts here. Managed profiles ask an administrator to change it. |
| **Link a device** | Approve a TV's Quick Connect code. |
| **Password** | **Change password** (at least 8 characters). If you don't have one yet, **Set password** to sign in from outside the home or on devices without PIN sign-in. Not shown for managed profiles. |
| **Two-factor sign-in** | Asks for a code from an authenticator app (1Password, Google Authenticator, Authy…) whenever you sign in with your password. Scan the QR code, enter **Code from the app**, and keep the recovery codes. PIN sign-in at home isn't affected. Shown once you have a password. |
| **Last.fm** | Connect your Last.fm account to scrobble what you listen to on any device. |
| **ListenBrainz** | Paste your **User token** (listenbrainz.org → Settings → User token). Tracks played halfway or more are sent as listens. |
| **Profile PIN** | A 4-digit PIN asked when someone switches to your profile. **Remove PIN** to drop it. |
| **Playback** | See below. |
| **Subtitle appearance** | See below. |
| **Your stats** | What you've watched and listened to, and Your Year in Music. |

**Playback**

| Setting | Default | Options |
|---|---|---|
| **Preferred audio language** | Default | Default, English, Japanese, Spanish, French, German, Italian, Korean, Chinese, Portuguese |
| **Preferred subtitle language** | Default | As above |
| **Subtitles** | When audio is in another language | Off, Forced only, When audio is in another language, Always |
| **Quality at home** | Original (no transcoding) | Original, or 1080p · 12 Mbps down to 360p · 1 Mbps |
| **Quality away from home** | Automatic | Automatic (adapts to your connection), or a fixed step |
| **Play trailers before movies** | On | When the server has cinema trailers on and a film starts from the beginning. |
| **Night mode by default** | Off | Starts videos with night mode on (see below). |
| **Ask me to rate after watching** | On | Shown when the server asks people to rate (**Settings → General**). |

Choose **Save preferences** when you're done.

**Night mode** makes loud scenes quieter and dialogue clearer: the server lifts the centre (dialogue) channel, mixes the sound down to stereo and evens out the volume. Switch it for the video you're watching in the player's **Audio and subtitles** menu (playback carries on where it was), or turn on **Night mode by default** above. The picture is still sent as it is when it can be; only the sound is converted, so **Playback info** shows *night mode converts the audio*. Downloads for offline viewing keep the original sound.

**Subtitle appearance** changes how text subtitles look in every app, with a live preview. Styled (ASS) subtitles keep their own look.

| Setting | Options |
|---|---|
| **Size** | Small, Medium, Large, Huge |
| **Colour** | White, Yellow, Cyan, Green, or **Custom** |
| **Background** | None (drop shadow), Outline, Translucent box, Solid box |
| **Position** | Bottom, Raised (above the controls) |

**Reset to default** puts it back; **Save appearance** saves it.

**Home rows.** Each person arranges their own Home screen: choose **Edit Home** on Home to drag rows (or use the arrows) into order and hide the ones you don't want. Pin collections and playlists to Home from their own pages with **Pin to Home**.

**Suggestions on Home.** Home's suggestion rows are worked out for each person from their favourites, ratings and what they've finished (including what they watched or rated on VOD):

- **Your tastes, not an average.** Marquee groups what you like into a few tastes (say, space films and romantic comedies) and gives each its share of **Recommended for You**, instead of averaging them into something that fits neither.
- **Every row is ordered for you.** Top Rated, Your Genres, Best of a Decade, Holiday Picks and the other category rows put what fits your taste first, along with how well rated it is.
- **Variety.** A row doesn't show near-copies of one film or more than one film of a series, the order turns a little each day, and titles you've scrolled past for several days without opening slowly make way for others.
- **Why it's there.** Tiles say why they were picked: *Because you loved Heat*, *Like Arrival*, *Next in The Dark Knight Collection*.
- **New rows:** **Continue the Saga** (the next film of a series you've started), **More From** a director or **More With** an actor whose films you loved, and **Hidden Gems** (well-rated titles nobody on your server has watched yet).
- **Not interested.** Open a suggestion's menu (right-click or **…** on the web, long press or the context menu in the apps) and choose **Not interested**. It disappears from every suggestion row and Marquee steers away from titles like it. The web shows **Suggest it again** on the title's page to undo it.
- **Rows that don't work for you sink.** If a row has shown you plenty of titles over a month and you've never played anything from it, it moves below your other suggestion rows. This only happens while you keep Home's default order; once you arrange Home yourself, your order stays.

---

[Guide contents](README.md) · Previous: [First run](first-run.md) · Next: [Server settings](settings-server.md) · [Troubleshooting](troubleshooting.md)
