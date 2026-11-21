// Package musicbrainz is the music metadata provider, written from the
// public documentation of the MusicBrainz web service (ws/2, JSON) and the
// Cover Art Archive API. It follows MusicBrainz's rules for API clients: at
// most one request per second from this process, a User-Agent naming the
// application, its version and a contact, and caching of every answer
// (D-039).
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/version"
)

// Default addresses.
const (
	DefaultBaseURL     = "https://musicbrainz.org"
	DefaultCoverArtURL = "https://coverartarchive.org"
	// ProjectURL is the contact when the operator set none.
	ProjectURL = "https://github.com/mmzlopasa/locarr"
)

const (
	maxBody      = 8 << 20
	maxImage     = 10 << 20
	timeout      = 20 * time.Second
	providerName = "musicbrainz"
	pageSize     = 100
	// maxPages bounds one browse (100 per page) so a huge discography cannot run away.
	maxPages = 60
)

// MBID matches a MusicBrainz identifier.
var MBID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Client talks to MusicBrainz and the Cover Art Archive.
type Client struct {
	BaseURL     string
	CoverArtURL string
	// Contact returns the operator's contact for the User-Agent ("" = ProjectURL).
	Contact func(ctx context.Context) string
	Cache   *metadata.Cache
	// Limiter: one request per second to MusicBrainz (the Cover Art Archive
	// has no such limit but shares it to stay polite).
	Limiter *metadata.Limiter
	HTTP    *http.Client
}

var (
	_ metadata.MusicProvider = (*Client)(nil)
	_ metadata.ImageSource   = (*Client)(nil)
)

// New returns a client (empty URLs = the public services).
func New(baseURL, coverURL string, cache *metadata.Cache) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if coverURL == "" {
		coverURL = DefaultCoverArtURL
	}
	c := &Client{BaseURL: strings.TrimRight(baseURL, "/"), CoverArtURL: strings.TrimRight(coverURL, "/"), Cache: cache,
		Limiter: metadata.NewLimiter(1, 1)}
	c.HTTP = &http.Client{Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 || !redirectAllowed(via[0].URL.Host, req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return c
}

// redirectAllowed: the same host, or the Cover Art Archive's documented
// storage at archive.org (D-040).
func redirectAllowed(from string, to *url.URL) bool {
	if to.Scheme != "https" && to.Scheme != "http" {
		return false
	}
	h := strings.ToLower(to.Hostname())
	if strings.EqualFold(to.Host, from) {
		return true
	}
	return to.Scheme == "https" && (h == "archive.org" || strings.HasSuffix(h, ".archive.org"))
}

// UserAgent is what every request sends.
func (c *Client) UserAgent(ctx context.Context) string {
	contact := ""
	if c.Contact != nil {
		contact = strings.TrimSpace(c.Contact(ctx))
	}
	if contact == "" {
		contact = ProjectURL
	}
	return fmt.Sprintf("Locarr/%s ( %s )", version.Version, contact)
}

// get fetches /ws/2/{path} as JSON through the cache.
func (c *Client) get(ctx context.Context, path string, q url.Values, ttl time.Duration, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("fmt", "json")
	key := metadata.CacheKey(providerName, path+"?"+q.Encode())
	if ttl > 0 {
		if body, ok := c.Cache.Get(ctx, key); ok {
			return json.Unmarshal(body, out)
		}
	}
	body, err := c.fetch(ctx, c.BaseURL+"/ws/2/"+path+"?"+q.Encode(), "application/json", maxBody)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("%w: bad answer from %s: %w", metadata.ErrUnavailable, path, err)
	}
	if ttl > 0 {
		c.Cache.Put(ctx, key, body, ttl)
	}
	return nil
}

