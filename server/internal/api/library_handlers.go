package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/movies"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

func errBody(err error) gen.Error {
	if se := mapError(err); se != nil {
		return gen.Error{Code: se.Code, Message: se.Message}
	}
	return gen.Error{Code: "internal", Message: err.Error()}
}

// GetMovieCollection implements GET /movies/collections/{tmdbId}.
func (h *Handler) GetMovieCollection(ctx context.Context, p gen.GetMovieCollectionParams) (*gen.MovieCollection, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	c, err := h.Movies.Collection(ctx, p.TmdbId)
	if err != nil {
		return nil, err
	}
	out := &gen.MovieCollection{ID: c.ID, Name: c.Name, Overview: c.Overview, Poster: c.Poster, Backdrop: c.Backdrop,
		Members: make([]gen.MovieLookup, 0, len(c.Members))}
	for _, m := range c.Members {
		out.Members = append(out.Members, toLookup(m.MovieSummary, m.MovieID))
	}
	return out, nil
}

// AddMovieCollection implements POST /movies/collections/{tmdbId}.
func (h *Handler) AddMovieCollection(ctx context.Context, req *gen.AddOptions, p gen.AddMovieCollectionParams) ([]gen.Movie, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	added, err := h.Movies.AddMissing(ctx, p.TmdbId, addInput(*req))
	if err != nil {
		return nil, err
	}
	out := make([]gen.Movie, 0, len(added))
	for _, m := range added {
		if mo, err := h.movieOut(ctx, m.ID); err == nil {
			out = append(out, *mo)
		}
	}
	return out, nil
}

func summaryPtr(s *metadata.MovieSummary) gen.OptMovieLookup {
	if s == nil {
		return gen.OptMovieLookup{}
	}
	return gen.NewOptMovieLookup(toLookup(*s, nil))
}

// ScanExistingMovies implements GET /movies/import/scan.
func (h *Handler) ScanExistingMovies(ctx context.Context, p gen.ScanExistingMoviesParams) ([]gen.ExistingFolder, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Movies.ScanRootFolder(ctx, p.RootFolderId)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ExistingFolder, 0, len(list))
	for _, f := range list {
		g := gen.ExistingFolder{Path: f.Path, Name: f.Name, Title: f.Title, Year: f.Year, Video: optStr(f.Video), Match: summaryPtr(f.Match),
			Candidates: make([]gen.MovieLookup, 0, len(f.Candidates)), Error: optStr(f.Error)}
		if f.VideoSize > 0 {
			g.VideoSizeBytes = gen.NewOptInt64(f.VideoSize)
		}
		for _, c := range f.Candidates {
			g.Candidates = append(g.Candidates, toLookup(c, nil))
		}
		out = append(out, g)
	}
	return out, nil
}

