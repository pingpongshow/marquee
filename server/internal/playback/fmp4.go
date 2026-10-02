package playback

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
)

// Fragmented-MP4 helpers for keyframe-aligned HLS when video is copied (D48).
//
// FFmpeg writes one fragmented MP4 stream (a fragment per video keyframe) to a pipe;
// splitFragments groups the fragments into the HLS segments of the session's plan. A
// fragment's timestamp decides its segment, so the same segment always gets the same
// fragments, whether it was produced in one run or after a seek restarted FFmpeg.

type box struct {
	typ  string
	data []byte // the whole box, header included
}

// readBox reads one top-level box.
func readBox(r io.Reader) (box, error) {
	var hdr [8]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return box{}, err
	}
	size := uint64(binary.BigEndian.Uint32(hdr[:4]))
	typ := string(hdr[4:8])
	head := hdr[:]
	if size == 1 {
		var ext [8]byte
		if _, err := io.ReadFull(r, ext[:]); err != nil {
			return box{}, err
		}
		size = binary.BigEndian.Uint64(ext[:])
		head = append(append([]byte{}, hdr[:]...), ext[:]...)
	}
	if size < uint64(len(head)) || size > 1<<31 {
		return box{}, fmt.Errorf("bad %q box size %d", typ, size)
	}
	data := make([]byte, size)
	copy(data, head)
	if _, err := io.ReadFull(r, data[len(head):]); err != nil {
		return box{}, err
	}
	return box{typ: typ, data: data}, nil
}

// children iterates the child boxes inside payload.
func children(payload []byte, fn func(typ string, body []byte) bool) {
	for len(payload) >= 8 {
		size := int(binary.BigEndian.Uint32(payload[:4]))
		typ := string(payload[4:8])
		hdr := 8
		if size == 1 && len(payload) >= 16 {
			size = int(binary.BigEndian.Uint64(payload[8:16]))
			hdr = 16
		} else if size == 0 {
			size = len(payload)
		}
		if size < hdr || size > len(payload) {
			return
		}
		if !fn(typ, payload[hdr:size]) {
			return
		}
		payload = payload[size:]
	}
}

func find(payload []byte, path ...string) []byte {
	cur := payload
	for _, p := range path {
		var next []byte
		children(cur, func(typ string, body []byte) bool {
			if typ == p {
				next = body
				return false
			}
			return true
		})
		if next == nil {
			return nil
		}
		cur = next
	}
	return cur
}

// videoTrack returns the track ID and timescale of the first video track in a moov box.
func videoTrack(moov []byte) (id uint32, timescale uint32, err error) {
	children(moov[8:], func(typ string, trak []byte) bool {
		if typ != "trak" {
			return true
		}
		hdlr := find(trak, "mdia", "hdlr")
		if len(hdlr) < 12 || string(hdlr[8:12]) != "vide" {
			return true
		}
		tkhd := find(trak, "tkhd")
		mdhd := find(trak, "mdia", "mdhd")
		if len(tkhd) < 24 || len(mdhd) < 24 {
			return true
		}
		if tkhd[0] == 1 {
			id = binary.BigEndian.Uint32(tkhd[20:24])
		} else {
			id = binary.BigEndian.Uint32(tkhd[12:16])
		}
		if mdhd[0] == 1 {
			timescale = binary.BigEndian.Uint32(mdhd[20:24])
		} else {
			timescale = binary.BigEndian.Uint32(mdhd[12:16])
		}
		return false
	})
	if id == 0 || timescale == 0 {
		return 0, 0, errors.New("no video track in moov")
	}
	return id, timescale, nil
}

