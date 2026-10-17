package optimize

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"marquee/internal/settings"
)

// Filter picks files for the Large files list. Zero values don't filter.
type Filter struct {
	LibraryID      int64
	Type           string   // movie, episode
	MinSizeBytes   int64    // at least this big
	MinBitrateKbps int      // at least this overall bitrate
	Resolution     int      // 2160, 1080, 720 or 480 (SD): only this class
	Codecs         []string // only these video codecs (ffprobe names: h264, mpeg2video, vc1, …)
	HDR            *bool    // only HDR (true) or only SDR (false)
	Sort           string   // size (default), bitrate, title
}

// File is one file in the list.
type File struct {
	FileID, ItemID int64
	Title          string
	Subtitle       string // the show and episode, or the year
	Type           string
	Path           string
	Size           int64
	Width, Height  int
	VideoCodec     string
	HDR            string // hdr_format, "" = SDR
	BitrateKbps    int
	DurationMS     int64
	Estimate       int64
	// Job is the file's latest optimizer job, if any: its state (queued, running, done, …).
	JobState string
	JobID    int64
	info     FileInfo
}

// Page is a page of the list with totals for everything that matches.
type Page struct {
	Files         []File
	Total         int
	TotalSize     int64
	TotalEstimate int64
}

// resolutionSQL is the resolution class of f by width or height, as Class.
const resolutionSQL = `(CASE WHEN COALESCE(f.width, 0) >= 3200 OR COALESCE(f.height, 0) >= 2000 THEN 2160
	WHEN COALESCE(f.width, 0) >= 1700 OR COALESCE(f.height, 0) >= 1000 THEN 1080
	WHEN COALESCE(f.width, 0) >= 1200 OR COALESCE(f.height, 0) >= 700 THEN 720 ELSE 480 END)`

// where is the SQL condition and arguments for a filter, over media_files f, items i and
// libraries l: available movie and episode files (not extras) in video libraries, except
// Live TV recordings (the DVR manages those: commercials, quotas).
func (flt Filter) where() (string, []any) {
	conds := []string{"f.available = 1", "f.size > 0", "f.video_codec IS NOT NULL", "i.extra_type IS NULL",
		"l.type IN ('movies', 'shows', 'anime', 'videos')", "l.recordings = 0"}
	var args []any
	switch flt.Type {
	case "movie", "episode":
		conds = append(conds, "i.type = ?")
		args = append(args, flt.Type)
	default:
		conds = append(conds, "i.type IN ('movie', 'episode')")
	}
	if flt.LibraryID > 0 {
		conds = append(conds, "f.library_id = ?")
		args = append(args, flt.LibraryID)
	}
	if flt.MinSizeBytes > 0 {
		conds = append(conds, "f.size >= ?")
		args = append(args, flt.MinSizeBytes)
	}
	if flt.MinBitrateKbps > 0 {
		conds = append(conds, "COALESCE(f.bitrate_kbps, 0) >= ?")
		args = append(args, flt.MinBitrateKbps)
	}
	if flt.Resolution > 0 {
		conds = append(conds, resolutionSQL+" = ?")
		args = append(args, flt.Resolution)
	}
	if len(flt.Codecs) > 0 {
		ph := make([]string, len(flt.Codecs))
		for i, c := range flt.Codecs {
			ph[i] = "?"
			args = append(args, strings.ToLower(strings.TrimSpace(c)))
		}
		conds = append(conds, "LOWER(f.video_codec) IN ("+strings.Join(ph, ",")+")")
	}
	if flt.HDR != nil {
		if *flt.HDR {
			conds = append(conds, "COALESCE(f.hdr_format, '') <> ''")
		} else {
			conds = append(conds, "COALESCE(f.hdr_format, '') = ''")
		}
	}
	return strings.Join(conds, " AND "), args
}

const fromSQL = ` FROM media_files f CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
	CROSS JOIN libraries l ON l.id = f.library_id`

