package indexers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
)

// tokenTTL is how long a download link from a search stays valid.
const tokenTTL = 24 * time.Hour

const tokenAAD = "indexers:download" // #nosec G101 -- an AAD label, not a credential

type tokenBody struct {
	I int64  `json:"i"`
	U string `json:"u"`
	T string `json:"t"`
	E int64  `json:"e"`
}

// DownloadToken returns an opaque, encrypted token for a release's download
// link, for the API's download endpoint. The link (which may carry tracker
// passkeys) never reaches the client.
func (s *Service) DownloadToken(r Release) (string, error) {
	if r.DownloadURL == "" {
		return "", ErrNoDownload
	}
	raw, err := json.Marshal(tokenBody{I: r.IndexerID, U: r.DownloadURL, T: r.Title, E: s.now().Add(tokenTTL).Unix()})
	if err != nil {
		return "", err
	}
	ct, err := s.Box.Seal(raw, tokenAAD)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(ct), nil
}

// File is a downloaded release file, or the magnet link the indexer sent
// instead (Magnet set, no Data).
type File struct {
	Name        string
	ContentType string
	Data        []byte
	Magnet      string
}

// FetchToken downloads the release a token points at (for indexer id).
func (s *Service) FetchToken(ctx context.Context, id int64, token string) (File, error) {
	ct, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return File{}, ErrBadToken
	}
	raw, err := s.Box.Open(ct, tokenAAD)
	if err != nil {
		return File{}, ErrBadToken
	}
	var t tokenBody
	if json.Unmarshal(raw, &t) != nil || t.I != id || s.now().Unix() > t.E {
		return File{}, ErrBadToken
	}
	return s.Fetch(ctx, Release{IndexerID: t.I, DownloadURL: t.U, Title: t.T})
}

// Fetch downloads a release's .torrent or .nzb with the indexer's credentials.
// The link must be on the indexer's own host; the indexer may redirect to
// another host (the tracker) or to a magnet link (see httpx.FollowRelease and
// D-080). It counts as a grab against the
// indexer's daily limit (ErrGrabLimit when reached).
func (s *Service) Fetch(ctx context.Context, r Release) (File, error) {
	if r.DownloadURL == "" {
		return File{}, ErrNoDownload
	}
	ind, err := s.Get(ctx, r.IndexerID)
	if err != nil {
		return File{}, err
	}
	link, err := url.Parse(r.DownloadURL)
	if err != nil || (link.Scheme != "http" && link.Scheme != "https") {
		return File{}, ErrNoDownload
	}
	base, _ := url.Parse(ind.BaseURL)
	if base == nil || !strings.EqualFold(link.Hostname(), base.Hostname()) {
		return File{}, fmt.Errorf("%w: the link is not on the indexer's host", ErrNoDownload)
	}
	c, err := s.client(ctx, ind)
	if err != nil {
		return File{}, err
	}
	if err := s.ReserveGrab(ctx, ind); err != nil {
		return File{}, err
	}
	full := strings.ReplaceAll(r.DownloadURL, keyPlaceholder, c.secret())
	res, err := c.Fetch(ctx, full)
	if err != nil {
		// A bad file says something about the release, not the indexer's health.
		if countsAsFailure(ctx, err) && !strings.Contains(err.Error(), httpx.ErrNotReleaseFile.Error()) {
			s.recordFailure(ctx, ind, err)
		}
		return File{}, err
	}
	if res.Magnet != "" {
		return File{Magnet: res.Magnet, Name: safeFileName(r.Title, strconv.FormatInt(r.IndexerID, 10)) + ".magnet",
			ContentType: "text/plain"}, nil
	}
	f := File{Data: res.Body, ContentType: "application/octet-stream"}
	if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil && params["filename"] != "" {
		f.Name = path.Base(params["filename"])
	}
	ext := ".torrent"
	if ind.Protocol() == Usenet {
		ext = ".nzb"
	}
	if f.Name == "" || f.Name == "." || f.Name == "/" {
		f.Name = safeFileName(r.Title, strconv.FormatInt(r.IndexerID, 10)) + ext
	}
	if ind.Protocol() == Usenet {
		f.ContentType = "application/x-nzb"
	} else {
		f.ContentType = "application/x-bittorrent"
	}
	return f, nil
}

func safeFileName(title, fallback string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '/' || r == '\\' || r == '"' || r < 32 || r == 127:
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())
	if s == "" {
		return fallback
	}
	if len(s) > 180 {
		s = s[:180]
	}
	return s
}
