// Package probe runs ffprobe and normalises its output into the fields Marquee stores
// for media files, streams and chapters.
package probe

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Result struct {
	Container   string
	DurationMS  int64
	BitrateKbps int
	Tags        map[string]string // format-level tags, keys lowercased (music metadata)
	Streams     []Stream
	Chapters    []Chapter
	Raw         []byte
}

type Stream struct {
	Index           int
	Kind            string // video, audio, subtitle
	Codec           string
	Tag             string // container codec tag (hvc1, hev1, avc1…)
	Profile         string
	Level           int
	Language        string
	Title           string
	Default         bool
	Forced          bool
	HearingImpaired bool
	Channels        int
	ChannelLayout   string
	SampleRate      int
	BitrateKbps     int
	Width, Height   int
	FrameRate       float64
	BitDepth        int
	ColorTransfer   string
	ColorPrimaries  string
	HDRFormat       string // "", hdr10, hlg, dolby_vision
	DVProfile       int
	AttachedPic     bool // embedded cover art (music)
}

type Chapter struct {
	Title          string
	StartMS, EndMS int64
}

// Video returns the primary video stream, if any.
func (r *Result) Video() *Stream {
	for i := range r.Streams {
		if r.Streams[i].Kind == "video" && !r.Streams[i].AttachedPic {
			return &r.Streams[i]
		}
	}
	return nil
}

// Audio returns the default (or first) audio stream, if any.
func (r *Result) Audio() *Stream {
	var first *Stream
	for i := range r.Streams {
		s := &r.Streams[i]
		if s.Kind != "audio" {
			continue
		}
		if s.Default {
			return s
		}
		if first == nil {
			first = s
		}
	}
	return first
}

type Prober struct {
	Path    string
	Timeout time.Duration
	sem     chan struct{}
}

// New returns a prober limited to concurrency simultaneous ffprobe processes.
func New(path string, concurrency int) *Prober {
	return &Prober{Path: path, Timeout: 60 * time.Second, sem: make(chan struct{}, concurrency)}
}

func (p *Prober) Probe(ctx context.Context, file string) (*Result, error) {
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.sem }()

	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, p.Path, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", "-show_chapters", "--", file)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w: %s", file, err, strings.TrimSpace(stderr.String()))
	}
	return Parse(stdout.Bytes())
}

// ---- ffprobe JSON ----

