// Command marquee runs the Marquee media server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"marquee/internal/ai"
	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/avatars"
	"marquee/internal/bazarr"
	"marquee/internal/config"
	"marquee/internal/db"
	"marquee/internal/discovery"
	"marquee/internal/downloads"
	"marquee/internal/images"
	"marquee/internal/introdetect"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/livetv"
	"marquee/internal/logbuf"
	"marquee/internal/loudness"
	"marquee/internal/lyrics"
	"marquee/internal/mediatrash"
	"marquee/internal/metadata"
	"marquee/internal/muse"
	"marquee/internal/netclass"
	"marquee/internal/notify"
	"marquee/internal/playback"
	"marquee/internal/plex"
	"marquee/internal/probe"
	"marquee/internal/remote"
	"marquee/internal/requests"
	"marquee/internal/scanner"
	"marquee/internal/scrobble"
	"marquee/internal/server"
	"marquee/internal/settings"
	"marquee/internal/soundprint"
	"marquee/internal/subtitles"
	"marquee/internal/syncplay"
	"marquee/internal/tasks"
	"marquee/internal/trickplay"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
	"marquee/internal/watcher"
	"marquee/internal/webhooks"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "dev-scan":
			os.Exit(devScan(os.Args[2:]))
		case "dev-transcode":
			os.Exit(devTranscode(os.Args[2:]))
		case "plex-preview":
			os.Exit(devPlexPreview(os.Args[2:]))
		}
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// healthcheck is used by the Docker HEALTHCHECK so the image needs no curl.
func healthcheck() int {
	port := os.Getenv("MARQUEE_PORT")
	if port == "" {
		port = "32500"
	}
	c := http.Client{Timeout: 5 * time.Second}
	resp, err := c.Get("http://127.0.0.1:" + port + "/api/v1/system/health")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func run() error {
	startedAt := time.Now()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logs := logbuf.New(5000)
	// Logs go to stderr (docker logs), the admin log viewer and config/logs, which outlives
	// container updates (10 MB × 5).
	logFile := &logbuf.RotatingFile{Dir: filepath.Join(cfg.ConfigDir, "logs"), MaxBytes: 10 << 20, Keep: 5}
	slog.SetDefault(slog.New(logbuf.NewHandler(slog.NewTextHandler(io.MultiWriter(os.Stderr, logFile), &slog.HandlerOptions{Level: cfg.LogLevel}), logs)))
	slog.Info("starting Marquee", "version", config.Version, "config", cfg.ConfigDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.ApplyPendingRestore(cfg.DBPath(), cfg.ConfigDir, cfg.BackupDir()); err != nil {
		return fmt.Errorf("restore backup: %w", err)
	}
	database, err := db.Open(ctx, cfg.DBPath(), cfg.BackupDir())
	if err != nil {
		return err
	}
	defer database.Close()

	store, err := settings.Open(ctx, database)
	if err != nil {
		return err
	}
	cur := store.Get()
	classifier := netclass.New(cur.Network.LANSubnets, cur.RemoteAccess.RemoteURL)
	store.Subscribe(func(s settings.Settings) {
		classifier.Configure(s.Network.LANSubnets, s.RemoteAccess.RemoteURL)
	})

	authSvc := auth.NewService(database)
	player := &playback.Manager{DB: database, Settings: store, FFmpeg: cfg.FFmpegPath, TranscodeDir: cfg.TranscodeDir,
		Encoders: playback.DetectEncoders(ctx, cfg.FFmpegPath)}
	player.RemoteFFprobe = cfg.FFprobePath // movies on demand from Xtream Codes servers
	go player.Run(ctx)
	// Webhooks (ADM-5).
	hooks := &webhooks.Dispatcher{DB: database, Settings: store, Server: func() webhooks.Server {
		return webhooks.Server{ID: store.ServerID(), Name: store.Get().General.ServerName, Version: config.Version}
	}}
	go hooks.Run(ctx)
	// Email and admin alerts.
	mailer := &notify.Mailer{Settings: store}
	alerts := &notify.Alerts{DB: database, Settings: store, Mail: mailer, Webhooks: hooks}
	scrobbler := &scrobble.Service{DB: database, LastFM: func() (string, string) {
		i := store.Get().Integrations
		return i.LastFMAPIKey, i.LastFMSecret
	}}
	player.Events = func(kind string, s *playback.Session, pos int64) {
		if s.ItemType == "track" && (kind == "playback.started" || kind == "playback.watched") {
			go scrobbler.Played(ctx, s.UserID, s.ItemID, kind == "playback.started", s.StartedAt)
		}
		go func() {
			hooks.Publish(webhooks.Event{Event: kind, User: &webhooks.User{ID: s.UserID, Name: s.UserName},
				Device:   &webhooks.Device{Name: s.DeviceName, Platform: hooks.DevicePlatform(ctx, s.DeviceID)},
				Item:     hooks.LookupItem(ctx, s.ItemID),
				Playback: &webhooks.Playback{SessionID: s.ID, PositionMS: pos, Method: string(s.Decision.Method), Remote: s.Remote}})
		}()
	}
	var announceMu sync.Mutex
	announced := map[int64]time.Time{} // per library: titles added after this have been announced
	announce := func(ctx context.Context, libID int64) {
		announceMu.Lock()
		since, ok := announced[libID]
		if !ok {
			since = startedAt
		}
		announced[libID] = time.Now()
		announceMu.Unlock()
		hooks.AnnounceAdded(ctx, libID, since)
	}
	libraries := library.NewStore(database)
	scans := &tasks.Scans{
		DB:        database,
		Libraries: libraries,
		Settings:  store,
		Scanner: &scanner.Scanner{
			DB:      database,
			Prober:  probe.New(cfg.FFprobePath, 8),
			Workers: 8,
		},
	}
	meta := &metadata.Service{DB: database, Settings: store, CacheDir: filepath.Join(cfg.ConfigDir, "cache")}
	var afterMusicScan atomic.Pointer[func()] // set once the scheduler exists
	var afterVideoScan atomic.Pointer[func()]
	scans.AfterScan = func(ctx context.Context, lib library.Library, report func(scanner.Progress)) error {
		lang := ""
		if lib.Options.Language != nil {
			lang = *lib.Options.Language
		}
		if lib.Type == library.Music {
			err := meta.MusicArtwork(ctx, lib.ID, func(p metadata.Progress) {
				report(scanner.Progress{Phase: "metadata", Done: p.Done, Total: p.Total})
			})
			if f := afterMusicScan.Load(); f != nil {
				(*f)() // analyse new tracks now rather than at the next hourly run
			}
			announce(ctx, lib.ID)
			return err
		}
		if err := meta.MatchLibrary(ctx, lib.ID, string(lib.Type), lang, func(p metadata.Progress) {
			report(scanner.Progress{Phase: "metadata", Done: p.Done, Total: p.Total})
		}); err != nil {
			return err
		}
		announce(ctx, lib.ID) // after matching, so payloads carry proper titles
		if lib.Type == library.Anime {
			go meta.EnrichAnime(ctx) // AniList titles and details for new shows (META-2)
		}
		err := meta.RefreshRatings(ctx, lib.ID)
		if f := afterVideoScan.Load(); f != nil {
			(*f)() // index new movies and shows for Muse and recommendations
		}
		return err
	}
	// Adding a TMDB key starts matching everything that was scanned without one.
	store.Subscribe(func(s settings.Settings) {
		if s.Metadata.TMDBAPIKey != cur.Metadata.TMDBAPIKey && s.Metadata.TMDBAPIKey != "" {
			scans.QueueAll(ctx)
		}
		if s.Metadata.OMDbAPIKey != cur.Metadata.OMDbAPIKey && s.Metadata.OMDbAPIKey != "" {
			go refreshAllRatings(ctx, meta, libraries)
		}
		cur = s
	})
	go scans.Run(ctx)
	watch := &watcher.Watcher{Libraries: libraries, Queue: scans.Queue}
	watch.Sync(ctx, cur.Library.WatchFilesystem)
	watching := cur.Library.WatchFilesystem
	store.Subscribe(func(s settings.Settings) {
		if s.Library.WatchFilesystem != watching {
			watching = s.Library.WatchFilesystem
			watch.Sync(ctx, watching)
		}
	})
	// Libraries with items still to match (e.g. a restart interrupted matching, or a matcher
	// improvement in an upgrade) get a scan, which is cheap for unchanged files and shows
	// progress in the activity indicator.
	go func() {
		meta.ResetFailedIfMatcherChanged(ctx)
		list, err := libraries.List(ctx)
		if err != nil {
			return
		}
		for _, l := range list {
			if meta.NeedsMatching(ctx, l.ID) {
				scans.Queue(l.ID)
			}
		}
	}()
	backups := &tasks.Backups{DB: database, Dir: cfg.BackupDir(), ConfigDir: cfg.ConfigDir,
		Retention: func() int { return store.Get().Tasks.BackupRetention }}
	scheduler := &tasks.Scheduler{DB: database, Settings: store}
	scheduler.Register(backups.Task())
	scheduler.Register(tasks.Task{ID: "optimize", Name: "Optimize database", Window: true,
		Description: "Updates query statistics and compacts the write-ahead log.",
		Run: func(ctx context.Context) (string, error) {
			if _, err := database.ExecContext(ctx, `PRAGMA optimize`); err != nil {
				return "", err
			}
			_, err := database.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`)
			return "Done", err
		}})
	mediaTrash := &mediatrash.Service{DB: database}
	scheduler.Register(tasks.Task{ID: "empty-trash", Name: "Empty media trash", Window: true,
		Description: "Removes files deleted from Library Health more than 30 days ago from the .marquee-trash folders in library folders.",
		Run:         mediaTrash.Empty})
	scheduler.Register(tasks.Task{ID: "scan-all", Name: "Scan all libraries",
		Description: "Looks for new, changed and removed files in every library now. Libraries also rescan on their own schedule, and the file watcher catches most changes as they happen.",
		Run: func(ctx context.Context) (string, error) {
			scans.QueueAll(ctx)
			return "Queued", nil
		}})
	scheduler.Register(tasks.Task{ID: "ratings", Name: "Refresh ratings", Every: 6 * time.Hour,
		Description: "Fetches IMDb, Rotten Tomatoes and Metacritic ratings from OMDb within the daily request budget.",
		Run: func(ctx context.Context) (string, error) {
			refreshAllRatings(ctx, meta, libraries)
			return "Done", nil
		}})
	scheduler.Register(tasks.Task{ID: "refresh-metadata", Name: "Refresh metadata", Window: true,
		Description: "Updates shows with new or recently aired episodes (titles and summaries TMDB fills in after airing) and movies not refreshed for six months. Edited fields stay as they are.",
		Run:         meta.RefreshStale})
	metadata.MBUserAgent = "Marquee/" + config.Version + " (self-hosted media server)"
	scheduler.Register(tasks.Task{ID: "musicbrainz", Name: "Artist and album details", Every: 24 * time.Hour,
		Description: "With artist and album details on, looks up artists and albums on MusicBrainz, one request a second: genres and styles, solo artist or band, country, active years and a Wikipedia bio for artists; album types, original release dates and labels for albums. The first run covers the library (a few hours for a large one; it continues where it stopped), later runs new artists and albums.",
		Run:         meta.EnrichMusic, Progress: meta.MusicDetailsProgress})
	scheduler.Register(tasks.Task{ID: "video-details", Name: "Fetch movie and show details", Every: 24 * time.Hour,
		Description: "Fetches keywords, countries, original language and TMDB's recommendations for movies and shows matched before Marquee stored them, a few a second (new matches get them straight away).",
		Run: func(ctx context.Context) (string, error) {
			msg, err := meta.FetchVideoDetails(ctx)
			if err == nil && strings.HasPrefix(msg, "Fetched") {
				scheduler.RunNow(ctx, "embeddings") // descriptions now carry keywords and countries
			}
			return msg, err
		}, Progress: meta.VideoDetailsProgress})
	scheduler.Register(tasks.Task{ID: "anilist", Name: "Enrich anime from AniList", Window: true, Bounded: true,
		Description: "Adds the names anime is known by (romaji, English, Japanese — all searchable), studios, genres and AniList scores to shows and films in anime libraries.",
		Run:         meta.EnrichAnime})
	scheduler.Register(tasks.Task{ID: "collections", Name: "Update collections", Every: 7 * 24 * time.Hour,
		Description: "Groups movies into their film series (TMDB collections), including movies matched before collections existed.",
		Run:         meta.SyncCollections})
	// Music intelligence (M6.5): the Soundprint sidecar embeds tracks; the index serves radios etc.
	soundprintSvc := &soundprint.Service{DB: database, Client: &soundprint.Client{BaseURL: envOr("MARQUEE_SOUNDPRINT_URL", envOr("MARQUEE_SONIC_URL", "http://127.0.0.1:32501"))},
		Index: soundprint.NewIndex(), Enabled: func() bool { return store.Get().Music.SoundprintAnalysis },
		Learn: func() bool { return store.Get().AI.LearnFromBehaviour }}
	if err := soundprintSvc.Index.Load(ctx, database); err != nil {
		slog.Warn("soundprint index", "err", err)
	}
	scheduler.Register(tasks.Task{ID: "soundprint", Name: "Soundprint analysis", Every: time.Hour,
		Description: "Listens to new tracks on the GPU so radios, similar music, mixes and Muse include them.",
		Run:         soundprintSvc.Analyze})
	runSoundprint := func() { scheduler.RunNow(ctx, "soundprint") }
	afterMusicScan.Store(&runSoundprint)
	// Muse for movies and recommendations (USER-15, USER-16): text embeddings of movies and shows.
	// Optional AI (a local Ollama): smarter Muse, re-ranking, reasons and lyric themes. With
	// it on, movies and shows are embedded by Ollama's model instead of the sidecar's.
	aiClient := &ai.Client{Config: func() ai.Config {
		a := store.Get().AI
		return ai.Config{Enabled: a.Enabled, URL: a.URL, ChatModel: a.ChatModel, EmbedModel: a.EmbedModel, UnloadMinutes: a.UnloadMinutes}
	}}
	embedder := &items.Embedder{DB: database, Index: items.NewVideoIndex(), Client: &ai.SwitchEmbedder{AI: aiClient, Fallback: soundprintSvc.Client},
		Pace: 200 * time.Millisecond}
	if err := embedder.Index.Load(ctx, database); err != nil {
		slog.Warn("video embeddings", "err", err)
	}
	scheduler.Register(tasks.Task{ID: "embeddings", Name: "Index movies and shows", Window: true, Bounded: true,
		Description: "Reads the descriptions of new and changed movies and shows so Muse, recommendations and related items include them.",
		Run:         embedder.Run, Progress: embedder.Progress})
	runEmbed := func() { scheduler.RunNow(ctx, "embeddings") }
	afterVideoScan.Store(&runEmbed)
	loud := &loudness.Service{DB: database, FFmpeg: cfg.FFmpegPath, Workers: 4, Analyse: func() bool { return true }} // volume levelling is always on, so every track is measured
	scheduler.Register(tasks.Task{ID: "loudness", Name: "Measure music loudness", Window: true,
		Description: "Reads ReplayGain tags and measures loudness of tracks without them, so volume levelling works for everything.",
		Run:         loud.Run})
	trick := &trickplay.Service{DB: database, FFmpeg: cfg.FFmpegPath, Dir: filepath.Join(cfg.ConfigDir, "cache", "trickplay"), Workers: 2,
		Enabled: func() bool { return store.Get().Library.Trickplay }, CUDA: player.Encoders.NVENC}
	player.IFrames = func(fileID, durationMS int64) (string, []byte, bool) {
		p, ok := trick.IFrames(fileID)
		if !ok {
			return "", nil, false
		}
		pl, err := trickplay.IFramePlaylist(p, durationMS)
		return p, pl, err == nil
	}
	scheduler.Register(tasks.Task{ID: "trickplay", Name: "Make seek previews", Window: true, Bounded: true,
		Description: "Makes the thumbnails shown while seeking through videos. A large library takes a few nights; it continues where it stopped.",
		Run:         trick.Run, Progress: trick.Progress})
	intros := &introdetect.Service{DB: database, FFmpeg: cfg.FFmpegPath, Dir: filepath.Join(cfg.ConfigDir, "cache", "fingerprints"),
		Enabled: func() bool { return store.Get().Library.DetectIntros }, CUDA: player.Encoders.NVENC}
	scheduler.Register(tasks.Task{ID: "intros", Name: "Find intros and credits", Window: true, Bounded: true,
		Description: "Compares the audio of episodes in each season to find intros and end credits for Skip Intro and Skip Credits.",
		Run:         intros.Run, Progress: intros.Progress})
	subs := &subtitles.Service{DB: database, Dir: filepath.Join(cfg.ConfigDir, "subtitles"), Config: func() subtitles.Config {
		m := store.Get().Metadata
		return subtitles.Config{APIKey: m.OpenSubtitlesAPIKey, Username: m.OpenSubtitlesUser, Password: m.OpenSubtitlesPass,
			UserAgent: "Marquee v" + config.Version, Base: os.Getenv("MARQUEE_OPENSUBTITLES_URL")}
	}}
	// Offline downloads (M7): conversions for phones and tablets.
	dl := &downloads.Service{DB: database, Playback: player, Dir: filepath.Join(cfg.TranscodeDir, "downloads"),
		Request: func(ctx context.Context, userID, itemID, fileID int64) (playback.Request, error) {
			u, err := authSvc.GetUser(ctx, userID)
			if err != nil {
				return playback.Request{}, err
			}
			return playback.Request{UserID: userID, UserName: u.DisplayName, ItemID: itemID, FileID: fileID,
				UserAudioLang: u.Preferences.AudioLanguage, UserSubLang: u.Preferences.SubtitleLanguage, UserSubMode: u.Preferences.SubtitleMode}, nil
		}}
	go dl.Run(ctx)

	// Live TV (LIVE-1..4): channels and guide refreshed every few hours and when sources change.
	livetv.LogoDir = filepath.Join(cfg.ConfigDir, "cache", "livetv-logos")
	live := &livetv.Service{DB: database, Settings: store, Sessions: &livetv.Sessions{
		FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath, Dir: filepath.Join(cfg.TranscodeDir, "live"),
		Encoders: player.EncoderChoices, QSVDevice: player.Encoders.QSVDevice,
	}}
	liveWake := make(chan struct{}, 1)
	lastSources := ""
	store.Subscribe(func(s settings.Settings) {
		raw, _ := json.Marshal(s.Integrations.LiveTVSources)
		if string(raw) != lastSources {
			lastSources = string(raw)
			select {
			case liveWake <- struct{}{}:
			default:
			}
		}
	})
	raw, _ := json.Marshal(store.Get().Integrations.LiveTVSources)
	lastSources = string(raw)

	// Movies on demand (LIVE-6): film lists refreshed every few hours and when sources change;
	// TMDB matching uses the library's matcher.
	vodSvc := &vod.Service{DB: database, Settings: store, Matcher: meta, Streams: &vod.Streams{},
		ImageDir: filepath.Join(cfg.ConfigDir, "cache", "vod-images")}
	// Films that are files (Xtream Codes) play through the player's own HLS sessions.
	vodSvc.Streams.OnEnd = func(st *vod.Stream) {
		if st.PlaybackID != "" {
			player.Stop(context.Background(), st.PlaybackID)
		}
	}
	vodSvc.Streams.Active = func(st *vod.Stream) bool { return player.Exists(st.PlaybackID) }
	vodWake := make(chan struct{}, 1)
	vodConfig := func(s settings.Settings) string {
		raw, _ := json.Marshal([]any{s.Integrations.VODEnabled, s.Integrations.VODSources, s.Metadata.TMDBAPIKey != ""})
		return string(raw)
	}
	lastVOD := vodConfig(store.Get())
	store.Subscribe(func(s settings.Settings) {
		if c := vodConfig(s); c != lastVOD {
			lastVOD = c
			select {
			case vodWake <- struct{}{}:
			default:
			}
		}
	})
	go vodSvc.Streams.Run(ctx)

	// Saving films and episodes on demand into the library (admins only): a queue per
	// source that pauses while someone watches.
	vodSaves := &vodsave.Service{DB: database, Settings: store, VOD: vodSvc, Titles: meta,
		FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath, Encoders: player.EncoderChoices, QSVDevice: player.Encoders.QSVDevice,
		Libraries: libraries.List,
		Scan: func(id int64) {
			go func() {
				// A scan already running may have missed the new file: queue one after it.
				for scans.Status(id).State == tasks.Scanning {
					select {
					case <-ctx.Done():
						return
					case <-time.After(10 * time.Second):
					}
				}
				scans.Queue(id)
			}()
		},
		Activity: func(ctx context.Context) []vodsave.Playing {
			var out []vodsave.Playing
			for _, src := range vodSvc.WatchingSources(ctx, 2*time.Minute) {
				out = append(out, vodsave.Playing{Kind: "vod", SourceID: src, Host: vodSvc.SourceHost(src)})
			}
			for _, ls := range live.Sessions.List() {
				host := ""
				if ch, err := live.Channel(ctx, ls.ChannelID); err == nil {
					host = vodsave.HostOf(ch.StreamURL)
				}
				out = append(out, vodsave.Playing{Kind: "live", Host: host})
			}
			for _, ps := range player.List() {
				if !ps.External && !ps.Preloading() {
					out = append(out, vodsave.Playing{Kind: "library"})
				}
			}
			return out
		},
	}

	// DVR (LIVE-5): recordings land in "Recorded TV" / "Recorded Movies", made on first use.
	minutes := func(n int) time.Duration { return time.Duration(n) * time.Minute }
	dvr := &livetv.Recorder{Live: live, FFmpeg: cfg.FFmpegPath, Dir: cfg.RecordingsDir,
		Before: func() time.Duration { return minutes(store.Get().Integrations.DVRPaddingBefore) },
		After:  func() time.Duration { return minutes(store.Get().Integrations.DVRPaddingAfter) },
		Library: func(ctx context.Context, movies bool, dir string) (int64, error) {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return 0, err
			}
			libs, err := libraries.List(ctx)
			if err != nil {
				return 0, err
			}
			for _, l := range libs {
				if slices.Contains(l.Paths, dir) {
					return l.ID, nil
				}
			}
			name, t := "Recorded TV", library.Shows
			if movies {
				name, t = "Recorded Movies", library.Movies
			}
			l, err := libraries.Create(ctx, name, t, []string{dir}, library.Options{})
			if err != nil {
				return 0, err
			}
			slog.Info("dvr: created library", "name", name, "path", dir)
			return l.ID, nil
		},
		Scan: scans.Queue,
	}

	watchTogether := &syncplay.Service{}
	go watchTogether.Run(ctx)
	remoteHub := &remote.Hub{}
	go remoteHub.Run(ctx)
	lyricsSvc := &lyrics.Service{DB: database, Online: func() bool { return store.Get().Music.OnlineLyrics }}
	lyricThemes := &muse.LyricThemes{DB: database, AI: aiClient, Local: lyricsSvc.CacheLocal, Pace: 500 * time.Millisecond,
		Enabled: func() bool { a := store.Get().AI; return a.On(a.LyricsThemes) }}
	if err := lyricThemes.Load(ctx); err != nil {
		slog.Warn("lyric themes", "err", err)
	}
	// Lyrics for the whole library from LRCLIB, paced; new lyrics are then indexed for themes.
	lyricsSvc.Fetched = func() { scheduler.RunNow(ctx, "lyric-themes") }
	scheduler.Register(tasks.Task{ID: "lyrics-fetch", Name: "Fetch lyrics", Every: 24 * time.Hour,
		Description: "With online lyrics on, looks up lyrics on LRCLIB for tracks that haven't been looked up yet, about one a second (the first run covers the library, later runs new tracks).",
		Run:         lyricsSvc.FetchAll, Progress: lyricsSvc.FetchProgress})
	scheduler.Register(tasks.Task{ID: "lyric-themes", Name: "Index lyric themes", Every: 6 * time.Hour,
		Description: "With AI on, reads the lyrics of tracks (from their tags and .lrc files) with the embedding model, so Muse can find songs about a theme.",
		Run:         lyricThemes.Run, Progress: lyricThemes.Progress})
	// Artist and album descriptions for descriptive music prompts (AI only), refreshed after
	// artist and album details are looked up.
	musicDocs := &muse.MusicDocs{DB: database, AI: aiClient, Pace: 500 * time.Millisecond, Enabled: func() bool { return store.Get().AI.Enabled }}
	if err := musicDocs.Load(ctx); err != nil {
		slog.Warn("artist and album index", "err", err)
	}
	scheduler.Register(tasks.Task{ID: "music-docs", Name: "Index artists and albums", Every: 24 * time.Hour,
		Description: "With AI on, reads the descriptions of artists and albums (origin, years, styles, bio, label) with the embedding model, so Muse can find music by what the artists are like.",
		Run:         musicDocs.Run, Progress: musicDocs.Progress})
	meta.AfterMusicDetails = func() { scheduler.RunNow(ctx, "music-docs") }
	// Turning artist and album details on looks them up straight away.
	lastDetails := store.Get().Music.ArtistDetails
	store.Subscribe(func(s settings.Settings) {
		if s.Music.ArtistDetails != lastDetails {
			lastDetails = s.Music.ArtistDetails
			if lastDetails {
				scheduler.RunNow(ctx, "musicbrainz")
			}
		}
	})
	// Turning AI on or off, or changing the embedding model, re-indexes in the background.
	aiIndexKey := func(s settings.Settings) string {
		return fmt.Sprint(s.AI.Enabled, s.AI.EmbedModel, s.AI.LyricsThemes)
	}
	lastAIKey := aiIndexKey(store.Get())
	store.Subscribe(func(s settings.Settings) {
		if k := aiIndexKey(s); k != lastAIKey {
			lastAIKey = k
			scheduler.RunNow(ctx, "embeddings")
			scheduler.RunNow(ctx, "lyric-themes")
			scheduler.RunNow(ctx, "music-docs")
		}
	})
	// Alerts are wired before the services that raise them start.
	alertWatcher := wireAlerts(ctx, alertDeps{cfg: &cfg, store: store, alerts: alerts, auth: authSvc, scheduler: scheduler,
		live: live, dvr: dvr, vod: vodSvc, saves: vodSaves, player: player, libraries: libraries, ai: aiClient, soundprint: soundprintSvc})
	go alertWatcher.Run(ctx)
	go live.Run(ctx, liveWake)
	go vodSvc.Run(ctx, vodWake)
	go vodSaves.Run(ctx)
	go dvr.Run(ctx)
	go scheduler.Run(ctx)

	// Bonjour, so LAN apps find the server (D49).
	port, _ := strconv.Atoi(strings.TrimPrefix(cfg.ListenAddr, ":"))
	bonjour := &discovery.Advertiser{}
	advertise := func(s settings.Settings) {
		bonjour.Apply(s.Network.BonjourEnabled, discovery.Info{ServerID: store.ServerID(), Name: s.General.ServerName, Version: config.Version, Port: port}, s.Network.LANSubnets)
	}
	advertise(cur)
	store.Subscribe(advertise)
	defer bonjour.Stop()

	itemStore := items.NewStore(database)
	itemStore.Learn = func() bool { return store.Get().AI.LearnFromBehaviour }
	apiHandlers := &api.Handlers{
		DB: database, Auth: authSvc, Settings: store, Libraries: libraries,
		Items: itemStore, Scans: scans, Version: config.Version,
		Tasks: scheduler, Trickplay: trick, Webhooks: hooks, Subtitles: subs, Downloads: dl, Backups: backups, Restart: stop, Soundprint: soundprintSvc, Lyrics: lyricsSvc,
		Requests:    &requests.Service{DB: database, Settings: store},
		LiveTV:      live,
		VOD:         vodSvc,
		VODSaves:    vodSaves,
		Scrobble:    scrobbler,
		DVR:         dvr,
		SyncPlay:    watchTogether,
		Remote:      remoteHub,
		Embeddings:  embedder,
		AI:          aiClient,
		LyricThemes: lyricThemes,
		MusicDocs:   musicDocs,
		Reasons:     &muse.Reasons{},
		Trash:       mediaTrash,
		Mail:        mailer,
		Alerts:      alerts,
		Avatars:     &avatars.Store{DB: database, Dir: filepath.Join(cfg.ConfigDir, "avatars")},
		Images:      images.New(database, filepath.Join(cfg.ConfigDir, "cache", "images"), cfg.FFmpegPath),
		Logs:        logs,
		Metadata:    meta,
		Playback:    player,
		Plex: &plex.Importer{DB: database, Auth: authSvc, Libraries: libraries, Matcher: meta,
			PlexRoot: cfg.PlexDir, WorkDir: filepath.Join(cfg.ConfigDir, "plex-import"),
			ArtDir: filepath.Join(cfg.ConfigDir, "cache", "plex-artwork")},
		LibrariesChanged: func() { watch.Sync(ctx, store.Get().Library.WatchFilesystem) },
		Bazarr: &bazarr.Service{DB: database, Config: func() (string, string) {
			i := store.Get().Integrations
			return i.BazarrURL, i.BazarrAPIKey
		}},
	}
	handler := server.New(server.Deps{
		Handlers:   apiHandlers,
		Downloads:  apiHandlers.DownloadFiles(),
		Live:       live.Sessions.Handler(),
		VOD:        vodSvc.Streams.Handler(),
		Stream:     player.StreamHandler(filepath.Join(cfg.ConfigDir, "cache", "subtitles")),
		Auth:       authSvc,
		Classifier: classifier,
		WebDir:     cfg.WebDir,
	})

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		// No WriteTimeout: media responses are long-lived streams.
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.ListenAddr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("shutdown", "err", err)
		}
	}
	return nil
}

// refreshAllRatings fetches OMDb ratings for every library within today's budget.
func refreshAllRatings(ctx context.Context, meta *metadata.Service, libs *library.Store) {
	list, err := libs.List(ctx)
	if err != nil {
		return
	}
	for _, l := range list {
		if err := meta.RefreshRatings(ctx, l.ID); err != nil {
			slog.Warn("ratings refresh", "library", l.Name, "err", err)
			return
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