// fragmentTime returns the base decode time of track id in a moof box, in seconds.
func fragmentTime(moof []byte, id, timescale uint32) (float64, bool) {
	var t float64
	found := false
	children(moof[8:], func(typ string, traf []byte) bool {
		if typ != "traf" {
			return true
		}
		tfhd := find(traf, "tfhd")
		if len(tfhd) < 8 || binary.BigEndian.Uint32(tfhd[4:8]) != id {
			return true
		}
		tfdt := find(traf, "tfdt")
		switch {
		case len(tfdt) >= 12 && tfdt[0] == 1:
			t = float64(binary.BigEndian.Uint64(tfdt[4:12])) / float64(timescale)
			found = true
		case len(tfdt) >= 8:
			t = float64(binary.BigEndian.Uint32(tfdt[4:8])) / float64(timescale)
			found = true
		}
		return false
	})
	return t, found
}

// boundaryTolerance absorbs the gap between a keyframe's presentation time (the plan) and
// its fragment's decode time (B-frame reordering delay).
const boundaryTolerance = 0.3

// segmentFor returns the plan segment that a fragment starting at t belongs to.
func segmentFor(plan []float64, t float64) int {
	k := 0
	for k+1 < len(plan) && t >= plan[k+1]-boundaryTolerance {
		k++
	}
	return k
}

// splitFragments reads a fragmented MP4 stream and writes init.mp4 and <k>.m4s segment
// files into dir following plan (segment start times in seconds). Fragments before
// segment `first` are dropped (a seek restart begins at the keyframe before it). Each
// segment appears atomically once complete; the last one only if complete is true at EOF.
func splitFragments(r io.Reader, dir string, plan []float64, first int, complete func() bool) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var header []byte // ftyp + moov
	var trackID, timescale uint32
	cur := -1
	inSegment := 0 // fragments written to the current segment
	var f *os.File
	var w *bufio.Writer
	finish := func(keep bool) error {
		if f == nil {
			return nil
		}
		tmp := f.Name()
		err := w.Flush()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		f, w = nil, nil
		if err != nil || !keep {
			os.Remove(tmp)
			return err
		}
		return os.Rename(tmp, filepath.Join(dir, strconv.Itoa(cur)+".m4s"))
	}
	for {
		b, err := readBox(br)
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return finish(complete())
		}
		if err != nil {
			finish(false)
			return err
		}
		switch b.typ {
		case "ftyp", "moov":
			header = append(header, b.data...)
			if b.typ == "moov" {
				if trackID, timescale, err = videoTrack(b.data); err != nil {
					return err
				}
				if err := writeAtomic(filepath.Join(dir, "init.mp4"), header); err != nil {
					return err
				}
			}
			continue
		case "moof":
			if trackID == 0 {
				return errors.New("fragment before moov")
			}
			t, ok := fragmentTime(b.data, trackID, timescale)
			if ok {
				k := segmentFor(plan, t)
				if k < first {
					cur = -2 // still before the requested segment: drop
				} else if k != cur {
					if err := finish(cur >= 0); err != nil {
						return err
					}
					cur, inSegment = k, 0
					tmp, err := os.Create(filepath.Join(dir, strconv.Itoa(k)+".m4s.part"))
					if err != nil {
						return err
					}
					f, w = tmp, bufio.NewWriterSize(tmp, 1<<20)
				}
			}
			if cur >= 0 {
				// FFmpeg numbers fragments per run; number them by position in the plan so a
				// segment is byte-identical whichever run produced it.
				inSegment++
				setFragmentSequence(b.data, uint32(cur*1000+inSegment))
			}
		case "mfra":
			continue // end-of-file fragment index; not part of any segment
		}
		// moof, mdat and anything else (styp, sidx) go into the current segment.
		if w != nil && cur >= 0 {
			if _, err := w.Write(b.data); err != nil {
				finish(false)
				return err
			}
		}
	}
}

// setFragmentSequence rewrites the sequence number in a moof box's mfhd.
func setFragmentSequence(moof []byte, seq uint32) {
	children(moof[8:], func(typ string, body []byte) bool {
		if typ == "mfhd" && len(body) >= 8 {
			binary.BigEndian.PutUint32(body[4:8], seq)
			return false
		}
		return true
	})
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