func (c *Client) fetch(ctx context.Context, u, accept string, limit int64) ([]byte, error) {
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.Limiter.Wait(ctx); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", c.UserAgent(ctx))
		res, err := c.HTTP.Do(req)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
		_ = res.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("%w: %w", metadata.ErrUnavailable, err)
		}
		if int64(len(body)) > limit {
			return nil, fmt.Errorf("%w: answer too large", metadata.ErrUnavailable)
		}
		switch {
		case res.StatusCode == http.StatusServiceUnavailable || res.StatusCode == http.StatusTooManyRequests:
			// MusicBrainz answers 503 when a client goes over the limit.
			wait := 2 * time.Second
			if s, err := strconv.Atoi(res.Header.Get("Retry-After")); err == nil && s > 0 && s < 120 {
				wait = time.Duration(s) * time.Second
			}
			c.Limiter.Pause(time.Now().Add(wait))
			continue
		case res.StatusCode == http.StatusNotFound:
			return nil, metadata.ErrNotFound
		case res.StatusCode == http.StatusBadRequest:
			return nil, fmt.Errorf("%w: the request was refused (HTTP 400)", metadata.ErrNotFound)
		case res.StatusCode >= 300:
			return nil, fmt.Errorf("%w: HTTP %d", metadata.ErrUnavailable, res.StatusCode)
		}
		return body, nil
	}
	return nil, fmt.Errorf("%w: rate limited", metadata.ErrUnavailable)
}

// Wire types (MusicBrainz JSON).

type wireAlias struct {
	Name     string `json:"name"`
	SortName string `json:"sort-name"`
	Locale   string `json:"locale"`
	Type     string `json:"type"`
	Primary  bool   `json:"primary"`
}

type wireArtist struct {
	ID             string      `json:"id"`
	Name           string      `json:"name"`
	SortName       string      `json:"sort-name"`
	Type           string      `json:"type"`
	Country        string      `json:"country"`
	Disambiguation string      `json:"disambiguation"`
	Score          int         `json:"score"`
	Aliases        []wireAlias `json:"aliases"`
	LifeSpan       struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
	} `json:"life-span"`
	Genres []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"genres"`
}

func (a wireArtist) summary() metadata.ArtistSummary {
	return metadata.ArtistSummary{MBID: strings.ToLower(a.ID), Name: a.Name, SortName: a.SortName, Disambiguation: a.Disambiguation,
		Type: a.Type, Country: a.Country, Begin: a.LifeSpan.Begin, End: a.LifeSpan.End, Score: a.Score}
}

type wireReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
	Disambiguation   string   `json:"disambiguation"`
}

func lower(list []string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		out = append(out, strings.ToLower(s))
	}
	return out
}

func (g wireReleaseGroup) group() metadata.ReleaseGroup {
	return metadata.ReleaseGroup{MBID: strings.ToLower(g.ID), Title: g.Title, PrimaryType: strings.ToLower(g.PrimaryType),
		SecondaryTypes: lower(g.SecondaryTypes), FirstReleaseDate: g.FirstReleaseDate, Disambiguation: g.Disambiguation}
}

type wireCredit []struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
}

func (w wireCredit) String() string {
	var b strings.Builder
	for _, c := range w {
		b.WriteString(c.Name + c.JoinPhrase)
	}
	return b.String()
}

type wireRelease struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Status         string            `json:"status"`
	Date           string            `json:"date"`
	Country        string            `json:"country"`
	Disambiguation string            `json:"disambiguation"`
	Barcode        string            `json:"barcode"`
	ReleaseGroup   *wireReleaseGroup `json:"release-group"`
	LabelInfo      []struct {
		CatalogNumber string `json:"catalog-number"`
		Label         *struct {
			Name string `json:"name"`
		} `json:"label"`
	} `json:"label-info"`
	Media []struct {
		Position   int    `json:"position"`
		Format     string `json:"format"`
		Title      string `json:"title"`
		TrackCount int    `json:"track-count"`
		Tracks     []struct {
			ID        string `json:"id"`
			Number    string `json:"number"`
			Position  int    `json:"position"`
			Title     string `json:"title"`
			Length    int    `json:"length"`
			Recording struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Length int    `json:"length"`
			} `json:"recording"`
			ArtistCredit wireCredit `json:"artist-credit"`
		} `json:"tracks"`
	} `json:"media"`
}

func (r wireRelease) release(withTracks bool) metadata.Release {
	out := metadata.Release{MBID: strings.ToLower(r.ID), Title: r.Title, Status: strings.ToLower(r.Status), Date: r.Date, Country: r.Country,
		Disambiguation: r.Disambiguation, Barcode: r.Barcode, HasTracks: withTracks, Media: []metadata.Medium{}}
	if r.ReleaseGroup != nil {
		out.ReleaseGroupMBID = strings.ToLower(r.ReleaseGroup.ID)
	}
	for _, li := range r.LabelInfo {
		if li.Label != nil && out.Label == "" {
			out.Label, out.CatalogNumber = li.Label.Name, li.CatalogNumber
		}
	}
	for _, m := range r.Media {
		md := metadata.Medium{Position: m.Position, Format: m.Format, Title: m.Title, TrackCount: m.TrackCount}
		if withTracks {
			md.Tracks = []metadata.Track{}
			for _, t := range m.Tracks {
				length := t.Length
				if length == 0 {
					length = t.Recording.Length
				}
				md.Tracks = append(md.Tracks, metadata.Track{MBID: strings.ToLower(t.ID), RecordingMBID: strings.ToLower(t.Recording.ID),
					Number: t.Number, Position: t.Position, Title: t.Title, LengthMs: length, Artist: t.ArtistCredit.String()})
			}
			if md.TrackCount == 0 {
				md.TrackCount = len(md.Tracks)
			}
		}
		out.Media = append(out.Media, md)
	}
	return out
}

// escapeLucene escapes the query syntax characters MusicBrainz's search uses.
func escapeLucene(s string) string {
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(`+-&|!(){}[]^"~*?:\/`, r) {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SearchArtists implements metadata.MusicProvider. A MusicBrainz id finds that artist.
func (c *Client) SearchArtists(ctx context.Context, query string) ([]metadata.ArtistSummary, error) {
	query = strings.TrimSpace(query)
	id := strings.TrimSpace(strings.TrimPrefix(query, "mbid:"))
	if MBID.MatchString(id) {
		a, err := c.Artist(ctx, id)
		if errors.Is(err, metadata.ErrNotFound) {
			return []metadata.ArtistSummary{}, nil
		}
		if err != nil {
			return nil, err
		}
		a.Score = 100
		return []metadata.ArtistSummary{a.ArtistSummary}, nil
	}
	var res struct {
		Artists []wireArtist `json:"artists"`
	}
	if err := c.get(ctx, "artist", url.Values{"query": {escapeLucene(query)}, "limit": {"25"}}, metadata.TTLSearch, &res); err != nil {
		return nil, err
	}
	out := make([]metadata.ArtistSummary, 0, len(res.Artists))
	for _, a := range res.Artists {
		out = append(out, a.summary())
	}
	return out, nil
}

