// Package tracks reads the track list of a video file (Matroska/WebM and
// MP4/MOV) in pure Go: kind, codec, language, name and the default, forced
// and hearing-impaired flags. Only the headers are read, never the media
// data, so it is fast on large files. The image carries no ffprobe (D-033).
package tracks

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/languages"
)

// Track kinds.
const (
	Video    = "video"
	Audio    = "audio"
	Subtitle = "subtitle"
)

// Track is one stream of a video file.
type Track struct {
	// Index counts tracks of the same kind from 0 (ffmpeg's 0:s:N order).
	Index int    `json:"index"`
	Kind  string `json:"kind"`
	Codec string `json:"codec"`
	// Language is a catalogue code (internal/languages); "" = not set.
	Language        string `json:"language"`
	RawLanguage     string `json:"rawLanguage,omitempty"`
	Name            string `json:"name,omitempty"`
	Default         bool   `json:"default"`
	Forced          bool   `json:"forced"`
	HearingImpaired bool   `json:"hearingImpaired"`
	// Text is set for subtitle tracks that are text (SRT/ASS/WebVTT/tx3g),
	// which can be extracted and used as a sync reference.
	Text bool `json:"text"`
}

// ErrUnsupported is returned for containers other than Matroska and MP4.
var ErrUnsupported = errors.New("unsupported container (only Matroska and MP4 track headers are read)")

// Read returns the tracks of the file at path.
func Read(path string) ([]Track, error) {
	f, err := os.Open(path) // #nosec G304 -- callers pass paths checked with fs.SafePath
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ReadFrom(f)
}

