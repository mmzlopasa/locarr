package tags

import (
	"bytes"
	"encoding/binary"
	"io"
	"time"
)

// oggPackets reads the first packets of the first logical stream (enough for
// the identification and comment headers).
func oggPackets(r io.Reader, want int) ([][]byte, error) {
	var out [][]byte
	var cur []byte
	for pages := 0; pages < 512 && len(out) < want; pages++ {
		h, err := readFull(r, 27)
		if err != nil {
			return out, err
		}
		if string(h[:4]) != "OggS" {
			return out, ErrCorrupt
		}
		segs, err := readFull(r, int(h[26]))
		if err != nil {
			return out, err
		}
		for _, s := range segs {
			b, err := readFull(r, int(s))
			if err != nil {
				return out, err
			}
			cur = append(cur, b...)
			if s < 255 {
				out = append(out, cur)
				cur = nil
				if len(out) >= want {
					break
				}
			}
		}
	}
	return out, nil
}

// lastGranule finds the granule position of the last page.
func lastGranule(r io.ReadSeeker, size int64) int64 {
	n := min(size, 128<<10)
	if _, err := r.Seek(size-n, io.SeekStart); err != nil {
		return 0
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return 0
	}
	i := bytes.LastIndex(b, []byte("OggS"))
	if i < 0 || i+14 > len(b) {
		return 0
	}
	return int64(binary.LittleEndian.Uint64(b[i+6:])) // #nosec G115 -- granule positions fit
}

func readOgg(r io.ReadSeeker, size int64) (Tags, error) {
	var t Tags
	pk, err := oggPackets(r, 2)
	if len(pk) < 2 {
		if err == nil {
			err = ErrCorrupt
		}
		return t, err
	}
	id, comments := pk[0], pk[1]
	var rate int
	var preSkip int64
	switch {
	case len(id) >= 30 && string(id[:7]) == "\x01vorbis":
		t.Format = FormatVorbis
		t.Channels = int(id[11])
		rate = int(binary.LittleEndian.Uint32(id[12:]))
		if nominal := int32(binary.LittleEndian.Uint32(id[20:])); nominal > 0 { // #nosec G115 -- signed field
			t.Bitrate = int(nominal) / 1000
		}
		if len(comments) < 7 || string(comments[:7]) != "\x03vorbis" {
			return t, ErrCorrupt
		}
		if err := t.parseComments(comments[7:]); err != nil {
			return t, err
		}
		t.SampleRate = rate
	case len(id) >= 19 && string(id[:8]) == "OpusHead":
		t.Format = FormatOpus
		t.Channels = int(id[9])
		preSkip = int64(binary.LittleEndian.Uint16(id[10:]))
		t.SampleRate = int(binary.LittleEndian.Uint32(id[12:]))
		rate = 48000 // Opus granules always count 48 kHz samples
		if len(comments) < 8 || string(comments[:8]) != "OpusTags" {
			return t, ErrCorrupt
		}
		if err := t.parseComments(comments[8:]); err != nil {
			return t, err
		}
	default:
		return t, ErrUnsupported
	}
	if g := lastGranule(r, size) - preSkip; g > 0 && rate > 0 {
		t.Duration = time.Duration(float64(g) / float64(rate) * float64(time.Second))
	}
	return t, nil
}
