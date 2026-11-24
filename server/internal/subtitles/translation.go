package subtitles

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/jobs"
	"github.com/mmzlopasa/locarr/server/internal/languages"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/subfmt"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/tracks"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/translate"
)

// Translation errors.
var (
	ErrNoSource       = errors.New("no subtitle to translate from")
	ErrNoTranslator   = errors.New("no translation service is enabled (Settings → Subtitles → Translation)")
	ErrHumanSubtitle  = errors.New("a subtitle file that Locarr did not translate already exists under this name")
	ErrNothingToTrans = errors.New("no wanted language is missing")
)

// Timeline types of translations.
const (
	TypeTranslated      = "subtitle_translated"
	TypeTranslateFailed = "subtitle_translation_failed"
)

// Job kinds of translations.
const (
	TaskTranslateItem = "subtitles-translate-item"
	TaskTranslateDue  = "subtitles-translate-due"
	TranslateQueue    = "subtitles-translate"
)

// translatorNames are the names shown in timeline messages.
var translatorNames = map[string]string{
	translate.LibreTranslate: "LibreTranslate", translate.DeepL: "DeepL", translate.OpenAI: "OpenAI", translate.Anthropic: "Anthropic",
	translate.Gemini: "Gemini", translate.OpenAICompatible: "an OpenAI-compatible server",
}

func knownLanguage(code string) bool {
	_, ok := languages.ByCode(code)
	return ok && code != languages.Unknown
}

// translatableKeys are the missing keys a profile translates automatically.
func translatableKeys(p *Profile, missing []string) []string {
	var out []string
	for _, k := range missing {
		for _, w := range p.Languages {
			if w.Key() == k && w.Translator != TranslatorNone {
				out = append(out, k)
			}
		}
	}
	return out
}

// TranslationProgress is a running or queued translation of an item.
type TranslationProgress struct {
	Ref        string    `json:"ref"`
	Key        string    `json:"key"`
	Translator string    `json:"translator"`
	State      string    `json:"state"` // queued, running
	Done       int       `json:"done"`
	Total      int       `json:"total"`
	StartedAt  time.Time `json:"startedAt"`
}

func (s *Service) setProgress(p TranslationProgress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.progress == nil {
		s.progress = map[string]TranslationProgress{}
	}
	s.progress[p.Ref] = p
}

func (s *Service) clearProgress(ref string) {
	s.mu.Lock()
	delete(s.progress, ref)
	s.mu.Unlock()
}

// Progress returns the translation of an item that is queued or running.
func (s *Service) Progress(ref string) []TranslationProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok := s.progress[ref]; ok {
		return []TranslationProgress{p}
	}
	return []TranslationProgress{}
}

type translateArgs struct {
	Ref        string   `json:"ref"`
	Keys       []string `json:"keys,omitempty"`
	Translator string   `json:"translator,omitempty"`
	Reason     string   `json:"reason"`
}