func checkID(id string) error {
	if !MBID.MatchString(id) {
		return fmt.Errorf("%w: %q is not a MusicBrainz id", metadata.ErrNotFound, id)
	}
	return nil
}

// Artist implements metadata.MusicProvider.
func (c *Client) Artist(ctx context.Context, mbid string) (metadata.Artist, error) {
	if err := checkID(mbid); err != nil {
		return metadata.Artist{}, err
	}
	var a wireArtist
	if err := c.get(ctx, "artist/"+strings.ToLower(mbid), url.Values{"inc": {"aliases genres"}}, metadata.TTLArtist, &a); err != nil {
		return metadata.Artist{}, err
	}
	out := metadata.Artist{ArtistSummary: a.summary(), Aliases: []metadata.Alias{}, Genres: []string{}}
	for _, al := range a.Aliases {
		out.Aliases = append(out.Aliases, metadata.Alias{Name: al.Name, SortName: al.SortName, Locale: al.Locale, Type: al.Type, Primary: al.Primary})
	}
	for _, g := range a.Genres {
		out.Genres = append(out.Genres, g.Name)
	}
	return out, nil
}

// browse pages through a browse request; each page decodes into a fresh value.
func browse[T any](ctx context.Context, c *Client, path string, q url.Values, ttl time.Duration, items func(*T) (int, int)) error {
	for page := 0; page < maxPages; page++ {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("limit", strconv.Itoa(pageSize))
		qq.Set("offset", strconv.Itoa(page*pageSize))
		var v T
		if err := c.get(ctx, path, qq, ttl, &v); err != nil {
			return err
		}
		n, total := items(&v)
		if n == 0 || (page+1)*pageSize >= total {
			return nil
		}
	}
	return nil
}

