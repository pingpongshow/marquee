package playback

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Keyframe indexes for copied-video HLS (D48). Matroska Cues and the MP4 sync-sample
// table give the keyframe times without reading the media; other files fall back to an
// ffprobe packet scan. Only part of the keyframes is needed: segments just get longer.

var errNoIndex = errors.New("no keyframe index in file")

// Keyframes returns sorted keyframe times (seconds) of the first video track of path.
func Keyframes(ctx context.Context, ffprobe, path, container string) ([]float64, error) {
	var times []float64
	var err error
	switch container {
	case "mkv", "webm", "matroska":
		times, err = matroskaKeyframes(path)
	case "mp4", "mov", "m4v":
		times, err = mp4Keyframes(path)
	default:
		err = errNoIndex
	}
	if err != nil || len(times) < 2 {
		times, err = probeKeyframes(ctx, ffprobe, path)
	}
	if err != nil {
		return nil, err
	}
	sort.Float64s(times)
	return times, nil
}

// probeKeyframes lists keyframe packets with ffprobe (reads the whole file, no decoding).
func probeKeyframes(ctx context.Context, ffprobe, path string) ([]float64, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe keyframes: %w", err)
	}
	var times []float64
	for _, line := range strings.Split(string(out), "\n") {
		pts, flags, ok := strings.Cut(strings.TrimSpace(line), ",")
		if !ok || !strings.Contains(flags, "K") {
			continue
		}
		if t, err := strconv.ParseFloat(pts, 64); err == nil {
			times = append(times, t)
		}
	}
	if len(times) == 0 {
		return nil, errNoIndex
	}
	return times, nil
}

// ---------- Matroska ----------

const (
	ebmlSegment        = 0x18538067
	ebmlSeekHead       = 0x114D9B74
	ebmlSeek           = 0x4DBB
	ebmlSeekID         = 0x53AB
	ebmlSeekPosition   = 0x53AC
	ebmlInfo           = 0x1549A966
	ebmlTimecodeScale  = 0x2AD7B1
	ebmlTracks         = 0x1654AE6B
	ebmlTrackEntry     = 0xAE
	ebmlTrackNumber    = 0xD7
	ebmlTrackType      = 0x83
	ebmlCues           = 0x1C53BB6B
	ebmlCuePoint       = 0xBB
	ebmlCueTime        = 0xB3
	ebmlCueTrackPos    = 0xB7
	ebmlCueTrack       = 0xF7
	ebmlCluster        = 0x1F43B675
	ebmlUnknownSize    = math.MaxUint64
	maxMatroskaElement = 64 << 20 // Cues of a long film are a few MB at most
)

// ebmlVint reads an EBML variable-length integer. ID keeps the length marker; sizes don't.
func ebmlVint(r io.ByteReader, keepMarker bool) (uint64, int, error) {
	first, err := r.ReadByte()
	if err != nil {
		return 0, 0, err
	}
	n := 1
	for mask := byte(0x80); n <= 8 && first&mask == 0; mask >>= 1 {
		n++
	}
	if n > 8 {
		return 0, 0, errors.New("bad EBML vint")
	}
	v := uint64(first)
	if !keepMarker {
		v &= uint64(0xFF >> n)
	}
	allOnes := v == uint64(0xFF>>n)
	for i := 1; i < n; i++ {
		b, err := r.ReadByte()
		if err != nil {
			return 0, 0, err
		}
		allOnes = allOnes && b == 0xFF
		v = v<<8 | uint64(b)
	}
	if !keepMarker && allOnes {
		return ebmlUnknownSize, n, nil
	}
	return v, n, nil
}

type ebmlElem struct {
	id   uint64
	size uint64
	body []byte
}

// ebmlChildren parses the children of an element body.
func ebmlChildren(body []byte) []ebmlElem {
	var out []ebmlElem
	r := bytes.NewReader(body)
	for r.Len() > 0 {
		id, _, err := ebmlVint(r, true)
		if err != nil {
			break
		}
		size, _, err := ebmlVint(r, false)
		if err != nil || size > uint64(r.Len()) {
			break
		}
		start := len(body) - r.Len()
		out = append(out, ebmlElem{id: id, size: size, body: body[start : start+int(size)]})
		r.Seek(int64(size), io.SeekCurrent)
	}
	return out
}

