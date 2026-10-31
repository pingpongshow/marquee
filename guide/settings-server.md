# Server settings

Running the server day to day: its name, the people who use it, signed-in devices, email and alerts, webhooks, scheduled tasks and backups, and the activity pages.

Everything here is under **Settings** in the web app and needs an administrator account. Most sections have a save bar at the bottom: nothing changes until you press **Save**.

## Contents

- [General](#general)
- [Users](#users)
- [Devices](#devices)
- [Notifications](#notifications)
- [Webhooks](#webhooks)
- [Scheduled Tasks](#scheduled-tasks)
- [Backups and restore](#backups-and-restore)
- [Dashboard](#dashboard)
- [Statistics](#statistics)
- [Logs](#logs)

## General

**Settings → General** holds the server's name and a few household-wide defaults.

| Setting | Default | What it does |
|---|---|---|
| Server name | `Marquee` | Shown in every Marquee app, and in emails the server sends. |
| Default metadata language | English (US) | Titles, summaries and artwork are fetched in this language. Each library can override it. |
| Ask people to rate movies and episodes when they finish them | Off | Shows a card with stars when a movie or episode ends (on the Apple TV, a screen of its own, so the remote reaches the stars). Each person can turn it off for themselves under **Account → Ask me to rate after watching**. |

**Restart server** stops all playback and restarts Marquee. Docker starts it again within a few seconds, and the page reloads when it's back. Anyone watching or listening is interrupted.

## Users

**Settings → Users** is where you add the people in your household, decide what each of them can see and do, and invite friends from outside it.

### Sign-in

The **Sign-in** card at the top controls how people sign in on this server's apps and web page.

| Setting | Default | What it does |
|---|---|---|
| PIN sign-in | On the home network only (recommended) | Shows a "Who's watching?" profile picker where people enter their 4-digit PIN. **Everywhere, including remote** also allows PINs over Tailscale or the internet. **Off: always use a password** hides the picker. |

Five wrong PINs lock that profile for 15 minutes. People without a PIN use their password; managed profiles without a PIN open with one tap.

**Recommended:** keep PIN sign-in on the home network only. Over Tailscale, a password is safer than a 4-digit PIN.

### Adding a user

Press **Add user** and choose what kind of account it is:

- **Person:** signs in by choosing their profile, with an optional PIN or password. Can be made an administrator.
- **Managed profile:** for children or shared TVs. Has no password; it's opened by switching profile, optionally with a PIN. A managed profile can't be an administrator.

| Setting | Default | What it does |
|---|---|---|
| Username | (empty) | The sign-in name. Up to 64 characters. Can't be changed later. |
| Display name | Same as the username | The name shown on the profile picker and around the apps. Optional. |
| Email | (empty) | Optional. Used for reset links if they forget their password or PIN, and for alert emails if they're an administrator. |
| Password | (empty) | Not shown for managed profiles. Optional for everyone else, but **required for administrators** (at least 8 characters). Needed to sign in from outside the home network when PIN sign-in is home-only. |
| PIN | (empty) | Optional, 4 digits. Asked when choosing this profile. |
| Administrator | Off | Can change settings, libraries and users. Administrators have every permission, so the access settings below are hidden. |

### Access and limits

These appear when you add or edit anyone who isn't an administrator, and (most of them) when you invite a friend. Some only appear when the feature they control is set up. Administrators always have every permission here.

| Setting | Default | What it does |
|---|---|---|
| Access to all libraries | On | Includes libraries added later. Turn it off to tick the libraries this person may see. |
| Maximum content rating | No limit | Hides movies and shows rated higher, **and unrated ones**. Choices: TV-Y (all children), G / TV-G, PG / TV-PG, PG-13 / TV-14, R / TV-MA. Music isn't affected. |
| Can request movies and shows | Off | Lets them request titles through Seerr. Each request needs an administrator's approval unless **Approve their requests automatically** is on. Shown only when Seerr is connected ([Requests](settings-media.md#requests)). |
| Approve their requests automatically | Off | Their requests go straight to Seerr, the way an administrator's own do, instead of waiting in the approval queue. If Seerr refuses one, it waits in the queue to be approved again. Shown when **Can request** is on. |
| Request as Seerr user | Seerr's admin (the API key) | The Seerr user their requests appear under in Seerr. Shown when **Can request** is on. |
| Can watch Live TV | On | Live TV and the guide. The same switch also controls films and shows on demand (VOD). Shown only when Live TV is set up. |
| Can download from VOD | Off | Lets them queue films and episodes on demand in the VOD download tool, into the library's download folders: **Save** on a film's or show's VOD page, and **Download** on Discover. The pages show how each of their downloads is going. Managing the download queue (pausing, reordering, clearing) and upgrades that replace a copy already in the library stay with administrators. Needs **Can watch Live TV**. Shown only when VOD is set up. Not on invites. |
| Can record Live TV | On | Lets them schedule recordings and series. When off, their series are paused (kept) and nothing new is recorded for them. Shown when the DVR is available. |
| Can watch recordings | On | Lets them see and play the household's recordings, within their rating limit: the Recordings tab and the Recorded TV and Recorded Movies libraries. |
| Recording quota | Use the default | How much space their recordings may take. **Use the default** follows the server-wide quota; **No limit**; or **A set amount** in GB. Recordings in progress always finish. |
| Allow streaming away from home | On | Turn off to limit this person to the home network. |
| Remote quality limit | Server default (automatic) | Caps their streams away from home: 1080p · 12 Mbps, 1080p · 8 Mbps, 720p · 4 Mbps, 480p · 2 Mbps or 360p · 1 Mbps. |

The server-wide recording quota (**Recording quota per person**, default 0 = no limit) and what happens when someone goes over it (**When someone is over quota**, default *Don't record anything new*) are set in **Settings → Live TV**. See [Live TV and DVR](live-tv-and-dvr.md).

Administrators have no quota by default. When you edit an administrator, only **Recording quota** is shown, so you can give an admin (yourself included) a quota too.

**Recommended for a child's profile:** a managed profile with a PIN-free one-tap entry, **Maximum content rating** set to their age band, **Access to all libraries** off with only the family libraries ticked, **Allow streaming away from home** off if they only watch at home, and a **Daily limit** or **Viewing hours** if you want them (see [Time limits](#time-limits)).

### Time limits

Also under **Access and limits**, for anyone who isn't an administrator: a **Time limits** box that limits how much video they watch, and when. Everything in it is off until you set it, and administrators are never limited.

| Setting | Default | What it does |
|---|---|---|
| Daily limit | No limit | Minutes of video a day. Type minutes (`90`) or hours and minutes (`1:30`); empty means no limit. |
| Different limit at weekends | Off | Turn on to give Saturdays and Sundays their own limit. Leave its box empty for no limit at weekends. |
| Viewing hours | Off | Video only plays between **From** and **Until** (for example 07:00–20:00). The hours may cross midnight (20:00–01:00). |

What counts: movies, episodes, home videos, recordings, films and shows on demand, and Live TV, for the time they actually play (pausing and skipping ahead don't use it up). Music and cinema trailers don't count and aren't limited. The day follows the server's clock and starts again at midnight.

When time's up, or outside the viewing hours, starting a video shows a full-screen message with the reason, such as "Time's up for today — back at midnight" or "Viewing hours are 07:00–20:00". A video already playing when the time runs out stops within a few seconds with the same message, and downloading a video for offline viewing is refused too. Five minutes before the end, the player shows a gentle warning, and a person with a limit sees how much is left today ("45 min left today") at the top of their account menu. Videos already downloaded to a phone or tablet are checked by the app.

### Editing and deleting

Press the pencil next to a user to change their picture, display name, email, password or PIN, administrator status and access. Leave the password field empty to keep the current one. **Remove** next to the PIN clears it. You can't remove your own admin access if you're the only administrator.

The list shows each person's badges (Admin, Managed, PIN), a summary of their limits, when they were last active and, with the DVR on, how much their recordings use.

Deleting a user signs them out everywhere and removes their watch history and playlists. You can't delete yourself.

### Two-factor sign-in

Each person turns on two-factor sign-in for themselves under **Account → Two-factor sign-in**. Marquee then asks for a code from an authenticator app (1Password, Google Authenticator, Authy and so on) whenever that account signs in with its password, and gives a set of single-use recovery codes. PIN sign-in on the home network isn't affected.

If someone loses their phone and their recovery codes, edit them in **Settings → Users** and press **Turn off** next to "Two-factor sign-in is on".

### Inviting friends

The **Friends** card is for people outside your household. Friends sign in with their own password, aren't shown on the profile picker and can't switch profiles.

Press **Invite a friend**, fill in the form and press **Create invite link**. Send the link to your friend: they open it, pick a username and password, and join with the access you chose. Each link works once.

| Setting | Default | What it does |
|---|---|---|
| Who it's for | (empty) | A note for you, for example "Mum". Shown in the invite list. |
| Expires after (days) | 7 | 1 to 90 days. |
| Access and limits | As above | The same library, rating, Live TV, recording and remote settings as for household users. |

The link starts with the address you're using in the browser, so create invites from an address your friend can reach (your Tailscale or public address), not `localhost` or a home IP address. The invite list shows each invite as pending, used (and by whom) or expired; delete an invite to cancel it.

### Password and PIN resets

When email is set up (see [Notifications](#notifications)), the sign-in screen shows **Forgot password?** and, for PIN profiles, **Forgot PIN?**. Marquee emails a reset link to the address on that account. The link works once, for 30 minutes. After a reset every device on that account is signed out, and a confirmation email is sent.

Resets need three things: working email, an email address on the account, and a link address (the **Link address** in Notifications, or failing that the Remote Access address). Without an email address, an administrator can reset the password or PIN in **Settings → Users**.

## Devices

**Settings → Devices** lists every signed-in app and browser: its name, who it belongs to, the app and version, when it was last seen and its last address.

Press **Sign out** to sign a device out. It has to sign in again before it can be used. The device you're using is marked **This device** and can't be signed out from here.

## Notifications

**Settings → Notifications** sets up email, for password and PIN reset links and for alerts, and chooses which alerts administrators get.

### Email

Marquee sends mail through your mail server or provider over SMTP.

| Setting | Default | What it does |
|---|---|---|
| Mail server | (empty) | Your provider's SMTP server, for example `smtp.example.com`. Empty turns email off. |
| Port | `587` | Changes to the usual port when you change **Security** (587 for STARTTLS, 465 for TLS, 25 for none), unless you've typed an unusual one. |
| Security | STARTTLS | STARTTLS is usual on port 587, TLS on 465. With **None**, a password can't be sent. |
| Username | (empty) | Optional; leave empty if the server doesn't need a sign-in. |
| Password | (empty) | Your password or app password. Stored on the server and only sent to the mail server; shown as **Configured** once saved. Use **Replace** or **Remove** to change it. |
| From address | (empty) | The sender address, for example `marquee@example.com`. Required. |
| From name | `Marquee` | The sender name. |
| Link address | (empty) | Links in mail (reset links) start with this, for example `https://marquee.example.com`. Empty uses the Remote Access address; if that isn't set either, reset links can't be sent. |

Email is on once a mail server, port and from address are saved. Then use **Send a test email to** (it starts with your own address) and **Send test**. The test uses the saved settings, so save first. If it doesn't arrive, check the spam folder.

**Recommended:** set **Link address** to the address people use away from home (your Tailscale HTTPS address), so reset links work wherever they're opened.

**Example: Gmail**

Gmail needs an *app password*, not your normal password. Turn on 2-Step Verification for the Google account, create an app password in the account's security settings, then enter:

| Setting | Value |
|---|---|
| Mail server | `smtp.gmail.com` |
| Port | `587` |
| Security | STARTTLS |
| Username | your full Gmail address, e.g. `your.name@gmail.com` |
| Password | the 16-character app password |
| From address | the same Gmail address |

**Example: any other provider**

Look up your provider's "SMTP settings" page and copy them across. Most use port 587 with STARTTLS and your full email address as the username:

| Setting | Value |
|---|---|
| Mail server | `smtp.example.com` |
| Port | `587` (or `465` with TLS) |
| Security | STARTTLS (or TLS) |
| Username | `marquee@example.com` |
| Password | your mailbox or app password |
| From address | `marquee@example.com` |

Many providers only accept a **From address** that belongs to the account you sign in with.

### Email alerts

Alerts tell administrators when something goes wrong. **Every alert appears under the bell** in the top bar for administrators, and goes to any webhook subscribed to **Alerts**. The switches here choose which alerts are **also emailed** to the recipients below, once email is set up. The same alert is raised at most once an hour.

Three are emailed by default (VOD download failed, Large files conversion failed and Library folder can't be read).

| Emailed | Default | What it's about |
|---|---|---|
| A scheduled task failed | Off | Any scheduled task, including the nightly database backup. |
| Disk space is low | Off | The config, transcode or recordings folder, or a library folder. Checked every 15 minutes. |
| Low disk space threshold | 10 % | Alert when a volume has less free space than this (1–50 %). |
| A recording failed | Off | Live TV DVR recordings. |
| A Live TV or movie source couldn't be loaded | Off | Checked whenever the channel and film lists refresh. |
| A connected service isn't answering | Off | Seerr, Bazarr, Ollama (AI) and Soundprint, if set up. Reported after two checks in a row fail. |
| Repeated failed sign-ins | Off | Five wrong passwords for one account within 15 minutes. |
| An administrator signed in from a new device | Off | A browser or app that account hasn't used before. |
| Playback failed | Off | The transcoder couldn't produce video for a stream. |
| VOD download failed | **On** | Saving a movie or episode on demand into the library failed after its retries. |
| Saving to the library finished | Off | One summary when the VOD save queue empties, not one per episode. |
| Someone is over their recording quota | Off | A recording wasn't made, or their oldest watched recordings went to the media trash to make room. |
| Plex sync is failing | Off | Plex rejects the token, or hasn't been reachable for three sync runs in a row. |
| Large files conversion failed | **On** | A conversion in Library Health → Large files failed (not one skipped for saving too little). The original is unchanged. |
| Library folder can't be read | **On** | A scan couldn't read one of a library's folders (a drive that's offline, permissions). Its titles are kept until it can be read again; another notice says when it can. |
| New file is damaged | Off | A newly added file failed its playback check (its start or end doesn't decode). It's listed in Library Health → Unplayable files. |



**Recommended:** also email *A scheduled task failed* (so you hear if backups stop), *Disk space is low* and *Repeated failed sign-ins* if the server is reachable from outside your home.

### Alert email recipients

| Setting | Default | What it does |
|---|---|---|
| All administrators with an email address | On | Every administrator with an email address gets alert mail. Turn it off to tick individual administrators. Administrators without an address can't be ticked; add one in **Users** or on their **Account** page. |

## Webhooks

**Settings → Webhooks** tells other services (Home Assistant, Discord bridges, your own scripts) when something plays, when new titles arrive, or when an alert fires.

Each event is sent as a JSON `POST` to the webhook's URL. With a secret, the body is signed with HMAC-SHA256 in an `X-Marquee-Signature: sha256=…` header, so the receiver can check the request came from your server. Failed deliveries are retried twice.

Press **Add webhook** for each receiver, then **Save**.

| Setting | Default | What it does |
|---|---|---|
| Enabled | On | Turn off to stop sending without deleting the webhook. |
| Name | (empty) | A label for you, for example "Home Assistant". |
| URL | (empty) | Where events are posted, for example `http://homeassistant.local:8123/api/webhook/…`. Must start with `http://` or `https://`. |
| Secret | A random value | Optional. Signs each request. The key button makes a new one; clear the field to send unsigned requests. |
| Events | Playback started, Stopped | Tick the events to send. |

The events are:

| Event | Sent when |
|---|---|
| Playback started (`playback.started`) | Someone starts playing something. |
| Paused (`playback.paused`) | Playback is paused. |
| Resumed (`playback.resumed`) | Playback carries on after a pause. |
| Stopped (`playback.stopped`) | Playback stops. |
| Watched / played (`playback.watched`) | Playback passes the **Mark as watched after** point (90 % by default), for videos and tracks alike. |
| New in a library (`library.added`) | New titles are added to a library. |
| Alerts (`alert`) | An alert you've turned on in [Notifications](#alerts) fires. |

**Send test** posts a test event to the URL (with the secret) and shows whether it was delivered.

For monitoring, the server also publishes Prometheus metrics at `/api/v1/system/metrics`.

## Scheduled Tasks

**Settings → Scheduled Tasks** sets the maintenance window, lists every background job, and holds the database backups.

### Maintenance window

Heavy jobs (metadata refresh, seek previews, intro detection, backups and so on) run once a day inside this window, so they don't compete with evening viewing.

| Setting | Default | What it does |
|---|---|---|
| Window starts at | `03:00` | When the window opens, in the server's time zone (`TZ`). |
| Window length (hours) | `4` | 1 to 12 hours. Long jobs stop when the window closes and carry on the next night. |

**Recommended:** pick the hours when nobody watches. If your library is new and large, a longer window gets seek previews and intro detection done sooner.

### Tasks

Each task shows its schedule, when it last ran, its last result, and a **Running** or **Failed** badge when that applies.

- **The switch** on the left turns a task on or off. Every task is on by default. A task that's off never runs by itself (on its schedule, or after scans and recordings), and shows **Off**.
- **Run now** starts it straight away, whether it's on or off, and ignores the maintenance window.

| Task | Schedule | What it does |
|---|---|---|
| Back up database | Daily in the maintenance window | Saves a copy of the database and removes old copies beyond the number to keep. See [Backups and restore](#backups-and-restore). |
| Optimize database | Daily in the maintenance window | Updates query statistics and compacts the database's write-ahead log. |
| Empty media trash | Daily in the maintenance window | Permanently deletes everything in the `.marquee-trash` folders in library folders: files deleted from Library Health, originals the Large files optimizer or commercial removal kept, and recordings removed for the DVR quota. Nothing outside those folders is touched, and a file that's playing is skipped. |
| Scan all libraries | Manual | Looks for new, changed and removed files in every library now. Libraries also rescan on their own schedule, and the file watcher catches most changes as they happen. |
| Refresh ratings | Every 6 hours | Fetches IMDb, Rotten Tomatoes and Metacritic ratings from OMDb, within the daily lookup limit. Needs an OMDb key. |
| Refresh metadata | Daily in the maintenance window | Updates shows with new or recently aired episodes, and movies not refreshed for six months. Fields you've edited stay as they are. |
| Artist and album details | Every day | With artist and album details on, looks up artists and albums on MusicBrainz (and bios on Wikipedia), one request a second. The first run covers the library and can take a few hours; later runs cover new music. |
| Fetch movie and show details | Every day | Fetches keywords, countries, original language and TMDB's recommendations for titles matched before Marquee stored them. New matches get them straight away. |
| Enrich anime from AniList | Daily in the maintenance window | Adds the names anime is known by (romaji, English, Japanese, all searchable), studios, genres and AniList scores. |
| Update collections | Every 7 days | Groups movies into their film series (TMDB collections). |
| Soundprint analysis | Every hour | Analyses new tracks so radios, similar music, mixes and Muse include them: how they sound and what they're like (happy, sad, danceable, sung…). Also reads the language of their lyrics, and tries another sound model on the library when one is chosen in Settings → Music. Also runs right after a music scan. |
| Learn from listening | Daily in the maintenance window | Pairs the tracks people play in one sitting or keep in one playlist, so radios and mixes play them together. |
| Similar artists and tags | Every day | With similar artists and tags on, looks up which artists listeners consider alike and how they tag artists and tracks, on ListenBrainz and, with a Last.fm API key, Last.fm, a few requests a second. The first run covers the library and continues where it stopped; later runs cover new music and refresh old lookups. |
| Index movies and shows | Daily in the maintenance window | Reads the descriptions of new and changed movies and shows so Muse, recommendations and related items include them. Also runs after a video scan. |
| Measure music loudness | Daily in the maintenance window | Reads ReplayGain tags and measures the loudness of tracks without them, so volume levelling works for everything. |
| Make seek previews | Daily in the maintenance window | Makes the thumbnails shown while seeking. A large library takes a few nights; it continues where it stopped. |
| Generate missing thumbnails | Daily in the maintenance window | Takes a frame from episodes and personal videos that have no thumbnail. A real still found later replaces it. |
| Find intros and credits | Daily in the maintenance window | Compares the audio of episodes in each season to find intros and end credits for Skip Intro and Skip Credits. |
| Commercials in recordings | Daily in the maintenance window | Finds the commercials in Live TV recordings and, when Settings → Live TV says so, cuts them out (the original goes to the media trash). |
| Check files | Every hour | Checks each movie, episode and personal video added to the library (or replaced) for damage: a few seconds at its start and near its end must decode, and its length must be right where it's known. Damaged files are listed in Library Health → Unplayable files. Also checks the frame timing of every video file once, those already in the library included (the first run goes through the whole library, which can take hours), and lists files whose frames are timed out of order in Library Health → Frame timing errors. Runs after a scan adds files; low priority, one file at a time. Files already in the library before the damage check arrived aren't checked for damage. |
| Optimize large files | Shows the queue's state | Converts the files queued in Library Health → Large files, giving way to playback. Off, the queue waits (running conversions are suspended); **Run now** wakes it. |
| Fetch lyrics | Every day | With online lyrics on, looks up lyrics on LRCLIB for tracks not looked up yet, about one a second. |
| Index lyric themes | Every 6 hours | With AI on, reads track lyrics with the embedding model so Muse can find songs about a theme. |
| Index artists and albums | Every day | With AI on, reads artist and album descriptions with the embedding model so Muse can find music by what the artists are like. |
| Plex sync | On its interval while automatic sync is on; otherwise manual | Syncs watch status, plays, resume positions and ratings with Plex, both ways. See [Plex](settings-media.md#plex). |
| Fetch VOD suggestion data | Daily in the maintenance window | Fetches TMDB recommendations, keywords and vote counts for films and series on demand, for suggestions. Also fetches up to 200 newly matched titles after a VOD refresh. |

Tasks that depend on a feature (lyrics, AI, Plex sync, AniList, VOD) do nothing useful until that feature is set up.

**Recommended:** leave every task on. Turn **Empty media trash** off only for as long as you want to keep deleted files recoverable.

## Backups and restore

The **Database backups** card at the bottom of Scheduled Tasks keeps copies of Marquee's database: libraries, users, watch history, ratings and settings. Media files, artwork caches and Soundprint's models aren't included.

| Setting | Default | What it does |
|---|---|---|
| Keep nightly backups for | 7 days | 1 to 365 days. Older nightly backups are deleted; the newest backup is always kept. Saved with its own **Save** button. |

Backups are taken:

- **Daily**, by the *Back up database* task in the maintenance window.
- **Manual**, when you press **Back up now**.
- **Before upgrade**, automatically before a new version changes the database.
- **Before restore**, automatically before a restore replaces the database.

Nightly backups follow the setting above; Marquee keeps the five newest of each of the other kinds. They're stored as `.db` files in `config/backups/`.

Each backup in the list has:

- **Download:** saves the `.db` file to your computer. Keep one somewhere other than the server.
- **Restore:** asks for confirmation, then restarts Marquee with the database as it was at that time. Anything that changed since (watch history, playlists, settings) is lost. The current database is saved first as a *Before restore* backup, so you can undo it. The page reloads when the server is back.
- **Delete:** removes the backup for good.

**To move to a new server or rebuild one,** copy the whole `config` folder across before starting Marquee. To restore a downloaded backup instead, put the `.db` file in `config/backups/` (keep a name made of letters, numbers, dots, dashes and underscores), and it appears in the list to restore.

## Dashboard

**Settings → Dashboard** shows what the server is doing right now.

- **Streams, Transcodes, Local, Remote:** how many streams are playing, how many transcodes are running out of the maximum (with the encoder order), and the bandwidth going to local and remote devices (out of your upload speed, if you've set it under Remote Access).
- **Bandwidth:** local and remote bandwidth over the last hour, one sample a minute. It starts empty after a restart.
- **Now playing:** each session's title, person, device, local or remote, how it's delivered (Direct Play, Direct Stream or Transcode, with the reasons), the encoder, bitrate and progress. The square button stops a stream.
- **Recent plays:** the last 40 plays, with who played them and how. Plays imported from Plex are marked.
- The footer shows the Marquee version, when the server started and how many items each library holds.
- **Storage:** where the space goes.
  - **Disks:** each disk Marquee uses (the config, transcode and recordings folders and every library folder) as a bar of used space, with the free space and the percentage free. Folders on the same disk share one bar, named for everything on it. The bar turns red under 10% free. The disk space alert in Notifications watches the same disks.
  - **Libraries:** each library's size, file count and how much it grew or shrank over the period you pick (the last 30 or 90 days, or the last year). Only files that are there count. Libraries Live TV recordings go into are listed too.
  - **Growth:** the libraries' sizes stacked over the period. Marquee records each library's size once a day and keeps a year of readings, so the chart starts the day you upgrade and fills in from there.
  - **Biggest titles:** the largest movies, shows (all their episodes together) and home videos, biggest first. Click one to open it. A movie or show whose files average 20 GB or more has an **Optimize** link to Library Health → Large files, which can make a smaller copy.

## Statistics

**Settings → Statistics** shows what's been played, by whom, on which apps and how it was delivered, like Tautulli.

- **Period:** Last 7 days, Last 30 days, Last 90 days, Last year or All time.
- **Person:** Everyone, or one person.

You get totals (plays, hours watched and listened, number of people or hours per day), plays per day, the top movies, shows, artists, albums, tracks, people and apps, and **How it played**: the share of direct play, direct stream and transcode, and how much was at home versus away.

**Music playlists** lists each kind of radio and mix played from, such as Daily Mixes, Discovery Mix, Mood radios or Muse playlists. For each it shows:

- **Plays**
- **To the end:** the share listened to at least 80% of the way.
- **Skipped:** the share skipped in the first 30 seconds.
- **Favourited:** tracks favourited within a week of playing from it.

A high skip rate shows a kind that isn't working. Plays count here once the apps are updated to report where a play started.

Each person sees their own statistics under **Account**.

## Logs

**Settings → Logs** shows the most recent 5,000 server log entries since the server started, refreshing every few seconds.

- **Minimum level:** Debug, Info (the default), Warnings or Errors.
- **Download** saves the entries shown as a `.log` file, which is handy for bug reports.

The full logs are also in `config/logs/`. To record more detail from the start, set `MARQUEE_LOG_LEVEL=debug` in the container's environment.

---

[Install](install.md) · [First run](first-run.md) · [Server settings](settings-server.md) · [Media settings](settings-media.md) · [Live TV and DVR](live-tv-and-dvr.md) · [VOD and Dispatcharr](vod-and-dispatcharr.md) · [Apps and remote access](apps-and-remote.md) · [Troubleshooting](troubleshooting.md)