// ImportExistingMovies implements POST /movies/import/existing.
func (h *Handler) ImportExistingMovies(ctx context.Context, req *gen.ImportExistingRequest) ([]gen.ImportExistingResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	base := addInput(gen.AddOptions{RootFolderId: req.RootFolderId, QualityProfileId: req.QualityProfileId, MinimumAvailability: req.MinimumAvailability,
		Monitored: req.Monitored, Tags: req.Tags, SearchOnAdd: req.SearchOnAdd})
	list := make([]movies.ExistingImport, 0, len(req.Items))
	for _, it := range req.Items {
		in := base
		in.TMDbID = it.TmdbId
		list = append(list, movies.ExistingImport{Path: it.Path, AddInput: in})
	}
	added, errs := h.Movies.ImportExisting(ctx, list)
	out := make([]gen.ImportExistingResult, 0, len(list))
	for i := range list {
		r := gen.ImportExistingResult{Path: list[i].Path}
		if errs[i] != nil {
			r.Error = gen.NewOptError(errBody(errs[i]))
		}
		if added[i].ID > 0 {
			if mo, err := h.movieOut(ctx, added[i].ID); err == nil {
				r.Movie = gen.NewOptMovie(*mo)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// PreviewManualImport implements GET /movies/import/manual.
func (h *Handler) PreviewManualImport(ctx context.Context, p gen.PreviewManualImportParams) ([]gen.ManualImportCandidate, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Movies.PreviewImport(ctx, p.Path, p.MovieId.Or(0))
	if err != nil {
		return nil, err
	}
	out := make([]gen.ManualImportCandidate, 0, len(list))
	for _, c := range list {
		g := gen.ManualImportCandidate{Path: c.Path, RelativePath: c.Rel, SizeBytes: c.Size, Skipped: optStr(c.Skip), Sidecars: c.Sidecars,
			MovieTitle: optStr(c.MovieTitle), ReleaseGroup: optStr(c.Release.Group)}
		if g.Sidecars == nil {
			g.Sidecars = []string{}
		}
		if c.MovieID > 0 {
			g.MovieId, g.MovieYear = gen.NewOptInt64(c.MovieID), gen.NewOptInt(c.MovieYear)
		}
		if c.Skip == "" {
			g.Quality = gen.NewOptQuality(toQuality(c.Quality.ID))
		}
		if c.Rejection != nil {
			g.Rejection = gen.NewOptError(gen.Error{Code: c.Rejection.Code, Message: c.Rejection.Message})
		}
		out = append(out, g)
	}
	return out, nil
}

// ManualImport implements POST /movies/import/manual.
func (h *Handler) ManualImport(ctx context.Context, req *gen.ManualImportRequest) ([]gen.ManualImportResult, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	reqs := make([]movies.ImportRequest, 0, len(req.Items))
	for _, it := range req.Items {
		reqs = append(reqs, movies.ImportRequest{MovieID: it.MovieId, Path: it.Path, Mode: string(it.Mode.Or("")), Sidecars: it.Sidecars})
	}
	res, errs := h.Movies.ImportFiles(ctx, reqs)
	out := make([]gen.ManualImportResult, 0, len(reqs))
	for i, r := range reqs {
		g := gen.ManualImportResult{Path: r.Path, MovieId: r.MovieID}
		if errs[i] != nil {
			g.Error = gen.NewOptError(errBody(errs[i]))
		} else {
			g.ImportedPath, g.Mode, g.Upgraded = optStr(res[i].Path), optStr(res[i].Mode), gen.NewOptBool(res[i].Upgraded)
			g.Recycled = res[i].Recycled
		}
		out = append(out, g)
	}
	return out, nil
}

func toImportList(l movies.ImportList) gen.ImportList {
	return gen.ImportList{ID: l.ID, Name: l.Name, Kind: gen.ImportListKind(l.Kind), ListId: l.ListID, Enabled: l.Enabled,
		RootFolderId: l.RootFolderID, QualityProfileId: l.QualityProfileID, MinimumAvailability: gen.MinimumAvailability(l.MinimumAvailability),
		Monitored: l.Monitored, SearchOnAdd: l.SearchOnAdd, Tags: l.Tags, LastSyncAt: optTime(l.LastSyncAt), LastAdded: l.LastAdded,
		LastError: l.LastError, CreatedAt: l.CreatedAt}
}

func fromImportList(in *gen.ImportListInput) movies.ImportList {
	return movies.ImportList{Name: in.Name, Kind: string(in.Kind), ListID: in.ListId, Enabled: in.Enabled.Or(true), RootFolderID: in.RootFolderId,
		QualityProfileID: in.QualityProfileId, MinimumAvailability: library.Availability(in.MinimumAvailability.Or(gen.MinimumAvailabilityReleased)),
		Monitored: in.Monitored.Or(true), SearchOnAdd: in.SearchOnAdd.Or(false), Tags: in.Tags}
}

// ListImportLists implements GET /movies/importlists.
func (h *Handler) ListImportLists(ctx context.Context) ([]gen.ImportList, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Movies.Lists(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ImportList, 0, len(list))
	for _, l := range list {
		out = append(out, toImportList(l))
	}
	return out, nil
}

// CreateImportList implements POST /movies/importlists.
func (h *Handler) CreateImportList(ctx context.Context, req *gen.ImportListInput) (*gen.ImportList, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.Movies.SaveList(ctx, fromImportList(req))
	if err != nil {
		return nil, err
	}
	out := toImportList(l)
	return &out, nil
}

// UpdateImportList implements PUT /movies/importlists/{id}.
func (h *Handler) UpdateImportList(ctx context.Context, req *gen.ImportListInput, p gen.UpdateImportListParams) (*gen.ImportList, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	in := fromImportList(req)
	in.ID = p.ID
	l, err := h.Movies.SaveList(ctx, in)
	if err != nil {
		return nil, err
	}
	out := toImportList(l)
	return &out, nil
}

// DeleteImportList implements DELETE /movies/importlists/{id}.
func (h *Handler) DeleteImportList(ctx context.Context, p gen.DeleteImportListParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Movies.DeleteList(ctx, p.ID)
}

// SyncImportList implements POST /movies/importlists/{id}/sync.
func (h *Handler) SyncImportList(ctx context.Context, p gen.SyncImportListParams) (*gen.ImportListSyncResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := h.Movies.SyncList(ctx, p.ID)
	if err != nil && errors.Is(err, movies.ErrListNotFound) {
		return nil, err
	}
	return &gen.ImportListSyncResult{ListId: r.ListID, Found: r.Found, Added: r.Added, Excluded: r.Excluded, Error: optStr(r.Error)}, nil
}

// ListImportExclusions implements GET /movies/exclusions.
func (h *Handler) ListImportExclusions(ctx context.Context) ([]gen.ImportExclusion, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	list, err := h.Movies.Exclusions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ImportExclusion, 0, len(list))
	for _, e := range list {
		out = append(out, gen.ImportExclusion{TmdbId: e.TMDbID, Title: e.Title, Year: e.Year, CreatedAt: e.CreatedAt})
	}
	return out, nil
}

// DeleteImportExclusion implements DELETE /movies/exclusions/{tmdbId}.
func (h *Handler) DeleteImportExclusion(ctx context.Context, p gen.DeleteImportExclusionParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Movies.DeleteExclusion(ctx, p.TmdbId)
}

// explain derives the explanation for a ref.
func (h *Handler) explain(ctx context.Context, ref library.Ref) (timeline.Explanation, string, error) {
	if ref.Kind != library.RefMovie {
		return timeline.Explanation{}, "", fmt.Errorf("%w: only movies have timelines so far", library.ErrBadRef)
	}
	it, err := h.Movies.Item(ctx, ref.ID)
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	facts, err := h.Timeline.Facts(ctx, ref.String(), "movies")
	if err != nil {
		return timeline.Explanation{}, "", err
	}
	st := timeline.ItemState{Ref: ref.String(), Monitored: it.Monitored, Available: it.Available, AvailableFrom: it.AvailableFrom,
		HasFile: it.File != nil, CutoffMet: it.CutoffMet}
	return timeline.Explain(st, facts), it.Title, nil
}

func toExplanation(e timeline.Explanation) (gen.Explanation, error) {
	return convert[gen.Explanation](e)
}

// GetTimeline implements GET /timeline.
func (h *Handler) GetTimeline(ctx context.Context, p gen.GetTimelineParams) (*gen.Timeline, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := library.ParseRef(p.Item)
	if err != nil {
		return nil, err
	}
	ex, title, err := h.explain(ctx, ref)
	if err != nil {
		return nil, err
	}
	events, err := h.Timeline.List(ctx, ref.String(), p.Limit.Or(500))
	if err != nil {
		return nil, err
	}
	ge, err := toExplanation(ex)
	if err != nil {
		return nil, err
	}
	out := &gen.Timeline{Item: ref.String(), Title: optStr(title), Explanation: ge, Events: make([]gen.TimelineEvent, 0, len(events))}
	for _, e := range events {
		out.Events = append(out.Events, gen.TimelineEvent{ID: e.ID, At: e.At, Type: e.Type, Source: gen.TimelineEventSource(e.Source),
			Message: e.Message, Data: rawJSON(e.Data), DownloadId: optI64(e.DownloadID)})
	}
	return out, nil
}

// ExplainItem implements GET /timeline/explain.
func (h *Handler) ExplainItem(ctx context.Context, p gen.ExplainItemParams) (*gen.Explanation, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	ref, err := library.ParseRef(p.Item)
	if err != nil {
		return nil, err
	}
	ex, _, err := h.explain(ctx, ref)
	if err != nil {
		return nil, err
	}
	out, err := toExplanation(ex)
	return &out, err
}

func day(t time.Time) string { return t.Format("2006-01-02") }

// GetCalendar implements GET /calendar.
func (h *Handler) GetCalendar(ctx context.Context, p gen.GetCalendarParams) ([]gen.CalendarEntry, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	if p.End.Before(p.Start) || p.End.Sub(p.Start) > 400*24*time.Hour {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: "end must be after start and at most 400 days later"}
	}
	list, err := h.Movies.Calendar(ctx, day(p.Start), day(p.End))
	if err != nil {
		return nil, err
	}
	out := make([]gen.CalendarEntry, 0, len(list))
	for _, e := range list {
		d, _ := time.Parse("2006-01-02", e.Date)
		out = append(out, gen.CalendarEntry{Date: d, Kind: gen.CalendarEntryKind(e.Kind), MovieId: e.MovieID, Title: e.Title, Year: e.Year,
			HasFile: e.HasFile, Monitored: e.Monitored, Poster: e.Poster})
	}
	return out, nil
}

func tokenHash(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// GetCalendarToken implements GET /calendar/token.
func (h *Handler) GetCalendarToken(ctx context.Context) (*gen.SecretState, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	var n int
	if err := h.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM calendar_tokens WHERE user_id = ?`, p.User.ID).Scan(&n); err != nil {
		return nil, err
	}
	return &gen.SecretState{Configured: n > 0}, nil
}

// CreateCalendarToken implements POST /calendar/token.
func (h *Handler) CreateCalendarToken(ctx context.Context) (*gen.CalendarFeed, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if p.ReadOnly {
		return nil, errReadOnly
	}
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	tok := "lc_" + base64.RawURLEncoding.EncodeToString(b[:])
	if _, err := h.DB.W.ExecContext(ctx, `INSERT INTO calendar_tokens (user_id, token_hash, created_at) VALUES (?, ?, ?)
		ON CONFLICT (user_id) DO UPDATE SET token_hash = excluded.token_hash, created_at = excluded.created_at`,
		p.User.ID, tokenHash(tok), db.Millis(time.Now())); err != nil {
		return nil, err
	}
	return &gen.CalendarFeed{Token: tok, Path: "/api/v1/calendar/feed.ics?token=" + tok}, nil
}

// DeleteCalendarToken implements DELETE /calendar/token.
func (h *Handler) DeleteCalendarToken(ctx context.Context) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	_, err = h.DB.W.ExecContext(ctx, `DELETE FROM calendar_tokens WHERE user_id = ?`, p.User.ID)
	return err
}

func icalEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`, "\r", "").Replace(s)
}

// GetCalendarFeed implements GET /calendar/feed.ics (token in the link).
func (h *Handler) GetCalendarFeed(ctx context.Context, p gen.GetCalendarFeedParams) (gen.GetCalendarFeedOK, error) {
	var userID int64
	err := h.DB.R.QueryRowContext(ctx, `SELECT t.user_id FROM calendar_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = ? AND u.disabled = 0`, tokenHash(p.Token)).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return gen.GetCalendarFeedOK{}, &StatusError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "unknown calendar link"}
	}
	if err != nil {
		return gen.GetCalendarFeedOK{}, err
	}
	now := time.Now().UTC()
	list, err := h.Movies.Calendar(ctx, day(now.AddDate(0, 0, -p.PastDays.Or(30))), day(now.AddDate(0, 0, p.FutureDays.Or(365))))
	if err != nil {
		return gen.GetCalendarFeedOK{}, err
	}
	labels := map[string]string{movies.DateInCinemas: "in cinemas", movies.DateDigital: "digital release", movies.DatePhysical: "physical release"}
	var b bytes.Buffer
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Locarr//Calendar//EN\r\nCALSCALE:GREGORIAN\r\nX-WR-CALNAME:Locarr\r\n")
	stamp := now.Format("20060102T150405Z")
	for _, e := range list {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		fmt.Fprintf(&b, "BEGIN:VEVENT\r\nUID:locarr-movie-%d-%s@locarr\r\nDTSTAMP:%s\r\nDTSTART;VALUE=DATE:%s\r\nDTEND;VALUE=DATE:%s\r\nSUMMARY:%s\r\nEND:VEVENT\r\n",
			e.MovieID, e.Kind, stamp, d.Format("20060102"), d.AddDate(0, 0, 1).Format("20060102"),
			icalEscape(fmt.Sprintf("%s (%d) - %s", e.Title, e.Year, labels[e.Kind])))
	}
	b.WriteString("END:VCALENDAR\r\n")
	return gen.GetCalendarFeedOK{Data: &b}, nil
}

