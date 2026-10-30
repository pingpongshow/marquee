package frametiming

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// ErrNotMP4 is a file Rewrite can't retime: not an MP4/MOV, or one it doesn't understand
// (fragmented, no video track, a sample table that doesn't add up).
var ErrNotMP4 = errors.New("not an MP4 file that can be repaired")

// box is an MP4 box: a container's children, or a leaf's payload.
type box struct {
	typ  string
	data []byte // a leaf's payload, after the header
	kids []*box // a container's children
}

// containers are the boxes Rewrite looks inside; every other box is kept as it is.
var containers = map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true, "edts": true}

func parseBoxes(b []byte) ([]*box, error) {
	var out []*box
	for len(b) > 0 {
		if len(b) < 8 {
			return nil, ErrNotMP4
		}
		size, hdr := uint64(binary.BigEndian.Uint32(b)), uint64(8)
		switch size {
		case 1:
			if len(b) < 16 {
				return nil, ErrNotMP4
			}
			size, hdr = binary.BigEndian.Uint64(b[8:]), 16
		case 0:
			size = uint64(len(b))
		}
		if size < hdr || size > uint64(len(b)) {
			return nil, ErrNotMP4
		}
		bx := &box{typ: string(b[4:8])}
		if body := b[hdr:size]; containers[bx.typ] {
			kids, err := parseBoxes(body)
			if err != nil {
				return nil, err
			}
			bx.kids = kids
		} else {
			bx.data = body
		}
		out = append(out, bx)
		b = b[size:]
	}
	return out, nil
}

func (bx *box) size() uint64 {
	n := uint64(8 + len(bx.data))
	for _, k := range bx.kids {
		n += k.size()
	}
	if n > math.MaxUint32 {
		n += 8
	}
	return n
}

func (bx *box) write(w *bytes.Buffer) {
	if n := bx.size(); n > math.MaxUint32 {
		binary.Write(w, binary.BigEndian, uint32(1))
		w.WriteString(bx.typ)
		binary.Write(w, binary.BigEndian, n)
	} else {
		binary.Write(w, binary.BigEndian, uint32(n))
		w.WriteString(bx.typ)
	}
	w.Write(bx.data)
	for _, k := range bx.kids {
		k.write(w)
	}
}

func (bx *box) child(typ string) *box {
	for _, k := range bx.kids {
		if k.typ == typ {
			return k
		}
	}
	return nil
}

func (bx *box) path(types ...string) *box {
	for _, t := range types {
		if bx = bx.child(t); bx == nil {
			return nil
		}
	}
	return bx
}

// insertAfter puts k among bx's children after the first child of type after (first when
// there's none).
func (bx *box) insertAfter(after string, k *box) {
	i := 0
	for j, c := range bx.kids {
		if c.typ == after {
			i = j + 1
			break
		}
	}
	bx.kids = append(bx.kids[:i], append([]*box{k}, bx.kids[i:]...)...)
}

func (bx *box) remove(typ string) {
	kids := bx.kids[:0]
	for _, k := range bx.kids {
		if k.typ != typ {
			kids = append(kids, k)
		}
	}
	bx.kids = kids
}

// videoTrack is the moov's first video track.
func videoTrack(moov *box) *box {
	for _, t := range moov.kids {
		if t.typ != "trak" {
			continue
		}
		if h := t.path("mdia", "hdlr"); h != nil && len(h.data) >= 12 && string(h.data[8:12]) == "vide" {
			return t
		}
	}
	return nil
}

// decodeTimes are the samples' decode times from a sample-to-time table (stts).
func decodeTimes(stts []byte) ([]uint64, error) {
	if len(stts) < 8 {
		return nil, ErrNotMP4
	}
	n := int(binary.BigEndian.Uint32(stts[4:]))
	if len(stts) < 8+8*n {
		return nil, ErrNotMP4
	}
	var out []uint64
	var t uint64
	for e := 0; e < n; e++ {
		count := binary.BigEndian.Uint32(stts[8+8*e:])
		delta := uint64(binary.BigEndian.Uint32(stts[12+8*e:]))
		if len(out)+int(count) > 50_000_000 {
			return nil, ErrNotMP4
		}
		for range count {
			out = append(out, t)
			t += delta
		}
	}
	return out, nil
}

// offsets works out composition offsets that show the samples in display order: order[k]
// is the decode-order index of the k-th frame shown, which is shown at the k-th decode time
// plus the smallest delay that keeps every offset at zero or above.
func offsets(dts []uint64, order []int) (ctts []uint64, delay uint64, err error) {
	if len(order) != len(dts) {
		return nil, 0, fmt.Errorf("%d frames for %d samples", len(order), len(dts))
	}
	shown := make([]int, len(dts)) // decode index → display index
	seen := make([]bool, len(dts))
	for k, i := range order {
		if i < 0 || i >= len(dts) || seen[i] {
			return nil, 0, errors.New("the frame order doesn't match the samples")
		}
		seen[i], shown[i] = true, k
	}
	for i, k := range shown {
		if dts[i] > dts[k] {
			delay = max(delay, dts[i]-dts[k])
		}
	}
	ctts = make([]uint64, len(dts))
	for i, k := range shown {
		ctts[i] = dts[k] + delay - dts[i]
		if ctts[i] > math.MaxUint32 {
			return nil, 0, errors.New("a frame is shown too long after it's decoded")
		}
	}
	return ctts, delay, nil
}

