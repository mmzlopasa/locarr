package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Import rejection codes.
const (
	RejectNotUpgrade = "not_an_upgrade"
	RejectNoVideo    = "no_video_file"
	RejectNoEpisode  = "no_matching_episode"
	RejectNoSeries   = "no_matching_series"
	RejectExists     = "already_imported"
	RejectDisc       = "disc_structure"
)

// ImportError is an import that cannot go ahead, with a stable code.
type ImportError struct {
	Code    string
	Message string
}

func (e *ImportError) Error() string { return e.Message }

func rejectf(code, format string, a ...any) error {
	return &ImportError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// IsImportError returns the import error inside err.
func IsImportError(err error) (*ImportError, bool) {
	var ie *ImportError
	ok := errors.As(err, &ie)
	return ie, ok
}

type analysis struct {
	Release parser.Release
	Quality quality.Quality
	Formats []string
	Score   int
}

// parseFile reads a file's episodes from its path, falling back to the
// release name for the episodes (single-file releases) and for the quality
// when the file name says less.
func parseFile(path, releaseTitle string, single bool) parser.Release {
	rel := parser.ParsePath(path)
	if releaseTitle == "" {
		return rel
	}
	named := parser.Parse(releaseTitle)
	noEpisodes := len(rel.Episodes) == 0 && len(rel.AbsoluteEpisodes) == 0 && rel.AirDate == ""
	if noEpisodes && single && !IsPack(&named) {
		rel.Seasons, rel.Episodes, rel.AbsoluteEpisodes, rel.AirDate = named.Seasons, named.Episodes, named.AbsoluteEpisodes, named.AirDate
	}
	if quality.Identify(&rel, quality.Series).ID == quality.UnknownVideo || (named.Confidence > rel.Confidence && rel.Obfuscated) {
		rel.Video, rel.Audio, rel.Revision, rel.Languages = named.Video, named.Audio, named.Revision, named.Languages
		rel.StreamingService = named.StreamingService
		if rel.Group == "" {
			rel.Group = named.Group
		}
	}
	return rel
}

func (s *Service) analyse(ctx context.Context, v *Series, rel parser.Release, title string) (analysis, error) {
	a := analysis{Release: rel, Quality: quality.Identify(&rel, quality.Series)}
	p, err := s.Quality.Profile(ctx, v.QualityProfileID)
	if err != nil {
		return a, err
	}
	list, err := s.Formats.List(ctx)
	if err != nil {
		return a, err
	}
	compiled, _ := customformats.CompileAll(list)
	matches, score := customformats.Score(compiled, &p, customformats.Input{Title: title, Release: &rel, Quality: a.Quality,
		OriginalLanguage: v.OriginalLanguage})
	for _, mt := range matches {
		a.Formats = append(a.Formats, mt.Name)
	}
	a.Score = score
	return a, nil
}

func qualityName(id string) string {
	if q, ok := quality.ByID(id); ok {
		return q.Name
	}
	return id
}

// better reports whether a beats the existing file under the profile.
func better(p *quality.Profile, a analysis, old *File) (bool, string) {
	if old == nil {
		return true, ""
	}
	newPos, _ := p.Position(a.Quality.ID)
	oldPos, _ := p.Position(old.Quality)
	if newPos < 0 {
		newPos = len(p.Items)
	}
	if oldPos < 0 {
		oldPos = len(p.Items)
	}
	switch {
	case newPos < oldPos:
		return true, ""
	case newPos > oldPos:
		return false, fmt.Sprintf("%s is not better than the current %s", a.Quality.Name, qualityName(old.Quality))
	case a.Release.Revision.Version > max(old.Revision.Version, 1):
		return true, ""
	case a.Score > old.Score:
		return true, ""
	}
	return false, fmt.Sprintf("same quality (%s) and custom format score (%d) as the current file", a.Quality.Name, a.Score)
}

func mediaInfo(r *parser.Release) MediaInfo {
	mi := MediaInfo{Source: "name", Resolution: r.Video.Resolution, VideoCodec: r.Video.Codec, BitDepth: r.Video.BitDepth, HDR: r.Video.HDR,
		AudioChannels: r.Audio.Channels, Atmos: r.Audio.Atmos, Languages: r.Languages, Subtitles: r.SubtitleLanguages}
	if r.Video.Encoder != "" {
		mi.VideoCodec = r.Video.Encoder
	}
	if len(r.Audio.Codecs) > 0 {
		mi.AudioCodec = r.Audio.Codecs[0]
	}
	return mi
}

// episodeFormat returns the file template for the series type.
func (s *Service) episodeFormat(ctx context.Context, seriesType string) (string, error) {
	switch seriesType {
	case TypeDaily:
		return settings.Get(ctx, s.Settings, DailyEpisodeFormat)
	case TypeAnime:
		return settings.Get(ctx, s.Settings, AnimeEpisodeFormat)
	}
	return settings.Get(ctx, s.Settings, StandardEpisodeFormat)
}

func episodeInfos(eps []*Episode) []naming.EpisodeInfo {
	out := make([]naming.EpisodeInfo, 0, len(eps))
	for _, e := range eps {
		out = append(out, naming.EpisodeInfo{Season: e.Season, Number: e.Number, Absolute: e.Absolute, AirDate: e.AirDate, Title: e.Title})
	}
	return out
}

// seasonFolder returns the folder below the series folder ("" = none).
func (s *Service) seasonFolder(ctx context.Context, v *Series, season int) (string, error) {
	if season == 0 {
		tmpl, err := settings.Get(ctx, s.Settings, SpecialsFolderFormat)
		if err != nil {
			return "", err
		}
		return naming.Render(tmpl, naming.SeasonValues(s.info(v), 0), s.colon(ctx))
	}
	if !v.SeasonFolder {
		return "", nil
	}
	tmpl, err := settings.Get(ctx, s.Settings, SeasonFolderFormat)
	if err != nil {
		return "", err
	}
	return naming.Render(tmpl, naming.SeasonValues(s.info(v), season), s.colon(ctx))
}

// destination returns the folder and file name (without extension) for a file of eps.
func (s *Service) destination(ctx context.Context, v *Series, eps []*Episode, a *analysis, original string) (string, string, error) {
	folder := v.Path
	sub, err := s.seasonFolder(ctx, v, eps[0].Season)
	if err != nil {
		return "", "", err
	}
	if sub != "" {
		folder = filepath.Join(folder, sub)
	}
	rename, err := settings.Get(ctx, s.Settings, RenameEpisodes)
	if err != nil {
		return "", "", err
	}
	if !rename {
		return folder, strings.TrimSuffix(filepath.Base(original), filepath.Ext(original)), nil
	}
	tmpl, err := s.episodeFormat(ctx, v.SeriesType)
	if err != nil {
		return "", "", err
	}
	style, _ := settings.Get(ctx, s.Settings, MultiEpisodeStyle)
	var fi *naming.FileInfo
	if a != nil {
		rel := a.Release
		fi = &naming.FileInfo{Release: &rel, Quality: a.Quality, CustomFormats: a.Formats}
	}
	name, err := naming.Render(tmpl, naming.EpisodeValues(s.info(v), episodeInfos(eps), fi, style), s.colon(ctx))
	return folder, name, err
}

func (s *Service) transferMode(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		switch requested {
		case library.TransferHardlink, library.TransferCopy, library.TransferMove:
			return requested, nil
		}
		return "", invalid("unknown transfer mode %q", requested)
	}
	return settings.Get(ctx, s.Settings, library.TransferMode)
}

