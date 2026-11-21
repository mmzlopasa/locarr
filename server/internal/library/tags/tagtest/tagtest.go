// Package tagtest writes small but structurally valid audio files (FLAC, MP3
// with ID3v2.3 or ID3v2.4, M4A with AAC or ALAC, Ogg Vorbis and Opus) with
// the given tags and stream facts. Tests and the fake services use them as
// fixtures; the audio itself is silence-sized filler, not playable sound.
//
// Fields use Vorbis comment names: TITLE, ARTIST, ALBUMARTIST, ALBUM, DATE,
// TRACKNUMBER ("3/12"), DISCNUMBER ("1/2"), MUSICBRAINZ_TRACKID (recording),
// MUSICBRAINZ_RELEASETRACKID, MUSICBRAINZ_ALBUMID (release),
// MUSICBRAINZ_RELEASEGROUPID, MUSICBRAINZ_ARTISTID, MUSICBRAINZ_ALBUMARTISTID.
package tagtest

import (
	"bytes"
	"encoding/binary"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/library/tags"
)

// Fields are tag values by Vorbis comment name.
type Fields map[string]string

// Stream describes the audio.
type Stream struct {
	Duration   time.Duration
	SampleRate int // default 44100
	BitDepth   int // FLAC/ALAC, default 16
	Channels   int // default 2
	// Bitrate (kbit/s) and VBR for MP3/AAC/Vorbis.
	Bitrate int
	VBR     bool
	// Filler is the number of padding bytes standing in for audio (default 4096).
	Filler int
}

func (s Stream) norm() Stream {
	if s.SampleRate == 0 {
		s.SampleRate = 44100
	}
	if s.BitDepth == 0 {
		s.BitDepth = 16
	}
	if s.Channels == 0 {
		s.Channels = 2
	}
	if s.Bitrate == 0 {
		s.Bitrate = 320
	}
	if s.Filler == 0 {
		s.Filler = 4096
	}
	return s
}

func filler(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 31 & 0xFF) // #nosec G115 -- masked
	}
	return b
}

// FLAC returns a FLAC file.
func FLAC(f Fields, s Stream) []byte {
	s = s.norm()
	var b bytes.Buffer
	b.WriteString("fLaC")
	si := make([]byte, 34)
	binary.BigEndian.PutUint16(si[0:], 4096)
	binary.BigEndian.PutUint16(si[2:], 4096)
	samples := uint64(s.Duration.Seconds() * float64(s.SampleRate))
	x := si[10:18]
	x[0] = byte(s.SampleRate >> 12 & 0xFF) // #nosec G115 -- masked
	x[1] = byte(s.SampleRate >> 4 & 0xFF) // #nosec G115 -- masked
	x[2] = byte(s.SampleRate&0x0F)<<4 | byte((s.Channels-1)&0x07)<<1 | byte((s.BitDepth-1)>>4&0x01) // #nosec G115 -- masked
	x[3] = byte((s.BitDepth-1)&0x0F)<<4 | byte(samples>>32&0x0F)
	binary.BigEndian.PutUint32(x[4:], uint32(samples)) // #nosec G115 -- test data
	block := func(typ byte, last bool, data []byte) {
		if last {
			typ |= 0x80
		}
		b.Write([]byte{typ, byte(len(data) >> 16 & 0xFF), byte(len(data) >> 8 & 0xFF), byte(len(data) & 0xFF)}) // #nosec G115 -- masked
		b.Write(data)
	}
	block(0, false, si)
	block(4, false, tags.VorbisComments("tagtest", f))
	block(1, true, make([]byte, 64))
	b.Write([]byte{0xFF, 0xF8}) // frame sync of the (fake) first audio frame
	b.Write(filler(s.Filler))
	return b.Bytes()
}

// txxx maps fields to ID3 TXXX descriptions as tagging tools write them.
var txxx = map[string]string{"MUSICBRAINZ_ALBUMID": "MusicBrainz Album Id", "MUSICBRAINZ_RELEASEGROUPID": "MusicBrainz Release Group Id",
	"MUSICBRAINZ_ARTISTID": "MusicBrainz Artist Id", "MUSICBRAINZ_ALBUMARTISTID": "MusicBrainz Album Artist Id",
	"MUSICBRAINZ_RELEASETRACKID": "MusicBrainz Release Track Id"}

var id3Frames = map[string]string{"TITLE": "TIT2", "ARTIST": "TPE1", "ALBUMARTIST": "TPE2", "ALBUM": "TALB", "TRACKNUMBER": "TRCK",
	"DISCNUMBER": "TPOS", "GENRE": "TCON"}

func syncsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
}