// GetTmdbImage implements GET /images/tmdb/{size}/{file}.
func (h *Handler) GetTmdbImage(ctx context.Context, p gen.GetTmdbImageParams) (gen.GetTmdbImageRes, error) {
	if err := requireUser(ctx); err != nil {
		return nil, err
	}
	img, err := h.Images.Get(ctx, string(p.Size), p.File)
	if err != nil {
		return nil, err
	}
	cache := gen.NewOptString("private, max-age=604800")
	if inm, ok := p.IfNoneMatch.Get(); ok && strings.Contains(inm, img.ETag) {
		return &gen.GetTmdbImageNotModified{ETag: gen.NewOptString(img.ETag), CacheControl: cache}, nil
	}
	f, err := os.Open(img.Path) // #nosec G304 -- inside the image cache, name validated
	if err != nil {
		return nil, err
	}
	return &gen.GetTmdbImageOKHeaders{ETag: gen.NewOptString(img.ETag), CacheControl: cache, ContentType: img.ContentType,
		Response: gen.GetTmdbImageOK{Data: f}}, nil
}

// GetRecycleBin implements GET /recyclebin.
func (h *Handler) GetRecycleBin(ctx context.Context) (*gen.RecycleBin, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	folder, _ := h.Recycle.Folder(ctx)
	days, err := settings.Get(ctx, h.Settings, library.RecycleBinDays)
	if err != nil {
		return nil, err
	}
	list, err := h.Recycle.List(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.RecycleBin{Folder: folder, RetentionDays: days, Entries: make([]gen.RecycleEntry, 0, len(list))}
	for _, e := range list {
		out.Entries = append(out.Entries, gen.RecycleEntry{ID: e.ID, OriginalPath: e.OriginalPath, RecycledPath: e.RecycledPath, SizeBytes: e.Size,
			IsDir: e.IsDir, ItemRef: e.ItemRef, Reason: e.Reason, RecycledAt: e.RecycledAt})
	}
	return out, nil
}

// EmptyRecycleBin implements DELETE /recyclebin.
func (h *Handler) EmptyRecycleBin(ctx context.Context) (*gen.QueuedCount, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	n, err := h.Recycle.Empty(ctx)
	if err != nil {
		return nil, err
	}
	return &gen.QueuedCount{Count: n}, nil
}

// DeleteRecycleEntry implements DELETE /recyclebin/{id}.
func (h *Handler) DeleteRecycleEntry(ctx context.Context, p gen.DeleteRecycleEntryParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Recycle.Delete(ctx, p.ID)
}

// RestoreRecycleEntry implements POST /recyclebin/{id}/restore.
func (h *Handler) RestoreRecycleEntry(ctx context.Context, p gen.RestoreRecycleEntryParams) (*gen.RecycleEntry, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	e, err := h.Recycle.Restore(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	if ref, err := library.ParseRef(e.ItemRef); err == nil {
		_ = h.Timeline.Record(ctx, ref.String(), timeline.TypeRecycled, "restored from the recycle bin: "+e.OriginalPath,
			map[string]any{"path": e.OriginalPath, "restored": true}, nil)
	}
	return &gen.RecycleEntry{ID: e.ID, OriginalPath: e.OriginalPath, RecycledPath: e.RecycledPath, SizeBytes: e.Size, IsDir: e.IsDir,
		ItemRef: e.ItemRef, Reason: e.Reason, RecycledAt: e.RecycledAt}, nil
}

func (h *Handler) mediaManagement(ctx context.Context) (*gen.MediaManagementSettings, error) {
	var (
		out gen.MediaManagementSettings
		err error
	)
	get := func(f func() error) {
		if err == nil {
			err = f()
		}
	}
	str := func(k settings.Key[string], dst *string) {
		get(func() (e error) { *dst, e = settings.Get(ctx, h.Settings, k); return })
	}
	boolean := func(k settings.Key[bool], dst *bool) {
		get(func() (e error) { *dst, e = settings.Get(ctx, h.Settings, k); return })
	}
	integer := func(k settings.Key[int], dst *int) {
		get(func() (e error) { *dst, e = settings.Get(ctx, h.Settings, k); return })
	}
	var colon, mode string
	boolean(library.RenameMovies, &out.RenameMovies)
	str(library.MovieFolderFormat, &out.MovieFolderFormat)
	str(library.MovieFileFormat, &out.MovieFileFormat)
	str(library.ColonReplacement, &colon)
	str(library.TransferMode, &mode)
	str(library.RecycleBinPath, &out.RecycleBinPath)
	integer(library.RecycleBinDays, &out.RecycleBinRetentionDays)
	boolean(library.SetPermissions, &out.SetPermissions)
	str(library.FileMode, &out.FileMode)
	str(library.FolderMode, &out.FolderMode)
	integer(library.AvailabilityDelay, &out.AvailabilityDelayDays)
	boolean(library.SearchOnAddDefault, &out.SearchOnAddDefault)
	get(func() (e error) {
		out.ExtraExtensions, e = settings.Get(ctx, h.Settings, library.ExtraExtensions)
		return
	})
	out.ColonReplacement, out.TransferMode = gen.ColonReplacement(colon), gen.TransferMode(mode)
	return &out, err
}

// GetMediaManagement implements GET /settings/mediamanagement.
func (h *Handler) GetMediaManagement(ctx context.Context) (*gen.MediaManagementSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.mediaManagement(ctx)
}

func validation(msg string) error {
	return &StatusError{Status: http.StatusBadRequest, Code: "validation", Message: msg}
}

// UpdateMediaManagement implements PUT /settings/mediamanagement.
func (h *Handler) UpdateMediaManagement(ctx context.Context, req *gen.MediaManagementSettings) (*gen.MediaManagementSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if err := naming.Validate(req.MovieFolderFormat, naming.MovieTokens); err != nil {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_template", Message: "folder: " + err.Error()}
	}
	if err := naming.Validate(req.MovieFileFormat, naming.MovieTokens); err != nil {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_template", Message: "file: " + err.Error()}
	}
	bin := strings.TrimSpace(req.RecycleBinPath)
	if bin != "" {
		if _, err := lfs.SafePath(h.DataRoots, bin); err != nil {
			return nil, err
		}
	}
	var exts []string
	for _, e := range req.ExtraExtensions {
		e = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(e), "."))
		if e == "" {
			continue
		}
		for _, r := range e {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
				return nil, validation("extensions may only hold letters and digits")
			}
		}
		exts = append(exts, e)
	}
	if exts == nil {
		exts = []string{}
	}
	err := h.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, f := range []func() error{
			func() error { return settings.Set(ctx, h.Settings, tx, library.RenameMovies, req.RenameMovies) },
			func() error {
				return settings.Set(ctx, h.Settings, tx, library.MovieFolderFormat, req.MovieFolderFormat)
			},
			func() error { return settings.Set(ctx, h.Settings, tx, library.MovieFileFormat, req.MovieFileFormat) },
			func() error {
				return settings.Set(ctx, h.Settings, tx, library.ColonReplacement, string(req.ColonReplacement))
			},
			func() error { return settings.Set(ctx, h.Settings, tx, library.TransferMode, string(req.TransferMode)) },
			func() error { return settings.Set(ctx, h.Settings, tx, library.RecycleBinPath, bin) },
			func() error {
				return settings.Set(ctx, h.Settings, tx, library.RecycleBinDays, req.RecycleBinRetentionDays)
			},
			func() error { return settings.Set(ctx, h.Settings, tx, library.SetPermissions, req.SetPermissions) },
			func() error { return settings.Set(ctx, h.Settings, tx, library.FileMode, req.FileMode) },
			func() error { return settings.Set(ctx, h.Settings, tx, library.FolderMode, req.FolderMode) },
			func() error { return settings.Set(ctx, h.Settings, tx, library.ExtraExtensions, exts) },
			func() error {
				return settings.Set(ctx, h.Settings, tx, library.AvailabilityDelay, req.AvailabilityDelayDays)
			},
			func() error {
				return settings.Set(ctx, h.Settings, tx, library.SearchOnAddDefault, req.SearchOnAddDefault)
			},
		} {
			if err := f(); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return h.mediaManagement(ctx)
}

