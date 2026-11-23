package native

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

//go:embed definitions/*.yml
var builtinFS embed.FS

// maxDefinitionSize caps a custom definition file.
const maxDefinitionSize = 256 << 10

// LoadError is a definition file that could not be loaded.
type LoadError struct {
	File    string `json:"file"`
	Message string `json:"message"`
}

// Registry holds the built-in definitions and the administrator's custom
// ones (YAML files in a folder, re-read on Reload).
type Registry struct {
	// Dir is the custom definitions folder ("" = none).
	Dir string

	mu     sync.RWMutex
	defs   map[string]*Definition
	errors []LoadError
}

// Builtins parses the definitions shipped in the binary.
func Builtins() ([]*Definition, error) {
	entries, err := fs.ReadDir(builtinFS, "definitions")
	if err != nil {
		return nil, err
	}
	var out []*Definition
	for _, e := range entries {
		data, err := builtinFS.ReadFile("definitions/" + e.Name())
		if err != nil {
			return nil, err
		}
		d, err := Parse(data)
		if err != nil {
			return nil, fmt.Errorf("built-in definition %s: %w", e.Name(), err)
		}
		if d.ID+".yml" != e.Name() {
			return nil, fmt.Errorf("built-in definition %s has id %q", e.Name(), d.ID)
		}
		d.Builtin = true
		out = append(out, d)
	}
	return out, nil
}

// NewRegistry loads the built-ins and the custom folder.
func NewRegistry(dir string) (*Registry, error) {
	r := &Registry{Dir: dir}
	if err := r.Reload(); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload re-reads the custom folder. Invalid files are skipped and reported
// by Errors; a custom file with a built-in's id replaces it.
func (r *Registry) Reload() error {
	builtins, err := Builtins()
	if err != nil {
		return err
	}
	defs := map[string]*Definition{}
	for _, d := range builtins {
		defs[d.ID] = d
	}
	var errs []LoadError
	if r.Dir != "" {
		entries, err := os.ReadDir(r.Dir)
		if err != nil && !os.IsNotExist(err) {
			errs = append(errs, LoadError{File: r.Dir, Message: err.Error()})
		}
		custom := map[string]string{}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || strings.HasPrefix(name, ".") || (!strings.HasSuffix(name, ".yml") && !strings.HasSuffix(name, ".yaml")) {
				continue
			}
			d, err := readCustom(filepath.Join(r.Dir, name))
			if err != nil {
				errs = append(errs, LoadError{File: name, Message: err.Error()})
				continue
			}
			if prev, ok := custom[d.ID]; ok {
				errs = append(errs, LoadError{File: name, Message: fmt.Sprintf("id %q is already used by %s", d.ID, prev)})
				continue
			}
			custom[d.ID] = name
			d.File = name
			if b, ok := defs[d.ID]; ok && b.Builtin {
				d.Overrides = true
			}
			defs[d.ID] = d
		}
	}
	r.mu.Lock()
	r.defs, r.errors = defs, errs
	r.mu.Unlock()
	return nil
}

func readCustom(path string) (*Definition, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > maxDefinitionSize {
		return nil, fmt.Errorf("the file is larger than %d KiB", maxDefinitionSize>>10)
	}
	data, err := os.ReadFile(path) // #nosec G304 -- files in the administrator's definitions folder
	if err != nil {
		return nil, err
	}
	d, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if d.Impl != "" {
		return nil, fmt.Errorf("custom definitions cannot use impl; describe the site with a search block")
	}
	return d, nil
}

// Get returns a definition by id.
func (r *Registry) Get(id string) (*Definition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.defs[id]
	return d, ok
}

// List returns every definition by name.
func (r *Registry) List() []*Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Definition, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	slices.SortFunc(out, func(a, b *Definition) int { return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	return out
}

// Errors returns the custom files that failed to load.
func (r *Registry) Errors() []LoadError {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.errors)
}

// Match finds the definition of a site known by another tool's name or
// address (for importing): the name, an alias or a host of the site.
func (r *Registry) Match(names []string, urls []string) (*Definition, bool) {
	norm := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "", "(api)", "", "(proxy)", "").Replace(s)
	}
	hostOf := func(u string) string {
		u = strings.ToLower(strings.TrimSpace(u))
		u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
		if i := strings.IndexAny(u, "/:?"); i >= 0 {
			u = u[:i]
		}
		return strings.TrimPrefix(u, "www.")
	}
	for _, d := range r.List() {
		keys := []string{norm(d.ID), norm(d.Name)}
		for _, a := range d.Aliases {
			keys = append(keys, norm(a))
		}
		for _, n := range names {
			if n != "" && slices.Contains(keys, norm(n)) {
				return d, true
			}
		}
		hosts := d.Hosts()
		for _, u := range urls {
			if h := hostOf(u); h != "" && slices.Contains(hosts, h) {
				return d, true
			}
		}
	}
	return nil, false
}