func keys(f Fields) []string {
	out := make([]string, 0, len(f))
	for k := range f {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MP3 returns an MP3 file with an ID3v2 tag of the given major version (3 or
// 4; v2.4 text is UTF-8, v2.3 UTF-16) and a Xing (VBR) or Info (CBR) frame.
func MP3(major int, f Fields, s Stream) []byte {
	s = s.norm()
	var frames bytes.Buffer
	frame := func(id string, data []byte) {
		frames.WriteString(id)
		if major >= 4 {
			frames.Write(syncsafe(len(data)))
		} else {
			_ = binary.Write(&frames, binary.BigEndian, uint32(len(data))) // #nosec G115 -- small
		}
		frames.Write([]byte{0, 0})
		frames.Write(data)
	}
	text := func(s string) []byte {
		if major >= 4 {
			return append([]byte{3}, s...)
		}
		out := []byte{1, 0xFF, 0xFE}
		for _, r := range s {
			if r > 0xFFFF {
				r = '?'
			}
			out = append(out, byte(r&0xFF), byte(r>>8&0xFF)) // #nosec G115 -- masked
		}
		return out
	}
	for _, k := range keys(f) {
		v := f[k]
		switch {
		case id3Frames[k] != "":
			frame(id3Frames[k], text(v))
		case k == "DATE":
			if major >= 4 {
				frame("TDRC", text(v))
			} else {
				frame("TYER", text(v[:min(4, len(v))]))
			}
		case k == "MUSICBRAINZ_TRACKID":
			frame("UFID", append([]byte("http://musicbrainz.org\x00"), v...))
		case txxx[k] != "":
			d := text(txxx[k])
			if major >= 4 {
				d = append(d, 0)
			} else {
				d = append(d, 0, 0, 0xFF, 0xFE)
				for _, r := range v {
					d = append(d, byte(r&0xFF), byte(r>>8&0xFF)) // #nosec G115 -- masked
				}
				frame("TXXX", d)
				continue
			}
			frame("TXXX", append(d, v...))
		}
	}
	frames.Write(make([]byte, 128)) // padding
	var b bytes.Buffer
	b.Write([]byte{'I', 'D', '3', byte(major), 0, 0}) // #nosec G115 -- small test values
	b.Write(syncsafe(frames.Len()))
	b.Write(frames.Bytes())
	// First frame: MPEG-1 layer III, 44.1 kHz, stereo; bitrate index 9 (128) for VBR, else the real one.
	idx := map[int]byte{128: 9, 160: 10, 192: 11, 224: 12, 256: 13, 320: 14}[s.Bitrate]
	if idx == 0 || s.VBR {
		idx = 9
	}
	hdr := []byte{0xFF, 0xFB, idx << 4, 0x00}
	fr := make([]byte, 417)
	copy(fr, hdr)
	tag := "Info"
	if s.VBR {
		tag = "Xing"
	}
	copy(fr[36:], tag)
	binary.BigEndian.PutUint32(fr[40:], 3)
	nframes := uint32(s.Duration.Seconds() * 44100 / 1152)
	nbytes := uint32(s.Duration.Seconds() * float64(s.Bitrate) * 1000 / 8)
	binary.BigEndian.PutUint32(fr[44:], nframes)
	binary.BigEndian.PutUint32(fr[48:], nbytes)
	b.Write(fr)
	b.Write(filler(s.Filler))
	return b.Bytes()
}

func atom(typ string, parts ...[]byte) []byte {
	n := 8
	for _, p := range parts {
		n += len(p)
	}
	out := make([]byte, 8, n)
	binary.BigEndian.PutUint32(out, uint32(n)) // #nosec G115 -- small
	copy(out[4:], typ)
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func u32(v int) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, uint32(v)); return b } // #nosec G115
func u16(v int) []byte { b := make([]byte, 2); binary.BigEndian.PutUint16(b, uint16(v)); return b } // #nosec G115

var mp4Names = map[string]string{"TITLE": "\xa9nam", "ARTIST": "\xa9ART", "ALBUMARTIST": "aART", "ALBUM": "\xa9alb", "DATE": "\xa9day",
	"GENRE": "\xa9gen"}

var mp4Freeform = map[string]string{"MUSICBRAINZ_TRACKID": "MusicBrainz Track Id", "MUSICBRAINZ_ALBUMID": "MusicBrainz Album Id",
	"MUSICBRAINZ_RELEASEGROUPID": "MusicBrainz Release Group Id", "MUSICBRAINZ_ARTISTID": "MusicBrainz Artist Id",
	"MUSICBRAINZ_ALBUMARTISTID": "MusicBrainz Album Artist Id", "MUSICBRAINZ_RELEASETRACKID": "MusicBrainz Release Track Id"}

func pair(v string) (int, int) {
	a, b, _ := strings.Cut(v, "/")
	x, _ := strconv.Atoi(a)
	y, _ := strconv.Atoi(b)
	return x, y
}

// M4A returns an MP4 audio file with AAC (alac=false) or ALAC.
func M4A(alac bool, f Fields, s Stream) []byte {
	s = s.norm()
	data := func(kind int, v []byte) []byte { return atom("data", u32(kind), u32(0), v) }
	var items [][]byte
	for _, k := range keys(f) {
		v := f[k]
		switch {
		case mp4Names[k] != "":
			items = append(items, atom(mp4Names[k], data(1, []byte(v))))
		case k == "TRACKNUMBER" || k == "DISCNUMBER":
			n, total := pair(v)
			name := map[string]string{"TRACKNUMBER": "trkn", "DISCNUMBER": "disk"}[k]
			items = append(items, atom(name, data(0, append(append(append(u16(0), u16(n)...), u16(total)...), u16(0)...))))
		case mp4Freeform[k] != "":
			items = append(items, atom("----", atom("mean", u32(0), []byte("com.apple.iTunes")), atom("name", u32(0), []byte(mp4Freeform[k])),
				data(1, []byte(v))))
		}
	}
	ilst := atom("ilst", items...)
	meta := atom("meta", u32(0), atom("hdlr", u32(0), u32(0), []byte("mdir"), make([]byte, 13)), ilst)
	scale := 1000
	mvhd := atom("mvhd", u32(0), u32(0), u32(0), u32(scale), u32(int(s.Duration.Milliseconds())), make([]byte, 80))
	format := "mp4a"
	size := 16
	if alac {
		format, size = "alac", s.BitDepth
	}
	entry := atom(format, make([]byte, 6), u16(1), u16(0), u16(0), u32(0), u16(s.Channels), u16(size), u16(0), u16(0), u32(s.SampleRate<<16))
	stsd := atom("stsd", u32(0), u32(1), entry)
	trak := atom("trak", atom("mdia", atom("minf", atom("stbl", stsd))))
	moov := atom("moov", mvhd, trak, atom("udta", meta))
	ftyp := atom("ftyp", []byte("M4A "), u32(0), []byte("M4A mp42isom"))
	mdat := atom("mdat", filler(s.Filler))
	return append(append(ftyp, moov...), mdat...)
}

var crcTable = func() [256]uint32 {
	var t [256]uint32
	for i := range t {
		r := uint32(i) << 24 // #nosec G115
		for j := 0; j < 8; j++ {
			if r&0x80000000 != 0 {
				r = r<<1 ^ 0x04c11db7
			} else {
				r <<= 1
			}
		}
		t[i] = r
	}
	return t
}()

func oggPage(headerType byte, granule int64, seq uint32, packet []byte) []byte {
	var segs []byte
	n := len(packet)
	for n >= 255 {
		segs = append(segs, 255)
		n -= 255
	}
	segs = append(segs, byte(n&0xFF)) // #nosec G115 -- below 255
	p := make([]byte, 27, 27+len(segs)+len(packet))
	copy(p, "OggS")
	p[5] = headerType
	binary.LittleEndian.PutUint64(p[6:], uint64(granule)) // #nosec G115
	binary.LittleEndian.PutUint32(p[14:], 0x4C4F4341)
	binary.LittleEndian.PutUint32(p[18:], seq)
	p[26] = byte(len(segs) & 0xFF) // #nosec G115 -- test pages are small
	p = append(p, segs...)
	p = append(p, packet...)
	var crc uint32
	for _, c := range p {
		crc = crc<<8 ^ crcTable[byte(crc>>24)^c]
	}
	binary.LittleEndian.PutUint32(p[22:], crc)
	return p
}

// Ogg returns an Ogg Vorbis (opus=false) or Ogg Opus file.
func Ogg(opus bool, f Fields, s Stream) []byte {
	s = s.norm()
	var id, comments []byte
	var granule int64
	if opus {
		id = []byte("OpusHead")
		id = append(id, 1, byte(s.Channels)) // #nosec G115 -- small test values
		id = binary.LittleEndian.AppendUint16(id, 312)
		id = binary.LittleEndian.AppendUint32(id, uint32(s.SampleRate)) // #nosec G115
		id = append(id, 0, 0, 0)
		comments = append([]byte("OpusTags"), tags.VorbisComments("tagtest", f)...)
		granule = int64(s.Duration.Seconds()*48000) + 312
	} else {
		id = []byte("\x01vorbis")
		id = binary.LittleEndian.AppendUint32(id, 0)
		id = append(id, byte(s.Channels)) // #nosec G115 -- small test values
		id = binary.LittleEndian.AppendUint32(id, uint32(s.SampleRate)) // #nosec G115
		id = binary.LittleEndian.AppendUint32(id, 0)
		id = binary.LittleEndian.AppendUint32(id, uint32(s.Bitrate*1000)) // #nosec G115
		id = binary.LittleEndian.AppendUint32(id, 0)
		id = append(id, 0xB8, 1)
		comments = append([]byte("\x03vorbis"), tags.VorbisComments("tagtest", f)...)
		comments = append(comments, 1)
		granule = int64(s.Duration.Seconds() * float64(s.SampleRate))
	}
	out := oggPage(0x02, 0, 0, id)
	out = append(out, oggPage(0, 0, 1, comments)...)
	out = append(out, oggPage(0x04, granule, 2, filler(min(s.Filler, 250)))...)
	return out
}