func (s *Service) perms(ctx context.Context) importer.Permissions {
	on, _ := settings.Get(ctx, s.Settings, library.SetPermissions)
	if !on {
		return importer.Permissions{}
	}
	f, _ := settings.Get(ctx, s.Settings, library.FileMode)
	d, _ := settings.Get(ctx, s.Settings, library.FolderMode)
	return importer.Permissions{File: importer.ParseMode(f), Folder: importer.ParseMode(d)}
}

// ImportRequest imports one file into episodes of one series.
type ImportRequest struct {
	SeriesID int64
	Path     string
	// EpisodeIDs pins the episodes (manual import); empty = from the name.
	EpisodeIDs   []int64
	ReleaseTitle string
	Mode         string
	Sidecars     []string
	DownloadID   *int64
	// Single: the file is the only video of its download (its episodes may
	// come from the release name).
	Single bool
}

// ImportResult says what happened to one file.
type ImportResult struct {
	SeriesID   int64
	EpisodeIDs []int64
	Source     string
	Path       string
	Mode       string
	Upgraded   bool
	Recycled   []string
	Sidecars   []string
}

func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false
	}
	return os.SameFile(sa, sb)
}

// ImportFile imports one video file (and its sidecars) for the episodes it
// holds: each must be missing or the file must be an upgrade over its
// current one. Old files no episode needs any more go to the recycle bin.
func (s *Service) ImportFile(ctx context.Context, req ImportRequest) (ImportResult, error) {
	vw, err := s.loadView(ctx, req.SeriesID)
	if err != nil {
		return ImportResult{}, err
	}
	v := &vw.series
	src, err := lfs.MustExist(s.DataRoots, req.Path)
	if err != nil {
		return ImportResult{}, err
	}
	rel := parseFile(src, req.ReleaseTitle, req.Single)
	var eps []*Episode
	if len(req.EpisodeIDs) > 0 {
		want := map[int64]bool{}
		for _, id := range req.EpisodeIDs {
			want[id] = true
		}
		for i := range vw.eps {
			if want[vw.eps[i].ID] {
				eps = append(eps, &vw.eps[i])
			}
		}
		if len(eps) != len(want) {
			return ImportResult{}, rejectf(RejectNoEpisode, "an episode does not belong to %s", v.Title)
		}
	} else {
		eps = MatchRelease(v.SeriesType, vw.eps, &rel)
		if IsPack(&rel) {
			eps = nil // a file is never a whole season
		}
	}
	if len(eps) == 0 {
		return ImportResult{}, rejectf(RejectNoEpisode, "%s: no episode of %s matches the name", filepath.Base(src), v.Title)
	}
	title := req.ReleaseTitle
	if title == "" || !req.Single {
		title = filepath.Base(src)
	}
	a, err := s.analyse(ctx, v, rel, title)
	if err != nil {
		return ImportResult{}, err
	}
	olds := map[int64]*File{}
	for _, e := range eps {
		if e.File == nil {
			continue
		}
		if sameFile(src, e.File.Path) {
			return ImportResult{}, rejectf(RejectExists, "%s is already the file of S%02dE%02d", src, e.Season, e.Number)
		}
		if ok, why := better(vw.profile, a, e.File); !ok {
			return ImportResult{}, rejectf(RejectNotUpgrade, "S%02dE%02d not imported: %s", e.Season, e.Number, why)
		}
		olds[e.File.ID] = e.File
	}
	mode, err := s.transferMode(ctx, req.Mode)
	if err != nil {
		return ImportResult{}, err
	}
	folder, name, err := s.destination(ctx, v, eps, &a, src)
	if err != nil {
		return ImportResult{}, err
	}
	res := ImportResult{SeriesID: v.ID, Source: src}
	for _, e := range eps {
		res.EpisodeIDs = append(res.EpisodeIDs, e.ID)
	}
	mainRef := eps[0].Ref()
	replace := func(p string) error {
		en, err := s.Recycle.Recycle(ctx, p, mainRef.String(), "replaced by an upgrade")
		if err != nil {
			return err
		}
		res.Recycled = append(res.Recycled, en.RecycledPath)
		return nil
	}
	main, sides, err := importer.Place(importer.Placement{Roots: s.DataRoots, Source: src, Sidecars: req.Sidecars, Folder: folder,
		Name: name, Mode: mode, Perms: s.perms(ctx), Replace: replace})
	if err != nil {
		return res, err
	}
	res.Path, res.Mode = main.Path, main.Mode
	for _, sd := range sides {
		res.Sidecars = append(res.Sidecars, sd.Path)
	}
	f := &File{Path: main.Path, Size: main.Size, Quality: a.Quality.ID, Revision: rel.Revision, CustomFormats: a.Formats, Score: a.Score,
		ReleaseGroup: rel.Group, Languages: rel.Languages, MediaInfo: mediaInfo(&rel), SceneName: req.ReleaseTitle, DownloadID: req.DownloadID}
	if f.Revision.Version == 0 {
		f.Revision.Version = 1
	}
	if f.Languages == nil {
		f.Languages = []string{}
	}
	now := s.now()
	var orphaned []*File
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := saveFile(ctx, tx, v.ID, f, now); err != nil {
			return err
		}
		for _, e := range eps {
			if _, err := tx.ExecContext(ctx, `UPDATE episodes SET file_id = ? WHERE id = ?`, f.ID, e.ID); err != nil {
				return err
			}
		}
		for id, old := range olds {
			if id == f.ID {
				continue
			}
			var left int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodes WHERE file_id = ?`, id).Scan(&left); err != nil {
				return err
			}
			if left == 0 {
				if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, id); err != nil {
					return err
				}
				orphaned = append(orphaned, old)
			}
		}
		return nil
	})
	if err != nil {
		return res, err
	}
	// An old file no episode uses any more goes to the recycle bin (never deleted).
	for _, old := range orphaned {
		if old.Path == main.Path {
			continue
		}
		if _, err := os.Stat(old.Path); err == nil {
			if err := replace(old.Path); err != nil {
				s.record(ctx, mainRef, timeline.TypeImportFailed, "imported the new file but could not move the old one to the recycle bin: "+err.Error(),
					map[string]any{"path": old.Path}, req.DownloadID)
			}
		}
	}
	res.Upgraded = len(olds) > 0
	data := map[string]any{"source": src, "path": main.Path, "mode": main.Mode, "quality": a.Quality.Name, "score": a.Score,
		"customFormats": a.Formats, "size": main.Size, "sidecars": res.Sidecars, "episodes": len(eps)}
	for _, e := range eps {
		ref := e.Ref()
		s.record(ctx, ref, timeline.TypeFileImported, fmt.Sprintf("imported %s (%s, %s)", filepath.Base(main.Path), a.Quality.Name, main.Mode), data, req.DownloadID)
		if filepath.Base(src) != filepath.Base(main.Path) {
			s.record(ctx, ref, timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(src), filepath.Base(main.Path)),
				map[string]any{"from": filepath.Base(src), "to": filepath.Base(main.Path)}, req.DownloadID)
		}
		if e.File != nil {
			s.record(ctx, ref, timeline.TypeUpgraded, fmt.Sprintf("upgraded from %s to %s", qualityName(e.File.Quality), a.Quality.Name),
				map[string]any{"from": qualityName(e.File.Quality), "to": a.Quality.Name, "oldPath": e.File.Path, "recycled": res.Recycled}, req.DownloadID)
		}
	}
	for _, r := range res.Recycled {
		s.record(ctx, mainRef, timeline.TypeRecycled, "moved the replaced file to the recycle bin: "+r, map[string]any{"path": r}, req.DownloadID)
	}
	s.notify(ctx, v, eps, map[bool]string{true: "upgraded", false: "imported"}[res.Upgraded], main.Path)
	return res, nil
}

func (s *Service) notify(ctx context.Context, v *Series, eps []*Episode, action, path string) {
	if s.Notifier == nil || s.Notifier.Name() == "" {
		return
	}
	err := s.Notifier.Notify(ctx, mediaserver.Change{ItemRef: eps[0].Ref().String(), Kind: "episode", Action: action, Title: v.Title, Year: v.Year,
		Paths: []string{path}, IDs: map[string]string{"tvdb": fmt.Sprint(v.TVDbID), "tmdb": fmt.Sprint(v.TMDbID), "imdb": v.IMDbID}})
	if err == nil && mediaserver.RecordsOutcome(s.Notifier) {
		return
	}
	msg := "notified " + s.Notifier.Name()
	if err != nil {
		msg = "could not notify " + s.Notifier.Name() + ": " + err.Error()
	}
	s.record(ctx, eps[0].Ref(), timeline.TypeNotified, msg, map[string]any{"server": s.Notifier.Name(), "ok": err == nil}, nil)
}

// sidecarsFor picks the sidecars that belong to a video: those named after
// it, or every one when it is the only video.
func sidecarsFor(video string, all []importer.File, single bool) []string {
	base := strings.ToLower(strings.TrimSuffix(filepath.Base(video), filepath.Ext(video)))
	var out []string
	for _, sc := range all {
		if single || strings.HasPrefix(strings.ToLower(filepath.Base(sc.Path)), base+".") {
			out = append(out, sc.Path)
		}
	}
	return out
}

// FileOutcome is one file of a download import.
type FileOutcome struct {
	Path     string
	Imported bool
	Code     string
	Message  string
	Result   *ImportResult
}

// ImportDownload imports every episode file of a completed download (one
// episode, a multi-episode file or a whole season pack). Files that match no
// episode or are no upgrade are reported and left alone; the download is
// imported when at least one file was (MarkImported once for all its
// episodes) and failed otherwise.
func (s *Service) ImportDownload(ctx context.Context, downloadID int64) error {
	d, err := s.Downloads.Get(ctx, downloadID)
	if err != nil {
		return err
	}
	if d.Status != downloads.StatusCompleted {
		return nil
	}
	ref := library.RefFromJSON(d.ItemRef)
	covers := library.CoversFromJSON(d.ItemRef)
	fail := func(err error) error {
		if !ref.IsZero() {
			s.record(ctx, ref, timeline.TypeImportFailed, err.Error(), map[string]any{"download": d.Title, "path": d.LocalPath}, &d.ID)
		}
		return s.Downloads.MarkImportFailed(ctx, d.ID, err.Error())
	}
	seriesID, err := s.seriesOf(ctx, ref)
	if err != nil {
		return fail(rejectf(RejectNoSeries, "the download is not linked to an episode of a series in the library"))
	}
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(d.LocalPath, extras)
	if err != nil {
		return fail(fmt.Errorf("cannot read %s: %w", d.LocalPath, err))
	}
	if len(disc.Videos) == 0 {
		if disc.Disc {
			return fail(rejectf(RejectDisc, "%s is a disc structure; import it manually", d.LocalPath))
		}
		return fail(rejectf(RejectNoVideo, "no video file in %s (%d samples or extras skipped)", d.LocalPath, len(disc.Skipped)))
	}
	single := len(disc.Videos) == 1
	sort.SliceStable(disc.Videos, func(i, j int) bool { return disc.Videos[i].Rel < disc.Videos[j].Rel })
	var outcomes []FileOutcome
	imported := map[int64]bool{}
	for _, vid := range disc.Videos {
		res, err := s.ImportFile(ctx, ImportRequest{SeriesID: seriesID, Path: vid.Path, ReleaseTitle: d.Title, Sidecars: sidecarsFor(vid.Path, disc.Sidecars, single),
			DownloadID: &d.ID, Single: single})
		o := FileOutcome{Path: vid.Path}
		if err != nil {
			o.Message = err.Error()
			if ie, ok := IsImportError(err); ok {
				o.Code = ie.Code
			} else {
				o.Code = "error"
			}
		} else {
			o.Imported, o.Result = true, &res
			for _, id := range res.EpisodeIDs {
				imported[id] = true
			}
		}
		outcomes = append(outcomes, o)
	}
	var skipped []string
	for _, o := range outcomes {
		if !o.Imported {
			skipped = append(skipped, filepath.Base(o.Path)+": "+o.Message)
			s.record(ctx, ref, timeline.TypeImportSkipped, o.Message, map[string]any{"path": o.Path, "code": o.Code, "download": d.Title}, &d.ID)
		}
	}
	// Episodes the download was for but did not contain are searched again soon.
	var missingRefs []library.Ref
	for _, c := range append([]library.Ref{ref}, covers...) {
		if c.Kind == library.RefEpisode && !imported[c.ID] {
			missingRefs = append(missingRefs, c)
			s.record(ctx, c, timeline.TypeImportSkipped, "the download "+d.Title+" did not contain this episode",
				map[string]any{"code": RejectNoEpisode, "download": d.Title}, &d.ID)
			if s.MakeDue != nil {
				s.MakeDue(ctx, c)
			}
		}
	}
	if len(imported) == 0 {
		msg := "no file could be imported"
		if len(skipped) > 0 {
			msg += ": " + strings.Join(skipped, "; ")
		}
		return s.Downloads.MarkImportFailed(ctx, d.ID, msg)
	}
	files := 0
	for _, o := range outcomes {
		if o.Imported {
			files++
		}
	}
	msg := fmt.Sprintf("imported %d of %d files (%d episodes) to %s", files, len(outcomes), len(imported), filepath.Dir(firstPath(outcomes)))
	if len(skipped) > 0 {
		msg += "; not imported: " + strings.Join(skipped, "; ")
	}
	if len(missingRefs) > 0 {
		msg += fmt.Sprintf("; %d wanted episodes were not in the download", len(missingRefs))
	}
	return s.Downloads.MarkImported(ctx, d.ID, msg)
}

func firstPath(list []FileOutcome) string {
	for _, o := range list {
		if o.Imported && o.Result != nil {
			return o.Result.Path
		}
	}
	return ""
}
