// Package trackstest writes small Matroska and MP4 files for tests: track
// headers with languages and flags, and optionally real 16-bit PCM audio and
// text subtitle blocks in Matroska, which ffmpeg can decode (used by the
// audio sync tests and the end-to-end fixtures).
package trackstest

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"time"
)

// Track describes one track to write.
type Track struct {
	Kind            string // video, audio, subtitle
	Codec           string // Matroska codec id (V_MPEG4/ISO/AVC, A_PCM/INT/LIT, S_TEXT/UTF8…) or MP4 sample entry (avc1, mp4a, tx3g)
	Language        string // ISO 639-2 ("hrv") or BCP 47 for Matroska
	Name            string
	Default         bool
	Forced          bool
	HearingImpaired bool
}

// Cue is a text block of a subtitle track.
type Cue struct {
	Start, End time.Duration
	Text       string
}

// MKV is the content of a Matroska file.
type MKV struct {
	Tracks []Track
	// PCM is mono 16-bit audio for the first audio track (A_PCM/INT/LIT) at Rate Hz.
	PCM  []int16
	Rate int
	// Cues are blocks of the first subtitle track.
	Cues []Cue
}

func vintSize(n uint64) []byte {
	for l := 1; l <= 8; l++ {
		if n < (uint64(1)<<(7*l))-1 {
			b := make([]byte, l)
			v := n | uint64(1)<<(7*l)
			for i := l - 1; i >= 0; i-- {
				b[i] = byte(v)
				v >>= 8
			}
			return b
		}
	}
	panic("too large")
}

func idBytes(id uint32) []byte {
	b := binary.BigEndian.AppendUint32(nil, id)
	switch {
	case id > 0xFFFFFF:
		return b
	case id > 0xFFFF:
		return b[1:]
	case id > 0xFF:
		return b[2:]
	}
	return b[3:]
}

func el(id uint32, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	out := append(idBytes(id), vintSize(uint64(len(b)))...)
	return append(out, b...)
}

func u(id uint32, v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	i := 0
	for i < 7 && b[i] == 0 {
		i++
	}
	return el(id, b[i:])
}

func s(id uint32, v string) []byte { return el(id, []byte(v)) }

func f(id uint32, v float64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, math.Float64bits(v))
	return el(id, b)
}

func boolU(v bool) uint64 {
	if v {
		return 1
	}
	return 0
}

// Bytes renders the Matroska file.
func (m MKV) Bytes() []byte {
	head := el(0x1A45DFA3, u(0x4286, 1), u(0x42F7, 1), u(0x42F2, 4), u(0x42F3, 8), s(0x4282, "matroska"), u(0x4287, 4), u(0x4285, 2))
	rate := m.Rate
	if rate == 0 {
		rate = 16000
	}
	dur := time.Duration(len(m.PCM)) * time.Second / time.Duration(rate)
	for _, c := range m.Cues {
		dur = max(dur, c.End)
	}
	info := el(0x1549A966, u(0x2AD7B1, 1000000), s(0x4D80, "trackstest"), s(0x5741, "trackstest"), f(0x4489, float64(dur/time.Millisecond)))
	var entries [][]byte
	audioNo, subNo := 0, 0
	for i, t := range m.Tracks {
		no := uint64(i + 1)
		typ := map[string]uint64{"video": 1, "audio": 2, "subtitle": 0x11}[t.Kind]
		parts := [][]byte{u(0xD7, no), u(0x73C5, no), u(0x83, typ), s(0x86, t.Codec), u(0x88, boolU(t.Default)), u(0x55AA, boolU(t.Forced))}
		if t.HearingImpaired {
			parts = append(parts, u(0x55AB, 1))
		}
		if t.Language != "" {
			parts = append(parts, s(0x22B59C, t.Language))
		}
		if t.Name != "" {
			parts = append(parts, s(0x536E, t.Name))
		}
		switch t.Kind {
		case "video":
			parts = append(parts, el(0xE0, u(0xB0, 320), u(0xBA, 240)))
		case "audio":
			parts = append(parts, el(0xE1, f(0xB5, float64(rate)), u(0x9F, 1), u(0x6264, 16)))
			if audioNo == 0 {
				audioNo = i + 1
			}
		case "subtitle":
			if subNo == 0 {
				subNo = i + 1
			}
		}
		entries = append(entries, el(0xAE, parts...))
	}
	tracks := el(0x1654AE6B, entries...)
	var clusters [][]byte
	// One cluster per second of audio, subtitle blocks in the cluster of their start.
	seconds := int(dur/time.Second) + 1
	for sec := 0; sec < seconds; sec++ {
		start := time.Duration(sec) * time.Second
		parts := [][]byte{u(0xE7, uint64(sec*1000))}
		var blocks []struct {
			at   time.Duration
			data []byte
		}
		if audioNo > 0 && len(m.PCM) > 0 {
			from, to := sec*rate, min((sec+1)*rate, len(m.PCM))
			if from < to {
				pcm := make([]byte, 2*(to-from))
				for i, v := range m.PCM[from:to] {
					binary.LittleEndian.PutUint16(pcm[2*i:], uint16(v)) // #nosec G115 -- two's complement on purpose
				}
				blocks = append(blocks, struct {
					at   time.Duration
					data []byte
				}{start, simpleBlock(audioNo, 0, pcm)})
			}
		}
		for _, c := range m.Cues {
			if subNo == 0 || c.Start < start || c.Start >= start+time.Second {
				continue
			}
			rel := int16((c.Start - start) / time.Millisecond) // #nosec G115 -- < 1000
			blk := el(0xA1, blockBody(subNo, rel, []byte(c.Text)))
			dur := uint64((c.End - c.Start) / time.Millisecond) // #nosec G115 -- positive test durations
			blocks = append(blocks, struct {
				at   time.Duration
				data []byte
			}{c.Start, el(0xA0, blk, u(0x9B, dur))})
		}
		if len(blocks) == 0 && sec > 0 {
			continue
		}
		for _, b := range blocks {
			parts = append(parts, b.data)
		}
		clusters = append(clusters, el(0x1F43B675, parts...))
	}
	seg := el(0x18538067, append([][]byte{info, tracks}, clusters...)...)
	return append(head, seg...)
}