type rawOutput struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		BitRate    string            `json:"bit_rate"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		Index          int               `json:"index"`
		CodecType      string            `json:"codec_type"`
		CodecName      string            `json:"codec_name"`
		CodecTag       string            `json:"codec_tag_string"`
		Profile        string            `json:"profile"`
		Level          int               `json:"level"`
		Width          int               `json:"width"`
		Height         int               `json:"height"`
		AvgFrameRate   string            `json:"avg_frame_rate"`
		RFrameRate     string            `json:"r_frame_rate"`
		PixFmt         string            `json:"pix_fmt"`
		BitsPerRaw     string            `json:"bits_per_raw_sample"`
		BitsPerSample  int               `json:"bits_per_sample"`
		ColorTransfer  string            `json:"color_transfer"`
		ColorPrimaries string            `json:"color_primaries"`
		Channels       int               `json:"channels"`
		ChannelLayout  string            `json:"channel_layout"`
		SampleRate     string            `json:"sample_rate"`
		BitRate        string            `json:"bit_rate"`
		Tags           map[string]string `json:"tags"`
		Disposition    map[string]int    `json:"disposition"`
		SideData       []map[string]any  `json:"side_data_list"`
	} `json:"streams"`
	Chapters []struct {
		StartTime string            `json:"start_time"`
		EndTime   string            `json:"end_time"`
		Tags      map[string]string `json:"tags"`
	} `json:"chapters"`
}

// Parse converts ffprobe JSON output into a Result.
func Parse(data []byte) (*Result, error) {
	var raw rawOutput
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode ffprobe output: %w", err)
	}
	r := &Result{
		Container:   containerName(raw.Format.FormatName),
		DurationMS:  secondsToMS(raw.Format.Duration),
		BitrateKbps: atoi(raw.Format.BitRate) / 1000,
		Tags:        lowerKeys(raw.Format.Tags),
		Raw:         data,
	}
	for _, s := range raw.Streams {
		switch s.CodecType {
		case "video", "audio", "subtitle":
		default:
			continue
		}
		tags := lowerKeys(s.Tags)
		st := Stream{
			Index:           s.Index,
			Kind:            s.CodecType,
			Codec:           s.CodecName,
			Tag:             s.CodecTag,
			Profile:         s.Profile,
			Level:           s.Level,
			Language:        normalizeLang(tags["language"]),
			Title:           tags["title"],
			Default:         s.Disposition["default"] == 1,
			Forced:          s.Disposition["forced"] == 1,
			HearingImpaired: s.Disposition["hearing_impaired"] == 1,
			AttachedPic:     s.Disposition["attached_pic"] == 1,
			Channels:        s.Channels,
			ChannelLayout:   s.ChannelLayout,
			SampleRate:      atoi(s.SampleRate),
			BitrateKbps:     atoi(s.BitRate) / 1000,
			Width:           s.Width,
			Height:          s.Height,
			ColorTransfer:   s.ColorTransfer,
			ColorPrimaries:  s.ColorPrimaries,
		}
		if st.BitrateKbps == 0 {
			// MKV stores per-stream bitrate in a statistics tag.
			st.BitrateKbps = atoi(tags["bps"]) / 1000
		}
		if s.CodecType == "video" {
			st.FrameRate = frameRate(s.AvgFrameRate)
			if st.FrameRate == 0 {
				st.FrameRate = frameRate(s.RFrameRate)
			}
			st.BitDepth = bitDepth(s.PixFmt, s.BitsPerRaw)
			switch s.ColorTransfer {
			case "smpte2084":
				st.HDRFormat = "hdr10"
			case "arib-std-b67":
				st.HDRFormat = "hlg"
			}
			for _, sd := range s.SideData {
				if t, _ := sd["side_data_type"].(string); strings.Contains(t, "DOVI") {
					st.HDRFormat = "dolby_vision"
					if p, ok := sd["dv_profile"].(float64); ok {
						st.DVProfile = int(p)
					}
				}
			}
		}
		if s.CodecType == "audio" {
			st.BitDepth = audioBitDepth(s.CodecName, s.BitsPerRaw, s.BitsPerSample)
		}
		if s.CodecType == "subtitle" && strings.Contains(strings.ToLower(st.Title), "sdh") {
			st.HearingImpaired = true
		}
		r.Streams = append(r.Streams, st)
	}
	for _, c := range raw.Chapters {
		r.Chapters = append(r.Chapters, Chapter{
			Title:   lowerKeys(c.Tags)["title"],
			StartMS: secondsToMS(c.StartTime),
			EndMS:   secondsToMS(c.EndTime),
		})
	}
	if r.BitrateKbps == 0 {
		for _, s := range r.Streams {
			r.BitrateKbps += s.BitrateKbps
		}
	}
	return r, nil
}

func containerName(f string) string {
	switch {
	case strings.Contains(f, "matroska"):
		return "mkv"
	case strings.Contains(f, "mp4"), strings.Contains(f, "mov"):
		return "mp4"
	}
	if i := strings.IndexByte(f, ','); i > 0 {
		return f[:i]
	}
	return f
}

func secondsToMS(s string) int64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(f) {
		return 0
	}
	return int64(math.Round(f * 1000))
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func frameRate(s string) float64 {
	num, den, ok := strings.Cut(s, "/")
	if !ok {
		f, _ := strconv.ParseFloat(s, 64)
		return f
	}
	n, _ := strconv.ParseFloat(num, 64)
	d, _ := strconv.ParseFloat(den, 64)
	if d == 0 {
		return 0
	}
	return math.Round(n/d*1000) / 1000
}

func bitDepth(pixFmt, raw string) int {
	if n := atoi(raw); n > 0 {
		return n
	}
	switch {
	case strings.Contains(pixFmt, "12"):
		return 12
	case strings.Contains(pixFmt, "10"):
		return 10
	case pixFmt != "":
		return 8
	}
	return 0
}

// audioBitDepth is the bits per sample of lossless audio (FLAC 16/24, PCM); lossy formats
// have none worth showing, so they get 0.
func audioBitDepth(codec, raw string, perSample int) int {
	if n := atoi(raw); n > 0 {
		return n
	}
	if strings.HasPrefix(codec, "pcm_") {
		for _, b := range []int{8, 16, 24, 32, 64} {
			if strings.Contains(codec, strconv.Itoa(b)) {
				return b
			}
		}
	}
	switch codec {
	case "flac", "alac", "ape", "wavpack", "tta":
		return perSample
	}
	return 0
}

func lowerKeys(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[strings.ToLower(k)] = v
	}
	return out
}

// normalizeLang maps "und"/empty to "" and keeps ISO 639-2 codes otherwise.
func normalizeLang(l string) string {
	l = strings.ToLower(strings.TrimSpace(l))
	if l == "und" {
		return ""
	}
	return l
}