// cttsBox is a composition offset table (version 0) holding offs, equal neighbours merged.
func cttsBox(offs []uint64) *box {
	var entries [][2]uint32
	for _, o := range offs {
		if n := len(entries); n > 0 && entries[n-1][1] == uint32(o) {
			entries[n-1][0]++
			continue
		}
		entries = append(entries, [2]uint32{1, uint32(o)})
	}
	b := make([]byte, 8+8*len(entries))
	binary.BigEndian.PutUint32(b[4:], uint32(len(entries)))
	for i, e := range entries {
		binary.BigEndian.PutUint32(b[8+8*i:], e[0])
		binary.BigEndian.PutUint32(b[12+8*i:], e[1])
	}
	return &box{typ: "ctts", data: b}
}

// setStart makes the track's edit list start at media time start, so the first frame shown
// is at the start of the film (as before, while the edit list started at the encoder's
// delay). A track without an edit list gets one.
func setStart(trak *box, start uint64) error {
	if elst := trak.path("edts", "elst"); elst != nil {
		d := elst.data
		if len(d) < 8 {
			return ErrNotMP4
		}
		v1 := d[0] == 1
		size := 12
		if v1 {
			size = 20
		}
		n := int(binary.BigEndian.Uint32(d[4:]))
		if len(d) < 8+size*n {
			return ErrNotMP4
		}
		for e := 0; e < n; e++ {
			at := d[8+size*e:]
			if v1 {
				if int64(binary.BigEndian.Uint64(at[8:])) != -1 { // -1: an empty edit (a pause)
					binary.BigEndian.PutUint64(at[8:], start)
					return nil
				}
			} else if int32(binary.BigEndian.Uint32(at[4:])) != -1 {
				if start > math.MaxInt32 {
					return ErrNotMP4
				}
				binary.BigEndian.PutUint32(at[4:], uint32(start))
				return nil
			}
		}
		return nil // only empty edits: nothing to move
	}
	tkhd := trak.child("tkhd")
	if tkhd == nil || len(tkhd.data) < 24 {
		return ErrNotMP4
	}
	var duration uint64
	if tkhd.data[0] == 1 {
		if len(tkhd.data) < 36 {
			return ErrNotMP4
		}
		duration = binary.BigEndian.Uint64(tkhd.data[28:])
	} else {
		duration = uint64(binary.BigEndian.Uint32(tkhd.data[20:]))
	}
	elst := make([]byte, 28)
	elst[0] = 1
	binary.BigEndian.PutUint32(elst[4:], 1)
	binary.BigEndian.PutUint64(elst[8:], duration)
	binary.BigEndian.PutUint64(elst[16:], start)
	binary.BigEndian.PutUint32(elst[24:], 0x00010000) // rate 1.0
	trak.insertAfter("tkhd", &box{typ: "edts", kids: []*box{{typ: "elst", data: elst}}})
	return nil
}

// chunkTable is one track's chunk offsets, as read, so they can be moved by any amount.
type chunkTable struct {
	stbl *box
	offs []uint64
}

func chunkTables(moov *box) ([]chunkTable, error) {
	var out []chunkTable
	for _, t := range moov.kids {
		if t.typ != "trak" {
			continue
		}
		stbl := t.path("mdia", "minf", "stbl")
		if stbl == nil {
			continue
		}
		ct := chunkTable{stbl: stbl}
		if b := stbl.child("stco"); b != nil {
			n := int(binary.BigEndian.Uint32(b.data[4:]))
			if len(b.data) < 8+4*n {
				return nil, ErrNotMP4
			}
			for i := range n {
				ct.offs = append(ct.offs, uint64(binary.BigEndian.Uint32(b.data[8+4*i:])))
			}
		} else if b := stbl.child("co64"); b != nil {
			n := int(binary.BigEndian.Uint32(b.data[4:]))
			if len(b.data) < 8+8*n {
				return nil, ErrNotMP4
			}
			for i := range n {
				ct.offs = append(ct.offs, binary.BigEndian.Uint64(b.data[8+8*i:]))
			}
		} else {
			continue
		}
		out = append(out, ct)
	}
	return out, nil
}

// moveChunks rewrites each table with the chunks from after on moved by delta, in 64 bits
// where 32 no longer hold them.
func moveChunks(tables []chunkTable, delta, after int64) {
	moved := func(o uint64) uint64 {
		if int64(o) >= after {
			return uint64(int64(o) + delta)
		}
		return o
	}
	for _, ct := range tables {
		big := false
		for _, o := range ct.offs {
			if moved(o) > math.MaxUint32 {
				big = true
			}
		}
		var b []byte
		typ := "stco"
		if big {
			typ, b = "co64", make([]byte, 8+8*len(ct.offs))
			for i, o := range ct.offs {
				binary.BigEndian.PutUint64(b[8+8*i:], moved(o))
			}
		} else {
			b = make([]byte, 8+4*len(ct.offs))
			for i, o := range ct.offs {
				binary.BigEndian.PutUint32(b[8+4*i:], uint32(moved(o)))
			}
		}
		binary.BigEndian.PutUint32(b[4:], uint32(len(ct.offs)))
		old := ct.stbl.child("stco")
		if old == nil {
			old = ct.stbl.child("co64")
		}
		old.typ, old.data = typ, b
	}
}

