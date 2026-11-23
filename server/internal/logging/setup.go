package logging

import (
	"io"
	"log/slog"
	"sync"
)

// Sinks are where the server's log goes.
type Sinks struct {
	// Ring feeds the in-app log viewer.
	Ring *Ring
	// Dir holds the rotated files ("" = no files).
	Dir  string
	file *RotatingFile
}

// Close closes the log file.
func (s *Sinks) Close() error {
	if s == nil || s.file == nil {
		return nil
	}
	return s.file.Close()
}

// fanout writes every record to each writer; one failing writer does not
// stop the others (unlike io.MultiWriter).
type fanout struct {
	mu sync.Mutex
	ws []io.Writer
}

func (f *fanout) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.ws {
		_, _ = w.Write(p)
	}
	return len(p), nil
}

// Setup returns the server logger writing to out, to rotating files in dir
// (when dir is not empty and can be created) and to an in-memory ring.
func Setup(out io.Writer, level, dir string) (*slog.Logger, *Sinks) {
	s := &Sinks{Ring: NewRing(5000)}
	ws := []io.Writer{out, s.Ring}
	if dir != "" {
		if f, err := OpenRotating(dir, DefaultMaxBytes, DefaultKeep); err == nil {
			s.file, s.Dir = f, dir
			ws = append(ws, f)
		}
	}
	return New(&fanout{ws: ws}, level), s
}
