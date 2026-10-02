package trickplay

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// IFramePlaylist builds the HLS I-frame playlist for an I-frame MP4: the init segment
// (ftyp+moov) as EXT-X-MAP and one byte range (moof+mdat) per frame, each lasting one
// Interval. Players request ranges of the same file, "iframes.mp4".
func IFramePlaylist(path string, durationMS int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type frag struct{ off, size int64 }
	var initEnd int64
	var frags []frag
	var moof int64 = -1
	var off int64
	hdr := make([]byte, 16)
	for {
		if _, err := io.ReadFull(f, hdr[:8]); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		size := int64(binary.BigEndian.Uint32(hdr[:4]))
		typ := string(hdr[4:8])
		if size == 1 { // 64-bit size
			if _, err := io.ReadFull(f, hdr[8:16]); err != nil {
				return nil, err
			}
			size = int64(binary.BigEndian.Uint64(hdr[8:16]))
		}
		if size < 8 {
			return nil, fmt.Errorf("bad box %q at %d", typ, off)
		}
		switch typ {
		case "moov":
			initEnd = off + size
		case "moof":
			moof = off
		case "mdat":
			if moof >= 0 {
				frags = append(frags, frag{moof, off + size - moof})
				moof = -1
			}
		}
		off += size
		if _, err := f.Seek(off, io.SeekStart); err != nil {
			return nil, err
		}
	}
	if initEnd == 0 || len(frags) == 0 {
		return nil, errors.New("no frames")
	}
	step := Interval.Seconds()
	var b bytes.Buffer
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-I-FRAMES-ONLY\n", int(step))
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=\"iframes.mp4\",BYTERANGE=\"%d@0\"\n", initEnd)
	total := float64(durationMS) / 1000
	for i, fr := range frags {
		d := step
		if left := total - float64(i)*step; i == len(frags)-1 && left > 0 && left < step {
			d = left
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n#EXT-X-BYTERANGE:%d@%d\niframes.mp4\n", d, fr.size, fr.off)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.Bytes(), nil
}