// examplePreview is the movie naming previews use.
func examplePreview() (naming.MovieInfo, naming.FileInfo) {
	r := parser.Parse("The.Matrix.1999.Remastered.1080p.BluRay.DTS-HD.MA.5.1.x265.10bit-GROUP")
	q := quality.Identify(&r, quality.Movies)
	return naming.MovieInfo{Title: "The Matrix", OriginalTitle: "The Matrix", Year: 1999, TMDbID: 603, IMDbID: "tt0133093",
		Collection: "The Matrix Collection"}, naming.FileInfo{Release: &r, Quality: q, CustomFormats: []string{"x265"}}
}

// PreviewNaming implements POST /settings/mediamanagement/preview.
func (h *Handler) PreviewNaming(ctx context.Context, req *gen.NamingPreviewRequest) (*gen.NamingPreview, error) {
	if _, err := manager(ctx); err != nil {
		return nil, err
	}
	mi, fi := examplePreview()
	opts := naming.Options{Colon: string(req.ColonReplacement.Or(gen.ColonReplacementDash))}
	out := &gen.NamingPreview{Tokens: make([]gen.NamingToken, 0, len(naming.MovieTokens))}
	for _, t := range naming.MovieTokens {
		out.Tokens = append(out.Tokens, gen.NamingToken{Name: t.Name, Example: t.Example})
	}
	if err := naming.Validate(req.MovieFolderFormat, naming.MovieTokens); err != nil {
		out.FolderError = gen.NewOptString(err.Error())
	} else if s, err := naming.Render(req.MovieFolderFormat, naming.MovieValues(mi, nil), opts); err == nil {
		out.Folder = s
	}
	if err := naming.Validate(req.MovieFileFormat, naming.MovieTokens); err != nil {
		out.FileError = gen.NewOptString(err.Error())
	} else if s, err := naming.Render(req.MovieFileFormat, naming.MovieValues(mi, &fi), opts); err == nil {
		out.File = naming.WithExtension(s, ".mkv")
	}
	return out, nil
}