// ReadFrom reads tracks from a seekable stream.
func ReadFrom(r io.ReadSeeker) ([]Track, error) {
	var head [12]byte
	n, err := io.ReadFull(r, head[:])
	if err != nil && n < 8 {
		return nil, ErrUnsupported
	}
	if _, err := r.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var out []Track
	switch {
	case binary.BigEndian.Uint32(head[:4]) == idEBML:
		out, err = readMKV(r)
	case string(head[4:8]) == "ftyp" || string(head[4:8]) == "moov" || string(head[4:8]) == "mdat" ||
		string(head[4:8]) == "free" || string(head[4:8]) == "wide" || string(head[4:8]) == "skip":
		out, err = readMP4(r)
	default:
		return nil, ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	counts := map[string]int{}
	for i := range out {
		out[i].Index = counts[out[i].Kind]
		counts[out[i].Kind]++
		out[i].Language = languages.FromContainer(out[i].RawLanguage)
	}
	return out, nil
}

// --- Matroska ---------------------------------------------------------------

const (
	idEBML            = 0x1A45DFA3
	idSegment         = 0x18538067
	idTracks          = 0x1654AE6B
	idTrackEntry      = 0xAE
	idTrackType       = 0x83
	idCodecID         = 0x86
	idLanguage        = 0x22B59C
	idLanguageBCP47   = 0x22B59D
	idName            = 0x536E
	idFlagDefault     = 0x88
	idFlagForced      = 0x55AA
	idFlagHearingImp  = 0x55AB
	idCluster         = 0x1F43B675
	maxTracksElement  = 16 << 20
	maxHeaderElements = 10000
)

type ebml struct {
	r   *bufio.Reader
	s   io.ReadSeeker
	pos int64
}

func (e *ebml) byte() (byte, error) {
	b, err := e.r.ReadByte()
	if err == nil {
		e.pos++
	}
	return b, err
}

// vint reads an EBML variable-length integer; keepMarker keeps the length
// bit (element ids) or drops it (sizes). unknown reports an all-ones size.
func (e *ebml) vint(keepMarker bool) (v uint64, unknown bool, err error) {
	b, err := e.byte()
	if err != nil {
		return 0, false, err
	}
	l := 1
	for mask := byte(0x80); l <= 8 && b&mask == 0; mask >>= 1 {
		l++
	}
	if l > 8 {
		return 0, false, fmt.Errorf("invalid EBML length at %d", e.pos-1)
	}
	if keepMarker {
		v = uint64(b)
	} else {
		v = uint64(b & (0xFF >> l))
	}
	allOnes := v == uint64(0xFF>>l)
	for i := 1; i < l; i++ {
		c, err := e.byte()
		if err != nil {
			return 0, false, err
		}
		v = v<<8 | uint64(c)
		if c != 0xFF {
			allOnes = false
		}
	}
	return v, !keepMarker && allOnes, nil
}

func (e *ebml) skip(n uint64) error {
	if n > 1<<50 {
		return fmt.Errorf("element of %d bytes is too large", n)
	}
	size := int64(n) // #nosec G115 -- bounded above
	if size <= int64(e.r.Buffered()) {
		_, err := e.r.Discard(int(size))
		e.pos += size
		return err
	}
	if _, err := e.s.Seek(e.pos+size, io.SeekStart); err != nil {
		return err
	}
	e.pos += size
	e.r.Reset(e.s)
	return nil
}

func (e *ebml) read(n uint64) ([]byte, error) {
	if n > maxTracksElement {
		return nil, fmt.Errorf("element of %d bytes is too large", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(e.r, b); err != nil {
		return nil, err
	}
	e.pos += int64(n)
	return b, nil
}

func readMKV(r io.ReadSeeker) ([]Track, error) {
	e := &ebml{r: bufio.NewReaderSize(r, 64<<10), s: r}
	for range maxHeaderElements {
		id, _, err := e.vint(true)
		if err != nil {
			return nil, fmt.Errorf("no track list in the Matroska file: %w", err)
		}
		size, unknown, err := e.vint(false)
		if err != nil {
			return nil, err
		}
		switch id {
		case idSegment:
			continue // descend
		case idTracks:
			if unknown {
				return nil, errors.New("track list of unknown size")
			}
			b, err := e.read(size)
			if err != nil {
				return nil, err
			}
			return parseMKVTracks(b)
		case idCluster:
			return nil, errors.New("media data before the track list")
		}
		if unknown {
			return nil, errors.New("element of unknown size before the track list")
		}
		if err := e.skip(size); err != nil {
			return nil, err
		}
	}
	return nil, errors.New("no track list in the first elements of the Matroska file")
}

// children splits an element body into (id, body) pairs.
func children(b []byte) ([][2]any, error) {
	var out [][2]any
	e := &ebml{r: bufio.NewReader(strings.NewReader(string(b)))}
	for e.pos < int64(len(b)) {
		id, _, err := e.vint(true)
		if err != nil {
			return out, err
		}
		size, _, err := e.vint(false)
		if err != nil {
			return out, err
		}
		if size > uint64(len(b)) || e.pos+int64(size) > int64(len(b)) { // #nosec G115 -- size is checked against len(b) first
			return out, errors.New("truncated element")
		}
		n := int(size) // #nosec G115 -- at most len(b)
		body := b[e.pos : e.pos+int64(n)]
		if _, err := e.r.Discard(n); err != nil {
			return out, err
		}
		e.pos += int64(n)
		out = append(out, [2]any{id, body})
	}
	return out, nil
}

func uintOf(b []byte) uint64 {
	var v uint64
	for _, c := range b {
		v = v<<8 | uint64(c)
	}
	return v
}

func parseMKVTracks(b []byte) ([]Track, error) {
	entries, err := children(b)
	if err != nil && len(entries) == 0 {
		return nil, err
	}
	var out []Track
	for _, en := range entries {
		if en[0].(uint64) != idTrackEntry {
			continue
		}
		fields, _ := children(en[1].([]byte))
		t := Track{Default: true, RawLanguage: "eng"} // Matroska defaults
		typ := uint64(0)
		bcp := ""
		for _, f := range fields {
			v := f[1].([]byte)
			switch f[0].(uint64) {
			case idTrackType:
				typ = uintOf(v)
			case idCodecID:
				t.Codec = strings.TrimRight(string(v), "\x00")
			case idLanguage:
				t.RawLanguage = strings.TrimRight(string(v), "\x00")
			case idLanguageBCP47:
				bcp = strings.TrimRight(string(v), "\x00")
			case idName:
				t.Name = strings.TrimRight(string(v), "\x00")
			case idFlagDefault:
				t.Default = uintOf(v) == 1
			case idFlagForced:
				t.Forced = uintOf(v) == 1
			case idFlagHearingImp:
				t.HearingImpaired = uintOf(v) == 1
			}
		}
		if bcp != "" {
			t.RawLanguage = bcp
		}
		switch typ {
		case 1:
			t.Kind = Video
		case 2:
			t.Kind = Audio
		case 0x11:
			t.Kind = Subtitle
			t.Text = strings.HasPrefix(t.Codec, "S_TEXT/")
		default:
			continue
		}
		t.Codec = mkvCodec(t.Codec)
		nameHints(&t)
		out = append(out, t)
	}
	return out, nil
}

func mkvCodec(c string) string {
	switch c {
	case "S_TEXT/UTF8", "S_TEXT/ASCII":
		return "srt"
	case "S_TEXT/ASS":
		return "ass"
	case "S_TEXT/SSA":
		return "ssa"
	case "S_TEXT/WEBVTT":
		return "webvtt"
	case "S_HDMV/PGS":
		return "pgs"
	case "S_VOBSUB":
		return "vobsub"
	case "S_DVBSUB":
		return "dvbsub"
	case "V_MPEG4/ISO/AVC":
		return "h264"
	case "V_MPEGH/ISO/HEVC":
		return "h265"
	case "V_AV1":
		return "av1"
	case "V_VP9":
		return "vp9"
	case "A_AAC", "A_AAC/MPEG4/LC", "A_AAC/MPEG2/LC":
		return "aac"
	case "A_AC3":
		return "ac3"
	case "A_EAC3":
		return "eac3"
	case "A_DTS":
		return "dts"
	case "A_TRUEHD":
		return "truehd"
	case "A_OPUS":
		return "opus"
	case "A_VORBIS":
		return "vorbis"
	case "A_FLAC":
		return "flac"
	case "A_MPEG/L3":
		return "mp3"
	case "A_PCM/INT/LIT", "A_PCM/INT/BIG":
		return "pcm"
	}
	return strings.ToLower(c)
}

// nameHints reads forced/SDH from track names when the flags are not set
// ("English (Forced)", "SDH").
func nameHints(t *Track) {
	if t.Kind != Subtitle {
		return
	}
	n := strings.ToLower(t.Name)
	if strings.Contains(n, "forced") {
		t.Forced = true
	}
	if strings.Contains(n, "sdh") || strings.Contains(n, "hearing") || strings.Contains(n, "cc") && len(n) <= 12 {
		t.HearingImpaired = true
	}
}

// --- MP4 --------------------------------------------------------------------

type box struct {
	typ        string
	start, end int64 // body range
}

func readBox(r io.ReadSeeker, at, limit int64) (box, error) {
	if _, err := r.Seek(at, io.SeekStart); err != nil {
		return box{}, err
	}
	var h [16]byte
	if _, err := io.ReadFull(r, h[:8]); err != nil {
		return box{}, err
	}
	size := int64(binary.BigEndian.Uint32(h[:4]))
	b := box{typ: string(h[4:8]), start: at + 8}
	switch size {
	case 1:
		if _, err := io.ReadFull(r, h[8:16]); err != nil {
			return box{}, err
		}
		size = int64(binary.BigEndian.Uint64(h[8:16])) // #nosec G115 -- checked below
		b.start = at + 16
	case 0:
		size = limit - at
	}
	b.end = at + size
	if size < b.start-at || b.end > limit {
		return box{}, fmt.Errorf("bad %q box size", b.typ)
	}
	return b, nil
}

func listBoxes(r io.ReadSeeker, start, end int64) ([]box, error) {
	var out []box
	for at := start; at+8 <= end && len(out) < maxHeaderElements; {
		b, err := readBox(r, at, end)
		if err != nil {
			return out, err
		}
		out = append(out, b)
		at = b.end
	}
	return out, nil
}

func find(bs []box, typ string) (box, bool) {
	for _, b := range bs {
		if b.typ == typ {
			return b, true
		}
	}
	return box{}, false
}

func readBody(r io.ReadSeeker, b box, limit int64) ([]byte, error) {
	n := min(b.end-b.start, limit)
	if _, err := r.Seek(b.start, io.SeekStart); err != nil {
		return nil, err
	}
	buf := make([]byte, n)
	_, err := io.ReadFull(r, buf)
	return buf, err
}

func readMP4(r io.ReadSeeker) ([]Track, error) {
	end, err := r.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	top, err := listBoxes(r, 0, end)
	moov, ok := find(top, "moov")
	if !ok {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("no moov box in the MP4 file")
	}
	kids, err := listBoxes(r, moov.start, moov.end)
	if err != nil {
		return nil, err
	}
	var out []Track
	for _, k := range kids {
		if k.typ != "trak" {
			continue
		}
		t, ok, err := readTrak(r, k)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, t)
		}
	}
	return out, nil
}

func readTrak(r io.ReadSeeker, trak box) (Track, bool, error) {
	var t Track
	kids, err := listBoxes(r, trak.start, trak.end)
	if err != nil {
		return t, false, err
	}
	if tkhd, ok := find(kids, "tkhd"); ok {
		b, err := readBody(r, tkhd, 4)
		if err == nil && len(b) == 4 {
			t.Default = b[3]&0x1 != 0 // track_enabled
		}
	}
	mdia, ok := find(kids, "mdia")
	if !ok {
		return t, false, nil
	}
	mk, err := listBoxes(r, mdia.start, mdia.end)
	if err != nil {
		return t, false, err
	}
	if mdhd, ok := find(mk, "mdhd"); ok {
		b, err := readBody(r, mdhd, 40)
		if err == nil && len(b) >= 4 {
			off := 20 // version 0: 4 flags + 4 + 4 + 4 + 4
			if b[0] == 1 {
				off = 32
			}
			if len(b) >= off+2 {
				code := binary.BigEndian.Uint16(b[off:])
				t.RawLanguage = string([]byte{byte(code>>10&0x1f) + 0x60, byte(code>>5&0x1f) + 0x60, byte(code&0x1f) + 0x60})
			}
		}
	}
	if elng, ok := find(mk, "elng"); ok {
		if b, err := readBody(r, elng, 64); err == nil && len(b) > 4 {
			t.RawLanguage = strings.TrimRight(string(b[4:]), "\x00")
		}
	}
	hdlr, ok := find(mk, "hdlr")
	if !ok {
		return t, false, nil
	}
	hb, err := readBody(r, hdlr, 64)
	if err != nil || len(hb) < 12 {
		return t, false, nil
	}
	switch string(hb[8:12]) {
	case "vide":
		t.Kind = Video
	case "soun":
		t.Kind = Audio
	case "sbtl", "subt", "text", "clcp":
		t.Kind = Subtitle
	default:
		return t, false, nil
	}
	if len(hb) > 24 {
		t.Name = strings.TrimRight(string(hb[24:]), "\x00")
	}
	t.Codec = sampleEntry(r, mk)
	if t.Kind == Subtitle {
		t.Text = t.Codec == "tx3g" || t.Codec == "wvtt" || t.Codec == "stpp"
		if t.Codec == "tx3g" {
			t.Codec = "mov_text"
		}
		nameHints(&t)
	}
	return t, true, nil
}

func sampleEntry(r io.ReadSeeker, mdia []box) string {
	minf, ok := find(mdia, "minf")
	if !ok {
		return ""
	}
	mk, err := listBoxes(r, minf.start, minf.end)
	if err != nil {
		return ""
	}
	stbl, ok := find(mk, "stbl")
	if !ok {
		return ""
	}
	sk, err := listBoxes(r, stbl.start, stbl.end)
	if err != nil {
		return ""
	}
	stsd, ok := find(sk, "stsd")
	if !ok {
		return ""
	}
	b, err := readBody(r, stsd, 16)
	if err != nil || len(b) < 16 {
		return ""
	}
	switch c := string(b[12:16]); c {
	case "avc1", "avc3":
		return "h264"
	case "hvc1", "hev1":
		return "h265"
	case "av01":
		return "av1"
	case "mp4a":
		return "aac"
	case "ac-3":
		return "ac3"
	case "ec-3":
		return "eac3"
	default:
		return strings.TrimSpace(c)
	}
}