// Matching lists every file the filter picks (ids only), biggest first.
func (s *Service) Matching(ctx context.Context, flt Filter) ([]int64, error) {
	w, args := flt.where()
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id`+fromSQL+` WHERE `+w+` ORDER BY f.size DESC, f.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// List is a page of the files the filter picks, with the size each would be after the
// preset and totals for all of them. It reads only what the library knows (no probing).
func (s *Service) List(ctx context.Context, flt Filter, p settings.OptimizePreset, offset, limit int) (Page, error) {
	w, args := flt.where()
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, i.id, i.title, i.type, COALESCE(i.year, 0), COALESCE(i.idx, 0),
			COALESCE(par.idx, 0), COALESCE(gp.title, ''), f.path, f.size, COALESCE(f.width, 0), COALESCE(f.height, 0),
			COALESCE(f.video_codec, ''), COALESCE(f.hdr_format, ''), COALESCE(f.bitrate_kbps, 0), COALESCE(f.duration_ms, 0),
			COALESCE(j.id, 0), COALESCE(j.state, '')`+fromSQL+`
		LEFT JOIN items par ON par.id = i.parent_id LEFT JOIN items gp ON gp.id = i.grandparent_id
		LEFT JOIN optimize_jobs j ON j.id = (SELECT MAX(id) FROM optimize_jobs WHERE file_id = f.id)
		WHERE `+w, args...)
	if err != nil {
		return Page{}, err
	}
	var all []File
	byID := map[int64]int{}
	for rows.Next() {
		var f File
		var year, idx, season int
		var show string
		if err := rows.Scan(&f.FileID, &f.ItemID, &f.Title, &f.Type, &year, &idx, &season, &show, &f.Path, &f.Size, &f.Width, &f.Height,
			&f.VideoCodec, &f.HDR, &f.BitrateKbps, &f.DurationMS, &f.JobID, &f.JobState); err != nil {
			rows.Close()
			return Page{}, err
		}
		if f.Type == "episode" {
			f.Subtitle = fmt.Sprintf("%s · S%02dE%02d", show, season, idx)
		} else if year > 0 {
			f.Subtitle = strconv.Itoa(year)
		}
		f.info = FileInfo{Size: f.Size, DurationMS: f.DurationMS, Width: f.Width, Height: f.Height, HDR: f.HDR != ""}
		byID[f.FileID] = len(all)
		all = append(all, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Page{}, err
	}
	// Audio streams for the estimates.
	arows, err := s.DB.QueryContext(ctx, `SELECT s.file_id, s.codec, COALESCE(s.channels, 2), COALESCE(s.bitrate_kbps, 0), s.is_default,
			COALESCE(s.title, '') FROM streams s WHERE s.kind = 'audio' AND s.file_id IN (SELECT f.id`+fromSQL+` WHERE `+w+`) ORDER BY s.file_id, s.stream_index`, args...)
	if err != nil {
		return Page{}, err
	}
	for arows.Next() {
		var id int64
		var a AudioInfo
		var title string
		if arows.Scan(&id, &a.Codec, &a.Channels, &a.Kbps, &a.Default, &title) == nil {
			a.Commentary = strings.Contains(strings.ToLower(title), "commentary")
			if i, ok := byID[id]; ok {
				all[i].info.Audio = append(all[i].info.Audio, a)
			}
		}
	}
	arows.Close()

	page := Page{Total: len(all), Files: []File{}}
	for i := range all {
		all[i].Estimate = Estimate(all[i].info, p)
		page.TotalSize += all[i].Size
		page.TotalEstimate += all[i].Estimate
	}
	sort.SliceStable(all, func(a, b int) bool {
		x, y := all[a], all[b]
		switch flt.Sort {
		case "bitrate":
			if x.BitrateKbps != y.BitrateKbps {
				return x.BitrateKbps > y.BitrateKbps
			}
		case "title":
			if x.Title != y.Title {
				return strings.ToLower(x.Title) < strings.ToLower(y.Title)
			}
		case "savings":
			if dx, dy := x.Size-x.Estimate, y.Size-y.Estimate; dx != dy {
				return dx > dy
			}
		}
		if x.Size != y.Size {
			return x.Size > y.Size
		}
		return x.FileID < y.FileID
	})
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 {
		limit = 50
	}
	if offset < len(all) {
		page.Files = all[offset:min(len(all), offset+limit)]
	}
	return page, nil
}