func (h *Handler) metadataSettings(ctx context.Context) (*gen.MetadataSettings, error) {
	ok, err := h.Settings.Configured(ctx, settings.TMDbAPIKey)
	if err != nil {
		return nil, err
	}
	lang, err := settings.Get(ctx, h.Settings, metadata.Language)
	if err != nil {
		return nil, err
	}
	region, err := settings.Get(ctx, h.Settings, metadata.Region)
	if err != nil {
		return nil, err
	}
	return &gen.MetadataSettings{TmdbApiKey: gen.SecretState{Configured: ok}, Language: lang, Region: region}, nil
}

// GetMetadataSettings implements GET /settings/metadata.
func (h *Handler) GetMetadataSettings(ctx context.Context) (*gen.MetadataSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.metadataSettings(ctx)
}

// UpdateMetadataSettings implements PUT /settings/metadata.
func (h *Handler) UpdateMetadataSettings(ctx context.Context, req *gen.MetadataSettingsInput) (*gen.MetadataSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	if key, ok := req.TmdbApiKey.Get(); ok {
		key = strings.TrimSpace(key)
		if key != "" && h.TMDb != nil {
			if err := h.TMDb.Validate(ctx, key); err != nil {
				return nil, err
			}
		}
		if err := h.Settings.SetSecret(ctx, settings.TMDbAPIKey, key); err != nil {
			return nil, err
		}
	}
	changed := false
	if v, ok := req.Language.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, metadata.Language, v); err != nil {
			return nil, err
		}
		changed = true
	}
	if v, ok := req.Region.Get(); ok {
		if err := settings.Set(ctx, h.Settings, nil, metadata.Region, v); err != nil {
			return nil, err
		}
		changed = true
	}
	if changed && h.MetaCache != nil {
		_ = h.MetaCache.Clear(ctx)
	}
	return h.metadataSettings(ctx)
}
