package music

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// CoverFetcher gets cover art (the MusicBrainz client implements it).
type CoverFetcher interface {
	FetchCover(ctx context.Context, releaseMBID, groupMBID, size string) ([]byte, error)
}

// Service is the music library.
type Service struct {
	DB        *db.DB
	Settings  *settings.Store
	Meta      metadata.MusicProvider
	Covers    CoverFetcher
	Quality   *quality.Store
	Formats   *customformats.Store
	Roots     *rootfolders.Service
	DataRoots []string
	Timeline  *timeline.Store
	Recycle   *recycle.Bin
	Downloads *downloads.Service
	Notifier  mediaserver.Notifier
	Log       *slog.Logger
	Now       func() time.Time
	// SearchRequested asks the wanted orchestration to search an album.
	SearchRequested func(ctx context.Context, ref library.Ref, reason string)
	// RefreshRequested queues a full refresh of an artist (releases and track
	// lists) in the background; nil = refresh in place.
	RefreshRequested func(ctx context.Context, artistID int64)
}

// Timeline event types of the music library.
const (
	TypeAlbumAdded     = "album_added"
	TypeAlbumRemoved   = "album_removed"
	TypeReleaseChanged = "release_changed"
	TypeMatched        = "release_matched"
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) record(ctx context.Context, ref library.Ref, typ, msg string, data any, downloadID *int64) {
	if s.Timeline == nil {
		return
	}
	if err := s.Timeline.Record(ctx, ref.String(), typ, msg, data, downloadID); err != nil {
		s.log().Warn("timeline", "err", err)
	}
}

func (s *Service) requestRefresh(ctx context.Context, artistID int64) {
	if s.RefreshRequested != nil {
		s.RefreshRequested(ctx, artistID)
		return
	}
	if _, err := s.Refresh(ctx, artistID, true); err != nil {
		s.log().Warn("refresh artist", "artist", artistID, "err", err)
	}
}

// LookupResult is a provider artist, marked when in the library.
type LookupResult struct {
	metadata.ArtistSummary
	ArtistID *int64
}

// Lookup searches MusicBrainz for artists (text or "mbid:<id>").
func (s *Service) Lookup(ctx context.Context, term string) ([]LookupResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return []LookupResult{}, nil
	}
	list, err := s.Meta.SearchArtists(ctx, term)
	if err != nil {
		return nil, err
	}
	out := make([]LookupResult, 0, len(list))
	for _, a := range list {
		lr := LookupResult{ArtistSummary: a}
		if ex, err := s.ArtistByMBID(ctx, a.MBID); err == nil {
			id := ex.ID
			lr.ArtistID = &id
		}
		out = append(out, lr)
	}
	return out, nil
}

// AddInput adds an artist.
type AddInput struct {
	MBID              string
	RootFolderID      int64
	QualityProfileID  int64
	MetadataProfileID int64
	Monitored         bool
	// Monitor picks the albums monitored now: all, future, latest or none.
	Monitor string
	// MonitorNewAlbums: what happens to albums that appear later (all, none, future).
	MonitorNewAlbums string
	Tags             []string
	SearchOnAdd      bool
	// Path overrides the folder (existing library import); "" = from the template.
	Path string
}

func (s *Service) rootFolder(ctx context.Context, id int64) (rootfolders.Folder, error) {
	list, err := s.Roots.List(ctx)
	if err != nil {
		return rootfolders.Folder{}, err
	}
	for _, f := range list {
		if f.ID == id {
			if f.Kind != rootfolders.KindMusic {
				return f, invalid("root folder %s is for %s, not music", f.Path, f.Kind)
			}
			return f, nil
		}
	}
	return rootfolders.Folder{}, rootfolders.ErrNotFound
}

func (s *Service) checkQualityProfile(ctx context.Context, id int64) error {
	p, err := s.Quality.Profile(ctx, id)
	if err != nil {
		return err
	}
	if p.Kind != quality.Music {
		return invalid("quality profile %s is for %s, not music", p.Name, p.Kind)
	}
	return nil
}

