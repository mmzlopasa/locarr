// Package tags reads the tags and stream facts of audio files: FLAC (Vorbis
// comments, STREAMINFO), MP3 (ID3v2.2/2.3/2.4 and the MPEG frame or Xing/Info
// header), MP4/M4A (iTunes-style ilst atoms, AAC or ALAC) and Ogg (Vorbis and
// Opus comment headers). It is a small reader written from the public format
// descriptions (D-038); it only reads, except WriteFLAC which rewrites the
// Vorbis comment block of a FLAC file.
package tags

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Formats.
const (
	FormatFLAC   = "flac"
	FormatMP3    = "mp3"
	FormatAAC    = "aac"
	FormatALAC   = "alac"
	FormatVorbis = "vorbis"
	FormatOpus   = "opus"
	FormatWAV    = "wav"
)

// ErrUnsupported is returned for a file that is not a known audio format.
var ErrUnsupported = errors.New("not a supported audio file")

// ErrCorrupt is returned for a file whose structure cannot be read.
var ErrCorrupt = errors.New("audio file structure is damaged")

// Tags is what a file says about itself.
type Tags struct {
	Title       string `json:"title,omitempty"`
	Artist      string `json:"artist,omitempty"`
	AlbumArtist string `json:"albumArtist,omitempty"`
	Album       string `json:"album,omitempty"`
	Date        string `json:"date,omitempty"`
	Track       int    `json:"track,omitempty"`
	TrackTotal  int    `json:"trackTotal,omitempty"`
	Disc        int    `json:"disc,omitempty"`
	DiscTotal   int    `json:"discTotal,omitempty"`
	Genre       string `json:"genre,omitempty"`

	// MusicBrainz identifiers as tagging tools write them.
	RecordingID    string `json:"recordingId,omitempty"`    // "MusicBrainz Track Id"
	TrackID        string `json:"trackId,omitempty"`        // "MusicBrainz Release Track Id"
	ReleaseID      string `json:"releaseId,omitempty"`      // "MusicBrainz Album Id"
	ReleaseGroupID string `json:"releaseGroupId,omitempty"` // "MusicBrainz Release Group Id"
	ArtistID       string `json:"artistId,omitempty"`
	AlbumArtistID  string `json:"albumArtistId,omitempty"`

	// Stream facts.
	Format     string        `json:"format"`
	Duration   time.Duration `json:"duration"`
	SampleRate int           `json:"sampleRate,omitempty"`
	BitDepth   int           `json:"bitDepth,omitempty"`
	Channels   int           `json:"channels,omitempty"`
	// Bitrate is in kbit/s (average for VBR files).
	Bitrate int  `json:"bitrate,omitempty"`
	VBR     bool `json:"vbr,omitempty"`
	// HasPicture: an embedded cover.
	HasPicture bool `json:"hasPicture,omitempty"`
}

// Year is the four-digit year of Date (0 when unknown).
func (t *Tags) Year() int {
	if len(t.Date) >= 4 {
		if y, err := strconv.Atoi(t.Date[:4]); err == nil {
			return y
		}
	}
	return 0
}

// Lossless reports a lossless format.
func (t *Tags) Lossless() bool {
	return t.Format == FormatFLAC || t.Format == FormatALAC || t.Format == FormatWAV
}

// Extensions are the audio file extensions the library imports.
var Extensions = map[string]bool{".flac": true, ".mp3": true, ".m4a": true, ".m4b": false, ".mp4": false, ".aac": false,
	".ogg": true, ".oga": true, ".opus": true, ".wav": false, ".alac": true}

// IsAudio reports an importable audio file by its extension.
func IsAudio(path string) bool { return Extensions[strings.ToLower(filepath.Ext(path))] }

// maxHeader bounds how much of a file the reader looks at for tags (covers
// can be large; anything past this is ignored).
const maxHeader = 64 << 20

