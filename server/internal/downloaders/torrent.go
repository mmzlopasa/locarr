package downloaders

import (
	"bytes"
	"crypto/sha1" // #nosec G505 -- BitTorrent v1 info hashes are SHA-1 by definition
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// ErrBadTorrent is returned for data that is not a valid .torrent file.
var ErrBadTorrent = errors.New("not a valid torrent file")

// TorrentInfo is what Locarr reads from a .torrent file.
type TorrentInfo struct {
	InfoHash string // lower-case hex (v1)
	Name     string
	Size     int64
}

// ParseTorrent validates a .torrent file and returns its v1 info hash, name and size.
func ParseTorrent(b []byte) (TorrentInfo, error) {
	d := &bdecoder{b: b}
	if d.peek() != 'd' {
		return TorrentInfo{}, ErrBadTorrent
	}
	var (
		info      TorrentInfo
		infoStart = -1
		infoEnd   = -1
	)
	d.pos++
	for d.peek() != 'e' {
		key, err := d.str()
		if err != nil {
			return TorrentInfo{}, ErrBadTorrent
		}
		start := d.pos
		if key == "info" {
			v, err := d.value(0)
			if err != nil {
				return TorrentInfo{}, ErrBadTorrent
			}
			infoStart, infoEnd = start, d.pos
			m, ok := v.(map[string]any)
			if !ok {
				return TorrentInfo{}, ErrBadTorrent
			}
			info.Name, _ = m["name"].(string)
			if l, ok := m["length"].(int64); ok {
				info.Size = l
			}
			if files, ok := m["files"].([]any); ok {
				for _, f := range files {
					if fm, ok := f.(map[string]any); ok {
						if l, ok := fm["length"].(int64); ok {
							info.Size += l
						}
					}
				}
			}
			continue
		}
		if _, err := d.value(0); err != nil {
			return TorrentInfo{}, ErrBadTorrent
		}
	}
	if infoStart < 0 {
		return TorrentInfo{}, ErrBadTorrent
	}
	sum := sha1.Sum(b[infoStart:infoEnd]) // #nosec G401 -- see import
	info.InfoHash = hex.EncodeToString(sum[:])
	return info, nil
}

type bdecoder struct {
	b   []byte
	pos int
}

func (d *bdecoder) peek() byte {
	if d.pos >= len(d.b) {
		return 0
	}
	return d.b[d.pos]
}

func (d *bdecoder) str() (string, error) {
	i := bytes.IndexByte(d.b[d.pos:], ':')
	if i <= 0 || i > 10 {
		return "", ErrBadTorrent
	}
	n, err := strconv.Atoi(string(d.b[d.pos : d.pos+i]))
	if err != nil || n < 0 || d.pos+i+1+n > len(d.b) {
		return "", ErrBadTorrent
	}
	s := string(d.b[d.pos+i+1 : d.pos+i+1+n])
	d.pos += i + 1 + n
	return s, nil
}

func (d *bdecoder) value(depth int) (any, error) {
	if depth > 64 {
		return nil, ErrBadTorrent
	}
	switch c := d.peek(); {
	case c == 'i':
		end := bytes.IndexByte(d.b[d.pos:], 'e')
		if end < 2 {
			return nil, ErrBadTorrent
		}
		n, err := strconv.ParseInt(string(d.b[d.pos+1:d.pos+end]), 10, 64)
		if err != nil {
			return nil, ErrBadTorrent
		}
		d.pos += end + 1
		return n, nil
	case c == 'l':
		d.pos++
		var out []any
		for d.peek() != 'e' {
			if d.pos >= len(d.b) {
				return nil, ErrBadTorrent
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		d.pos++
		return out, nil
	case c == 'd':
		d.pos++
		out := map[string]any{}
		for d.peek() != 'e' {
			if d.pos >= len(d.b) {
				return nil, ErrBadTorrent
			}
			k, err := d.str()
			if err != nil {
				return nil, err
			}
			v, err := d.value(depth + 1)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		d.pos++
		return out, nil
	case c >= '0' && c <= '9':
		return d.str()
	}
	return nil, ErrBadTorrent
}

// Bencode encodes strings, []byte, ints, lists and string-keyed maps (keys sorted).
func Bencode(v any) []byte {
	var buf bytes.Buffer
	bencode(&buf, v)
	return buf.Bytes()
}

func bencode(buf *bytes.Buffer, v any) {
	switch x := v.(type) {
	case string:
		buf.WriteString(strconv.Itoa(len(x)) + ":" + x)
	case []byte:
		buf.WriteString(strconv.Itoa(len(x)) + ":")
		buf.Write(x)
	case int:
		buf.WriteString("i" + strconv.Itoa(x) + "e")
	case int64:
		buf.WriteString("i" + strconv.FormatInt(x, 10) + "e")
	case []any:
		buf.WriteByte('l')
		for _, e := range x {
			bencode(buf, e)
		}
		buf.WriteByte('e')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		buf.WriteByte('d')
		for _, k := range keys {
			bencode(buf, k)
			bencode(buf, x[k])
		}
		buf.WriteByte('e')
	default:
		panic(fmt.Sprintf("bencode: unsupported %T", v))
	}
}

// MagnetInfo is what Locarr reads from a magnet link.
type MagnetInfo struct {
	InfoHash string // lower-case hex
	Name     string
}

// ParseMagnet validates a magnet link and returns its BitTorrent info hash
// (hex or base32 btih, converted to lower-case hex).
func ParseMagnet(link string) (MagnetInfo, error) {
	u, err := url.Parse(strings.TrimSpace(link))
	if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
		return MagnetInfo{}, fmt.Errorf("%w: not a magnet link", ErrInvalidRequest)
	}
	q := u.Query()
	for _, xt := range q["xt"] {
		h, ok := strings.CutPrefix(strings.ToLower(xt), "urn:btih:")
		if !ok {
			continue
		}
		switch len(h) {
		case 40:
			if _, err := hex.DecodeString(h); err == nil {
				return MagnetInfo{InfoHash: h, Name: q.Get("dn")}, nil
			}
		case 32:
			raw, err := base32.StdEncoding.DecodeString(strings.ToUpper(h))
			if err == nil && len(raw) == 20 {
				return MagnetInfo{InfoHash: hex.EncodeToString(raw), Name: q.Get("dn")}, nil
			}
		}
	}
	return MagnetInfo{}, fmt.Errorf("%w: magnet link has no BitTorrent info hash", ErrInvalidRequest)
}
