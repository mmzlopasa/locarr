package downloads

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

type sqlTx = sql.Tx

// QueueItem is a row of the queue: a tracked download, or an item someone else
// put in one of Locarr's categories (Unknown; Download has only display fields).
type QueueItem struct {
	Download Download
	Unknown  bool
	// Suggestion is a remote path mapping that would make a finished
	// download visible.
	Suggestion *PathSuggestion
}

// Queue returns the downloads that are still in a client (not yet imported,
// plus failed ones waiting for the user), and unknown items from the last poll.
func (s *Service) Queue(ctx context.Context, includeUnknown bool) ([]QueueItem, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+downloadColumns+` FROM downloads
		WHERE status IN ('active', 'completed') OR (status = 'failed' AND missing_count < 2)
		ORDER BY added_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	mappings, err := s.Mappings(ctx)
	if err != nil {
		return nil, err
	}
	out := []QueueItem{}
	for rows.Next() {
		d, err := scanDownload(rows)
		if err != nil {
			return nil, err
		}
		q := QueueItem{Download: d}
		if sug := s.DownloadSuggestion(d.ID); sug != nil && d.Status == StatusActive && !covered(mappings, *sug) {
			q.Suggestion = sug
		}
		out = append(out, q)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !includeUnknown {
		return out, nil
	}
	clients, err := s.Clients(ctx)
	if err != nil {
		return nil, err
	}
	snaps := s.Snapshots()
	for _, c := range clients {
		sn, ok := snaps[c.ID]
		if !ok {
			continue
		}
		for _, it := range sn.Unknown {
			id := c.ID
			d := Download{ClientID: &id, ClientName: c.Name, ClientItemID: it.ID, Protocol: c.Protocol, Title: it.Name,
				Category: it.Category, Status: StatusActive, AddedAt: it.AddedAt, UpdatedAt: sn.CheckedAt}
			copyItem(&d, it)
			d.Message = it.Message
			out = append(out, QueueItem{Download: d, Unknown: true})
		}
	}
	return out, nil
}

// actionTarget loads a tracked download that is still in its client.
func (s *Service) actionTarget(ctx context.Context, id int64) (Download, downloaders.Client, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, err
	}
	if d.Status == StatusRemoved || d.MissingCount >= 2 {
		return d, nil, invalid("the download is no longer in the client")
	}
	c, err := s.clientOf(ctx, d)
	if err != nil {
		return d, nil, err
	}
	cl, err := s.connect(ctx, c)
	return d, cl, err
}

// Pause pauses a tracked download.
func (s *Service) Pause(ctx context.Context, id int64) error {
	d, cl, err := s.actionTarget(ctx, id)
	if err != nil {
		return err
	}
	if err := cl.Pause(ctx, d.ClientItemID); err != nil {
		return err
	}
	s.Kick()
	return nil
}

// Resume resumes a tracked download.
func (s *Service) Resume(ctx context.Context, id int64) error {
	d, cl, err := s.actionTarget(ctx, id)
	if err != nil {
		return err
	}
	if err := cl.Resume(ctx, d.ClientItemID); err != nil {
		return err
	}
	s.Kick()
	return nil
}

// SetPriority moves a tracked download in the client's queue.
func (s *Service) SetPriority(ctx context.Context, id int64, p downloaders.Priority) error {
	if !p.Valid() {
		return invalid("unknown priority %q", p)
	}
	d, cl, err := s.actionTarget(ctx, id)
	if err != nil {
		return err
	}
	if !cl.Capabilities().Priority {
		return downloaders.ErrUnsupported
	}
	if err := cl.SetPriority(ctx, d.ClientItemID, p); err != nil {
		return err
	}
	s.Kick()
	return nil
}

// RemoveOptions say how to remove a download.
type RemoveOptions struct {
	DeleteData bool
	Blocklist  bool
	// Message is shown in the blocklist (why the user blocked it).
	Message string
}

// Remove removes a tracked download from its client (and optionally deletes
// its data and blocks the release). Items Locarr did not add cannot be removed.
func (s *Service) Remove(ctx context.Context, id int64, o RemoveOptions) error {
	d, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	inClient := d.Status != StatusRemoved && d.MissingCount < 2
	if inClient {
		c, err := s.clientOf(ctx, d)
		if err == nil {
			cl, err := s.connect(ctx, c)
			if err != nil {
				return err
			}
			if err := s.removeFromClient(ctx, cl, d, o.DeleteData); err != nil {
				return err
			}
		} else if !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	now := s.now()
	msg := "removed by user"
	if o.DeleteData {
		msg = "removed by user, data deleted"
	}
	from := string(d.State)
	if d.Status == StatusActive || d.Status == StatusCompleted {
		d.Status = StatusRemoved
		d.FinishedAt = &now
	}
	d.MissingCount, d.UpdatedAt = 2, now
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := save(ctx, tx, &d); err != nil {
			return err
		}
		if err := s.addEvent(ctx, tx, d.ID, EventRemoved, from, "", msg, map[string]bool{"deleteData": o.DeleteData}); err != nil {
			return err
		}
		if o.Blocklist {
			if _, err := s.addBlock(ctx, tx, BlockEntry{Title: d.Title, Protocol: string(d.Protocol), IndexerID: d.IndexerID,
				Indexer: d.Indexer, InfoHash: d.InfoHash, GUID: d.GUID, Reason: ReasonManual, Message: o.Message,
				ItemRef: d.ItemRef, DownloadID: &d.ID}); err != nil {
				return err
			}
			return s.addEvent(ctx, tx, d.ID, EventBlocklisted, "", "", o.Message, nil)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if o.Blocklist {
		s.resetBlockIndex()
	}
	s.Bus.Publish(ctx, Event{Type: EventRemoved, Download: d, Reason: "removed_by_user", Message: msg})
	return nil
}

// TestResult is the outcome of testing a client configuration.
type TestResult struct {
	OK                bool
	ErrorCode         string
	Message           string
	Version           string
	FreeSpace         *int64
	MissingCategories []string
	CanCreate         bool
	// PathChecks: the client's folders and whether Locarr sees them.
	PathChecks []PathCheck
}

// Test checks a client configuration. With id set, secrets left nil come from
// the stored client (the form never shows them).
func (s *Service) Test(ctx context.Context, id int64, in ClientInput) (TestResult, error) {
	impl, err := in.normalise()
	if err != nil {
		return TestResult{ErrorCode: "invalid", Message: err.Error()}, nil
	}
	var pw, key string
	if in.Password != nil {
		pw = *in.Password
	}
	if in.APIKey != nil {
		key = *in.APIKey
	}
	if id > 0 && (in.Password == nil || in.APIKey == nil) {
		spw, skey, err := s.secrets(ctx, id)
		if err != nil {
			return TestResult{}, err
		}
		if in.Password == nil {
			pw = spw
		}
		if in.APIKey == nil {
			key = skey
		}
	}
	c := Client{Host: in.Host, Port: in.Port, UseTLS: in.UseTLS, URLBase: in.URLBase, Username: in.Username,
		Directory: in.Directory, AllowSelfSigned: in.AllowSelfSigned, CategoryMovies: in.CategoryMovies,
		CategorySeries: in.CategorySeries, CategoryMusic: in.CategoryMusic}
	cl, err := s.build(impl, configOf(c, pw, key))
	if err != nil {
		return TestResult{ErrorCode: ErrorCode(err), Message: err.Error()}, nil
	}
	tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	st, err := cl.Test(tctx)
	if err != nil {
		return TestResult{ErrorCode: ErrorCode(err), Message: err.Error()}, nil
	}
	res := TestResult{OK: true, Version: st.Version, FreeSpace: st.FreeSpace, CanCreate: cl.Capabilities().CreateCategories}
	if caps := cl.Capabilities(); caps.Categories && len(c.CategorySet()) > 0 {
		cats, err := cl.Categories(tctx)
		if err != nil && !errors.Is(err, downloaders.ErrUnsupported) {
			return TestResult{ErrorCode: ErrorCode(err), Message: err.Error()}, nil
		}
		if err == nil && (caps.CreateCategories || len(cats) > 0 || impl.Name == "nzbget") {
			for _, want := range c.CategorySet() {
				if !slices.ContainsFunc(cats, func(x downloaders.Category) bool { return strings.EqualFold(x.Name, want) }) {
					res.MissingCategories = append(res.MissingCategories, want)
				}
			}
		}
		if errors.Is(err, downloaders.ErrUnsupported) {
			// For example Deluge without its Label plugin.
			res.MissingCategories = c.CategorySet()
			res.CanCreate = false
			res.Message = err.Error()
		}
	}
	if len(res.MissingCategories) > 0 {
		res.ErrorCode = "category_missing"
	}
	if mappings, err := s.Mappings(ctx); err == nil {
		c.ID = id
		c.Name = in.Name
		c.Host = strings.ToLower(strings.TrimSpace(in.Host))
		res.PathChecks = s.checkPaths(tctx, c, cl, st.SavePath, mappings)
	}
	return res, nil
}

// CreateCategories creates the client's missing categories.
func (s *Service) CreateCategories(ctx context.Context, id int64) ([]string, error) {
	c, err := s.GetClient(ctx, id)
	if err != nil {
		return nil, err
	}
	cl, err := s.connect(ctx, c)
	if err != nil {
		return nil, err
	}
	created := []string{}
	for _, cat := range c.CategorySet() {
		if err := cl.EnsureCategory(ctx, cat, ""); err != nil {
			return created, err
		}
		created = append(created, cat)
	}
	s.mu.Lock()
	delete(s.categories, id)
	s.mu.Unlock()
	return created, nil
}

// TestSaved tests a saved client as it is configured (health checks use it
// to find missing categories).
func (s *Service) TestSaved(ctx context.Context, id int64) (TestResult, error) {
	c, err := s.GetClient(ctx, id)
	if err != nil {
		return TestResult{}, err
	}
	return s.Test(ctx, id, ClientInput{Name: c.Name, Implementation: c.Implementation, Enabled: c.Enabled, Priority: c.Priority,
		Host: c.Host, Port: c.Port, UseTLS: c.UseTLS, AllowSelfSigned: c.AllowSelfSigned, URLBase: c.URLBase, Username: c.Username,
		Directory: c.Directory, CategoryMovies: c.CategoryMovies, CategorySeries: c.CategorySeries, CategoryMusic: c.CategoryMusic,
		RecentPriority: c.RecentPriority, OlderPriority: c.OlderPriority, InitialState: c.InitialState, RemoveCompleted: c.RemoveCompleted,
		Sequential: c.Sequential, FirstLastPiece: c.FirstLastPiece, Tags: c.Tags})
}