// Read reads the tags of one file.
func Read(path string) (Tags, error) {
	f, err := os.Open(path) // #nosec G304 -- callers pass paths checked with fs.SafePath
	if err != nil {
		return Tags{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return Tags{}, err
	}
	var magic [12]byte
	n, _ := io.ReadFull(f, magic[:])
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return Tags{}, err
	}
	var t Tags
	switch {
	case n >= 4 && string(magic[:4]) == "fLaC":
		t, err = readFLAC(f)
	case n >= 4 && string(magic[:4]) == "OggS":
		t, err = readOgg(f, st.Size())
	case n >= 8 && string(magic[4:8]) == "ftyp":
		t, err = readMP4(f, st.Size())
	case n >= 3 && string(magic[:3]) == "ID3", n >= 2 && magic[0] == 0xFF && magic[1]&0xE0 == 0xE0:
		t, err = readMP3(f, st.Size())
	default:
		return Tags{}, fmt.Errorf("%w: %s", ErrUnsupported, filepath.Base(path))
	}
	if err != nil {
		return t, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if t.Bitrate == 0 && t.Duration > 0 {
		t.Bitrate = int(float64(st.Size()) * 8 / t.Duration.Seconds() / 1000)
	}
	return t, nil
}

// setField stores a Vorbis-comment-style field (also used for MP4 freeform
// and ID3 TXXX names, upper-cased with spaces kept).
func (t *Tags) setField(key, val string) {
	val = strings.TrimSpace(strings.TrimRight(val, "\x00"))
	if val == "" {
		return
	}
	k := strings.ToUpper(strings.TrimSpace(key))
	k = strings.ReplaceAll(k, " ", "_")
	switch k {
	case "TITLE":
		t.Title = first(t.Title, val)
	case "ARTIST":
		t.Artist = first(t.Artist, val)
	case "ALBUMARTIST", "ALBUM_ARTIST":
		t.AlbumArtist = first(t.AlbumArtist, val)
	case "ALBUM":
		t.Album = first(t.Album, val)
	case "DATE", "YEAR", "ORIGINALDATE":
		if t.Date == "" || k == "DATE" {
			t.Date = val
		}
	case "GENRE":
		t.Genre = first(t.Genre, val)
	case "TRACKNUMBER", "TRACK":
		t.Track, t.TrackTotal = numPair(val, t.TrackTotal)
	case "TRACKTOTAL", "TOTALTRACKS":
		t.TrackTotal = atoi(val)
	case "DISCNUMBER", "DISC":
		t.Disc, t.DiscTotal = numPair(val, t.DiscTotal)
	case "DISCTOTAL", "TOTALDISCS":
		t.DiscTotal = atoi(val)
	case "MUSICBRAINZ_TRACKID", "MUSICBRAINZ_TRACK_ID":
		t.RecordingID = lowerID(val)
	case "MUSICBRAINZ_RELEASETRACKID", "MUSICBRAINZ_RELEASE_TRACK_ID":
		t.TrackID = lowerID(val)
	case "MUSICBRAINZ_ALBUMID", "MUSICBRAINZ_ALBUM_ID":
		t.ReleaseID = lowerID(val)
	case "MUSICBRAINZ_RELEASEGROUPID", "MUSICBRAINZ_RELEASE_GROUP_ID":
		t.ReleaseGroupID = lowerID(val)
	case "MUSICBRAINZ_ARTISTID", "MUSICBRAINZ_ARTIST_ID":
		t.ArtistID = lowerID(firstOf(val))
	case "MUSICBRAINZ_ALBUMARTISTID", "MUSICBRAINZ_ALBUM_ARTIST_ID":
		t.AlbumArtistID = lowerID(firstOf(val))
	}
}

func first(cur, v string) string {
	if cur != "" {
		return cur
	}
	return v
}

// firstOf returns the first of several ids joined by "/" or ";".
func firstOf(v string) string {
	if i := strings.IndexAny(v, "/;"); i > 0 {
		return strings.TrimSpace(v[:i])
	}
	return v
}

func lowerID(v string) string { return strings.ToLower(strings.TrimSpace(v)) }

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// numPair reads "3" or "3/12".
func numPair(s string, total int) (int, int) {
	a, b, ok := strings.Cut(s, "/")
	if ok && atoi(b) > 0 {
		total = atoi(b)
	}
	return atoi(a), total
}

// parseComments reads a Vorbis comment block (vendor + list), little endian.
func (t *Tags) parseComments(b []byte) error {
	r := &le{b: b}
	vendor, ok := r.u32()
	if !ok || !r.skip(int(vendor)) {
		return ErrCorrupt
	}
	count, ok := r.u32()
	if !ok {
		return ErrCorrupt
	}
	for i := uint32(0); i < count; i++ {
		n, ok := r.u32()
		if !ok {
			return ErrCorrupt
		}
		s, ok := r.bytes(int(n))
		if !ok {
			return ErrCorrupt
		}
		if k, v, found := strings.Cut(string(s), "="); found {
			t.setField(k, v)
		}
	}
	return nil
}

type le struct {
	b   []byte
	off int
}

func (r *le) u32() (uint32, bool) {
	if r.off+4 > len(r.b) {
		return 0, false
	}
	v := uint32(r.b[r.off]) | uint32(r.b[r.off+1])<<8 | uint32(r.b[r.off+2])<<16 | uint32(r.b[r.off+3])<<24
	r.off += 4
	return v, true
}

func (r *le) skip(n int) bool {
	if n < 0 || r.off+n > len(r.b) {
		return false
	}
	r.off += n
	return true
}

func (r *le) bytes(n int) ([]byte, bool) {
	if n < 0 || r.off+n > len(r.b) {
		return nil, false
	}
	out := r.b[r.off : r.off+n]
	r.off += n
	return out, true
}

func be16(b []byte) int { return int(b[0])<<8 | int(b[1]) }
func be32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
func be64(b []byte) uint64 { return uint64(be32(b))<<32 | uint64(be32(b[4:])) }

func readFull(r io.Reader, n int) ([]byte, error) {
	if n < 0 || n > maxHeader {
		return nil, ErrCorrupt
	}
	// Grow as data arrives, so a damaged length never allocates much.
	b, err := io.ReadAll(io.LimitReader(r, int64(n)))
	if err != nil || len(b) != n {
		return nil, ErrCorrupt
	}
	return b, nil
}
