package music

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/tags"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// ExistingAlbum is an album folder found in an existing library.
type ExistingAlbum struct {
	Path   string
	Name   string
	Album  string // from the tags
	Year   int
	Files  int
	Group  string // release group id in the tags
	Errors int
}

// ExistingArtist is an artist folder found in an existing library.
type ExistingArtist struct {
	Path       string
	Name       string
	TagArtist  string
	Albums     []ExistingAlbum
	Match      *metadata.ArtistSummary
	Candidates []metadata.ArtistSummary
	ArtistID   *int64 // already in the library
	Error      string
}

func audioIn(dir string) []string {
	var out []string
	_ = filepath.WalkDir(dir, func(p string, e os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if e.IsDir() && p != dir && strings.Count(rel(dir, p), string(filepath.Separator)) >= 1 {
			return filepath.SkipDir
		}
		if !e.IsDir() && tags.IsAudio(p) && !strings.HasPrefix(e.Name(), ".") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// ScanRootFolder lists artist folders (with their album folders) under a
// music root folder that are not in the library yet, read from the first
// file's tags of each album, and matched to MusicBrainz (one search per
// artist; ids in the tags are used first).
func (s *Service) ScanRootFolder(ctx context.Context, rootFolderID int64) ([]ExistingArtist, error) {
	root, err := s.rootFolder(ctx, rootFolderID)
	if err != nil {
		return nil, err
	}
	dir, err := lfs.MustExist(s.DataRoots, root.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	known := map[string]int64{}
	list, err := s.artists(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, a := range list {
		known[a.Path] = a.ID
	}
	out := []ExistingArtist{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "recycle-bin" {
			continue
		}
		ea := ExistingArtist{Path: filepath.Join(dir, e.Name()), Name: e.Name(), Albums: []ExistingAlbum{}, Candidates: []metadata.ArtistSummary{}}
		if id, ok := known[ea.Path]; ok {
			ea.ArtistID = &id
		}
		subs, _ := os.ReadDir(ea.Path)
		artistIDs := map[string]int{}
		names := map[string]int{}
		for _, sd := range subs {
			if !sd.IsDir() || strings.HasPrefix(sd.Name(), ".") {
				continue
			}
			ap := filepath.Join(ea.Path, sd.Name())
			files := audioIn(ap)
			if len(files) == 0 {
				continue
			}
			al := ExistingAlbum{Path: ap, Name: sd.Name(), Files: len(files)}
			if t, err := tags.Read(files[0]); err == nil {
				al.Album, al.Year, al.Group = t.Album, t.Year(), t.ReleaseGroupID
				if id := first(t.AlbumArtistID, t.ArtistID); id != "" {
					artistIDs[id]++
				}
				if n := first(t.AlbumArtist, t.Artist); n != "" {
					names[n]++
				}
			} else {
				al.Errors++
			}
			ea.Albums = append(ea.Albums, al)
		}
		if len(ea.Albums) == 0 {
			continue
		}
		ea.TagArtist = top(names)
		if ea.ArtistID == nil {
			s.matchArtist(ctx, &ea, top(artistIDs))
		}
		out = append(out, ea)
	}
	return out, nil
}

func top(m map[string]int) string {
	best, n := "", 0
	for k, v := range m {
		if v > n || (v == n && k < best) {
			best, n = k, v
		}
	}
	return best
}

func (s *Service) matchArtist(ctx context.Context, ea *ExistingArtist, mbid string) {
	if mbid != "" {
		if a, err := s.Meta.Artist(ctx, mbid); err == nil {
			ea.Match = &a.ArtistSummary
			ea.Candidates = append(ea.Candidates, a.ArtistSummary)
			return
		}
	}
	name := ea.TagArtist
	if name == "" {
		name = ea.Name
	}
	list, err := s.Meta.SearchArtists(ctx, name)
	if err != nil {
		ea.Error = err.Error()
		return
	}
	if len(list) > 8 {
		list = list[:8]
	}
	ea.Candidates = list
	for i := range list {
		if strings.EqualFold(list[i].Name, name) || Similarity(list[i].Name, name) >= 0.95 {
			ea.Match = &list[i]
			break
		}
	}
}

// ExistingImport is one accepted artist folder.
type ExistingImport struct {
	Path string
	AddInput
}

// ImportExisting adds the accepted artists with their existing folders
// (nothing is moved or renamed); the files are linked to albums afterwards
// by LinkExisting (a background job).
func (s *Service) ImportExisting(ctx context.Context, items []ExistingImport) ([]Artist, []error) {
	added := make([]Artist, len(items))
	errs := make([]error, len(items))
	for i, it := range items {
		in := it.AddInput
		in.Path = it.Path
		if _, err := lfs.MustExist(s.DataRoots, it.Path); err != nil {
			errs[i] = err
			continue
		}
		added[i], errs[i] = s.Add(ctx, in)
	}
	return added, errs
}

// LinkResult says what linking an existing artist folder found.
type LinkResult struct {
	Albums   int
	Files    int
	Unknown  []string // album folders without a matching album
	Rejected []string
}

// LinkExisting reads the album folders inside an artist's folder, matches
// each to one of the artist's albums and editions, and records the files
// in place (no transfer, no rename).
func (s *Service) LinkExisting(ctx context.Context, artistID int64) (LinkResult, error) {
	var res LinkResult
	a, err := s.Artist(ctx, artistID)
	if err != nil {
		return res, err
	}
	albums, err := s.Albums(ctx, a.ID)
	if err != nil {
		return res, err
	}
	subs, err := os.ReadDir(a.Path)
	if err != nil {
		return res, err
	}
	for _, sd := range subs {
		if !sd.IsDir() || strings.HasPrefix(sd.Name(), ".") {
			continue
		}
		dir := filepath.Join(a.Path, sd.Name())
		folder, err := ScanFolder(dir)
		if err != nil || len(folder.Files) == 0 {
			continue
		}
		al := pickAlbum(albums, &folder, sd.Name())
		if al == nil {
			res.Unknown = append(res.Unknown, dir)
			s.record(ctx, a.Ref(), timeline.TypeImportFailed, fmt.Sprintf("%s matches none of the artist's albums (check the metadata profile)", sd.Name()),
				map[string]any{"path": dir}, nil)
			continue
		}
		n, err := s.linkFolder(ctx, &a, al, dir)
		if err != nil {
			res.Rejected = append(res.Rejected, dir)
			s.record(ctx, al.Ref(), timeline.TypeImportFailed, "existing files not linked: "+err.Error(), map[string]any{"path": dir}, nil)
			continue
		}
		res.Albums++
		res.Files += n
	}
	return res, nil
}

// pickAlbum finds the album a folder holds: by release group id in the tags,
// else by the album tag (or folder name) and year.
func pickAlbum(albums []Album, f *Folder, folderName string) *Album {
	group := majority(f.Files, func(x *LocalFile) string { return x.Tags.ReleaseGroupID })
	for i := range albums {
		if group != "" && albums[i].MBID == group {
			return &albums[i]
		}
	}
	title := majority(f.Files, func(x *LocalFile) string { return x.Tags.Album })
	if title == "" {
		title = strings.TrimSpace(yearSuffix.ReplaceAllString(folderName, ""))
	}
	var best *Album
	bestScore := 0.0
	for i := range albums {
		sc := Similarity(title, albums[i].Title)
		if sc > bestScore {
			best, bestScore = &albums[i], sc
		}
	}
	if bestScore < 0.85 {
		return nil
	}
	return best
}

// linkFolder records the files of an album folder in place.
func (s *Service) linkFolder(ctx context.Context, a *Artist, al *Album, dir string) (int, error) {
	an, err := s.Analyse(ctx, al.ID, dir, 0)
	if err != nil {
		return 0, err
	}
	if an.Rejection != nil {
		return 0, an.Rejection
	}
	best := an.Best()
	if (al.ReleaseID == nil || *al.ReleaseID != best.Release.ID) && !al.ReleaseLocked {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET release_id = ? WHERE id = ?`, best.Release.ID, al.ID); err != nil {
			return 0, err
		}
	}
	existing, err := s.Files(ctx, al.ID)
	if err != nil {
		return 0, err
	}
	tracks := append([]Track{}, best.Tracks...)
	LinkFiles(tracks, existing)
	now := s.now()
	n := 0
	for _, pr := range best.Match.Pairs {
		t, f := &tracks[pr.Track], &an.Folder.Files[pr.File]
		if t.File != nil {
			continue
		}
		placed := importer.Placed{Path: f.Path, Size: f.Size, Mode: "existing", Source: f.Path}
		if err := s.saveFile(ctx, a, al, t, f, placed, QualityOf(&f.Tags), ImportOptions{}, nil, now); err != nil {
			return n, err
		}
		n++
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET path = ? WHERE id = ?`, dir, al.ID); err != nil {
		return n, err
	}
	s.record(ctx, al.Ref(), timeline.TypeFileFound, fmt.Sprintf("found %d existing files in %s (score %d, %s)", n, dir, best.Match.Score,
		best.Release.FormatSummary()), map[string]any{"path": dir, "files": n, "score": best.Match.Score, "missing": len(best.Match.Missing)}, nil)
	return n, nil
}

var yearSuffix = regexp.MustCompile(`[\s\-]*[(\[]?(?:19|20)\d{2}[)\]]?\s*$|^(?:19|20)\d{2}\s*-\s*`)

// ErrNoArtistFolder is returned when an artist's folder does not exist yet.
var ErrNoArtistFolder = errors.New("the artist folder does not exist")
