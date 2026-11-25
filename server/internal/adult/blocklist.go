package adult

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/decision"
)

// Term is one entry of the owner's blocklist.
type Term struct {
	ID      int64  `json:"id"`
	Term    string `json:"term"`
	Builtin bool   `json:"builtin"`
}

// ProtectedTerms are always blocked and cannot be removed (D-109); the
// editable list holds only the owner's own terms and starts empty.
func ProtectedTerms() []string { return append([]string(nil), decision.ProtectedAdultTerms...) }

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

// OwnTerms are the owner's terms (the protected ones are applied apart).
func (s *Service) OwnTerms(ctx context.Context) []string {
	list, err := s.Blocklist(ctx)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.Term)
	}
	return out
}

// Terms is every blocked term: the protected ones and the owner's.
func (s *Service) Terms(ctx context.Context) []string {
	return append(ProtectedTerms(), s.OwnTerms(ctx)...)
}

// AddTerm adds a term (lower case, words only).
func (s *Service) AddTerm(ctx context.Context, term string) (Term, error) {
	term = decision.NormWords(term)
	if utf8.RuneCountInString(strings.ReplaceAll(term, " ", "")) < 2 {
		return Term{}, ErrBadTerm
	}
	if slices.Contains(decision.ProtectedAdultTerms, term) {
		return Term{}, ErrProtectedTerm
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
	term, _, ok := s.BlockedMatch(ctx, nil, texts...)
	return term, ok
}

// BlockedMatch is BlockedText that also says whether the term is one of the
// protected ones. Protected terms inside one of the performer names (the
// ones the metadata source lists for the scene) do not count (D-109).
func (s *Service) BlockedMatch(ctx context.Context, performers []string, texts ...string) (term string, protected, ok bool) {
	own := s.OwnTerms(ctx)
	for _, t := range texts {
		if term, ok := decision.BlockedTermExcept(t, decision.ProtectedAdultTerms, performers); ok {
			return term, true, true
		}
		if term, ok := decision.BlockedTerm(t, own); ok {
			return term, false, true
		}
	}
	return "", false, false
}
