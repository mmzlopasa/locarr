package tags

import (
	"bytes"
	"io"
	"strings"
	"time"
	"unicode/utf16"
)

// readMP3 reads an ID3v2 tag (when present) and the first MPEG audio frame
// for the stream facts (Xing/Info header for VBR files and frame counts).
func readMP3(r io.ReadSeeker, size int64) (Tags, error) {
	t := Tags{Format: FormatMP3}
	audioAt := int64(0)
	var h [10]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return t, ErrCorrupt
	}
	if string(h[:3]) == "ID3" {
		n := syncsafe(h[6:10])
		if h[5]&0x10 != 0 {
			n += 10 // footer
		}
		body, err := readFull(r, n)
		if err != nil {
			return t, err
		}
		t.parseID3(h[3], h[5], body)
		audioAt = int64(10 + n)
	}
	if _, err := r.Seek(audioAt, io.SeekStart); err != nil {
		return t, ErrCorrupt
	}
	// Look for the first frame within the next 64 KiB (some files have junk).
	buf := make([]byte, 64<<10)
	n, _ := io.ReadFull(r, buf)
	buf = buf[:n]
	for i := 0; i+4 <= len(buf); i++ {
		if buf[i] != 0xFF || buf[i+1]&0xE0 != 0xE0 {
			continue
		}
		fr, ok := parseFrameHeader(buf[i : i+4])
		if !ok {
			continue
		}
		t.SampleRate, t.Channels = fr.sampleRate, fr.channels
		audio := size - audioAt - int64(i)
		if x := xingOffset(fr); i+x+16 <= len(buf) {
			tag := string(buf[i+x : i+x+4])
			if tag == "Xing" || tag == "Info" {
				flags := be32(buf[i+x+4:])
				p := i + x + 8
				var frames, bytesN uint32
				if flags&1 != 0 && p+4 <= len(buf) {
					frames = be32(buf[p:])
					p += 4
				}
				if flags&2 != 0 && p+4 <= len(buf) {
					bytesN = be32(buf[p:])
				}
				t.VBR = tag == "Xing"
				if frames > 0 && fr.sampleRate > 0 {
					t.Duration = time.Duration(float64(frames) * float64(fr.samples) / float64(fr.sampleRate) * float64(time.Second))
					if bytesN == 0 {
						bytesN = uint32(min(audio, int64(^uint32(0)))) // #nosec G115 -- clamped
					}
					t.Bitrate = int(float64(bytesN) * 8 / t.Duration.Seconds() / 1000)
					if !t.VBR && fr.bitrate > 0 {
						t.Bitrate = fr.bitrate
					}
					return t, nil
				}
			}
		}
		// CBR without a header: duration from the size.
		t.Bitrate = fr.bitrate
		if fr.bitrate > 0 {
			t.Duration = time.Duration(float64(audio) * 8 / float64(fr.bitrate*1000) * float64(time.Second))
		}
		return t, nil
	}
	if t.Title == "" && t.Album == "" {
		return t, ErrCorrupt
	}
	return t, nil
}

type mpegFrame struct {
	version    int // 1, 2 or 25 (2.5)
	layer      int
	bitrate    int // kbit/s
	sampleRate int
	channels   int
	samples    int
}

var bitratesV1L3 = [16]int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
var bitratesV2L3 = [16]int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
var sampleRates = map[int][3]int{1: {44100, 48000, 32000}, 2: {22050, 24000, 16000}, 25: {11025, 12000, 8000}}

func parseFrameHeader(b []byte) (mpegFrame, bool) {
	var f mpegFrame
	switch (b[1] >> 3) & 0x03 {
	case 3:
		f.version = 1
	case 2:
		f.version = 2
	case 0:
		f.version = 25
	default:
		return f, false
	}
	if (b[1]>>1)&0x03 != 1 { // layer III only
		return f, false
	}
	f.layer = 3
	bi := int(b[2] >> 4)
	si := int((b[2] >> 2) & 0x03)
	if bi == 0 || bi == 15 || si == 3 {
		return f, false
	}
	if f.version == 1 {
		f.bitrate = bitratesV1L3[bi]
		f.samples = 1152
	} else {
		f.bitrate = bitratesV2L3[bi]
		f.samples = 576
	}
	f.sampleRate = sampleRates[f.version][si]
	f.channels = 2
	if b[3]>>6 == 3 {
		f.channels = 1
	}
	return f, true
}

// xingOffset is where a Xing/Info header starts after the frame header (side
// information size + 4).
func xingOffset(f mpegFrame) int {
	switch {
	case f.version == 1 && f.channels == 2:
		return 4 + 32
	case f.version == 1:
		return 4 + 17
	case f.channels == 2:
		return 4 + 17
	}
	return 4 + 9
}

func syncsafe(b []byte) int {
	return int(b[0]&0x7F)<<21 | int(b[1]&0x7F)<<14 | int(b[2]&0x7F)<<7 | int(b[3]&0x7F)
}

