package adult

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
)

// DefaultBlocklist is the conservative seed list (D-101): terms commonly
// used for content involving minors or without consent. It is applied to
// searches, RSS, imports and metadata entries; the administrator may add
// terms and remove seeded ones.
var DefaultBlocklist = []string{
	"underage", "under age", "minor", "minors", "child", "children", "kid", "kids", "preteen", "pre teen", "teen", "teens",
	"barely legal", "jailbait", "lolita", "loli", "shota", "pthc", "schoolgirl", "school girl", "age play", "ageplay",
	"rape", "raped", "non consensual", "nonconsensual", "noncon", "forced", "drugged", "unconscious", "passed out",
	"hidden cam", "hidden camera", "spycam", "spy cam", "upskirt", "voyeur", "leaked", "revenge porn", "stolen",
	"blackmail", "trafficking", "snuff", "bestiality",
}

// Term is one blocklist entry.
type Term struct {
	ID      int64  `json:"id"`
	Term    string `json:"term"`
	Builtin bool   `json:"builtin"`
}

// SeedBlocklist inserts the default terms once (the first time the feature
// is enabled, or when the list is empty).
func (s *Service) SeedBlocklist(ctx context.Context) error {
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_tag_blocklist`).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	now := db.Millis(s.now())
	for _, t := range DefaultBlocklist {
		if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO adult_tag_blocklist (term, builtin, added_at) VALUES (?, 1, ?) ON CONFLICT DO NOTHING`,
			t, now); err != nil {
			return err
		}
	}
	return nil
}

// Blocklist lists the terms.
func (s *Service) Blocklist(ctx context.Context) ([]Term, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, term, builtin FROM adult_tag_blocklist ORDER BY term`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Term{}
	for rows.Next() {
		var t Term
		if err := rows.Scan(&t.ID, &t.Term, &t.Builtin); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Terms is the blocklist as plain terms. An empty table (never enabled)
// still blocks the defaults.
func (s *Service) Terms(ctx context.Context) []string {
	list, err := s.Blocklist(ctx)
	if err != nil || len(list) == 0 {
		return DefaultBlocklist
	}
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Term)
	}
	return out
}

// AddTerm adds a term (lower case, words only).
func (s *Service) AddTerm(ctx context.Context, term string) (Term, error) {
	term = decision.NormWords(term)
	if utf8.RuneCountInString(strings.ReplaceAll(term, " ", "")) < 2 {
		return Term{}, ErrBadTerm
	}
	if err := s.SeedBlocklist(ctx); err != nil {
		return Term{}, err
	}
	t := Term{Term: term}
	err := s.DB.W.QueryRowContext(ctx, `INSERT INTO adult_tag_blocklist (term, builtin, added_at) VALUES (?, 0, ?)
		ON CONFLICT (term) DO UPDATE SET term = excluded.term RETURNING id, builtin`, term, db.Millis(s.now())).Scan(&t.ID, &t.Builtin)
	return t, err
}

// RemoveTerm deletes a term.
func (s *Service) RemoveTerm(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM adult_tag_blocklist WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// BlockedText returns the blocked term found in any of the texts.
func (s *Service) BlockedText(ctx context.Context, texts ...string) (string, bool) {
	terms := s.Terms(ctx)
	for _, t := range texts {
		if term, ok := decision.BlockedTerm(t, terms); ok {
			return term, true
		}
	}
	return "", false
}