func (s *Service) colon(ctx context.Context) string {
	c, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
	return c
}

func (s *Service) artistInfo(a *Artist) naming.ArtistInfo {
	return naming.ArtistInfo{Name: a.Name, Disambiguation: a.Disambiguation, MBID: a.MBID}
}

// insidePath checks that p lies inside root (and the data roots).
func (s *Service) insidePath(root, p string) (string, error) {
	sp, err := lfs.SafePath(s.DataRoots, p)
	if err != nil {
		return "", err
	}
	sroot, err := lfs.SafePath(s.DataRoots, root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(sroot, sp)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", invalid("the artist folder must be inside the root folder %s", root)
	}
	return sp, nil
}

func validMonitor(v string, adding bool) bool {
	switch v {
	case MonitorAll, MonitorNone, MonitorFuture:
		return true
	case MonitorLatest:
		return adding
	}
	return false
}

func fromMeta(a *Artist, md metadata.Artist) {
	a.MBID, a.Name, a.SortName, a.Disambiguation, a.Type, a.Country = strings.ToLower(md.MBID), md.Name, md.SortName, md.Disambiguation, md.Type, md.Country
	a.Aliases = []string{}
	seen := map[string]bool{strings.ToLower(md.Name): true}
	for _, al := range md.Aliases {
		if k := strings.ToLower(al.Name); !seen[k] {
			seen[k] = true
			a.Aliases = append(a.Aliases, al.Name)
		}
	}
	a.Genres = md.Genres
	if a.Genres == nil {
		a.Genres = []string{}
	}
}

// Add adds an artist from MusicBrainz with its albums (by the metadata
// profile). Editions and track lists load in the background.
func (s *Service) Add(ctx context.Context, in AddInput) (Artist, error) {
	in.MBID = strings.ToLower(strings.TrimSpace(in.MBID))
	if in.MBID == "" {
		return Artist{}, invalid("mbid is required")
	}
	if in.Monitor == "" {
		in.Monitor = MonitorAll
	}
	if in.MonitorNewAlbums == "" {
		in.MonitorNewAlbums = MonitorAll
	}
	if !validMonitor(in.Monitor, true) || !validMonitor(in.MonitorNewAlbums, false) {
		return Artist{}, invalid("unknown monitor option")
	}
	if ex, err := s.ArtistByMBID(ctx, in.MBID); err == nil {
		return ex, ErrExists
	}
	root, err := s.rootFolder(ctx, in.RootFolderID)
	if err != nil {
		return Artist{}, err
	}
	if err := s.checkQualityProfile(ctx, in.QualityProfileID); err != nil {
		return Artist{}, err
	}
	if _, err := s.Profile(ctx, in.MetadataProfileID); err != nil {
		return Artist{}, err
	}
	md, err := s.Meta.Artist(ctx, in.MBID)
	if err != nil {
		return Artist{}, err
	}
	a := Artist{Monitored: in.Monitored, MonitorNewAlbums: in.MonitorNewAlbums, QualityProfileID: in.QualityProfileID,
		MetadataProfileID: in.MetadataProfileID, RootFolderID: &root.ID, Tags: quality.NormTags(in.Tags)}
	if a.Tags == nil {
		a.Tags = []string{}
	}
	fromMeta(&a, md)
	path := in.Path
	if path == "" {
		tmpl, err := settings.Get(ctx, s.Settings, ArtistFolderFormat)
		if err != nil {
			return a, err
		}
		name, err := naming.Render(tmpl, naming.MusicValues(s.artistInfo(&a), nil, nil), naming.Options{Colon: s.colon(ctx)})
		if err != nil {
			return a, err
		}
		path = filepath.Join(root.Path, name)
	}
	if a.Path, err = s.insidePath(root.Path, path); err != nil {
		return a, err
	}
	now := s.now()
	if err := s.DB.Tx(ctx, func(tx *sql.Tx) error { return insertArtist(ctx, tx, &a, now) }); err != nil {
		return a, err
	}
	s.record(ctx, a.Ref(), timeline.TypeAdded, fmt.Sprintf("added %s to %s", a.Name, a.Path), map[string]any{"path": a.Path,
		"monitored": a.Monitored, "monitor": in.Monitor, "monitorNewAlbums": a.MonitorNewAlbums, "profileId": a.QualityProfileID,
		"metadataProfileId": a.MetadataProfileID}, nil)
	if _, err := s.syncAlbums(ctx, &a, in.Monitor); err != nil {
		s.log().Warn("load albums", "artist", a.Name, "err", err)
	}
	s.requestRefresh(ctx, a.ID)
	if in.SearchOnAdd && a.Monitored && s.SearchRequested != nil {
		albums, _ := s.Albums(ctx, a.ID)
		for i := range albums {
			if albums[i].Monitored && s.released(&albums[i]) {
				s.SearchRequested(ctx, albums[i].Ref(), "added")
			}
		}
	}
	return s.Artist(ctx, a.ID)
}