func ebmlUint(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

type countingReader struct {
	r   *bufio.Reader
	pos int64
}

func (c *countingReader) ReadByte() (byte, error) {
	b, err := c.r.ReadByte()
	if err == nil {
		c.pos++
	}
	return b, err
}

func matroskaKeyframes(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cr := &countingReader{r: bufio.NewReader(f)}
	readHeader := func() (uint64, uint64, error) {
		id, _, err := ebmlVint(cr, true)
		if err != nil {
			return 0, 0, err
		}
		size, _, err := ebmlVint(cr, false)
		return id, size, err
	}
	skip := func(n uint64) error {
		if _, err := f.Seek(cr.pos+int64(n), io.SeekStart); err != nil {
			return err
		}
		cr.pos += int64(n)
		cr.r.Reset(f)
		return nil
	}
	readBody := func(n uint64) ([]byte, error) {
		if n > maxMatroskaElement {
			return nil, errors.New("matroska element too large")
		}
		b := make([]byte, n)
		for i := range b {
			c, err := cr.ReadByte()
			if err != nil {
				return nil, err
			}
			b[i] = c
		}
		return b, nil
	}

	// EBML header, then the Segment.
	id, size, err := readHeader()
	if err != nil || id != 0x1A45DFA3 {
		return nil, errNoIndex
	}
	if err := skip(size); err != nil {
		return nil, err
	}
	if id, _, err = readHeader(); err != nil || id != ebmlSegment {
		return nil, errNoIndex
	}
	segStart := cr.pos

	timecodeScale := uint64(1_000_000)
	var videoTrack uint64
	var cuesPos int64 = -1
	var cues []byte
	// Top-level elements up to the first Cluster: SeekHead, Info, Tracks (and Cues if early).
	for cues == nil {
		id, size, err := readHeader()
		if err != nil || id == ebmlCluster || size == ebmlUnknownSize {
			break
		}
		switch id {
		case ebmlSeekHead, ebmlInfo, ebmlTracks, ebmlCues:
			body, err := readBody(size)
			if err != nil {
				return nil, err
			}
			switch id {
			case ebmlSeekHead:
				for _, s := range ebmlChildren(body) {
					if s.id != ebmlSeek {
						continue
					}
					var sid, spos uint64
					for _, c := range ebmlChildren(s.body) {
						switch c.id {
						case ebmlSeekID:
							sid = ebmlUint(c.body)
						case ebmlSeekPosition:
							spos = ebmlUint(c.body)
						}
					}
					if sid == ebmlCues {
						cuesPos = segStart + int64(spos)
					}
				}
			case ebmlInfo:
				for _, c := range ebmlChildren(body) {
					if c.id == ebmlTimecodeScale {
						timecodeScale = ebmlUint(c.body)
					}
				}
			case ebmlTracks:
				for _, t := range ebmlChildren(body) {
					if t.id != ebmlTrackEntry {
						continue
					}
					var num, typ uint64
					for _, c := range ebmlChildren(t.body) {
						switch c.id {
						case ebmlTrackNumber:
							num = ebmlUint(c.body)
						case ebmlTrackType:
							typ = ebmlUint(c.body)
						}
					}
					if typ == 1 && videoTrack == 0 {
						videoTrack = num
					}
				}
			case ebmlCues:
				cues = body
			}
		default:
			if err := skip(size); err != nil {
				return nil, err
			}
		}
	}
	if cues == nil && cuesPos > 0 {
		if _, err := f.Seek(cuesPos, io.SeekStart); err != nil {
			return nil, err
		}
		cr.pos = cuesPos
		cr.r.Reset(f)
		id, size, err := readHeader()
		if err != nil || id != ebmlCues {
			return nil, errNoIndex
		}
		if cues, err = readBody(size); err != nil {
			return nil, err
		}
	}
	if cues == nil || videoTrack == 0 {
		return nil, errNoIndex
	}
	var times []float64
	for _, cp := range ebmlChildren(cues) {
		if cp.id != ebmlCuePoint {
			continue
		}
		var t uint64
		video := false
		for _, c := range ebmlChildren(cp.body) {
			switch c.id {
			case ebmlCueTime:
				t = ebmlUint(c.body)
			case ebmlCueTrackPos:
				for _, p := range ebmlChildren(c.body) {
					if p.id == ebmlCueTrack && ebmlUint(p.body) == videoTrack {
						video = true
					}
				}
			}
		}
		if video {
			times = append(times, float64(t)*float64(timecodeScale)/1e9)
		}
	}
	if len(times) == 0 {
		return nil, errNoIndex
	}
	return times, nil
}

// ---------- MP4 ----------

// mp4Keyframes reads the sync samples of the first video track from the moov box.
func mp4Keyframes(path string) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Walk top-level boxes to moov without reading mdat.
	var moov []byte
	var off int64
	for {
		var hdr [16]byte
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			return nil, errNoIndex
		}
		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		typ := string(hdr[4:8])
		if size == 1 {
			if _, err := f.ReadAt(hdr[8:16], off+8); err != nil {
				return nil, errNoIndex
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:16]))
		}
		if size < 8 {
			return nil, errNoIndex
		}
		if typ == "moov" {
			if size > maxMatroskaElement {
				return nil, errors.New("moov too large")
			}
			moov = make([]byte, size)
			if _, err := f.ReadAt(moov, off); err != nil {
				return nil, err
			}
			break
		}
		off += size
	}
	var times []float64
	children(moov[8:], func(typ string, trak []byte) bool {
		if typ != "trak" {
			return true
		}
		hdlr := find(trak, "mdia", "hdlr")
		if len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
			return true
		}
		mdhd := find(trak, "mdia", "mdhd")
		stbl := find(trak, "mdia", "minf", "stbl")
		if len(mdhd) < 24 || stbl == nil {
			return false
		}
		timescale := binary.BigEndian.Uint32(mdhd[12:16])
		if mdhd[0] == 1 {
			timescale = binary.BigEndian.Uint32(mdhd[20:24])
		}
		stss, stts, ctts := find(stbl, "stss"), find(stbl, "stts"), find(stbl, "ctts")
		if len(stss) < 8 || len(stts) < 8 || timescale == 0 {
			return false
		}
		// Sample decode times from stts runs.
		var dts []uint64
		var t uint64
		n := int(binary.BigEndian.Uint32(stts[4:8]))
		for i := 0; i < n && 8+i*8+8 <= len(stts); i++ {
			count := binary.BigEndian.Uint32(stts[8+i*8:])
			delta := uint64(binary.BigEndian.Uint32(stts[12+i*8:]))
			for j := uint32(0); j < count && len(dts) < 10_000_000; j++ {
				dts = append(dts, t)
				t += delta
			}
		}
		// Composition offsets turn decode times into presentation times.
		offsets := func(sample int) int64 { return 0 }
		if len(ctts) >= 8 {
			version := ctts[0]
			var runs [][2]int64
			cn := int(binary.BigEndian.Uint32(ctts[4:8]))
			for i := 0; i < cn && 8+i*8+8 <= len(ctts); i++ {
				c := int64(binary.BigEndian.Uint32(ctts[8+i*8:]))
				o := int64(binary.BigEndian.Uint32(ctts[12+i*8:]))
				if version == 1 {
					o = int64(int32(uint32(o)))
				}
				runs = append(runs, [2]int64{c, o})
			}
			offsets = func(sample int) int64 {
				for _, r := range runs {
					if int64(sample) < r[0] {
						return r[1]
					}
					sample -= int(r[0])
				}
				return 0
			}
		}
		sn := int(binary.BigEndian.Uint32(stss[4:8]))
		for i := 0; i < sn && 8+i*4+4 <= len(stss); i++ {
			s := int(binary.BigEndian.Uint32(stss[8+i*4:])) - 1 // 1-based
			if s >= 0 && s < len(dts) {
				times = append(times, float64(int64(dts[s])+offsets(s))/float64(timescale))
			}
		}
		return false
	})
	if len(times) == 0 {
		return nil, errNoIndex
	}
	return times, nil
}

