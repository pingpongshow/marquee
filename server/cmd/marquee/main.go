// Command marquee runs the Marquee media server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/config"
	"marquee/internal/db"
	"marquee/internal/images"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/logbuf"
	"marquee/internal/metadata"
	"marquee/internal/netclass"
	"marquee/internal/playback"
	"marquee/internal/plex"
	"marquee/internal/probe"
	"marquee/internal/scanner"
	"marquee/internal/server"
	"marquee/internal/settings"
	"marquee/internal/tasks"
	"marquee/internal/watcher"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logs := logbuf.New(5000)
	slog.SetDefault(slog.New(logbuf.NewHandler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: cfg.LogLevel}), logs)))
	slog.Info("starting Marquee", "version", config.Version, "config", cfg.ConfigDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

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
	scans.AfterScan = func(ctx context.Context, lib library.Library, report func(scanner.Progress)) error {
		lang := ""
		if lib.Options.Language != nil {
			lang = *lib.Options.Language
		}
		if lib.Type == library.Music {
			return meta.MusicArtwork(ctx, lib.ID, func(p metadata.Progress) {
				report(scanner.Progress{Phase: "metadata", Done: p.Done, Total: p.Total})
			})
		}
		if err := meta.MatchLibrary(ctx, lib.ID, string(lib.Type), lang, func(p metadata.Progress) {
			report(scanner.Progress{Phase: "metadata", Done: p.Done, Total: p.Total})
		}); err != nil {
			return err
		}
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
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refreshAllRatings(ctx, meta, libraries)
			}
		}
	}()

	handler := server.New(server.Deps{
		Handlers: &api.Handlers{
			DB: database, Auth: authSvc, Settings: store, Libraries: libraries,
			Items: items.NewStore(database), Scans: scans, Version: config.Version,
			Images:   images.New(database, filepath.Join(cfg.ConfigDir, "cache", "images"), cfg.FFmpegPath),
			Logs:     logs,
			Metadata: meta,
			Playback: player,
			Plex: &plex.Importer{DB: database, Auth: authSvc, Libraries: libraries, Matcher: meta,
				PlexRoot: cfg.PlexDir, WorkDir: filepath.Join(cfg.ConfigDir, "plex-import"),
				ArtDir: filepath.Join(cfg.ConfigDir, "cache", "plex-artwork")},
			LibrariesChanged: func() { watch.Sync(ctx, store.Get().Library.WatchFilesystem) },
		},
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