// TranslationAvailable reports whether any translation service is enabled.
func (s *Service) TranslationAvailable(ctx context.Context) bool {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_translators WHERE enabled = 1`).Scan(&n)
	return n > 0
}

// QueueTranslate queues translations of the missing languages of items
// (keys limits them; translator overrides the configured service).
func (s *Service) QueueTranslate(ctx context.Context, refs []library.Ref, keys []string, translator, reason string) (int, error) {
	if translator != "" {
		t, err := s.Translator(ctx, translator)
		if err != nil {
			return 0, err
		}
		if !t.Enabled {
			return 0, invalid("%s is not enabled", translator)
		}
	} else if !s.TranslationAvailable(ctx) {
		return 0, ErrNoTranslator
	}
	n := 0
	for _, r := range refs {
		if r.Kind != library.RefMovie && r.Kind != library.RefEpisode {
			continue
		}
		if _, _, err := s.Jobs.Enqueue(ctx, TaskTranslateItem, translateArgs{Ref: r.String(), Keys: keys, Translator: translator, Reason: reason},
			jobs.EnqueueOptions{UniqueKey: "subtitles-translate:" + r.String()}); err != nil {
			return n, err
		}
		key := ""
		if len(keys) == 1 {
			key = keys[0]
		}
		s.setProgress(TranslationProgress{Ref: r.String(), Key: key, Translator: translator, State: "queued", StartedAt: s.now()})
		n++
	}
	return n, nil
}

// pickTranslator chooses the service for a wanted entry: the one asked for,
// the profile language's, the language's default, the general default,
// else the first enabled one.
func (s *Service) pickTranslator(ctx context.Context, w Want, explicit string) (TranslatorState, error) {
	name := explicit
	if name == "" {
		name = w.Translator
	}
	if name == TranslatorNone && explicit == "" {
		return TranslatorState{}, invalid("translation is turned off for %s in the profile", w.Key())
	}
	if name == "" || name == TranslatorNone {
		m, _ := settings.Get(ctx, s.Settings, TranslationLanguages)
		name = m[w.Language]
	}
	if name == TranslatorNone {
		return TranslatorState{}, invalid("translation into %s is turned off", languages.Name(w.Language))
	}
	if name == "" {
		name, _ = settings.Get(ctx, s.Settings, TranslationDefault)
	}
	if name != "" {
		t, err := s.Translator(ctx, name)
		if err != nil {
			return t, err
		}
		if !t.Enabled {
			return t, invalid("%s is chosen for %s but is not enabled", name, languages.Name(w.Language))
		}
		return t, nil
	}
	all, err := s.TranslatorStates(ctx)
	if err != nil {
		return TranslatorState{}, err
	}
	for _, t := range all {
		if t.Enabled {
			return t, nil
		}
	}
	return TranslatorState{}, ErrNoTranslator
}

// source is the subtitle a translation is made from.
type source struct {
	Language string
	Desc     string // subtitle:<file name> or track:<n>
	Label    string // for messages
	Doc      *subfmt.Doc
	FromFile bool
}

type candidate struct {
	lang    string
	sub     *Subtitle
	track   *tracks.Track
	hi      bool
	langPos int
}

// pickSource finds the best subtitle to translate w from: preferred source
// languages first (English by default), then any; a subtitle file before a
// text track inside the video; not for the hearing impaired unless wanted.
// Picture tracks (PGS, VobSub) are skipped: they would need OCR.
func (s *Service) pickSource(ctx context.Context, t *Target, w Want, pairs translate.Pairs) (source, []string, error) {
	prefer, _ := settings.Get(ctx, s.Settings, TranslationSources)
	pos := func(lang string) int {
		for i, l := range prefer {
			if l == lang {
				return i
			}
		}
		return len(prefer)
	}
	subs, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return source{}, nil, err
	}
	trs, _, _, err := s.Tracks(ctx, t.FileID)
	if err != nil {
		return source{}, nil, err
	}
	var (
		cands   []candidate
		skipped []string
	)
	for i := range subs {
		sub := &subs[i]
		if sub.Translated() || sub.Language == w.Language || !knownLanguage(sub.Language) || sub.Forced != w.Forced {
			continue
		}
		cands = append(cands, candidate{lang: sub.Language, sub: sub, hi: sub.HearingImpaired, langPos: pos(sub.Language)})
	}
	for i := range trs {
		tr := &trs[i]
		if tr.Kind != tracks.Subtitle || tr.Language == w.Language || !knownLanguage(tr.Language) || tr.Forced != w.Forced {
			continue
		}
		label := fmt.Sprintf("embedded track %d (%s, %s)", tr.Index+1, languages.Name(tr.Language), tr.Codec)
		switch {
		case !tr.Text:
			skipped = append(skipped, label+" is a picture-based subtitle (PGS/VobSub) that would need OCR")
			continue
		case !s.SyncAvailable():
			skipped = append(skipped, label+" needs ffmpeg to be read")
			continue
		}
		cands = append(cands, candidate{lang: tr.Language, track: tr, hi: tr.HearingImpaired, langPos: pos(tr.Language)})
	}
	wantHI := w.HearingImpaired == HIOnly || w.HearingImpaired == HIPrefer
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.langPos != b.langPos {
			return a.langPos < b.langPos
		}
		if (a.sub != nil) != (b.sub != nil) {
			return a.sub != nil
		}
		if a.hi != b.hi {
			return a.hi == wantHI
		}
		return false
	})
	supported := map[string]bool{}
	for _, c := range cands {
		if pairs != nil {
			ok, seen := supported[c.lang]
			if !seen {
				var err error
				if ok, err = pairs.Supports(ctx, c.lang, w.Language); err != nil {
					ok = true // unknown: let the translation itself answer
				}
				supported[c.lang] = ok
			}
			if !ok {
				skipped = append(skipped, fmt.Sprintf("%s → %s is not offered by the service", languages.Name(c.lang), languages.Name(w.Language)))
				continue
			}
		}
		if c.sub != nil {
			d, err := s.load(*c.sub)
			if err != nil {
				skipped = append(skipped, filepath.Base(c.sub.Path)+": "+err.Error())
				continue
			}
			return source{Language: c.lang, Desc: "subtitle:" + filepath.Base(c.sub.Path), Label: filepath.Base(c.sub.Path), Doc: d, FromFile: true}, skipped, nil
		}
		video, err := lfs.MustExist(s.DataRoots, t.Path)
		if err != nil {
			return source{}, skipped, err
		}
		cues, err := s.FFmpeg.TextTrack(ctx, video, c.track.Index)
		if err != nil {
			if ctx.Err() != nil {
				return source{}, skipped, ctx.Err()
			}
			skipped = append(skipped, fmt.Sprintf("embedded track %d: %s", c.track.Index+1, err))
			continue
		}
		return source{Language: c.lang, Desc: fmt.Sprintf("track:%d", c.track.Index),
			Label: fmt.Sprintf("embedded track %d", c.track.Index+1), Doc: &subfmt.Doc{Format: subfmt.SRT, Cues: cues}}, skipped, nil
	}
	msg := ErrNoSource.Error() + " into " + languages.Name(w.Language)
	if len(skipped) > 0 {
		msg += ": " + strings.Join(skipped, "; ")
	} else {
		msg += ": the file has no subtitle in another language"
	}
	return source{}, skipped, fmt.Errorf("%w%s", ErrNoSource, strings.TrimPrefix(msg, ErrNoSource.Error()))
}

// translatedPath is where a translation goes: "Movie.hr.translated.srt"
// (or without the marker when the suffix setting is empty).
func translatedPath(video string, w Want, marker, ext string) string {
	p := SidecarPath(video, w.Language, w.Forced, w.HearingImpaired == HIOnly, ext)
	if marker == "" {
		return p
	}
	return strings.TrimSuffix(p, "."+ext) + "." + marker + "." + ext
}

type originalLine struct {
	StartMs int64  `json:"s"`
	Text    string `json:"t"`
}

// unitRef points a translation unit at its cue and piece.
type unitRef struct{ cue, piece int }

// TranslateDoc translates every cue of src from → to with c and returns a
// new document: the same cues, times and (for ASS/SSA) styles and override
// blocks, the text translated and wrapped to at most two lines. before is
// asked ahead of every batch (the monthly cap); progress reports units done.
func TranslateDoc(ctx context.Context, c translate.Translator, src *subfmt.Doc, from, to, title string,
	before func(est int64, so translate.Usage) error, progress func(done, total int)) (*subfmt.Doc, int, translate.Usage, error) {
	doc := src.Clone()
	var (
		pieces = make([][]translate.Piece, len(doc.Cues))
		units  []string
		refs   []unitRef
		usage  translate.Usage
	)
	for i := range doc.Cues {
		pieces[i] = translate.SplitCue(doc.EditableText(i))
		for j, p := range pieces[i] {
			if !p.Keep {
				units = append(units, p.Text)
				refs = append(refs, unitRef{i, j})
			}
		}
	}
	if len(units) == 0 {
		return nil, 0, usage, fmt.Errorf("%w: the subtitle has no text", ErrNoSource)
	}
	out := make([]string, len(units))
	for _, b := range translate.Batches(units, c.Limits()) {
		if before != nil {
			if err := before(translate.Chars(units[b[0]:b[1]]...), usage); err != nil {
				return nil, len(units), usage, err
			}
		}
		res, u, err := c.Translate(ctx, translate.Request{Texts: units[b[0]:b[1]], From: from, To: to, Title: title,
			Previous: units[max(0, b[0]-3):b[0]]})
		usage.Add(u)
		if err != nil {
			return nil, len(units), usage, err
		}
		if len(res) != b[1]-b[0] {
			return nil, len(units), usage, translate.ErrCountMismatch
		}
		copy(out[b[0]:b[1]], res)
		if progress != nil {
			progress(b[1], len(units))
		}
	}
	texts := make([][]string, len(doc.Cues))
	for i := range doc.Cues {
		texts[i] = make([]string, len(pieces[i]))
	}
	for k, r := range refs {
		texts[r.cue][r.piece] = out[k]
	}
	maxLen := translate.MaxLine(to)
	for i := range doc.Cues {
		if len(pieces[i]) > 0 {
			doc.SetText(i, translate.JoinCue(pieces[i], texts[i], maxLen))
		}
	}
	return doc, len(units), usage, nil
}

// TranslateWant translates one wanted entry of a target and saves it.
func (s *Service) translateWant(ctx context.Context, t *Target, w Want, explicit, reason string) (Subtitle, error) {
	started := time.Now()
	tr, err := s.pickTranslator(ctx, w, explicit)
	if err != nil {
		return Subtitle{}, err
	}
	info, _ := TranslatorInfoOf(tr.Name)
	if reason == "scheduled" && tr.RetryAt != nil && s.now().Before(*tr.RetryAt) {
		return Subtitle{}, fmt.Errorf("%s waits until %s after: %s", tr.Name, fmtWhen(tr.RetryAt), tr.LastError)
	}
	if tr.CapReached() {
		err := &CapError{Translator: tr.Name, Cap: tr.MonthlyCap, Used: tr.Used(), Unit: info.Unit}
		s.translatorFailed(ctx, tr, err)
		return Subtitle{}, err
	}
	client, err := s.translatorClient(tr)
	if err != nil {
		return Subtitle{}, err
	}
	pairs, _ := client.(translate.Pairs)
	src, _, err := s.pickSource(ctx, t, w, pairs)
	if err != nil {
		return Subtitle{}, err
	}
	title := t.ItemTitle
	if t.Year > 0 {
		title = fmt.Sprintf("%s (%d)", title, t.Year)
	}
	prog := TranslationProgress{Ref: t.Ref.String(), Key: w.Key(), Translator: tr.Name, State: "running", StartedAt: s.now()}
	s.setProgress(prog)
	before := func(est int64, so translate.Usage) error {
		if tr.MonthlyCap <= 0 {
			return nil
		}
		if info.Unit == translate.UnitTokens {
			est = est/2 + 800
		}
		if used := tr.Used() + so.Amount(info.Unit); used+est > tr.MonthlyCap {
			return &CapError{Translator: tr.Name, Cap: tr.MonthlyCap, Used: used, Unit: info.Unit}
		}
		return nil
	}
	progress := func(done, total int) {
		prog.Done, prog.Total = done, total
		s.setProgress(prog)
	}
	doc, units, usage, err := TranslateDoc(ctx, client, src.Doc, src.Language, w.Language, title, before, progress)
	if err != nil {
		s.addUsage(ctx, tr.Name, usage, 0, 0)
		if ctx.Err() == nil && !errors.Is(err, ErrNoSource) {
			s.translatorFailed(ctx, tr, err)
		}
		return Subtitle{}, err
	}
	originals := make([]originalLine, 0, 20)
	for _, c := range src.Doc.Sorted() {
		if len(originals) >= 20 {
			break
		}
		if strings.TrimSpace(c.Text) != "" {
			originals = append(originals, originalLine{StartMs: c.Start.Milliseconds(), Text: c.Text})
		}
	}
	format := subfmt.SRT
	preferred, _ := settings.Get(ctx, s.Settings, PreferredFormat)
	if (doc.Format == subfmt.ASS || doc.Format == subfmt.SSA || doc.Format == subfmt.VTT) && preferred != subfmt.SRT {
		format = doc.Format
	}
	text, err := doc.Write(format)
	if err != nil {
		return Subtitle{}, err
	}
	sub, recycled, err := s.saveTranslation(ctx, t, w, tr.Name, src, format, text, originals)
	s.addUsage(ctx, tr.Name, usage, units, btoi(err == nil))
	if err != nil {
		return Subtitle{}, err
	}
	s.translatorOK(ctx, tr.Name)
	secs := int(time.Since(started).Round(time.Second).Seconds())
	s.record(ctx, t.Ref.String(), TypeTranslated, fmt.Sprintf("translated %s from %s with %s, %d lines, %d s", w.Key(), src.Language,
		translatorNames[tr.Name], len(doc.Cues), secs), map[string]any{"language": w.Language, "key": w.Key(), "from": src.Language,
		"translator": tr.Name, "lines": len(doc.Cues), "units": units, "seconds": secs, "path": sub.Path, "source": src.Desc,
		"characters": usage.Characters, "inputTokens": usage.InputTokens, "outputTokens": usage.OutputTokens, "reason": reason, "recycled": recycled})
	if src.FromFile {
		if on, _ := settings.Get(ctx, s.Settings, TranslationAutoSync); on && s.SyncAvailable() {
			if _, err := s.QueueSync(ctx, sub.ID, RefAudio, true); err != nil {
				s.log().Warn("queue sync of a translation", "subtitle", sub.ID, "err", err)
			}
		}
	}
	return sub, nil
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// saveTranslation writes a translation next to the video. It never
// overwrites a subtitle Locarr did not translate; an older translation of
// the same name goes to the recycle bin.
func (s *Service) saveTranslation(ctx context.Context, t *Target, w Want, translator string, src source, format, text string,
	originals []originalLine) (Subtitle, string, error) {
	marker, _ := settings.Get(ctx, s.Settings, TranslationSuffix)
	dst := translatedPath(t.Path, w, marker, format)
	var existing Subtitle
	found := false
	subs, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return Subtitle{}, "", err
	}
	for _, sub := range subs {
		if sub.Path == dst {
			existing, found = sub, true
		}
	}
	if _, err := os.Stat(dst); err == nil && (!found || !existing.Translated()) {
		return Subtitle{}, "", fmt.Errorf("%w: %s", ErrHumanSubtitle, filepath.Base(dst))
	}
	recycled, err := s.writeAtomic(ctx, dst, []byte(text), t.Ref.String(), "replaced by a new translation")
	if err != nil {
		return Subtitle{}, "", err
	}
	// Another translation of the same language under another name (other format, older marker) goes too.
	for _, sub := range subs {
		if sub.Translated() && sub.Path != dst && sub.Language == w.Language && sub.Forced == w.Forced {
			if _, err := os.Stat(sub.Path); err == nil {
				if e, err := s.Recycle.Recycle(ctx, sub.Path, t.Ref.String(), "replaced by a new translation"); err == nil {
					recycled = e.RecycledPath
				}
			}
			_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, sub.ID)
		}
	}
	orig, _ := json.Marshal(originals)
	now := db.Millis(s.now())
	var id int64
	err = s.DB.W.QueryRowContext(ctx, `INSERT INTO subtitles (media_file_id, item_ref, path, language, forced, hearing_impaired, format, source,
		provider, release, encoding, translated_from, translation_source, translation_original, added_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'translated', ?, ?, 'utf-8', ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET media_file_id = excluded.media_file_id, item_ref = excluded.item_ref, language = excluded.language,
		forced = excluded.forced, hearing_impaired = excluded.hearing_impaired, format = excluded.format, source = 'translated',
		provider = excluded.provider, provider_id = '', release = excluded.release, score = NULL, encoding = 'utf-8',
		translated_from = excluded.translated_from, translation_source = excluded.translation_source,
		translation_original = excluded.translation_original, sync_state = '', sync_message = '', sync_offset_ms = 0, sync_scale = 1,
		sync_at = NULL, updated_at = excluded.updated_at RETURNING id`,
		t.FileID, t.Ref.String(), dst, w.Language, w.Forced, w.HearingImpaired == HIOnly, format, translator, src.Label, src.Language, src.Desc,
		string(orig), now, now).Scan(&id)
	if err != nil {
		return Subtitle{}, recycled, err
	}
	sub, err := s.Get(ctx, id)
	return sub, recycled, err
}

// retireTranslations moves machine translations to the recycle bin once a
// human subtitle of the same language (and kind) is there.
func (s *Service) retireTranslations(ctx context.Context, t *Target) {
	subs, err := s.FileSubtitles(ctx, t.FileID)
	if err != nil {
		return
	}
	for _, tr := range subs {
		if !tr.Translated() {
			continue
		}
		for _, h := range subs {
			if h.Translated() || h.Language != tr.Language || h.Forced != tr.Forced {
				continue
			}
			recycled := ""
			if _, err := os.Stat(tr.Path); err == nil {
				e, err := s.Recycle.Recycle(ctx, tr.Path, t.Ref.String(), "replaced by a human subtitle")
				if err != nil {
					s.log().Warn("recycle a translation", "path", tr.Path, "err", err)
					break
				}
				recycled = e.RecycledPath
			}
			if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, tr.ID); err != nil {
				break
			}
			s.record(ctx, t.Ref.String(), TypeDeleted, fmt.Sprintf("moved the machine translation %s to the recycle bin: %s arrived",
				filepath.Base(tr.Path), filepath.Base(h.Path)), map[string]any{"path": tr.Path, "recycled": recycled, "language": tr.Language,
				"reason": "human_subtitle", "replacedBy": h.Path, "translated": true})
			break
		}
	}
}

// TranslateItem translates the missing languages of a movie or episode
// (only keys, when given).
func (s *Service) TranslateItem(ctx context.Context, ref library.Ref, keys []string, translator, reason string) ([]Subtitle, error) {
	defer s.clearProgress(ref.String())
	t, err := s.Target(ctx, ref)
	if err != nil {
		return nil, err
	}
	ip, st, err := s.Status(ctx, &t)
	if err != nil {
		return nil, err
	}
	var wants []Want
	for _, w := range st {
		if w.Status != "missing" && (len(keys) == 0 || w.Status != "skipped") {
			continue
		}
		if len(keys) > 0 && !contains(keys, w.Key) {
			continue
		}
		if reason == "scheduled" && w.Translator == TranslatorNone {
			continue
		}
		wants = append(wants, w.Want)
	}
	// A language outside the profile can be asked for explicitly ("hr").
	if ip.Profile == nil || len(wants) == 0 {
		for _, k := range keys {
			lang, flag, _ := strings.Cut(k, ":")
			if !knownLanguage(lang) {
				return nil, ErrBadLanguage
			}
			w := Want{Language: lang, Forced: flag == "forced", HearingImpaired: HIInclude}
			if flag == "hi" {
				w.HearingImpaired = HIOnly
			}
			present := false
			for _, x := range st {
				if x.Key == k && (x.Status == "present" || x.Status == "embedded" || x.Status == "translated") {
					present = true
				}
			}
			if !present && !containsWant(wants, w) {
				wants = append(wants, w)
			}
		}
	}
	if len(wants) == 0 {
		return nil, ErrNothingToTrans
	}
	var (
		out  []Subtitle
		errs []error
	)
	for _, w := range wants {
		sub, err := s.translateWant(ctx, &t, w, translator, reason)
		if err != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			errs = append(errs, err)
			s.record(ctx, ref.String(), TypeTranslateFailed, fmt.Sprintf("could not translate %s: %s", w.Key(), err),
				map[string]any{"language": w.Language, "key": w.Key(), "error": err.Error(), "code": TranslationErrorCode(err), "reason": reason})
			continue
		}
		out = append(out, sub)
	}
	if err := s.UpdateState(ctx, &t); err != nil {
		return out, err
	}
	return out, errors.Join(errs...)
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func containsWant(list []Want, w Want) bool {
	for _, x := range list {
		if x.Key() == w.Key() {
			return true
		}
	}
	return false
}

// TranslateDue runs the scheduled translations: items whose profile
// translates after N hours, still missing a language, at most
// TranslationsPerHour per run. A failure waits 6 h, doubling up to a week.
func (s *Service) TranslateDue(ctx context.Context) (int, error) {
	limit, _ := settings.Get(ctx, s.Settings, TranslationsPerHour)
	if limit <= 0 || !s.TranslationAvailable(ctx) {
		return 0, nil
	}
	now := db.Millis(s.now())
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_ref, translate_attempts FROM subtitle_state WHERE translate_at IS NOT NULL AND translate_at <= ?
		AND missing_count > 0 AND searches > 0 AND (translate_retry_at IS NULL OR translate_retry_at <= ?)
		AND item_ref NOT LIKE 'scene:%' AND item_ref NOT LIKE 'adult_movie:%' ORDER BY translate_at LIMIT ?`, now, now, limit) // adult items are never translated (D-102)
	if err != nil {
		return 0, err
	}
	type due struct {
		ref      library.Ref
		attempts int
	}
	var list []due
	for rows.Next() {
		var (
			r string
			a int
		)
		if err := rows.Scan(&r, &a); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if ref, err := library.ParseRef(r); err == nil {
			list = append(list, due{ref, a})
		}
	}
	_ = rows.Close()
	n := 0
	for _, d := range list {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		s.setProgress(TranslationProgress{Ref: d.ref.String(), State: "queued", StartedAt: s.now()})
		subs, err := s.TranslateItem(ctx, d.ref, nil, "", "scheduled")
		if len(subs) > 0 {
			n++
		}
		if err != nil {
			var ce *CapError
			retry := s.now().Add(SearchBackoff(d.attempts + 1))
			if errors.As(err, &ce) {
				retry = nextMonth(s.now())
			}
			_, _ = s.DB.W.ExecContext(ctx, `UPDATE subtitle_state SET translate_retry_at = ?, translate_attempts = translate_attempts + 1 WHERE item_ref = ?`,
				db.Millis(retry), d.ref.String())
			s.log().Warn("scheduled subtitle translation", "item", d.ref.String(), "err", err)
			continue
		}
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE subtitle_state SET translate_retry_at = NULL, translate_attempts = 0 WHERE item_ref = ?`, d.ref.String())
	}
	return n, nil
}

// translationOriginal returns the stored source lines of a translation by start time.
func translationOriginal(sub Subtitle) map[int64]string {
	var lines []originalLine
	if sub.original == "" || json.Unmarshal([]byte(sub.original), &lines) != nil {
		return nil
	}
	out := make(map[int64]string, len(lines))
	for _, l := range lines {
		out[l.StartMs] = l.Text
	}
	return out
}

// registerTranslation registers the translation jobs.
func (s *Service) registerTranslation() {
	s.Jobs.Register(TaskTranslateItem, func(ctx context.Context, j *jobs.Job) error {
		var a translateArgs
		if err := j.Decode(&a); err != nil {
			return jobs.Permanent(err)
		}
		ref, err := library.ParseRef(a.Ref)
		if err != nil {
			return jobs.Permanent(err)
		}
		_, err = s.TranslateItem(ctx, ref, a.Keys, a.Translator, a.Reason)
		if err != nil && ctx.Err() == nil {
			// Already on the timeline; a retry would spend the quota again.
			s.log().Warn("subtitle translation", "item", a.Ref, "err", err)
			return nil
		}
		return err
	}, jobs.KindOptions{Queue: TranslateQueue, MaxAttempts: 1, Timeout: 2 * time.Hour})
	s.Jobs.Register(TaskTranslateDue, func(ctx context.Context, _ *jobs.Job) error {
		n, err := s.TranslateDue(ctx)
		if n > 0 {
			s.log().Info("scheduled subtitle translation", "items", n)
		}
		return err
	}, jobs.KindOptions{Queue: TranslateQueue, MaxAttempts: 1, Timeout: 55 * time.Minute})
	s.Jobs.Schedule(TaskTranslateDue, TaskTranslateDue, time.Hour)
}