// released reports whether an album is out (its first release date passed).
func (s *Service) released(al *Album) bool {
	t := ReleaseTime(al.ReleaseDate)
	return !t.IsZero() && !s.now().Before(t)
}

// SyncResult says what a refresh changed.
type SyncResult struct {
	Added, Removed, Kept, Updated int
}

// syncAlbums brings the artist's albums in line with MusicBrainz and the
// metadata profile. monitor is the rule for albums added now ("" = the
// artist's rule for new albums). Albums with files are never removed.
func (s *Service) syncAlbums(ctx context.Context, a *Artist, monitor string) (SyncResult, error) {
	var res SyncResult
	profile, err := s.Profile(ctx, a.MetadataProfileID)
	if err != nil {
		return res, err
	}
	groups, err := s.Meta.ReleaseGroups(ctx, a.MBID)
	if err != nil {
		return res, err
	}
	existing, err := s.Albums(ctx, a.ID)
	if err != nil {
		return res, err
	}
	byMBID := map[string]*Album{}
	for i := range existing {
		byMBID[existing[i].MBID] = &existing[i]
	}
	initial := monitor != ""
	if monitor == "" {
		monitor = a.MonitorNewAlbums
	}
	latest := ""
	if monitor == MonitorLatest {
		for _, g := range groups {
			if profile.WantsGroup(g.PrimaryType, g.SecondaryTypes) && g.FirstReleaseDate > latest && !ReleaseTime(g.FirstReleaseDate).After(s.now()) {
				latest = g.FirstReleaseDate
			}
		}
	}
	now := s.now()
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.MBID] = true
		wants := profile.WantsGroup(g.PrimaryType, g.SecondaryTypes)
		ex := byMBID[g.MBID]
		if ex == nil {
			if !wants {
				continue
			}
			mon := false
			switch monitor {
			case MonitorAll:
				mon = true
			case MonitorFuture:
				t := ReleaseTime(g.FirstReleaseDate)
				mon = t.IsZero() || !t.Before(a.AddedAt.Truncate(24*time.Hour))
			case MonitorLatest:
				mon = g.FirstReleaseDate == latest
			}
			r, err := s.DB.W.ExecContext(ctx, `INSERT INTO albums (artist_id, mbid, title, sort_title, disambiguation, primary_type, secondary_types,
				release_date, monitored, added_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, g.MBID, g.Title,
				naming.SortTitle(g.Title), g.Disambiguation, g.PrimaryType, jsonText(g.SecondaryTypes), g.FirstReleaseDate, mon, db.Millis(now), db.Millis(now))
			if err != nil {
				if strings.Contains(err.Error(), "UNIQUE") {
					continue // belongs to another artist in the library
				}
				return res, err
			}
			res.Added++
			if !initial {
				id, _ := r.LastInsertId()
				al := Album{ID: id, Title: g.Title}
				s.record(ctx, a.Ref(), TypeAlbumAdded, fmt.Sprintf("new album %s (%s)", g.Title, g.FirstReleaseDate),
					map[string]any{"albumId": id, "title": g.Title, "monitored": mon}, nil)
				s.record(ctx, al.Ref(), timeline.TypeAdded, fmt.Sprintf("appeared on MusicBrainz; monitored: %v", mon), map[string]any{"monitored": mon}, nil)
				if mon && a.Monitored && s.SearchRequested != nil && !ReleaseTime(g.FirstReleaseDate).After(now) && !ReleaseTime(g.FirstReleaseDate).IsZero() {
					s.SearchRequested(ctx, al.Ref(), "added")
				}
			}
			continue
		}
		hasFiles := s.fileCount(ctx, ex.ID) > 0
		if !wants && !hasFiles {
			if err := s.removeAlbum(ctx, a, ex, "the metadata profile no longer includes it"); err != nil {
				return res, err
			}
			res.Removed++
			continue
		}
		filtered := !wants
		if ex.Title != g.Title || ex.ReleaseDate != g.FirstReleaseDate || ex.PrimaryType != g.PrimaryType ||
			jsonText(ex.SecondaryTypes) != jsonText(g.SecondaryTypes) || ex.Filtered != filtered || ex.Disambiguation != g.Disambiguation {
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET title = ?, sort_title = ?, disambiguation = ?, primary_type = ?, secondary_types = ?,
				release_date = ?, filtered = ?, updated_at = ? WHERE id = ?`, g.Title, naming.SortTitle(g.Title), g.Disambiguation, g.PrimaryType,
				jsonText(g.SecondaryTypes), g.FirstReleaseDate, filtered, db.Millis(now), ex.ID); err != nil {
				return res, err
			}
			res.Updated++
		}
		if filtered {
			res.Kept++
		}
	}
	for _, ex := range existing {
		if seen[ex.MBID] {
			continue
		}
		if s.fileCount(ctx, ex.ID) > 0 {
			res.Kept++
			continue
		}
		if err := s.removeAlbum(ctx, a, &ex, "no longer on MusicBrainz"); err != nil {
			return res, err
		}
		res.Removed++
	}
	return res, nil
}