// ReleaseGroups implements metadata.MusicProvider.
func (c *Client) ReleaseGroups(ctx context.Context, artistMBID string) ([]metadata.ReleaseGroup, error) {
	if err := checkID(artistMBID); err != nil {
		return nil, err
	}
	out := []metadata.ReleaseGroup{}
	type page struct {
		Count  int                `json:"release-group-count"`
		Groups []wireReleaseGroup `json:"release-groups"`
	}
	err := browse(ctx, c, "release-group", url.Values{"artist": {strings.ToLower(artistMBID)}}, metadata.TTLReleases, func(p *page) (int, int) {
		for _, g := range p.Groups {
			out = append(out, g.group())
		}
		return len(p.Groups), p.Count
	})
	return out, err
}

type releasePage struct {
	Count    int           `json:"release-count"`
	Releases []wireRelease `json:"releases"`
}

func (c *Client) releases(ctx context.Context, key, id string) ([]metadata.Release, error) {
	if err := checkID(id); err != nil {
		return nil, err
	}
	out := []metadata.Release{}
	err := browse(ctx, c, "release", url.Values{key: {strings.ToLower(id)}, "inc": {"release-groups media labels"}}, metadata.TTLReleases,
		func(p *releasePage) (int, int) {
			for _, r := range p.Releases {
				out = append(out, r.release(false))
			}
			return len(p.Releases), p.Count
		})
	return out, err
}

// Releases implements metadata.MusicProvider.
func (c *Client) Releases(ctx context.Context, artistMBID string) ([]metadata.Release, error) {
	return c.releases(ctx, "artist", artistMBID)
}

// GroupReleases implements metadata.MusicProvider.
func (c *Client) GroupReleases(ctx context.Context, groupMBID string) ([]metadata.Release, error) {
	list, err := c.releases(ctx, "release-group", groupMBID)
	for i := range list {
		if list[i].ReleaseGroupMBID == "" {
			list[i].ReleaseGroupMBID = strings.ToLower(groupMBID)
		}
	}
	return list, err
}

// Release implements metadata.MusicProvider.
func (c *Client) Release(ctx context.Context, mbid string) (metadata.Release, error) {
	if err := checkID(mbid); err != nil {
		return metadata.Release{}, err
	}
	var r wireRelease
	if err := c.get(ctx, "release/"+strings.ToLower(mbid), url.Values{"inc": {"recordings artist-credits media labels release-groups"}},
		metadata.TTLRelease, &r); err != nil {
		return metadata.Release{}, err
	}
	return r.release(true), nil
}

// Cover image names: "rg-<mbid>.jpg" (release group front) or "r-<mbid>.jpg" (release front).
var coverFile = regexp.MustCompile(`^(rg|r)-([0-9a-f-]{36})\.jpg$`)

// CoverSizes are the thumbnail sizes served (Cover Art Archive: 250, 500, 1200).
var CoverSizes = map[string]bool{"250": true, "500": true, "1200": true}

// CoverName is the image name of a release group's front cover.
func CoverName(groupMBID string) string { return "rg-" + strings.ToLower(groupMBID) + ".jpg" }

// FetchImage implements metadata.ImageSource for Cover Art Archive fronts.
func (c *Client) FetchImage(ctx context.Context, size, file string) ([]byte, string, error) {
	m := coverFile.FindStringSubmatch(file)
	if m == nil || !CoverSizes[size] {
		return nil, "", metadata.ErrBadImage
	}
	kind := map[string]string{"rg": "release-group", "r": "release"}[m[1]]
	body, err := c.fetch(ctx, fmt.Sprintf("%s/%s/%s/front-%s", c.CoverArtURL, kind, m[2], size), "image/*", maxImage)
	if err != nil {
		return nil, "", err
	}
	return body, http.DetectContentType(body), nil
}

// FetchCover returns the front cover of a release (or, failing that, of its group) at size.
func (c *Client) FetchCover(ctx context.Context, releaseMBID, groupMBID, size string) ([]byte, error) {
	if releaseMBID != "" {
		if b, _, err := c.FetchImage(ctx, size, "r-"+strings.ToLower(releaseMBID)+".jpg"); err == nil {
			return b, nil
		}
	}
	if groupMBID == "" {
		return nil, metadata.ErrNotFound
	}
	b, _, err := c.FetchImage(ctx, size, CoverName(groupMBID))
	return b, err
}
