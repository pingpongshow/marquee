package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/config"
	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/playback"
	"marquee/internal/plex"
	"marquee/internal/probe"
	"marquee/internal/scanner"
	"marquee/internal/settings"
)

// devScan scans a folder into a throwaway database and prints a summary. It never touches
// the server's real database. Usage:
//
//	marquee dev-scan -type movies -path /media/video/Movies -db /tmp/scan-test.db
func devScan(args []string) int {
	fs := flag.NewFlagSet("dev-scan", flag.ExitOnError)
	typ := fs.String("type", "movies", "library type: movies, shows, anime, music, videos")
	path := fs.String("path", "", "folder to scan")
	dbPath := fs.String("db", filepath.Join(os.TempDir(), "marquee-devscan.db"), "throwaway database path")
	ffprobe := fs.String("ffprobe", "/usr/lib/jellyfin-ffmpeg/ffprobe", "ffprobe binary")
	workers := fs.Int("workers", 8, "concurrent probes")
	fs.Parse(args)
	if *path == "" {
		fmt.Fprintln(os.Stderr, "-path is required")
		return 2
	}
	ctx := context.Background()
	database, err := db.Open(ctx, *dbPath, filepath.Dir(*dbPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	libs := library.NewStore(database)
	lib, err := libs.Create(ctx, "dev-scan "+*typ, library.Type(*typ), []string{*path}, library.Options{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	sc := &scanner.Scanner{DB: database, Prober: probe.New(*ffprobe, *workers), Workers: *workers}
	last := time.Now()
	st, err := sc.Scan(ctx, lib, settings.Defaults().Library.IgnorePatterns, func(p scanner.Progress) {
		if time.Since(last) > 5*time.Second {
			fmt.Printf("  %s %d/%d\n", p.Phase, p.Done, p.Total)
			last = time.Now()
		}
	})
	fmt.Println(st)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	rows, _ := database.Query(`SELECT type, COUNT(*), SUM(available = 0) FROM items WHERE library_id = ? GROUP BY type`, lib.ID)
	for rows.Next() {
		var t string
		var n, unavailable int
		rows.Scan(&t, &n, &unavailable)
		fmt.Printf("  %-8s %6d (unavailable %d)\n", t, n, unavailable)
	}
	rows.Close()
	return 0
}

// devPlexPreview prints what a Plex import would do, without changing anything.
//
//	marquee plex-preview
func devPlexPreview(args []string) int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	ctx := context.Background()
	database, err := db.Open(ctx, cfg.DBPath(), cfg.BackupDir())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer database.Close()
	work := filepath.Join(os.TempDir(), "plex-preview")
	defer os.RemoveAll(work)
	im := &plex.Importer{DB: database, Auth: auth.NewService(database), Libraries: library.NewStore(database),
		PlexRoot: cfg.PlexDir, WorkDir: work}
	pv, err := im.Preview(ctx, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	for _, s := range pv.Sections {
		fmt.Printf("section %-10s %5d/%5d files → library %d\n", s.Name, s.MatchedFiles, s.Files, s.LibraryID)
	}
	for _, a := range pv.Accounts {
		fmt.Printf("account %-16s → %s %d %q\n", a.Name, a.SuggestedAction, a.SuggestedUserID, a.SuggestedUsername)
	}
	found, missing := im.CheckArtwork(ctx)
	fmt.Printf("chosen artwork: %d found, %d missing\n", found, missing)
	return 0
}

// devTranscode runs the real transcode pipeline on a file and times the first segments.
//
//	marquee dev-transcode -path /media/... [-encoder nvenc|qsv|software] [-height 720] [-kbps 4000] [-sub 3] [-start 10]
func devTranscode(args []string) int {
	fs := flag.NewFlagSet("dev-transcode", flag.ExitOnError)
	path := fs.String("path", "", "media file")
	encoder := fs.String("encoder", "nvenc", "nvenc, qsv or software")
	kbps := fs.Int("kbps", 0, "bitrate limit (0 = none)")
	sub := fs.Int("sub", -1, "subtitle stream index to burn in")
	start := fs.Int("start", 0, "start segment")
	copyVideo := fs.Bool("copy", false, "direct stream (copy video)")
	fs.Parse(args)
	cfg, _ := config.Load()
	ctx := context.Background()
	res, err := probe.New(cfg.FFprobePath, 1).Probe(ctx, *path)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	m := playback.Media{Container: res.Container, BitrateKbps: res.BitrateKbps, DurationMS: res.DurationMS}
	vi, ai := -1, -1
	if v := res.Video(); v != nil {
		m.Video = &playback.VideoStream{Index: v.Index, Codec: v.Codec, Tag: v.Tag, Width: v.Width, Height: v.Height, BitDepth: v.BitDepth, HDR: v.HDRFormat}
		vi = v.Index
	}
	if a := res.Audio(); a != nil {
		m.Audio = &playback.AudioStream{Index: a.Index, Codec: a.Codec, Channels: a.Channels}
		ai = a.Index
	}
	image := false
	for _, s := range res.Streams {
		if s.Index == *sub {
			m.Subtitle = &playback.SubtitleStream{Index: s.Index, Codec: s.Codec}
			image = m.Subtitle.IsImage()
		}
	}
	browser := playback.DeviceProfile{Containers: []string{"mp4", "mkv"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, MaxAudioChannels: 2,
		HLS: true, HLSVideoCodecs: []string{"h264"}, HLSAudioCodecs: []string{"aac"}, TextSubtitles: !image}
	if *copyVideo && m.Video != nil {
		browser.HLSVideoCodecs = append(browser.HLSVideoCodecs, m.Video.Codec)
		browser.VideoCodecs = append(browser.VideoCodecs, m.Video.Codec)
		browser.TenBit = true
	}
	d := playback.Decide(m, browser, playback.Limits{MaxKbps: *kbps})
	fmt.Printf("decision: %s %v\n", d, d.Reasons)
	dir, _ := os.MkdirTemp(cfg.TranscodeDir, "dev-")
	tr := &playback.Transcoder{FFmpeg: cfg.FFmpegPath, TotalSegments: playback.SegmentCount(m.DurationMS), ThrottleAhead: 30,
		Encoders: []string{*encoder, "software"},
		Job: playback.Job{Input: *path, Decision: d, VideoIndex: vi, VideoCodec: m.Video.Codec, AudioIndex: ai, SubIndex: *sub, SubImage: image, SubRelIndex: 0,
			Dir: dir, Preset: "balanced", QSVDevice: "/dev/dri/renderD128"}}
	defer tr.Stop()
	fmt.Println("ffmpeg", strings.Join(func() []string { j := tr.Job; j.Encoder = *encoder; j.StartSegment = *start; return j.Args() }(), " "))
	t0 := time.Now()
	for k := *start; k < *start+3; k++ {
		p, err := tr.Segment(ctx, k)
		if err != nil {
			fmt.Fprintln(os.Stderr, "segment", k, err)
			return 1
		}
		st, _ := os.Stat(p)
		fmt.Printf("segment %d: %d KB after %s (encoder %s)\n", k, st.Size()/1024, time.Since(t0).Round(time.Millisecond), tr.Encoder())
	}
	return 0
}
