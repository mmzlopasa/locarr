package migrate

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
)

// PathMapping translates a folder as a source sees it (often inside its own
// container) to the folder Locarr sees. SourceID 0 applies to every source.
type PathMapping struct {
	SourceID int64  `json:"sourceId"`
	From     string `json:"from"`
	To       string `json:"to"`
}

// normSourcePath cleans a source path; Windows paths get forward slashes.
func normSourcePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if strings.Contains(p, `\`) {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	return strings.TrimRight(path.Clean(p), "/")
}

// Translator applies path mappings and checks the result against the data roots.
type Translator struct {
	Mappings  []PathMapping
	DataRoots []string
	// Cache resolves folders once (nil = every path in full).
	Cache *lfs.PathCache
}

func (t Translator) safe(p string) (string, error) {
	if t.Cache != nil {
		return t.Cache.SafePath(p)
	}
	return lfs.SafePath(t.DataRoots, p)
}

// Translate maps a source path to Locarr's view (longest matching prefix
// wins; a path without a mapping is used as it is). ok is false when the
// result is outside the data roots.
func (t Translator) Translate(sourceID int64, p string) (string, bool) {
	p = normSourcePath(p)
	if p == "" {
		return "", false
	}
	best, bestLen := "", -1
	for _, m := range t.Mappings {
		if m.SourceID != 0 && m.SourceID != sourceID {
			continue
		}
		from := normSourcePath(m.From)
		if from == "" {
			continue
		}
		if (p == from || strings.HasPrefix(p, from+"/") || from == "/") && len(from) > bestLen {
			rest := strings.TrimPrefix(p, from)
			best, bestLen = filepath.Join(m.To, filepath.FromSlash(rest)), len(from)
		}
	}
	if bestLen < 0 {
		best = filepath.FromSlash(p)
	}
	safe, err := t.safe(filepath.Clean(best))
	if err != nil {
		return filepath.Clean(best), false
	}
	return safe, true
}

// SuggestMappings proposes a mapping for each source root folder that is not
// visible to Locarr as it is: to a Locarr root folder (or data folder) whose
// path ends in the same name.
func SuggestMappings(sourceID int64, sourceRoots []string, localFolders []string, dataRoots []string) []PathMapping {
	var out []PathMapping
	t := Translator{DataRoots: dataRoots}
	for _, sr := range sourceRoots {
		sr = normSourcePath(sr)
		if sr == "" {
			continue
		}
		if local, ok := t.Translate(sourceID, sr); ok {
			if st, err := os.Stat(local); err == nil && st.IsDir() {
				continue // the same path works here
			}
		}
		base := path.Base(sr)
		var cands []string
		for _, lf := range localFolders {
			if strings.EqualFold(filepath.Base(lf), base) {
				cands = append(cands, lf)
			}
		}
		// Next to them: the same name under any data root, one or two levels deep.
		if len(cands) == 0 {
			for _, dr := range dataRoots {
				for _, c := range []string{filepath.Join(dr, base), filepath.Join(dr, "media", base)} {
					if st, err := os.Stat(c); err == nil && st.IsDir() {
						cands = append(cands, c)
					}
				}
			}
		}
		if len(cands) > 0 {
			sort.Strings(cands)
			out = append(out, PathMapping{SourceID: sourceID, From: sr, To: cands[0]})
		}
	}
	return out
}

// PathCheck is how many of a source folder's files Locarr can see.
type PathCheck struct {
	SourceID   int64    `json:"sourceId"`
	SourcePath string   `json:"sourcePath"`
	LocalPath  string   `json:"localPath"`
	Inside     bool     `json:"inside"` // inside the data roots
	Exists     bool     `json:"exists"`
	Files      int      `json:"files"`
	Found      int      `json:"found"`
	Missing    []string `json:"missing,omitempty"` // a few examples
}

// CheckPaths counts, per source root folder, the files that exist where the
// mappings say they are.
func CheckPaths(t Translator, snaps []*Snapshot) []PathCheck {
	var out []PathCheck
	for _, s := range snaps {
		roots := map[string]*PathCheck{}
		var order []string
		for _, r := range s.RootFolders {
			p := normSourcePath(r.Path)
			if _, ok := roots[p]; ok || p == "" {
				continue
			}
			local, inside := t.Translate(s.SourceID, p)
			pc := &PathCheck{SourceID: s.SourceID, SourcePath: p, LocalPath: local, Inside: inside}
			if st, err := os.Stat(local); err == nil && st.IsDir() && inside {
				pc.Exists = true
			}
			roots[p] = pc
			order = append(order, p)
		}
		count := func(file string) {
			fp := normSourcePath(file)
			var pc *PathCheck
			for _, r := range order {
				if strings.HasPrefix(fp, r+"/") && (pc == nil || len(r) > len(pc.SourcePath)) {
					pc = roots[r]
				}
			}
			if pc == nil {
				dir := path.Dir(path.Dir(fp))
				local, inside := t.Translate(s.SourceID, dir)
				pc = &PathCheck{SourceID: s.SourceID, SourcePath: dir, LocalPath: local, Inside: inside}
				if st, err := os.Stat(local); err == nil && st.IsDir() && inside {
					pc.Exists = true
				}
				roots[dir] = pc
				order = append(order, dir)
			}
			pc.Files++
			if local, ok := t.Translate(s.SourceID, fp); ok {
				if st, err := os.Stat(local); err == nil && !st.IsDir() {
					pc.Found++
					return
				}
			}
			if len(pc.Missing) < 5 {
				pc.Missing = append(pc.Missing, file)
			}
		}
		for _, m := range s.Movies {
			if m.File != nil {
				count(m.File.Path)
			}
		}
		for _, v := range s.Series {
			for _, f := range v.Files {
				count(f.Path)
			}
		}
		for _, a := range s.Artists {
			for _, al := range a.Albums {
				for _, f := range al.Files {
					count(f.Path)
				}
			}
		}
		for _, r := range order {
			out = append(out, *roots[r])
		}
	}
	return out
}
