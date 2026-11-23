package sync

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
)

// ErrNoFFmpeg is returned when no ffmpeg binary is available.
var ErrNoFFmpeg = errors.New("ffmpeg is not available, so the audio cannot be read")

// FFmpeg runs the ffmpeg binary at Path (D-062: the image carries a small
// audio-only build).
type FFmpeg struct {
	Path string
}

// Find returns an FFmpeg for path (or "ffmpeg" in PATH when path is empty);
// ok is false when the binary is missing.
func Find(path string) (FFmpeg, bool) {
	if path == "" {
		path = "ffmpeg"
	}
	p, err := exec.LookPath(path)
	if err != nil {
		return FFmpeg{}, false
	}
	return FFmpeg{Path: p}, true
}

// Rate is the sample rate audio is decoded at.
const Rate = 16000

// SpeechOf decodes audio track index (0 = first) of video as 16 kHz mono and
// returns its speech signal.
func (f FFmpeg) SpeechOf(ctx context.Context, video string, index int) ([]float64, error) {
	if f.Path == "" {
		return nil, ErrNoFFmpeg
	}
	// #nosec G204 -- fixed binary, arguments are not interpreted by a shell; video passed fs.SafePath
	cmd := exec.CommandContext(ctx, f.Path, "-nostdin", "-hide_banner", "-v", "error", "-i", video,
		"-map", "0:a:"+strconv.Itoa(index), "-vn", "-sn", "-dn", "-ac", "1", "-ar", strconv.Itoa(Rate), "-f", "s16le", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &limited{w: &stderr, n: 8 << 10}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	meter := &EnergyMeter{Rate: Rate}
	r := bufio.NewReaderSize(out, 64<<10)
	var buf [2]byte
	for {
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			break
		}
		meter.Add(int16(binary.LittleEndian.Uint16(buf[:]))) // #nosec G115 -- signed PCM
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("ffmpeg could not decode the audio: %s", firstLine(stderr.String(), err))
	}
	db := meter.Done()
	if len(db) == 0 {
		return nil, errors.New("the video has no decodable audio")
	}
	return Speech(db), nil
}

// TextTrack extracts embedded text subtitle track index (ffmpeg's 0:s:N) as cues.
func (f FFmpeg) TextTrack(ctx context.Context, video string, index int) ([]subfmt.Cue, error) {
	if f.Path == "" {
		return nil, ErrNoFFmpeg
	}
	// #nosec G204 -- fixed binary, no shell
	cmd := exec.CommandContext(ctx, f.Path, "-nostdin", "-hide_banner", "-v", "error", "-i", video,
		"-map", "0:s:"+strconv.Itoa(index), "-c:s", "subrip", "-f", "srt", "-")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limited{w: &stdout, n: 32 << 20}
	cmd.Stderr = &limited{w: &stderr, n: 8 << 10}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffmpeg could not read the subtitle track: %s", firstLine(stderr.String(), err))
	}
	d, err := subfmt.Parse(stdout.String())
	if err != nil {
		return nil, err
	}
	return d.Cues, nil
}

func firstLine(s string, err error) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return err.Error()
	}
	l, _, _ := strings.Cut(s, "\n")
	return l
}

// limited writes at most n bytes and drops the rest.
type limited struct {
	w io.Writer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		_, _ = l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
