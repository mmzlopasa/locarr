// Package sync re-times a subtitle against a reference: the speech in the
// video's audio (decoded by ffmpeg, D-062) or another subtitle of the same
// video (an embedded text track or a file in another language). Both are
// turned into on/off signals at 20 ms steps; cross-correlation (FFT) finds
// the constant offset, and a handful of frame-rate ratios (23.976↔25,
// 24↔25, …) cover the linear drift of a subtitle made for another frame rate.
package sync

import (
	"errors"
	"math"
	"math/cmplx"
	"sort"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
)

// Step is the resolution of the signals.
const Step = 20 * time.Millisecond

// MaxOffset bounds the offsets searched.
const MaxOffset = 10 * time.Minute

// ErrNoSignal is returned when a reference or the subtitle has nothing to align.
var ErrNoSignal = errors.New("nothing to align: the reference has no speech or no cues")

// Scales are the frame-rate ratios tried besides 1.
var Scales = []float64{25 / 23.976, 23.976 / 25, 24 / 23.976, 23.976 / 24, 25.0 / 24, 24.0 / 25, 30 / 29.97, 29.97 / 30}

// Result is a found re-timing: new time = Scale·t + Offset.
type Result struct {
	Offset time.Duration `json:"offsetMs"`
	Scale  float64       `json:"scale"`
	// Score is how far the best match stands out from the others (a z-score
	// of the correlation peak); Confident is Score above Threshold.
	Score     float64 `json:"score"`
	Confident bool    `json:"confident"`
	// Matched is the share of subtitle time that falls on reference activity
	// after (Matched) and before (Before) the re-timing.
	Matched float64 `json:"matched"`
	Before  float64 `json:"before"`
}

// Threshold is the minimum Score to apply a result automatically.
const Threshold = 8.0

// CueSignal is 1 while a cue is on screen (scaled by scale), 0 elsewhere.
func CueSignal(cues []subfmt.Cue, scale float64, n int) []float64 {
	if n <= 0 {
		for _, c := range cues {
			n = max(n, int(float64(c.End)*scale/float64(Step))+1)
		}
	}
	out := make([]float64, n)
	for _, c := range cues {
		a := int(float64(c.Start) * scale / float64(Step))
		b := int(float64(c.End) * scale / float64(Step))
		for i := max(a, 0); i < b && i < n; i++ {
			out[i] = 1
		}
	}
	return out
}

func fft(a []complex128, invert bool) {
	n := len(a)
	for i, j := 1, 0; i < n; i++ {
		bit := n >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			a[i], a[j] = a[j], a[i]
		}
	}
	for length := 2; length <= n; length <<= 1 {
		ang := 2 * math.Pi / float64(length)
		if invert {
			ang = -ang
		}
		wl := cmplx.Rect(1, ang)
		for i := 0; i < n; i += length {
			w := complex(1, 0)
			for k := 0; k < length/2; k++ {
				u, v := a[i+k], a[i+k+length/2]*w
				a[i+k], a[i+k+length/2] = u+v, u-v
				w *= wl
			}
		}
	}
	if invert {
		for i := range a {
			a[i] /= complex(float64(n), 0)
		}
	}
}

// correlate returns c[lag] = Σ ref[i]·sub[i-lag] for lag in [-maxLag, maxLag]
// (index lag+maxLag).
func correlate(ref, sub []float64, maxLag int) []float64 {
	n := 1
	for n < max(len(ref), len(sub))+maxLag+1 {
		n <<= 1
	}
	a := make([]complex128, n)
	b := make([]complex128, n)
	for i, v := range ref {
		a[i] = complex(v, 0)
	}
	for i, v := range sub {
		b[i] = complex(v, 0)
	}
	fft(a, false)
	fft(b, false)
	for i := range a {
		a[i] *= cmplx.Conj(b[i])
	}
	fft(a, true)
	out := make([]float64, 2*maxLag+1)
	for lag := -maxLag; lag <= maxLag; lag++ {
		idx := lag
		if idx < 0 {
			idx += n
		}
		out[lag+maxLag] = real(a[idx])
	}
	return out
}

func overlap(ref, sub []float64, lag int) float64 {
	on, hit := 0.0, 0.0
	for i, v := range sub {
		if v == 0 {
			continue
		}
		on++
		if j := i + lag; j >= 0 && j < len(ref) && ref[j] > 0 {
			hit++
		}
	}
	if on == 0 {
		return 0
	}
	return hit / on
}