func (s *Service) removeAlbum(ctx context.Context, a *Artist, al *Album, why string) error {
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM albums WHERE id = ?`, al.ID); err != nil {
		return err
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, al.Ref().String())
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, al.Ref().String())
	s.record(ctx, a.Ref(), TypeAlbumRemoved, fmt.Sprintf("removed album %s: %s", al.Title, why), map[string]any{"albumId": al.ID, "title": al.Title}, nil)
	return nil
}

func (s *Service) fileCount(ctx context.Context, albumID int64) int {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM album_files WHERE album_id = ?`, albumID).Scan(&n)
	return n
}

// Refresh reloads an artist from MusicBrainz: details, albums (new ones
// follow the monitor rule; files are never touched), and with full also the
// editions of every album and the track lists of the chosen ones.
func (s *Service) Refresh(ctx context.Context, id int64, full bool) (SyncResult, error) {
	a, err := s.Artist(ctx, id)
	if err != nil {
		return SyncResult{}, err
	}
	fresh := metadata.Fresh(ctx)
	md, err := s.Meta.Artist(fresh, a.MBID)
	if err != nil {
		return SyncResult{}, err
	}
	fromMeta(&a, md)
	now := s.now()
	a.RefreshedAt = &now
	if err := saveArtist(ctx, s.DB.W, &a, now); err != nil {
		return SyncResult{}, err
	}
	res, err := s.syncAlbums(fresh, &a, "")
	if err != nil {
		return res, err
	}
	if full {
		if err := s.syncReleases(fresh, &a); err != nil {
			return res, err
		}
	}
	s.record(ctx, a.Ref(), timeline.TypeRefreshed, fmt.Sprintf("refreshed from MusicBrainz: %d new, %d updated, %d removed albums",
		res.Added, res.Updated, res.Removed), map[string]any{"added": res.Added, "updated": res.Updated, "removed": res.Removed, "kept": res.Kept}, nil)
	return res, nil
}

