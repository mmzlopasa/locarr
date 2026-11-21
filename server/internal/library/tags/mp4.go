package tags

import (
	"io"
	"strconv"
	"time"
)

// readMP4 walks the atom tree for mvhd (duration), stsd (codec, channels,
// sample size and rate) and moov/udta/meta/ilst (tags).
func readMP4(r io.ReadSeeker, size int64) (Tags, error) {
	t := Tags{Format: FormatAAC}
	found := false
	err := walkAtoms(r, 0, size, 0, func(typ string, start, end int64) (bool, error) {
		switch typ {
		case "moov", "trak", "mdia", "minf", "stbl", "udta", "ilst":
			found = found || typ == "moov"
			return true, nil
		case "meta":
			return true, skipN(r, 4) // full box: version and flags before the children
		case "mvhd":
			b, err := readFull(r, int(min(end-start, 32)))
			if err != nil {
				return false, err
			}
			var scale, dur uint64
			if b[0] == 1 && len(b) >= 32 {
				scale, dur = uint64(be32(b[20:])), be64(b[24:])
			} else if len(b) >= 20 {
				scale, dur = uint64(be32(b[12:])), uint64(be32(b[16:]))
			}
			if scale > 0 {
				t.Duration = time.Duration(float64(dur) / float64(scale) * float64(time.Second))
			}
		case "stsd":
			b, err := readFull(r, int(min(end-start, 64)))
			if err != nil || len(b) < 8+36 {
				return false, err
			}
			e := b[8:] // after version/flags and entry count
			switch string(e[4:8]) {
			case "alac":
				t.Format = FormatALAC
			case "mp4a":
				t.Format = FormatAAC
			}
			// Audio sample entry: 6 reserved + 2 index + 8 version/vendor, then channels, sample size, 4, rate 16.16.
			a := e[8:]
			t.Channels = be16(a[16:])
			if t.Format == FormatALAC {
				t.BitDepth = be16(a[18:])
			}
			t.SampleRate = int(be32(a[24:]) >> 16)
		default:
			if len(typ) == 4 && (typ[0] == 0xA9 || typ == "aART" || typ == "trkn" || typ == "disk" || typ == "----" || typ == "covr") {
				b, err := readFull(r, int(min(end-start, 1<<20)))
				if err != nil {
					return false, err
				}
				t.ilstItem(typ, b)
			}
		}
		return false, nil
	})
	if err != nil {
		return t, err
	}
	if !found {
		return t, ErrCorrupt
	}
	return t, nil
}

func skipN(r io.Seeker, n int64) error {
	_, err := r.Seek(n, io.SeekCurrent)
	return err
}

// walkAtoms calls fn for each atom in [from, to); fn returns whether to
// descend into it (its body is then read from the reader's position).
func walkAtoms(r io.ReadSeeker, from, to int64, depth int, fn func(typ string, start, end int64) (bool, error)) error {
	if depth > 10 {
		return ErrCorrupt
	}
	pos := from
	for pos+8 <= to {
		if _, err := r.Seek(pos, io.SeekStart); err != nil {
			return ErrCorrupt
		}
		h, err := readFull(r, 8)
		if err != nil {
			return err
		}
		size := int64(be32(h))
		typ := string(h[4:8])
		hdr := int64(8)
		switch size {
		case 1:
			x, err := readFull(r, 8)
			if err != nil {
				return err
			}
			size, hdr = int64(be64(x)), 16 // #nosec G115 -- checked below
		case 0:
			size = to - pos
		}
		if size < hdr || pos+size > to {
			return ErrCorrupt
		}
		start, end := pos+hdr, pos+size
		descend, err := fn(typ, start, end)
		if err != nil {
			return err
		}
		if descend {
			cur, _ := r.Seek(0, io.SeekCurrent)
			if err := walkAtoms(r, cur, end, depth+1, fn); err != nil {
				return err
			}
		}
		pos = end
	}
	return nil
}

// ilstItem decodes one tag item (its body holds "data", and for "----" also
// "mean" and "name" atoms).
func (t *Tags) ilstItem(typ string, b []byte) {
	var name string
	var data []byte
	var kind uint32
	for p := 0; p+8 <= len(b); {
		n := int(be32(b[p:]))
		if n < 8 || p+n > len(b) {
			return
		}
		sub := string(b[p+4 : p+8])
		body := b[p+8 : p+n]
		switch sub {
		case "name":
			if len(body) >= 4 {
				name = string(body[4:])
			}
		case "data":
			if len(body) >= 8 && data == nil {
				kind, data = be32(body)&0x00FFFFFF, body[8:]
			}
		}
		p += n
	}
	if data == nil {
		return
	}
	text := string(data)
	switch typ {
	case "\xa9nam":
		t.setField("TITLE", text)
	case "\xa9ART":
		t.setField("ARTIST", text)
	case "aART":
		t.setField("ALBUMARTIST", text)
	case "\xa9alb":
		t.setField("ALBUM", text)
	case "\xa9day":
		t.setField("DATE", text)
	case "\xa9gen":
		t.setField("GENRE", text)
	case "trkn", "disk":
		if len(data) >= 6 {
			n, total := be16(data[2:]), be16(data[4:])
			v := strconv.Itoa(n)
			if total > 0 {
				v += "/" + strconv.Itoa(total)
			}
			t.setField(map[string]string{"trkn": "TRACKNUMBER", "disk": "DISCNUMBER"}[typ], v)
		}
	case "covr":
		t.HasPicture = true
	case "----":
		if kind == 1 && name != "" {
			t.setField(name, text)
		}
	}
}
