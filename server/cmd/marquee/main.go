// Command marquee runs the Marquee media server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/avatars"
	"marquee/internal/config"
	"marquee/internal/db"
	"marquee/internal/discovery"
	"marquee/internal/downloads"
	"marquee/internal/images"
	"marquee/internal/introdetect"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/logbuf"
	"marquee/internal/loudness"
	"marquee/internal/lyrics"
	"marquee/internal/metadata"
	"marquee/internal/netclass"
	"marquee/internal/playback"
	"marquee/internal/plex"
	"marquee/internal/probe"
	"marquee/internal/scanner"
	"marquee/internal/server"
	"marquee/internal/settings"
	"marquee/internal/sonic"
	"marquee/internal/subtitles"
	"marquee/internal/tasks"
	"marquee/internal/trickplay"
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
	slog.SetDefault(slog.New(logbuf.NewHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}), logs)))
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
	go player.Run(ctx)
	// Webhooks (ADM-5).
	hooks := &webhooks.Dispatcher{DB: database, Settings: store, Server: func() webhooks.Server {
		return webhooks.Server{ID: store.ServerID(), Name: store.Get().General.ServerName, Version: config.Version}
	}}
	go hooks.Run(ctx)
	player.Events = func(kind string, s *playback.Session, pos int64) {
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
	meta := &metadata.Service{DB: database, Settings: store}
	var afterMusicScan atomic.Pointer[func()] // set once the scheduler exists
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
		return meta.RefreshRatings(ctx, lib.ID)
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
	scheduler.Register(tasks.Task{ID: "collections", Name: "Update collections", Every: 7 * 24 * time.Hour,
		Description: "Groups movies into their film series (TMDB collections), including movies matched before collections existed.",
		Run:         meta.SyncCollections})
	// Music intelligence (M6.5): the sonic sidecar embeds tracks; the index serves radios etc.
	sonicSvc := &sonic.Service{DB: database, Client: &sonic.Client{BaseURL: envOr("MARQUEE_SONIC_URL", "http://127.0.0.1:32501")},
		Index: sonic.NewIndex(), Enabled: func() bool { return store.Get().Music.SonicAnalysis }}
	if err := sonicSvc.Index.Load(ctx, database); err != nil {
		slog.Warn("sonic index", "err", err)
	}
	scheduler.Register(tasks.Task{ID: "sonic", Name: "Analyse music", Every: time.Hour,
		Description: "Listens to new tracks on the GPU so radios, similar music, mixes and Sonic Sage include them.",
		Run:         sonicSvc.Analyze})
	runSonic := func() { scheduler.RunNow(ctx, "sonic") }
	afterMusicScan.Store(&runSonic)
	loud := &loudness.Service{DB: database, FFmpeg: cfg.FFmpegPath, Workers: 4, Analyse: func() bool { return store.Get().Music.LoudnessAnalysis }}
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
	lyricsSvc := &lyrics.Service{DB: database, Online: func() bool { return store.Get().Music.OnlineLyrics }}
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

	apiHandlers := &api.Handlers{
		DB: database, Auth: authSvc, Settings: store, Libraries: libraries,
		Items: items.NewStore(database), Scans: scans, Version: config.Version,
		Tasks: scheduler, Trickplay: trick, Webhooks: hooks, Subtitles: subs, Downloads: dl, Backups: backups, Restart: stop, Sonic: sonicSvc, Lyrics: lyricsSvc,
		Avatars:  &avatars.Store{DB: database, Dir: filepath.Join(cfg.ConfigDir, "avatars")},
		Images:   images.New(database, filepath.Join(cfg.ConfigDir, "cache", "images"), cfg.FFmpegPath),
		Logs:     logs,
		Metadata: meta,
		Playback: player,
		Plex: &plex.Importer{DB: database, Auth: authSvc, Libraries: libraries, Matcher: meta,
			PlexRoot: cfg.PlexDir, WorkDir: filepath.Join(cfg.ConfigDir, "plex-import"),
			ArtDir: filepath.Join(cfg.ConfigDir, "cache", "plex-artwork")},
		LibrariesChanged: func() { watch.Sync(ctx, store.Get().Library.WatchFilesystem) },
	}
	handler := server.New(server.Deps{
		Handlers:   apiHandlers,
		Downloads:  apiHandlers.DownloadFiles(),
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