// RefreshAll refreshes every artist (the daily job).
func (s *Service) RefreshAll(ctx context.Context) (int, error) {
	list, err := s.artists(ctx, `ORDER BY a.metadata_refreshed_at`)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range list {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, err := s.Refresh(ctx, a.ID, true); err != nil {
			s.log().Warn("refresh artist", "artist", a.Name, "err", err)
			continue
		}
		n++
	}
	return n, nil
}

func releaseRow(r metadata.Release) (formats []string, tracks int) {
	for _, m := range r.Media {
		formats = append(formats, m.Format)
		tracks += m.TrackCount
	}
	if formats == nil {
		formats = []string{}
	}
	return formats, tracks
}

// upsertRelease stores an edition (without tracks) and returns its id.
func (s *Service) upsertRelease(ctx context.Context, albumID int64, r metadata.Release) (int64, error) {
	formats, count := releaseRow(r)
	var id int64
	err := s.DB.W.QueryRowContext(ctx, `INSERT INTO album_releases (album_id, mbid, title, status, release_date, country, disambiguation, label,
		catalog_number, barcode, formats, track_count, media_count) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (mbid) DO UPDATE SET album_id = excluded.album_id, title = excluded.title, status = excluded.status,
		release_date = excluded.release_date, country = excluded.country, disambiguation = excluded.disambiguation, label = excluded.label,
		catalog_number = excluded.catalog_number, barcode = excluded.barcode, formats = excluded.formats, track_count = excluded.track_count,
		media_count = excluded.media_count RETURNING id`, albumID, r.MBID, r.Title, r.Status, r.Date, r.Country, r.Disambiguation, r.Label,
		r.CatalogNumber, r.Barcode, jsonText(formats), count, len(r.Media)).Scan(&id)
	return id, err
}

// maxGroupLookups bounds per-album release lookups in one refresh (1 request/s).
const maxGroupLookups = 30

