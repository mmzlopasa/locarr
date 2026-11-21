// Package downloads keeps Locarr's download clients and remote path mappings,
// sends grabbed releases to a client, tracks every download until it is
// imported, failed or removed, records each state change and announces
// completions and failures on an in-process bus for the importers.
package downloads

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// Errors.
var (
	ErrNotFound      = errors.New("not found")
	ErrNameTaken     = errors.New("a download client with this name already exists")
	ErrInvalid       = errors.New("invalid value")
	ErrNoClient      = errors.New("no enabled download client for this protocol")
	ErrAlreadyQueued = errors.New("this release is already in the download queue")
	ErrFetch         = errors.New("could not fetch the release file")
	ErrUnknownItem   = errors.New("items added outside Locarr are not managed")
)

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Media kinds a client category is chosen by.
const (
	KindMovies = "movies"
	KindSeries = "series"
	KindMusic  = "music"
)

// Initial states of a new download.
const (
	InitialStart = "start"
	InitialPause = "pause"
)

// Client is a configured download client. Secrets are never read back: only
// whether they are set.
type Client struct {
	ID              int64
	Name            string
	Implementation  string
	Protocol        downloaders.Protocol
	Enabled         bool
	Priority        int
	Host            string
	Port            int
	UseTLS          bool
	AllowSelfSigned bool
	URLBase         string
	Username        string
	PasswordSet     bool
	APIKeySet       bool
	Directory       string
	CategoryMovies  string
	CategorySeries  string
	CategoryMusic   string
	RecentPriority  downloaders.Priority
	OlderPriority   downloaders.Priority
	InitialState    string
	RemoveCompleted bool
	Sequential      bool
	FirstLastPiece  bool
	Tags            []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Category returns the client's category for a media kind.
func (c Client) Category(kind string) string {
	switch kind {
	case KindMovies:
		return c.CategoryMovies
	case KindSeries:
		return c.CategorySeries
	case KindMusic:
		return c.CategoryMusic
	}
	return ""
}

// CategorySet returns the distinct non-empty categories of the client.
func (c Client) CategorySet() []string {
	var out []string
	for _, v := range []string{c.CategoryMovies, c.CategorySeries, c.CategoryMusic} {
		if v != "" && !slices.ContainsFunc(out, func(x string) bool { return strings.EqualFold(x, v) }) {
			out = append(out, v)
		}
	}
	return out
}

// ClientInput creates or replaces a client. Password and APIKey: nil keeps the
// stored value, "" clears it.
type ClientInput struct {
	Name            string
	Implementation  string
	Enabled         bool
	Priority        int
	Host            string
	Port            int
	UseTLS          bool
	AllowSelfSigned bool
	URLBase         string
	Username        string
	Password        *string
	APIKey          *string
	Directory       string
	CategoryMovies  string
	CategorySeries  string
	CategoryMusic   string
	RecentPriority  downloaders.Priority
	OlderPriority   downloaders.Priority
	InitialState    string
	RemoveCompleted bool
	Sequential      bool
	FirstLastPiece  bool
	Tags            []string
}

func cleanCategory(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 64 || strings.ContainsAny(s, "\\\x00\n\r") || strings.Contains(s, "..") {
		return "", invalid("category %q is not valid", s)
	}
	return s, nil
}

// normalise validates the input against its implementation.
func (in *ClientInput) normalise() (downloaders.Implementation, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 100 {
		return downloaders.Implementation{}, invalid("name is required (up to 100 characters)")
	}
	impl, ok := downloaders.Lookup(in.Implementation)
	if !ok {
		return impl, invalid("unknown implementation %q", in.Implementation)
	}
	in.Host = strings.TrimSpace(in.Host)
	in.URLBase = strings.TrimSpace(in.URLBase)
	in.Username = strings.TrimSpace(in.Username)
	in.Directory = strings.TrimSpace(in.Directory)
	if in.Port == 0 {
		in.Port = impl.DefaultPort
	}
	if _, err := (downloaders.Config{Host: in.Host, Port: in.Port, URLBase: in.URLBase}).BaseURL(); err != nil {
		return impl, invalid("%s", strings.TrimPrefix(err.Error(), downloaders.ErrInvalidRequest.Error()+": "))
	}
	if in.Priority == 0 {
		in.Priority = 1
	}
	if in.Priority < 1 || in.Priority > 50 {
		return impl, invalid("priority must be 1-50")
	}
	var err error
	for _, p := range []*string{&in.CategoryMovies, &in.CategorySeries, &in.CategoryMusic} {
		if *p, err = cleanCategory(*p); err != nil {
			return impl, err
		}
	}
	for _, p := range []*downloaders.Priority{&in.RecentPriority, &in.OlderPriority} {
		if *p == "" {
			*p = downloaders.PriorityNormal
		}
		if !p.Valid() {
			return impl, invalid("unknown priority %q", *p)
		}
	}
	if in.InitialState == "" {
		in.InitialState = InitialStart
	}
	if in.InitialState != InitialStart && in.InitialState != InitialPause {
		return impl, invalid("initial state must be start or pause")
	}
	tags := make([]string, 0, len(in.Tags))
	for _, t := range in.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" || slices.Contains(tags, t) {
			continue
		}
		if len(t) > 50 {
			return impl, invalid("tags are up to 50 characters")
		}
		tags = append(tags, t)
	}
	in.Tags = tags
	windowsPath := len(in.Directory) > 2 && in.Directory[1] == ':'
	if in.Directory != "" && !strings.HasPrefix(in.Directory, "/") && !windowsPath {
		return impl, invalid("directory must be an absolute path as the client sees it")
	}
	return impl, nil
}