func blockBody(track int, rel int16, data []byte) []byte {
	b := append(vintSize(uint64(track)), byte(uint16(rel)>>8), byte(uint16(rel)), 0x80) // #nosec G115 -- relative timestamp
	return append(b, data...)
}

func simpleBlock(track int, rel int16, data []byte) []byte {
	return el(0xA3, blockBody(track, rel, data))
}

// Write writes the Matroska file.
func (m MKV) Write(path string) error { return os.WriteFile(path, m.Bytes(), 0o600) }

// --- MP4 --------------------------------------------------------------------

func mbox(typ string, body ...[]byte) []byte {
	b := bytes.Join(body, nil)
	out := make([]byte, 8, 8+len(b))
	binary.BigEndian.PutUint32(out, uint32(8+len(b))) // #nosec G115 -- small test files
	copy(out[4:], typ)
	return append(out, b...)
}

func lang6392(code string) []byte {
	var v uint16
	if len(code) == 3 {
		v = uint16(code[0]-0x60)<<10 | uint16(code[1]-0x60)<<5 | uint16(code[2]-0x60)
	}
	return []byte{byte(v >> 8), byte(v)}
}

// MP4 renders an MP4 file with the tracks' headers (no samples).
func MP4(ts []Track) []byte {
	ftyp := mbox("ftyp", []byte("isom"), []byte{0, 0, 2, 0}, []byte("isomiso2mp41"))
	var traks [][]byte
	for i, t := range ts {
		flags := byte(0)
		if t.Default {
			flags = 1
		}
		tkhd := mbox("tkhd", []byte{0, 0, 0, flags | 2}, make([]byte, 8), []byte{0, 0, 0, byte(i + 1)}, make([]byte, 68))
		mdhd := mbox("mdhd", make([]byte, 4), make([]byte, 16), lang6392(t.Language), []byte{0, 0})
		handler := map[string]string{"video": "vide", "audio": "soun", "subtitle": "sbtl"}[t.Kind]
		hdlr := mbox("hdlr", make([]byte, 8), []byte(handler), make([]byte, 12), []byte(t.Name), []byte{0})
		entry := mbox(t.Codec, make([]byte, 8))
		stsd := mbox("stsd", make([]byte, 4), []byte{0, 0, 0, 1}, entry)
		minf := mbox("minf", mbox("stbl", stsd))
		traks = append(traks, mbox("trak", tkhd, mbox("mdia", mdhd, hdlr, minf)))
	}
	moov := mbox("moov", append([][]byte{mbox("mvhd", make([]byte, 100))}, traks...)...)
	return append(append(ftyp, moov...), mbox("mdat", make([]byte, 64))...)
}

// Speech makes PCM that is loud during the given spans and near-silent
// elsewhere (a stand-in for dialogue), at rate Hz for total.
func Speech(spans []Cue, total time.Duration, rate int) []int16 {
	n := int(total * time.Duration(rate) / time.Second)
	out := make([]int16, n)
	seed := uint32(12345)
	noise := func() float64 {
		seed = seed*1664525 + 1013904223
		return float64(int32(seed>>8)%2000) / 2000 // #nosec G115 -- deterministic noise
	}
	for i := range out {
		out[i] = int16(noise() * 60) // background hiss
	}
	for _, sp := range spans {
		a := int(sp.Start * time.Duration(rate) / time.Second)
		b := min(int(sp.End*time.Duration(rate)/time.Second), n)
		for i := max(a, 0); i < b; i++ {
			t := float64(i) / float64(rate)
			// Voiced-like: two harmonics with a syllable-rate envelope.
			env := 0.6 + 0.4*math.Sin(2*math.Pi*4*t)
			v := env * (math.Sin(2*math.Pi*180*t) + 0.5*math.Sin(2*math.Pi*360*t))
			out[i] = int16(v*9000 + noise()*400)
		}
	}
	return out
}