// topBox is a top-level box of a file.
type topBox struct {
	typ       string
	off, size int64
}

func topBoxes(f *os.File) ([]topBox, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var out []topBox
	var hdr [16]byte
	for off := int64(0); off < st.Size(); {
		if _, err := f.ReadAt(hdr[:8], off); err != nil {
			return nil, ErrNotMP4
		}
		size := int64(binary.BigEndian.Uint32(hdr[:]))
		switch size {
		case 1:
			if _, err := f.ReadAt(hdr[8:16], off+8); err != nil {
				return nil, ErrNotMP4
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:]))
		case 0:
			size = st.Size() - off
		}
		if size < 8 || off+size > st.Size() {
			return nil, ErrNotMP4
		}
		out = append(out, topBox{typ: string(hdr[4:8]), off: off, size: size})
		off += size
	}
	return out, nil
}

// Rewrite copies src to dst with the video track's frames timed in display order (order[k]
// is the decode-order index of the k-th frame shown; see Order): a composition offset table
// is written (replacing any there was), the edit list starts at the first frame shown, and
// chunk offsets move with the index when it sits before the media. Every byte of audio and
// video is copied as it is. It returns the delay before the first frame (seconds).
func Rewrite(src, dst string, order []int) (float64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	tops, err := topBoxes(in)
	if err != nil {
		return 0, err
	}
	moovAt, mdat := -1, false
	for i, t := range tops {
		switch t.typ {
		case "moov":
			if moovAt >= 0 {
				return 0, ErrNotMP4
			}
			moovAt = i
		case "mdat":
			mdat = true
		case "moof":
			return 0, ErrNotMP4 // fragmented: timing lives in each fragment
		}
	}
	if moovAt < 0 || !mdat || tops[moovAt].size > 1<<30 {
		return 0, ErrNotMP4
	}
	mv := tops[moovAt]
	raw := make([]byte, mv.size)
	if _, err := in.ReadAt(raw, mv.off); err != nil {
		return 0, err
	}
	parsed, err := parseBoxes(raw)
	if err != nil || len(parsed) != 1 {
		return 0, ErrNotMP4
	}
	moov := parsed[0]
	if moov.child("mvex") != nil {
		return 0, ErrNotMP4
	}
	trak := videoTrack(moov)
	if trak == nil {
		return 0, ErrNotMP4
	}
	stbl := trak.path("mdia", "minf", "stbl")
	mdhd := trak.path("mdia", "mdhd")
	if stbl == nil || stbl.child("stts") == nil || mdhd == nil || len(mdhd.data) < 24 {
		return 0, ErrNotMP4
	}
	timescale := binary.BigEndian.Uint32(mdhd.data[12:])
	if mdhd.data[0] == 1 {
		if len(mdhd.data) < 32 {
			return 0, ErrNotMP4
		}
		timescale = binary.BigEndian.Uint32(mdhd.data[20:])
	}
	dts, err := decodeTimes(stbl.child("stts").data)
	if err != nil {
		return 0, err
	}
	ctts, delay, err := offsets(dts, order)
	if err != nil {
		return 0, err
	}
	stbl.remove("ctts")
	stbl.insertAfter("stts", cttsBox(ctts))
	if err := setStart(trak, delay); err != nil {
		return 0, err
	}
	// Media after the index (all of it, in a file made for streaming) moves by however much
	// the index grew; 64-bit offsets, where needed, grow it again.
	tables, err := chunkTables(moov)
	if err != nil {
		return 0, err
	}
	for i := 0; ; i++ {
		delta := int64(moov.size()) - mv.size
		moveChunks(tables, delta, mv.off+mv.size)
		if int64(moov.size())-mv.size == delta {
			break
		}
		if i == 3 {
			return 0, ErrNotMP4
		}
	}
	var buf bytes.Buffer
	buf.Grow(int(moov.size()))
	moov.write(&buf)

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return 0, err
	}
	ok := false
	defer func() {
		if !ok {
			out.Close()
			os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, io.NewSectionReader(in, 0, mv.off)); err != nil {
		return 0, err
	}
	if _, err := out.Write(buf.Bytes()); err != nil {
		return 0, err
	}
	st, err := in.Stat()
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(out, io.NewSectionReader(in, mv.off+mv.size, st.Size()-mv.off-mv.size)); err != nil {
		return 0, err
	}
	if err := out.Sync(); err != nil {
		return 0, err
	}
	if err := out.Close(); err != nil {
		return 0, err
	}
	ok = true
	if timescale == 0 {
		return 0, nil
	}
	return float64(delay) / float64(timescale), nil
}
