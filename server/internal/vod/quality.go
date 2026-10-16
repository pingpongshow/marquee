package vod

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Quality is what a film's or episode's file on its source is: from the source's details
// (Xtream Codes servers describe the video in "info.video") or from probing the file.
type Quality struct {
	Width, Height int
	VideoCodec    string // ffprobe's name: h264, hevc, av1…
	HDR           string // "" = SDR
	BitrateKbps   int    // the whole file's; 0 = unknown
	Origin        string // info, probe
	Error         string // the probe failed
	CheckedAt     time.Time
}

// Known reports whether the video's size is known.
func (q Quality) Known() bool { return q.Width > 0 || q.Height > 0 }

// xtreamVideo is the "video" an Xtream Codes server puts in a film's or episode's info:
// ffprobe's stream, as the server probed it.
type xtreamVideo struct {
	Codec         flexString `json:"codec_name"`
	Width         flexString `json:"width"`
	Height        flexString `json:"height"`
	ColorTransfer flexString `json:"color_transfer"`
	SideData      []struct {
		Type flexString `json:"side_data_type"`
	} `json:"side_data_list"`
}

// qualityFromInfo reads the video a server's info describes (ok false when it gives none).
func qualityFromInfo(info []byte) (Quality, bool) {
	var in struct {
		Video   json.RawMessage `json:"video"`
		Bitrate flexString      `json:"bitrate"`
	}
	if b := bytes.TrimSpace(info); len(b) == 0 || b[0] != '{' || json.Unmarshal(b, &in) != nil {
		return Quality{}, false
	}
	var v xtreamVideo
	if b := bytes.TrimSpace(in.Video); len(b) == 0 || b[0] != '{' || json.Unmarshal(b, &v) != nil {
		return Quality{}, false
	}
	q := Quality{Width: int(v.Width.Int()), Height: int(v.Height.Int()), VideoCodec: strings.ToLower(v.Codec.String()),
		BitrateKbps: int(in.Bitrate.Int()), Origin: "info"}
	q.HDR = HDRFormat(v.ColorTransfer.String(), false)
	for _, sd := range v.SideData {
		if strings.Contains(strings.ToLower(sd.Type.String()), "dovi") {
			q.HDR = "DV"
		}
	}
	if q.BitrateKbps < 0 || q.BitrateKbps > 500_000 {
		q.BitrateKbps = 0
	}
	return q, q.Known()
}

// HDRFormat names a video's HDR from its transfer characteristics ("" for SDR).
func HDRFormat(transfer string, dolbyVision bool) string {
	switch {
	case dolbyVision:
		return "DV"
	case transfer == "smpte2084":
		return "HDR10"
	case transfer == "arib-std-b67":
		return "HLG"
	}
	return ""
}

func (s *Service) storeQuality(ctx context.Context, ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, col string, id int64, q Quality) error {
	if _, err := ex.ExecContext(ctx, "DELETE FROM vod_quality WHERE "+col+"=?", id); err != nil {
		return err
	}
	_, err := ex.ExecContext(ctx, `INSERT INTO vod_quality(`+col+`, width, height, video_codec, hdr, bitrate_kbps, origin, error, checked_at)
		VALUES(?,?,?,?,?,?,?,?,?)`, id, q.Width, q.Height, q.VideoCodec, q.HDR, q.BitrateKbps, q.Origin, q.Error, now())
	return err
}

// StoreMovieQuality and StoreEpisodeQuality keep what a file is (from a probe).
func (s *Service) StoreMovieQuality(ctx context.Context, movieID int64, q Quality) error {
	return s.storeQuality(ctx, s.DB, "movie_id", movieID, q)
}

func (s *Service) StoreEpisodeQuality(ctx context.Context, episodeID int64, q Quality) error {
	return s.storeQuality(ctx, s.DB, "episode_id", episodeID, q)
}

const qualityCols = "width, height, video_codec, hdr, bitrate_kbps, origin, error, checked_at"

func scanQuality(sc interface{ Scan(...any) error }, extra ...any) (Quality, error) {
	var q Quality
	var at string
	err := sc.Scan(append(extra, &q.Width, &q.Height, &q.VideoCodec, &q.HDR, &q.BitrateKbps, &q.Origin, &q.Error, &at)...)
	q.CheckedAt, _ = time.Parse(time.RFC3339, at)
	return q, err
}

// MovieQuality is what's known of a film's file (ok false: nothing yet).
func (s *Service) MovieQuality(ctx context.Context, movieID int64) (Quality, bool, error) {
	q, err := scanQuality(s.DB.QueryRowContext(ctx, "SELECT "+qualityCols+" FROM vod_quality WHERE movie_id=?", movieID))
	if errors.Is(err, sql.ErrNoRows) {
		return q, false, nil
	}
	return q, err == nil, err
}

// EpisodeQualities is what's known of a series' episodes' files, by episode id.
func (s *Service) EpisodeQualities(ctx context.Context, seriesID int64) (map[int64]Quality, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT q.episode_id, `+qualityCols+` FROM vod_quality q
		JOIN vod_episodes e ON e.id=q.episode_id WHERE e.series_id=?`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Quality{}
	for rows.Next() {
		var id int64
		q, err := scanQuality(rows, &id)
		if err != nil {
			return nil, err
		}
		out[id] = q
	}
	return out, rows.Err()
}

// MovieQualityFromSource asks an Xtream Codes server for a film's details and keeps the
// video they describe (ok false when they describe none).
func (s *Service) MovieQualityFromSource(ctx context.Context, movieID int64) (Quality, bool, error) {
	var srcID, key string
	if err := s.DB.QueryRowContext(ctx, "SELECT source_id, source_key FROM vod_movies WHERE id=?", movieID).Scan(&srcID, &key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Quality{}, false, ErrNotFound
		}
		return Quality{}, false, err
	}
	src, ok := s.source(srcID)
	if !ok || !src.Xtream() || !strings.HasPrefix(key, "xtream:") {
		return Quality{}, false, ErrNotSavable
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, err := s.xtream(src).rawInfo(ctx, strings.TrimPrefix(key, "xtream:"))
	if err != nil {
		return Quality{}, false, err
	}
	q, ok := qualityFromInfo(raw)
	if !ok {
		return Quality{}, false, nil
	}
	return q, true, s.StoreMovieQuality(ctx, movieID, q)
}
