package downloads

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// Suggestion sources.
const (
	// SourceDownload: a finished download was found under another path.
	SourceDownload = "download"
	// SourceFolder: a client folder (default or category) has a folder of the
	// same name inside the data roots; weaker evidence than a download.
	SourceFolder = "folder"
)

// PathSuggestion is a remote path mapping Locarr worked out by itself. It is
// only ever shown; the user adds it (D-111).
type PathSuggestion struct {
	ClientID   int64
	ClientName string
	Host       string
	RemotePath string
	LocalPath  string
	// ClientPath is one path the client reported and FoundAt where Locarr
	// found the same content.
	ClientPath string
	FoundAt    string
	Source     string
	// ReplacesID is set when a mapping for the same host and remote path
	// exists but points elsewhere.
	ReplacesID int64
	// DownloadIDs are the downloads the mapping would let through.
	DownloadIDs []int64
}

// Search limits for the walk of the data roots.
const (
	suggestWalkDepth   = 4
	suggestWalkEntries = 50_000
	suggestWalkTime    = 2 * time.Second
	suggestRetry       = 10 * time.Minute
	sizeFilesLimit     = 20_000
)

type suggestEntry struct {
	remote string
	at     time.Time
	sug    *PathSuggestion
}

// splitRemote splits a client path into its drive ("C:" on Windows, else "")
// and elements. A path with ".." is refused.
func splitRemote(p string) (drive string, elems []string, win bool, ok bool) {
	p = strings.TrimSpace(p)
	win = strings.Contains(p, `\`) || (len(p) >= 2 && p[1] == ':')
	if win {
		if len(p) >= 2 && p[1] == ':' {
			drive, p = p[:2], p[2:]
		}
		p = strings.ReplaceAll(p, `\`, "/")
	}
	if !strings.HasPrefix(p, "/") {
		return "", nil, win, false
	}
	for _, e := range strings.Split(p, "/") {
		switch e {
		case "", ".":
			continue
		case "..":
			return "", nil, win, false
		}
		elems = append(elems, e)
	}
	return drive, elems, win, len(elems) > 0
}

func joinRemote(drive string, elems []string, win bool) string {
	if win {
		return drive + `\` + strings.Join(elems, `\`)
	}
	return "/" + strings.Join(elems, "/")
}

// DeriveMapping works out the mapping that turns remote (a path the client
// reports) into local (where Locarr found the same content). The two paths
// share their trailing elements; what is left in front of them is the
// mapping. The remote side keeps at least one folder, so a mapping never
// covers the client's whole file system: /downloads/X found at
// /data/downloads/X gives /downloads → /data/downloads.
func DeriveMapping(remote, local string) (remotePrefix, localPrefix string, ok bool) {
	drive, rs, win, ok := splitRemote(remote)
	if !ok || !filepath.IsAbs(local) {
		return "", "", false
	}
	var ls []string
	for _, e := range strings.Split(filepath.ToSlash(filepath.Clean(local)), "/") {
		if e != "" {
			ls = append(ls, e)
		}
	}
	eq := func(a, b string) bool {
		if win {
			return strings.EqualFold(a, b)
		}
		return a == b
	}
	n := 0
	for n < len(rs) && n < len(ls) && eq(rs[len(rs)-1-n], ls[len(ls)-1-n]) {
		n++
	}
	if n == 0 {
		return "", "", false
	}
	if keep := len(rs) - n; keep < 1 {
		n -= 1 - keep
	}
	if n < 1 {
		return "", "", false
	}
	remotePrefix = joinRemote(drive, rs[:len(rs)-n], win)
	localPrefix = filepath.FromSlash("/" + strings.Join(ls[:len(ls)-n], "/"))
	if !win && remotePrefix == filepath.ToSlash(localPrefix) {
		return "", "", false // the same path: a mapping would change nothing
	}
	return remotePrefix, localPrefix, true
}

// sizeMatches reports whether the file or folder at p has about size bytes
// (files exactly, folders within 10%: clients count only selected files and
// usenet clients unpack). size <= 0 means unknown.
func sizeMatches(p string, size int64) bool {
	st, err := os.Lstat(p)
	if err != nil {
		return false
	}
	if size <= 0 {
		return true
	}
	if st.Mode().IsRegular() {
		return st.Size() == size
	}
	if !st.IsDir() {
		return false
	}
	var total int64
	files := 0
	stop := fs.SkipAll
	_ = filepath.WalkDir(p, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if files++; files > sizeFilesLimit {
			return stop
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total >= size*9/10 && total <= size*11/10
}

// candidates are the likely places of name inside one data root, most likely first.
func candidates(root string, elems []string, category string) []string {
	name := elems[len(elems)-1]
	var out []string
	add := func(parts ...string) { out = append(out, filepath.Join(append([]string{root}, parts...)...)) }
	for _, top := range []string{"downloads", "torrents", "usenet"} {
		add(top, name)
		if category != "" {
			add(top, category, name)
		}
	}
	add("downloads", "complete", name)
	add("usenet", "complete", name)
	if category != "" {
		add("usenet", "complete", category, name)
	}
	// The client's own trailing folders under the root: /mnt/a/b/X → <root>/b/X, <root>/a/b/X.
	for k := 2; k <= min(len(elems), 4); k++ {
		add(elems[len(elems)-k:]...)
	}
	add(name)
	return out
}

// findLocal looks for the content a client reports at remote inside the data
// roots: first in the likely places, then (when walk) in a bounded walk.
// Every hit passes the safe-path check.
func (s *Service) findLocal(ctx context.Context, remote, category string, size int64, walk bool) string {
	_, elems, _, ok := splitRemote(remote)
	if !ok {
		return ""
	}
	name := elems[len(elems)-1]
	seen := map[string]bool{}
	try := func(p string) bool {
		if seen[p] {
			return false
		}
		seen[p] = true
		if _, err := lfs.MustExist(s.DataRoots, p); err != nil {
			return false
		}
		return sizeMatches(p, size)
	}
	for _, r := range s.DataRoots {
		for _, c := range candidates(filepath.Clean(r), elems, category) {
			if try(c) {
				return c
			}
		}
	}
	if !walk {
		return ""
	}
	deadline := time.Now().Add(suggestWalkTime)
	entries := 0
	for _, r := range s.DataRoots {
		root := filepath.Clean(r)
		type dir struct {
			path  string
			depth int
		}
		queue := []dir{{root, 0}}
		for len(queue) > 0 {
			if ctx.Err() != nil || time.Now().After(deadline) || entries > suggestWalkEntries {
				return ""
			}
			d := queue[0]
			queue = queue[1:]
			list, err := os.ReadDir(d.path)
			if err != nil {
				continue
			}
			entries += len(list)
			for _, e := range list {
				p := filepath.Join(d.path, e.Name())
				if e.Name() == name {
					if try(p) {
						return p
					}
					continue
				}
				// Symbolic links are never followed (e.Type() is not a dir for them).
				if e.IsDir() && d.depth+1 < suggestWalkDepth {
					queue = append(queue, dir{p, d.depth + 1})
				}
			}
		}
	}
	return ""
}

// suggestion builds and checks a suggestion: adding it must make remote
// resolve to found, inside the data roots.
func (s *Service) suggestion(mappings []Mapping, c Client, remote, found, source string) *PathSuggestion {
	rp, lp, ok := DeriveMapping(remote, found)
	if !ok {
		return nil
	}
	return s.checkedSuggestion(mappings, c, remote, found, source, rp, lp)
}

func (s *Service) checkedSuggestion(mappings []Mapping, c Client, remote, found, source, rp, lp string) *PathSuggestion {
	m := Mapping{Host: c.Host, RemotePath: rp, LocalPath: lp}
	if err := s.validMapping(&m); err != nil {
		return nil
	}
	sug := &PathSuggestion{ClientID: c.ID, ClientName: c.Name, Host: m.Host, RemotePath: m.RemotePath, LocalPath: m.LocalPath,
		ClientPath: remote, FoundAt: found, Source: source}
	with := []Mapping{m}
	for _, ex := range mappings {
		if ex.Host == m.Host && ex.RemotePath == m.RemotePath {
			if ex.LocalPath == m.LocalPath {
				return nil // already there; nothing to suggest
			}
			sug.ReplacesID = ex.ID
			continue
		}
		with = append(with, ex)
	}
	got, err := lfs.MustExist(s.DataRoots, Translate(with, c.Host, remote))
	if err != nil {
		return nil
	}
	if want, err := lfs.MustExist(s.DataRoots, found); err != nil || got != want {
		return nil // a longer mapping would still win
	}
	return sug
}

// suggestFor finds a mapping for a finished download whose path is not
// visible. Results are kept per download; a miss is retried after a while.
func (s *Service) suggestFor(ctx context.Context, c Client, d Download, remote string, mappings []Mapping) *PathSuggestion {
	s.mu.Lock()
	s.init()
	e, ok := s.suggest[d.ID]
	s.mu.Unlock()
	if ok && e.remote == remote && (e.sug != nil || s.now().Sub(e.at) < suggestRetry) {
		if e.sug == nil {
			return nil
		}
		// Recheck the cheap part: a mapping may have been added meanwhile.
		if sug := s.suggestion(mappings, c, remote, e.sug.FoundAt, SourceDownload); sug != nil {
			sug.DownloadIDs = []int64{d.ID}
			return s.keepSuggestion(d.ID, remote, sug)
		}
	}
	var sug *PathSuggestion
	if found := s.findLocal(ctx, remote, d.Category, d.Size, true); found != "" {
		sug = s.suggestion(mappings, c, remote, found, SourceDownload)
		if sug != nil {
			sug.DownloadIDs = []int64{d.ID}
		}
	}
	return s.keepSuggestion(d.ID, remote, sug)
}

func (s *Service) keepSuggestion(id int64, remote string, sug *PathSuggestion) *PathSuggestion {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	s.suggest[id] = &suggestEntry{remote: remote, at: s.now(), sug: sug}
	return sug
}

func (s *Service) dropSuggestion(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	delete(s.suggest, id)
}

// DownloadSuggestion is the suggestion for one download (nil when none).
func (s *Service) DownloadSuggestion(id int64) *PathSuggestion {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if e, ok := s.suggest[id]; ok && e.sug != nil {
		cp := *e.sug
		return &cp
	}
	return nil
}

// Suggestions lists the mappings Locarr suggests: from finished downloads it
// found elsewhere, then from client folders it cannot see. One entry per
// host and mapping; ones that were added meanwhile are left out.
func (s *Service) Suggestions(ctx context.Context) ([]PathSuggestion, error) {
	mappings, err := s.Mappings(ctx)
	if err != nil {
		return nil, err
	}
	has := func(p PathSuggestion) bool { return covered(mappings, p) }
	byKey := map[string]*PathSuggestion{}
	var order []string
	put := func(p PathSuggestion) {
		if has(p) {
			return
		}
		k := p.Host + "\x00" + p.RemotePath + "\x00" + p.LocalPath
		if ex, ok := byKey[k]; ok {
			ex.DownloadIDs = append(ex.DownloadIDs, p.DownloadIDs...)
			return
		}
		cp := p
		cp.DownloadIDs = append([]int64(nil), p.DownloadIDs...)
		byKey[k] = &cp
		order = append(order, k)
	}
	s.mu.Lock()
	s.init()
	var fromDownloads []PathSuggestion
	for _, e := range s.suggest {
		if e.sug != nil {
			fromDownloads = append(fromDownloads, *e.sug)
		}
	}
	var fromClients []PathSuggestion
	for _, sn := range s.snapshots {
		for _, pc := range sn.PathChecks {
			if pc.Suggestion != nil {
				fromClients = append(fromClients, *pc.Suggestion)
			}
		}
	}
	s.mu.Unlock()
	sort.Slice(fromDownloads, func(i, j int) bool { return fromDownloads[i].DownloadIDs[0] < fromDownloads[j].DownloadIDs[0] })
	for _, p := range fromDownloads {
		put(p)
	}
	for _, p := range fromClients {
		put(p)
	}
	out := make([]PathSuggestion, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out, nil
}

// covered reports whether the suggested mapping exists already.
func covered(mappings []Mapping, p PathSuggestion) bool {
	for _, m := range mappings {
		if m.Host == p.Host && m.RemotePath == p.RemotePath && m.LocalPath == p.LocalPath {
			return true
		}
	}
	return false
}

// PathCheck is one client folder and whether Locarr sees it.
type PathCheck struct {
	// Category is "" for the client's default folder.
	Category   string
	ClientPath string
	Visible    bool
	LocalPath  string
	Suggestion *PathSuggestion
}

// checkPaths checks the client's default folder and the folders of Locarr's
// categories. For a folder Locarr cannot see it suggests a mapping from the
// client's finished downloads, or else from a folder of the same name inside
// the data roots. Only cheap lookups, no walk: it runs on every test.
func (s *Service) checkPaths(ctx context.Context, c Client, cl downloaders.Client, savePath string, mappings []Mapping) []PathCheck {
	type folder struct{ cat, path string }
	var folders []folder
	if strings.TrimSpace(savePath) != "" {
		folders = append(folders, folder{"", savePath})
	}
	if cl.Capabilities().Categories && len(c.CategorySet()) > 0 {
		if cats, err := cl.Categories(ctx); err == nil {
			for _, want := range c.CategorySet() {
				for _, x := range cats {
					if strings.EqualFold(x.Name, want) && strings.TrimSpace(x.SavePath) != "" {
						folders = append(folders, folder{x.Name, x.SavePath})
					}
				}
			}
		}
	}
	var out []PathCheck
	var items []downloaders.Item
	listed := false
	for _, f := range folders {
		remote := normaliseRemote(f.path)
		pc := PathCheck{Category: f.cat, ClientPath: remote}
		if local := Translate(mappings, c.Host, remote); filepath.IsAbs(local) {
			if got, err := lfs.MustExist(s.DataRoots, local); err == nil && lfs.IsDir(got) {
				pc.Visible, pc.LocalPath = true, got
				out = append(out, pc)
				continue
			}
		}
		if !listed {
			items, _ = cl.List(ctx, "")
			listed = true
		}
		pc.Suggestion = s.suggestFromItems(ctx, c, remote, items, mappings)
		if pc.Suggestion == nil {
			pc.Suggestion = s.suggestFromFolder(c, remote, mappings)
		}
		out = append(out, pc)
	}
	return out
}

// suggestFromItems tries up to a few finished items saved under folder.
func (s *Service) suggestFromItems(ctx context.Context, c Client, folder string, items []downloaders.Item, mappings []Mapping) *PathSuggestion {
	tried := 0
	for _, it := range items {
		p := it.ContentPath
		if p == "" {
			p = it.SavePath
		}
		if it.Progress < 1 || p == "" || !hasPathPrefix(p, folder) || len(p) == len(folder) {
			continue
		}
		if tried++; tried > 20 {
			break
		}
		found := s.findLocal(ctx, p, it.Category, it.Size, false)
		if found == "" {
			continue
		}
		sug := s.suggestion(mappings, c, p, found, SourceDownload)
		if sug == nil || !hasPathPrefix(folder, sug.RemotePath) {
			continue
		}
		return sug
	}
	return nil
}

// suggestFromFolder looks for a folder of the same name (and parents) inside the data roots.
func (s *Service) suggestFromFolder(c Client, folder string, mappings []Mapping) *PathSuggestion {
	_, elems, _, ok := splitRemote(folder)
	if !ok {
		return nil
	}
	for _, r := range s.DataRoots {
		root := filepath.Clean(r)
		for k := min(len(elems), 4); k >= 1; k-- {
			p := filepath.Join(append([]string{root}, elems[len(elems)-k:]...)...)
			if got, err := lfs.MustExist(s.DataRoots, p); err != nil || !lfs.IsDir(got) {
				continue
			}
			if sug := s.checkedSuggestion(mappings, c, folder, p, SourceFolder, folder, p); sug != nil {
				return sug
			}
		}
	}
	return nil
}