// Align finds the re-timing of cues that best matches the reference
// activity signal (1 = speech or a reference cue, 0 = nothing; Step apart).
func Align(ref []float64, cues []subfmt.Cue) (Result, error) {
	if len(cues) == 0 || len(ref) == 0 {
		return Result{}, ErrNoSignal
	}
	mean := 0.0
	for _, v := range ref {
		mean += v
	}
	mean /= float64(len(ref))
	if mean == 0 || mean == 1 {
		return Result{}, ErrNoSignal
	}
	centred := make([]float64, len(ref))
	for i, v := range ref {
		centred[i] = v - mean
	}
	maxLag := int(MaxOffset / Step)
	base := CueSignal(cues, 1, 0)
	best := Result{Scale: 1, Score: math.Inf(-1)}
	bestRaw := math.Inf(-1)
	for _, scale := range append([]float64{1}, Scales...) {
		sig := base
		if scale != 1 {
			sig = CueSignal(cues, scale, 0)
		}
		on := 0.0
		for _, v := range sig {
			on += v
		}
		if on == 0 {
			continue
		}
		c := correlate(centred, sig, maxLag)
		peak, at := math.Inf(-1), 0
		for i, v := range c {
			if v > peak {
				peak, at = v, i
			}
		}
		raw := peak / on
		// A scale must clearly beat no scaling (it always fits a little better by chance).
		if scale != 1 && raw < bestRaw*1.04 {
			continue
		}
		if raw > bestRaw {
			bestRaw = raw
			best = Result{Offset: time.Duration(at-maxLag) * Step, Scale: scale, Score: zScore(c, at)}
			best.Matched = overlap(ref, sig, at-maxLag)
		}
	}
	if math.IsInf(best.Score, -1) {
		return Result{}, ErrNoSignal
	}
	best.Before = overlap(ref, base, 0)
	best.Confident = best.Score >= Threshold && best.Matched >= best.Before
	return best, nil
}

// zScore measures how much the peak stands out from correlations at lags
// more than two seconds away from it.
func zScore(c []float64, at int) float64 {
	guard := int(2 * time.Second / Step)
	var vals []float64
	for i, v := range c {
		if i < at-guard || i > at+guard {
			vals = append(vals, v)
		}
	}
	if len(vals) < 10 {
		return 0
	}
	sort.Float64s(vals)
	mean, sd := 0.0, 0.0
	for _, v := range vals {
		mean += v
	}
	mean /= float64(len(vals))
	for _, v := range vals {
		sd += (v - mean) * (v - mean)
	}
	sd = math.Sqrt(sd / float64(len(vals)))
	if sd == 0 {
		return 0
	}
	return (c[at] - mean) / sd
}

// Apply re-times the document with r.
func Apply(d *subfmt.Doc, r Result) {
	scale := r.Scale
	if scale == 0 {
		scale = 1
	}
	d.Transform(func(t time.Duration) time.Duration {
		return time.Duration(math.Round(float64(t)*scale)) + r.Offset
	})
}

// Speech turns frame energies (dB, one per Step) into a speech signal: a
// frame is speech when it is well above the noise floor, with short gaps
// bridged and single loud frames dropped.
func Speech(db []float64) []float64 {
	if len(db) == 0 {
		return nil
	}
	sorted := append([]float64(nil), db...)
	sort.Float64s(sorted)
	lo := sorted[len(sorted)/10]
	hi := sorted[len(sorted)*95/100]
	thr := lo + 0.4*(hi-lo)
	out := make([]float64, len(db))
	for i, v := range db {
		if v > thr {
			out[i] = 1
		}
	}
	// Bridge gaps up to 200 ms (pauses between words), drop blips under 60 ms.
	bridge(out, 10, 0)
	bridge(out, 3, 1)
	return out
}

// bridge sets runs of value v shorter than maxRun (between runs of the
// other value) to the other value.
func bridge(s []float64, maxRun int, v float64) {
	for i := 0; i < len(s); {
		if s[i] != v {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == v {
			j++
		}
		if i > 0 && j < len(s) && j-i < maxRun {
			for k := i; k < j; k++ {
				s[k] = 1 - v
			}
		}
		i = j
	}
}

// Energies computes the energy (dB) of every Step of 16-bit mono PCM at rate Hz.
func Energies(pcm []int16, rate int) []float64 {
	e := &EnergyMeter{Rate: rate}
	for _, v := range pcm {
		e.Add(v)
	}
	return e.Done()
}

// EnergyMeter accumulates frame energies from a PCM stream.
type EnergyMeter struct {
	Rate  int
	sum   float64
	count int
	out   []float64
}

// Add adds one sample.
func (e *EnergyMeter) Add(v int16) {
	f := float64(v)
	e.sum += f * f
	e.count++
	if e.count >= e.Rate*int(Step/time.Millisecond)/1000 {
		e.flush()
	}
}

func (e *EnergyMeter) flush() {
	if e.count == 0 {
		return
	}
	e.out = append(e.out, 10*math.Log10(e.sum/float64(e.count)+1))
	e.sum, e.count = 0, 0
}

// Done returns the energies.
func (e *EnergyMeter) Done() []float64 {
	e.flush()
	return e.out
}
