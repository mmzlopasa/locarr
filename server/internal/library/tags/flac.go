package tags

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FLAC metadata block types.
const (
	flacStreamInfo    = 0
	flacPadding       = 1
	flacVorbisComment = 4
	flacPicture       = 6
)

type flacBlock struct {
	typ  byte
	data []byte
}

// readFLACBlocks reads the metadata blocks after the "fLaC" marker; audio
// starts at the returned offset.
func readFLACBlocks(r io.Reader, keep func(typ byte) bool) ([]flacBlock, int64, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil || string(magic[:]) != "fLaC" {
		return nil, 0, ErrCorrupt
	}
	off := int64(4)
	var out []flacBlock
	for i := 0; i < 1024; i++ {
		var h [4]byte
		if _, err := io.ReadFull(r, h[:]); err != nil {
			return out, off, ErrCorrupt
		}
		last := h[0]&0x80 != 0
		typ := h[0] & 0x7F
		n := int(h[1])<<16 | int(h[2])<<8 | int(h[3])
		off += 4 + int64(n)
		if keep(typ) {
			b, err := readFull(r, n)
			if err != nil {
				return out, off, err
			}
			out = append(out, flacBlock{typ, b})
		} else if _, err := io.CopyN(io.Discard, r, int64(n)); err != nil {
			return out, off, ErrCorrupt
		}
		if last {
			return out, off, nil
		}
	}
	return out, off, ErrCorrupt
}

func readFLAC(r io.Reader) (Tags, error) {
	t := Tags{Format: FormatFLAC}
	blocks, _, err := readFLACBlocks(r, func(typ byte) bool { return typ == flacStreamInfo || typ == flacVorbisComment })
	for _, b := range blocks {
		switch b.typ {
		case flacStreamInfo:
			if len(b.data) < 18 {
				return t, ErrCorrupt
			}
			// 20 bits sample rate, 3 bits channels-1, 5 bits bps-1, 36 bits samples.
			x := b.data[10:18]
			t.SampleRate = int(x[0])<<12 | int(x[1])<<4 | int(x[2])>>4
			t.Channels = int(x[2]>>1&0x07) + 1
			t.BitDepth = int((x[2]&0x01)<<4|x[3]>>4) + 1
			samples := uint64(x[3]&0x0F)<<32 | uint64(x[4])<<24 | uint64(x[5])<<16 | uint64(x[6])<<8 | uint64(x[7])
			if t.SampleRate > 0 {
				t.Duration = time.Duration(float64(samples) / float64(t.SampleRate) * float64(time.Second))
			}
		case flacVorbisComment:
			if err := t.parseComments(b.data); err != nil {
				return t, err
			}
		}
	}
	if err != nil {
		// Readable stream info and comments before a damaged block are kept.
		if t.SampleRate == 0 {
			return t, err
		}
	}
	return t, nil
}

// VorbisComments builds a Vorbis comment block body from fields (sorted keys).
func VorbisComments(vendor string, fields map[string]string) []byte {
	var b bytes.Buffer
	w32 := func(v int) { _ = binary.Write(&b, binary.LittleEndian, uint32(v)) } // #nosec G115 -- small lengths
	w32(len(vendor))
	b.WriteString(vendor)
	keys := make([]string, 0, len(fields))
	for k, v := range fields {
		if v != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	w32(len(keys))
	for _, k := range keys {
		s := strings.ToUpper(k) + "=" + fields[k]
		w32(len(s))
		b.WriteString(s)
	}
	return b.Bytes()
}

// WriteFLAC replaces the Vorbis comments of a FLAC file with fields (other
// blocks and the audio stay as they are). The new file is written next to
// the old one and renamed over it, so a failure leaves the original intact.
// Callers must never point it at a hardlink shared with a download.
func WriteFLAC(path string, fields map[string]string) error {
	f, err := os.Open(path) // #nosec G304 -- library path checked by the caller
	if err != nil {
		return err
	}
	blocks, audioAt, err := readFLACBlocks(f, func(byte) bool { return true })
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	var out []flacBlock
	for _, b := range blocks {
		if b.typ == flacVorbisComment || b.typ == flacPadding {
			continue
		}
		out = append(out, b)
	}
	// Stream info first, then the new comments, then the rest, then padding.
	comments := flacBlock{typ: flacVorbisComment, data: VorbisComments("Locarr", fields)}
	if len(out) > 0 && out[0].typ == flacStreamInfo {
		out = append([]flacBlock{out[0], comments}, out[1:]...)
	} else {
		out = append([]flacBlock{comments}, out...)
	}
	out = append(out, flacBlock{typ: flacPadding, data: make([]byte, 1024)})
	tmp := path + ".locarr-tags"
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644) // #nosec G302,G304 -- library file
	if err != nil {
		_ = f.Close()
		return err
	}
	cleanup := func(e error) error {
		_ = w.Close()
		_ = os.Remove(tmp)
		_ = f.Close()
		return e
	}
	if _, err := w.Write([]byte("fLaC")); err != nil {
		return cleanup(err)
	}
	for i, b := range out {
		h := []byte{b.typ, byte(len(b.data) >> 16), byte(len(b.data) >> 8), byte(len(b.data))}
		if i == len(out)-1 {
			h[0] |= 0x80
		}
		if _, err := w.Write(h); err != nil {
			return cleanup(err)
		}
		if _, err := w.Write(b.data); err != nil {
			return cleanup(err)
		}
	}
	if _, err := f.Seek(audioAt, io.SeekStart); err != nil {
		return cleanup(err)
	}
	if _, err := io.Copy(w, f); err != nil {
		return cleanup(err)
	}
	if st, err := f.Stat(); err == nil {
		_ = w.Chmod(st.Mode().Perm())
	}
	_ = f.Close()
	if err := w.Sync(); err != nil {
		_ = w.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := w.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