// syncReleases loads the editions of the artist's albums, picks one per
// album when none is chosen, and loads its track list.
func (s *Service) syncReleases(ctx context.Context, a *Artist) error {
	profile, err := s.Profile(ctx, a.MetadataProfileID)
	if err != nil {
		return err
	}
	list, err := s.Meta.Releases(ctx, a.MBID)
	if err != nil {
		return err
	}
	byGroup := map[string][]metadata.Release{}
	for _, r := range list {
		byGroup[r.ReleaseGroupMBID] = append(byGroup[r.ReleaseGroupMBID], r)
	}
	albums, err := s.Albums(ctx, a.ID)
	if err != nil {
		return err
	}
	lookups := 0
	for i := range albums {
		al := &albums[i]
		rels := byGroup[al.MBID]
		if len(rels) == 0 && lookups < maxGroupLookups {
			lookups++
			if rels, err = s.Meta.GroupReleases(ctx, al.MBID); err != nil {
				s.log().Warn("album releases", "album", al.Title, "err", err)
				continue
			}
		}
		if err := s.storeReleases(ctx, a, al, &profile, rels); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) storeReleases(ctx context.Context, a *Artist, al *Album, profile *MetadataProfile, rels []metadata.Release) error {
	if len(rels) == 0 {
		return nil
	}
	keep := map[string]bool{}
	allowed := 0
	for _, r := range rels {
		if _, err := s.upsertRelease(ctx, al.ID, r); err != nil {
			return err
		}
		keep[r.MBID] = true
		if profile.WantsStatus(r.Status) {
			allowed++
		}
	}
	// Editions that disappeared go, except the chosen one.
	stored, err := s.Releases(ctx, al.ID)
	if err != nil {
		return err
	}
	for _, r := range stored {
		if !keep[r.MBID] && (al.ReleaseID == nil || *al.ReleaseID != r.ID) {
			_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM album_releases WHERE id = ?`, r.ID)
		}
	}
	// An album whose every edition has an unwanted status (only bootlegs, say) goes too.
	if allowed == 0 && s.fileCount(ctx, al.ID) == 0 {
		return s.removeAlbum(ctx, a, al, "no edition has a wanted release status")
	}
	cur, err := s.Album(ctx, al.ID)
	if err != nil {
		return err
	}
	if cur.ReleaseID == nil {
		if err := s.pickRelease(ctx, &cur, profile); err != nil {
			return err
		}
	}
	if cur.Monitored && cur.ReleaseID != nil {
		if _, err := s.EnsureTracks(ctx, *cur.ReleaseID); err != nil {
			s.log().Warn("track list", "album", cur.Title, "err", err)
		}
	}
	return nil
}

// formatRank prefers digital and CD editions over vinyl and tapes.
func formatRank(formats []string) int {
	worst := 0
	for _, f := range formats {
		r := 1
		switch strings.ToLower(f) {
		case "digital media", "cd", "enhanced cd", "hdcd", "sacd", "blu-spec cd", "shm-cd":
			r = 0
		case "vinyl", "12\" vinyl", "7\" vinyl", "10\" vinyl", "cassette", "dvd", "dvd-audio", "dvd-video", "blu-ray":
			r = 2
		}
		worst = max(worst, r)
	}
	return worst
}

// ChooseRelease picks the edition an album is matched against by default
// (D-042): an allowed status, the most common track count (the standard
// edition rather than a deluxe one), digital or CD, the preferred country
// (or worldwide), then the earliest date.
func ChooseRelease(rels []Release, profile *MetadataProfile, country string) *Release {
	if len(rels) == 0 {
		return nil
	}
	pool := []Release{}
	for _, r := range rels {
		if profile == nil || profile.WantsStatus(r.Status) {
			pool = append(pool, r)
		}
	}
	if len(pool) == 0 {
		pool = append(pool, rels...)
	}
	counts := map[int]int{}
	for _, r := range pool {
		counts[r.TrackCount]++
	}
	mode, best := 0, -1
	for n, c := range counts {
		if c > best || (c == best && n < mode) {
			mode, best = n, c
		}
	}
	country = strings.ToUpper(country)
	countryRank := func(c string) int {
		switch {
		case c != "" && c == country:
			return 0
		case c == "XW":
			return 1
		case c == "XE" || c == "":
			return 2
		}
		return 3
	}
	sort.SliceStable(pool, func(i, j int) bool {
		a, b := pool[i], pool[j]
		if (a.TrackCount == mode) != (b.TrackCount == mode) {
			return a.TrackCount == mode
		}
		if fa, fb := formatRank(a.Formats), formatRank(b.Formats); fa != fb {
			return fa < fb
		}
		if ca, cb := countryRank(a.Country), countryRank(b.Country); ca != cb {
			return ca < cb
		}
		if (a.Date == "") != (b.Date == "") {
			return a.Date != ""
		}
		if a.Date != b.Date {
			return a.Date < b.Date
		}
		return a.MBID < b.MBID
	})
	out := pool[0]
	return &out
}

func (s *Service) pickRelease(ctx context.Context, al *Album, profile *MetadataProfile) error {
	rels, err := s.Releases(ctx, al.ID)
	if err != nil {
		return err
	}
	region, _ := settings.Get(ctx, s.Settings, metadata.Region)
	r := ChooseRelease(rels, profile, region)
	if r == nil {
		return nil
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET release_id = ?, updated_at = ? WHERE id = ?`, r.ID, db.Millis(s.now()), al.ID); err != nil {
		return err
	}
	al.ReleaseID = &r.ID
	return nil
}

// EnsureReleases loads an album's editions when none are stored yet.
func (s *Service) EnsureReleases(ctx context.Context, al *Album) ([]Release, error) {
	rels, err := s.Releases(ctx, al.ID)
	if err != nil || len(rels) > 0 {
		return rels, err
	}
	a, err := s.Artist(ctx, al.ArtistID)
	if err != nil {
		return nil, err
	}
	profile, err := s.Profile(ctx, a.MetadataProfileID)
	if err != nil {
		return nil, err
	}
	list, err := s.Meta.GroupReleases(ctx, al.MBID)
	if err != nil {
		return nil, err
	}
	for _, r := range list {
		if _, err := s.upsertRelease(ctx, al.ID, r); err != nil {
			return nil, err
		}
	}
	if al.ReleaseID == nil {
		if err := s.pickRelease(ctx, al, &profile); err != nil {
			return nil, err
		}
	}
	return s.Releases(ctx, al.ID)
}

// EnsureTracks loads a release's track list from MusicBrainz once.
func (s *Service) EnsureTracks(ctx context.Context, releaseID int64) ([]Track, error) {
	r, err := s.ReleaseByID(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if r.TracksLoaded {
		return s.Tracks(ctx, releaseID)
	}
	md, err := s.Meta.Release(ctx, r.MBID)
	if err != nil {
		return nil, err
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tracks WHERE release_id = ?`, releaseID); err != nil {
			return err
		}
		for _, m := range md.Media {
			for _, t := range m.Tracks {
				if _, err := tx.ExecContext(ctx, `INSERT INTO tracks (release_id, mbid, recording_mbid, disc, position, number, title, length_ms, artist)
					VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (release_id, disc, position) DO NOTHING`, releaseID, t.MBID, t.RecordingMBID,
					max(m.Position, 1), t.Position, t.Number, t.Title, t.LengthMs, t.Artist); err != nil {
					return err
				}
			}
		}
		formats, count := releaseRow(md)
		_, err := tx.ExecContext(ctx, `UPDATE album_releases SET tracks_loaded = 1, formats = ?, track_count = ?, media_count = ? WHERE id = ?`,
			jsonText(formats), count, len(md.Media), releaseID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return s.Tracks(ctx, releaseID)
}

// ArtistUpdate changes how an artist is managed.
type ArtistUpdate struct {
	Monitored         *bool
	MonitorNewAlbums  *string
	QualityProfileID  *int64
	MetadataProfileID *int64
	Tags              *[]string
}

// EditArtist applies an update; a new metadata profile refreshes the albums.
func (s *Service) EditArtist(ctx context.Context, id int64, u ArtistUpdate) (Artist, error) {
	a, err := s.Artist(ctx, id)
	if err != nil {
		return a, err
	}
	changes := map[string]any{}
	if u.Monitored != nil && *u.Monitored != a.Monitored {
		a.Monitored, changes["monitored"] = *u.Monitored, *u.Monitored
	}
	if u.MonitorNewAlbums != nil && *u.MonitorNewAlbums != a.MonitorNewAlbums {
		if !validMonitor(*u.MonitorNewAlbums, false) {
			return a, invalid("unknown monitor option %q", *u.MonitorNewAlbums)
		}
		a.MonitorNewAlbums, changes["monitorNewAlbums"] = *u.MonitorNewAlbums, *u.MonitorNewAlbums
	}
	if u.QualityProfileID != nil && *u.QualityProfileID != a.QualityProfileID {
		if err := s.checkQualityProfile(ctx, *u.QualityProfileID); err != nil {
			return a, err
		}
		a.QualityProfileID, changes["profileId"] = *u.QualityProfileID, *u.QualityProfileID
	}
	profileChanged := false
	if u.MetadataProfileID != nil && *u.MetadataProfileID != a.MetadataProfileID {
		if _, err := s.Profile(ctx, *u.MetadataProfileID); err != nil {
			return a, err
		}
		a.MetadataProfileID, changes["metadataProfileId"], profileChanged = *u.MetadataProfileID, *u.MetadataProfileID, true
	}
	if u.Tags != nil {
		a.Tags, changes["tags"] = quality.NormTags(*u.Tags), *u.Tags
		if a.Tags == nil {
			a.Tags = []string{}
		}
	}
	if len(changes) == 0 {
		return a, nil
	}
	if err := saveArtist(ctx, s.DB.W, &a, s.now()); err != nil {
		return a, err
	}
	s.record(ctx, a.Ref(), timeline.TypeEdited, "changed: "+keys(changes), changes, nil)
	if profileChanged {
		s.requestRefresh(ctx, a.ID)
	}
	return s.Artist(ctx, a.ID)
}

func keys(m map[string]any) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

// DeleteArtist removes an artist; with deleteFiles its folder goes to the recycle bin.
func (s *Service) DeleteArtist(ctx context.Context, id int64, deleteFiles bool) error {
	a, err := s.Artist(ctx, id)
	if err != nil {
		return err
	}
	msg := "removed from the library; files left in place"
	if deleteFiles {
		if _, err := os.Stat(a.Path); err == nil {
			e, err := s.Recycle.Recycle(ctx, a.Path, a.Ref().String(), "artist deleted")
			if err != nil {
				return fmt.Errorf("move %s to the recycle bin: %w", a.Path, err)
			}
			msg = "removed from the library; folder moved to the recycle bin (" + e.RecycledPath + ")"
		} else {
			msg = "removed from the library; the folder did not exist"
		}
	}
	albums, _ := s.Albums(ctx, id)
	s.record(ctx, a.Ref(), timeline.TypeDeleted, msg, map[string]any{"deleteFiles": deleteFiles}, nil)
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM media_items WHERE id = ?`, id); err != nil {
		return err
	}
	for _, al := range albums {
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, al.Ref().String())
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, al.Ref().String())
	}
	return nil
}

// AlbumUpdate changes an album.
type AlbumUpdate struct {
	Monitored *bool
	// ReleaseID picks an edition (locking it); 0 unlocks and lets Locarr choose again.
	ReleaseID *int64
}

// EditAlbum applies an update.
func (s *Service) EditAlbum(ctx context.Context, id int64, u AlbumUpdate) (Album, error) {
	al, err := s.Album(ctx, id)
	if err != nil {
		return al, err
	}
	now := db.Millis(s.now())
	if u.Monitored != nil && *u.Monitored != al.Monitored {
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET monitored = ?, updated_at = ? WHERE id = ?`, *u.Monitored, now, id); err != nil {
			return al, err
		}
		s.record(ctx, al.Ref(), timeline.TypeEdited, fmt.Sprintf("monitored: %v", *u.Monitored), map[string]any{"monitored": *u.Monitored}, nil)
	}
	if u.ReleaseID != nil {
		if *u.ReleaseID == 0 {
			a, err := s.Artist(ctx, al.ArtistID)
			if err != nil {
				return al, err
			}
			p, err := s.Profile(ctx, a.MetadataProfileID)
			if err != nil {
				return al, err
			}
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET release_locked = 0, release_id = NULL WHERE id = ?`, id); err != nil {
				return al, err
			}
			al.ReleaseID = nil
			if err := s.pickRelease(ctx, &al, &p); err != nil {
				return al, err
			}
			s.record(ctx, al.Ref(), TypeReleaseChanged, "edition chosen automatically again", map[string]any{"locked": false}, nil)
		} else {
			r, err := s.ReleaseByID(ctx, *u.ReleaseID)
			if err != nil {
				return al, err
			}
			if r.AlbumID != id {
				return al, invalid("release %d is not an edition of this album", r.ID)
			}
			if _, err := s.DB.W.ExecContext(ctx, `UPDATE albums SET release_id = ?, release_locked = 1, updated_at = ? WHERE id = ?`, r.ID, now, id); err != nil {
				return al, err
			}
			if _, err := s.EnsureTracks(ctx, r.ID); err != nil {
				return al, err
			}
			s.record(ctx, al.Ref(), TypeReleaseChanged, fmt.Sprintf("edition set to %s (%s, %s, %d tracks)", r.Title, r.Country, r.FormatSummary(), r.TrackCount),
				map[string]any{"releaseId": r.ID, "mbid": r.MBID, "locked": true}, nil)
		}
	}
	return s.Album(ctx, id)
}

// SetMonitored changes several albums at once.
func (s *Service) SetMonitored(ctx context.Context, ids []int64, monitored bool) error {
	for _, id := range ids {
		if _, err := s.EditAlbum(ctx, id, AlbumUpdate{Monitored: &monitored}); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return nil
}