// unsync removes the 0x00 inserted after every 0xFF by the unsynchronisation scheme.
func unsync(b []byte) []byte { return bytes.ReplaceAll(b, []byte{0xFF, 0x00}, []byte{0xFF}) }

var id3v22 = map[string]string{"TT2": "TIT2", "TP1": "TPE1", "TP2": "TPE2", "TAL": "TALB", "TRK": "TRCK", "TPA": "TPOS",
	"TYE": "TYER", "TCO": "TCON", "TXX": "TXXX", "UFI": "UFID", "PIC": "APIC"}

func (t *Tags) parseID3(major, flags byte, body []byte) {
	if flags&0x80 != 0 && major < 4 {
		body = unsync(body)
	}
	p := 0
	if flags&0x40 != 0 && len(body) >= 4 { // extended header
		var n int
		if major >= 4 {
			n = syncsafe(body[:4])
		} else {
			n = int(be32(body)) + 4
		}
		p = n
	}
	for p < len(body) {
		var id string
		var size, hdr int
		var fflags int
		switch {
		case major == 2:
			if p+6 > len(body) {
				return
			}
			id = id3v22[string(body[p:p+3])]
			size = int(body[p+3])<<16 | int(body[p+4])<<8 | int(body[p+5])
			hdr = 6
		default:
			if p+10 > len(body) {
				return
			}
			id = string(body[p : p+4])
			if major >= 4 {
				size = syncsafe(body[p+4 : p+8])
			} else {
				size = int(be32(body[p+4:]))
			}
			fflags = be16(body[p+8:])
			hdr = 10
		}
		if body[p] == 0 || size <= 0 || p+hdr+size > len(body) {
			return // padding or damage
		}
		data := body[p+hdr : p+hdr+size]
		p += hdr + size
		if major >= 4 {
			if fflags&0x0008 != 0 || fflags&0x0004 != 0 { // compressed or encrypted
				continue
			}
			if fflags&0x0001 != 0 && len(data) >= 4 { // data length indicator
				data = data[4:]
			}
			if fflags&0x0002 != 0 {
				data = unsync(data)
			}
		}
		t.id3Frame(id, data)
	}
}

func (t *Tags) id3Frame(id string, data []byte) {
	if len(data) == 0 {
		return
	}
	switch id {
	case "TIT2":
		t.setField("TITLE", id3Text(data))
	case "TPE1":
		t.setField("ARTIST", id3Text(data))
	case "TPE2":
		t.setField("ALBUMARTIST", id3Text(data))
	case "TALB":
		t.setField("ALBUM", id3Text(data))
	case "TRCK":
		t.setField("TRACKNUMBER", id3Text(data))
	case "TPOS":
		t.setField("DISCNUMBER", id3Text(data))
	case "TDRC", "TYER":
		t.setField("DATE", id3Text(data))
	case "TCON":
		t.setField("GENRE", id3Text(data))
	case "TXXX":
		desc, val := id3Pair(data)
		t.setField(desc, val)
	case "UFID":
		owner, id, ok := bytes.Cut(data, []byte{0})
		if ok && strings.Contains(string(owner), "musicbrainz.org") {
			t.RecordingID = lowerID(string(id))
		}
	case "APIC":
		t.HasPicture = true
	}
}

// id3Text decodes a text frame (first value of a v2.4 list).
func id3Text(data []byte) string {
	s := decodeID3(data[0], data[1:])
	if i := strings.IndexByte(s, 0); i >= 0 {
		s = s[:i]
	}
	return s
}

// id3Pair decodes TXXX: description, value.
func id3Pair(data []byte) (string, string) {
	enc := data[0]
	s := decodeID3(enc, data[1:])
	d, v, _ := strings.Cut(s, "\x00")
	return d, strings.TrimRight(v, "\x00")
}

func decodeID3(enc byte, b []byte) string {
	switch enc {
	case 1, 2:
		bigEndian := enc == 2
		if len(b) >= 2 {
			switch {
			case b[0] == 0xFF && b[1] == 0xFE:
				bigEndian, b = false, b[2:]
			case b[0] == 0xFE && b[1] == 0xFF:
				bigEndian, b = true, b[2:]
			}
		}
		u := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			var c uint16
			if bigEndian {
				c = uint16(b[i])<<8 | uint16(b[i+1])
			} else {
				c = uint16(b[i+1])<<8 | uint16(b[i])
			}
			// A BOM may start the value after the description.
			if c == 0xFEFF && i > 0 && len(u) > 0 && u[len(u)-1] == 0 {
				continue
			}
			if c == 0xFFFE && i > 0 && len(u) > 0 && u[len(u)-1] == 0 {
				bigEndian = !bigEndian
				continue
			}
			u = append(u, c)
		}
		return string(utf16.Decode(u))
	case 3:
		return string(b)
	}
	// ISO-8859-1.
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}