// ---------- plans ----------

// KeyframePlan picks segment start times from keyframes: each segment starts at the first
// keyframe at least target seconds after the previous start. Segment 0 starts at 0.
func KeyframePlan(keyframes []float64, durationSec, target float64) []float64 {
	plan := []float64{0}
	for _, k := range keyframes {
		if k >= plan[len(plan)-1]+target && k < durationSec-0.5 {
			plan = append(plan, k)
		}
	}
	return plan
}

// FixedPlan is the plan for transcodes: segments every SegmentSeconds.
func FixedPlan(durationMS int64) []float64 {
	n := SegmentCount(durationMS)
	plan := make([]float64, n)
	for i := range plan {
		plan[i] = float64(i * SegmentSeconds)
	}
	return plan
}

// PlanPlaylist renders a VOD media playlist for a plan.
func PlanPlaylist(plan []float64, durationMS int64) []byte {
	total := float64(durationMS) / 1000
	durs := make([]float64, len(plan))
	maxDur := 1.0
	for i, start := range plan {
		end := total
		if i+1 < len(plan) {
			end = plan[i+1]
		}
		durs[i] = math.Max(end-start, 0.001)
		maxDur = math.Max(maxDur, durs[i])
	}
	var b bytes.Buffer
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", int(math.Ceil(maxDur)))
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	b.WriteString("#EXT-X-MAP:URI=\"init.mp4\"\n")
	for i, d := range durs {
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n%d.m4s\n", d, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.Bytes()
}
