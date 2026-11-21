// Package mediaserver tells media servers about library changes. P9 adds the
// Streamero notifier; until then the default does nothing.
package mediaserver

import "context"

// Change is what happened in the library.
type Change struct {
	// ItemRef is the library item ("movie:42").
	ItemRef string
	// Kind is movie, episode or album; Action is imported, upgraded, renamed or deleted.
	Kind   string
	Action string
	Title  string
	Year   int
	// Paths are the files (or folder) that changed.
	Paths []string
	IDs   map[string]string // tmdb, imdb, tvdb, musicbrainz
}

// Notifier is called after an import, upgrade, rename or delete. It must not
// block for long; errors are recorded on the timeline but never undo the import.
type Notifier interface {
	// Name is shown on the timeline ("" = do not record anything).
	Name() string
	Notify(ctx context.Context, c Change) error
}

// Noop is the default notifier.
type Noop struct{}

// Name implements Notifier.
func (Noop) Name() string { return "" }

// Notify implements Notifier.
func (Noop) Notify(context.Context, Change) error { return nil }