const clientColumns = `id, name, implementation, protocol, enabled, priority, host, port, use_tls, allow_self_signed,
	url_base, username, password IS NOT NULL AND length(password) > 0, api_key IS NOT NULL AND length(api_key) > 0, directory,
	category_movies, category_series, category_music, recent_priority, older_priority, initial_state,
	remove_completed, sequential, first_last_piece, tags, created_at, updated_at`

func scanClient(row interface{ Scan(...any) error }) (Client, error) {
	var (
		c                  Client
		tags               string
		created, updated   int64
		recent, older, pro string
	)
	err := row.Scan(&c.ID, &c.Name, &c.Implementation, &pro, &c.Enabled, &c.Priority, &c.Host, &c.Port, &c.UseTLS,
		&c.AllowSelfSigned, &c.URLBase, &c.Username, &c.PasswordSet, &c.APIKeySet, &c.Directory,
		&c.CategoryMovies, &c.CategorySeries, &c.CategoryMusic, &recent, &older, &c.InitialState,
		&c.RemoveCompleted, &c.Sequential, &c.FirstLastPiece, &tags, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.Protocol = downloaders.Protocol(pro)
	c.RecentPriority, c.OlderPriority = downloaders.Priority(recent), downloaders.Priority(older)
	_ = json.Unmarshal([]byte(tags), &c.Tags)
	if c.Tags == nil {
		c.Tags = []string{}
	}
	c.CreatedAt, c.UpdatedAt = db.Time(created), db.Time(updated)
	return c, nil
}

// Clients lists every configured client by protocol, priority and name.
func (s *Service) Clients(ctx context.Context) ([]Client, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+clientColumns+` FROM download_clients ORDER BY protocol, priority, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Client{}
	for rows.Next() {
		c, err := scanClient(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetClient returns one client.
func (s *Service) GetClient(ctx context.Context, id int64) (Client, error) {
	return scanClient(s.DB.R.QueryRowContext(ctx, `SELECT `+clientColumns+` FROM download_clients WHERE id = ?`, id))
}

func secretAAD(id int64, field string) string { return fmt.Sprintf("downloadclient:%d:%s", id, field) }

func (s *Service) seal(id int64, field string, v *string) (any, error) {
	if v == nil || *v == "" {
		return nil, nil
	}
	return s.Box.Seal([]byte(*v), secretAAD(id, field))
}

func (s *Service) checkRequired(impl downloaders.Implementation, in ClientInput, passwordSet, keySet bool) error {
	for _, f := range impl.Required {
		switch f {
		case downloaders.FieldPassword:
			if in.Password != nil && *in.Password == "" || in.Password == nil && !passwordSet {
				return invalid("%s needs a password", impl.DisplayName)
			}
		case downloaders.FieldAPIKey:
			if in.APIKey != nil && *in.APIKey == "" || in.APIKey == nil && !keySet {
				return invalid("%s needs an API key", impl.DisplayName)
			}
		case downloaders.FieldUsername:
			if in.Username == "" {
				return invalid("%s needs a username", impl.DisplayName)
			}
		case downloaders.FieldDirectory:
			if in.Directory == "" {
				return invalid("%s needs a download folder", impl.DisplayName)
			}
		}
	}
	return nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// CreateClient adds a client.
func (s *Service) CreateClient(ctx context.Context, in ClientInput) (Client, error) {
	impl, err := in.normalise()
	if err != nil {
		return Client{}, err
	}
	if err := s.checkRequired(impl, in, false, false); err != nil {
		return Client{}, err
	}
	tags, _ := json.Marshal(in.Tags)
	now := db.Millis(s.now())
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `INSERT INTO download_clients (name, implementation, protocol, enabled, priority, host, port,
			use_tls, allow_self_signed, url_base, username, directory, category_movies, category_series, category_music,
			recent_priority, older_priority, initial_state, remove_completed, sequential, first_last_piece, tags, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			in.Name, impl.Name, string(impl.Protocol), in.Enabled, in.Priority, in.Host, in.Port, in.UseTLS, in.AllowSelfSigned,
			in.URLBase, in.Username, in.Directory, in.CategoryMovies, in.CategorySeries, in.CategoryMusic,
			string(in.RecentPriority), string(in.OlderPriority), in.InitialState, in.RemoveCompleted, in.Sequential,
			in.FirstLastPiece, string(tags), now, now).Scan(&id)
		if err != nil {
			return err
		}
		pw, err := s.seal(id, "password", in.Password)
		if err != nil {
			return err
		}
		key, err := s.seal(id, "apiKey", in.APIKey)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE download_clients SET password = ?, api_key = ? WHERE id = ?`, pw, key, id)
		return err
	})
	if isUnique(err) {
		return Client{}, ErrNameTaken
	}
	if err != nil {
		return Client{}, err
	}
	s.forget(id)
	return s.GetClient(ctx, id)
}

// UpdateClient replaces a client's settings (secrets: see ClientInput).
func (s *Service) UpdateClient(ctx context.Context, id int64, in ClientInput) (Client, error) {
	cur, err := s.GetClient(ctx, id)
	if err != nil {
		return Client{}, err
	}
	impl, err := in.normalise()
	if err != nil {
		return Client{}, err
	}
	if impl.Name != cur.Implementation {
		return Client{}, invalid("the implementation of a client cannot change; add a new client instead")
	}
	if err := s.checkRequired(impl, in, cur.PasswordSet, cur.APIKeySet); err != nil {
		return Client{}, err
	}
	tags, _ := json.Marshal(in.Tags)
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `UPDATE download_clients SET name = ?, enabled = ?, priority = ?, host = ?, port = ?,
			use_tls = ?, allow_self_signed = ?, url_base = ?, username = ?, directory = ?, category_movies = ?,
			category_series = ?, category_music = ?, recent_priority = ?, older_priority = ?, initial_state = ?,
			remove_completed = ?, sequential = ?, first_last_piece = ?, tags = ?, updated_at = ? WHERE id = ?`,
			in.Name, in.Enabled, in.Priority, in.Host, in.Port, in.UseTLS, in.AllowSelfSigned, in.URLBase, in.Username,
			in.Directory, in.CategoryMovies, in.CategorySeries, in.CategoryMusic, string(in.RecentPriority),
			string(in.OlderPriority), in.InitialState, in.RemoveCompleted, in.Sequential, in.FirstLastPiece, string(tags),
			db.Millis(s.now()), id)
		if err != nil {
			return err
		}
		for field, v := range map[string]*string{"password": in.Password, "apiKey": in.APIKey} {
			if v == nil {
				continue
			}
			sealed, err := s.seal(id, field, v)
			if err != nil {
				return err
			}
			col := "password"
			if field == "apiKey" {
				col = "api_key"
			}
			if _, err := tx.ExecContext(ctx, `UPDATE download_clients SET `+col+` = ? WHERE id = ?`, sealed, id); err != nil {
				return err
			}
		}
		return nil
	})
	if isUnique(err) {
		return Client{}, ErrNameTaken
	}
	if err != nil {
		return Client{}, err
	}
	s.forget(id)
	return s.GetClient(ctx, id)
}

// DeleteClient removes a client. Its downloads stay in the history (client_id
// becomes NULL); nothing in the client itself is touched.
func (s *Service) DeleteClient(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM download_clients WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.forget(id)
	return nil
}

// secrets returns the stored password and API key of a client.
func (s *Service) secrets(ctx context.Context, id int64) (password, apiKey string, err error) {
	var pw, key []byte
	if err := s.DB.R.QueryRowContext(ctx, `SELECT password, api_key FROM download_clients WHERE id = ?`, id).Scan(&pw, &key); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", ErrNotFound
		}
		return "", "", err
	}
	if len(pw) > 0 {
		b, err := s.Box.Open(pw, secretAAD(id, "password"))
		if err != nil {
			return "", "", err
		}
		password = string(b)
	}
	if len(key) > 0 {
		b, err := s.Box.Open(key, secretAAD(id, "apiKey"))
		if err != nil {
			return "", "", err
		}
		apiKey = string(b)
	}
	return password, apiKey, nil
}
