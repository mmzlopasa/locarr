package migrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// The shared parts of the Sonarr, Radarr, Lidarr and Prowlarr APIs (v3 for
// Sonarr and Radarr, v1 for Lidarr and Prowlarr), read as their public API
// documentation describes them.

type arrStatus struct {
	AppName string `json:"appName"`
	Version string `json:"version"`
}

type arrField struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
}

func fieldMap(list []arrField) map[string]any {
	out := map[string]any{}
	for _, f := range list {
		out[f.Name] = f.Value
	}
	return out
}

// apiBase is the versioned API path of an *arr kind.
func apiBase(k Kind) string {
	switch k {
	case Sonarr, Radarr:
		return "/api/v3"
	}
	return "/api/v1"
}

// arrDetect reads system/status and checks the application and version.
func arrDetect(ctx context.Context, c *client) (arrStatus, error) {
	var st arrStatus
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/system/status", nil, &st); err != nil {
		var se *SourceError
		if errors.As(err, &se) && se.Code == "not_found" {
			return st, &SourceError{Code: "unsupported_version", Message: fmt.Sprintf("this does not look like %s v%s or later (no %s API)",
				c.conn.Kind.Label(), minVersion[c.conn.Kind], apiBase(c.conn.Kind))}
		}
		return st, err
	}
	if st.AppName != "" && !strings.EqualFold(st.AppName, c.conn.Kind.Label()) {
		return st, &SourceError{Code: "wrong_app", Message: fmt.Sprintf("this address is %s, not %s", st.AppName, c.conn.Kind.Label())}
	}
	if !versionAtLeast(st.Version, minVersion[c.conn.Kind]) {
		return st, &SourceError{Code: "unsupported_version", Message: fmt.Sprintf("%s %s is too old; version %s or later is needed",
			c.conn.Kind.Label(), st.Version, minVersion[c.conn.Kind])}
	}
	return st, nil
}

// minVersion is the oldest supported version per kind.
var minVersion = map[Kind]string{Sonarr: "3", Radarr: "3", Lidarr: "1", Prowlarr: "1", Bazarr: "1", Overseerr: "1", Seerr: "1",
	QBittorrent: "4.1", SABnzbd: "3"}

// versionAtLeast compares dotted versions numerically ("4.0.10" ≥ "4").
func versionAtLeast(v, minimum string) bool {
	if minimum == "" {
		return true
	}
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return true // unknown: let it try
	}
	a, b := strings.Split(v, "."), strings.Split(minimum, ".")
	for i := range b {
		var x, y int
		if i < len(a) {
			x, _ = strconv.Atoi(strings.TrimFunc(a[i], func(r rune) bool { return r < '0' || r > '9' }))
		}
		y, _ = strconv.Atoi(b[i])
		if x != y {
			return x > y
		}
	}
	return true
}

func major(v string) int {
	n, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(v, "v"), ".", 2)[0])
	return n
}

func arrTags(ctx context.Context, c *client) (map[int64]string, error) {
	var list []struct {
		ID    int64  `json:"id"`
		Label string `json:"label"`
	}
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/tag", nil, &list); err != nil {
		return nil, err
	}
	out := map[int64]string{}
	for _, t := range list {
		out[t.ID] = t.Label
	}
	return out, nil
}

type rawProfileItem struct {
	ID      int64            `json:"id"`
	Name    string           `json:"name"`
	Quality *QualityRef      `json:"quality"`
	Items   []rawProfileItem `json:"items"`
	Allowed bool             `json:"allowed"`
}

func (r rawProfileItem) item() ProfileItem {
	it := ProfileItem{ID: r.ID, Name: r.Name, Quality: r.Quality, Allowed: r.Allowed}
	for _, sub := range r.Items {
		it.Items = append(it.Items, sub.item())
	}
	return it
}

func arrQualityProfiles(ctx context.Context, c *client) ([]QualityProfile, error) {
	var raw []struct {
		ID                int64            `json:"id"`
		Name              string           `json:"name"`
		UpgradeAllowed    bool             `json:"upgradeAllowed"`
		Cutoff            int64            `json:"cutoff"`
		Items             []rawProfileItem `json:"items"`
		MinFormatScore    int              `json:"minFormatScore"`
		CutoffFormatScore int              `json:"cutoffFormatScore"`
		FormatItems       []FormatScore    `json:"formatItems"`
		Language          *QualityRef      `json:"language"`
	}
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/qualityprofile", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]QualityProfile, 0, len(raw))
	for _, r := range raw {
		p := QualityProfile{ID: r.ID, Name: r.Name, UpgradeAllowed: r.UpgradeAllowed, Cutoff: r.Cutoff, MinFormatScore: r.MinFormatScore,
			CutoffFormatScore: r.CutoffFormatScore, FormatScores: r.FormatItems}
		for _, it := range r.Items {
			p.Items = append(p.Items, it.item())
		}
		if r.Language != nil {
			p.Language = r.Language.Name
		}
		out = append(out, p)
	}
	return out, nil
}

func arrQualityDefinitions(ctx context.Context, c *client) ([]QualityDefinition, error) {
	var raw []struct {
		Quality       QualityRef `json:"quality"`
		MinSize       *float64   `json:"minSize"`
		MaxSize       *float64   `json:"maxSize"`
		PreferredSize *float64   `json:"preferredSize"`
	}
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/qualitydefinition", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]QualityDefinition, 0, len(raw))
	for _, r := range raw {
		d := QualityDefinition{Quality: r.Quality.Name, Max: r.MaxSize, Preferred: r.PreferredSize}
		if r.MinSize != nil {
			d.Min = *r.MinSize
		}
		out = append(out, d)
	}
	return out, nil
}

func arrCustomFormats(ctx context.Context, c *client) ([]CustomFormat, error) {
	var raw []json.RawMessage
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/customformat", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]CustomFormat, 0, len(raw))
	for _, r := range raw {
		var head struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		}
		if json.Unmarshal(r, &head) == nil && head.Name != "" {
			out = append(out, CustomFormat{ID: head.ID, Name: head.Name, Raw: r})
		}
	}
	return out, nil
}

// terms reads a term list that is an array (v4) or a comma-separated string (v3).
func terms(raw json.RawMessage) []string {
	var list []string
	if json.Unmarshal(raw, &list) == nil {
		return cleanStrings(list)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return cleanStrings(strings.Split(s, ","))
	}
	return []string{}
}

func cleanStrings(in []string) []string {
	out := []string{}
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func arrReleaseProfiles(ctx context.Context, c *client) ([]ReleaseProfile, error) {
	var raw []struct {
		ID        int64             `json:"id"`
		Name      string            `json:"name"`
		Enabled   *bool             `json:"enabled"`
		Required  json.RawMessage   `json:"required"`
		Ignored   json.RawMessage   `json:"ignored"`
		Preferred []json.RawMessage `json:"preferred"`
		IndexerID int64             `json:"indexerId"`
		Tags      []int64           `json:"tags"`
	}
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/releaseprofile", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]ReleaseProfile, 0, len(raw))
	for _, r := range raw {
		p := ReleaseProfile{ID: r.ID, Name: r.Name, Enabled: r.Enabled == nil || *r.Enabled, Required: terms(r.Required),
			Ignored: terms(r.Ignored), Preferred: len(r.Preferred), IndexerID: r.IndexerID, Tags: r.Tags}
		out = append(out, p)
	}
	return out, nil
}

func arrDelayProfiles(ctx context.Context, c *client) ([]DelayProfile, error) {
	var out []DelayProfile
	err := c.get(ctx, apiBase(c.conn.Kind)+"/delayprofile", nil, &out)
	return out, err
}

func arrRootFolders(ctx context.Context, c *client) ([]RootFolder, error) {
	var out []RootFolder
	err := c.get(ctx, apiBase(c.conn.Kind)+"/rootfolder", nil, &out)
	return out, err
}

func arrNaming(ctx context.Context, c *client) (map[string]any, error) {
	out := map[string]any{}
	err := c.get(ctx, apiBase(c.conn.Kind)+"/config/naming", nil, &out)
	return out, err
}

type rawProvider struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	Implementation          string     `json:"implementation"`
	Protocol                string     `json:"protocol"`
	Enable                  *bool      `json:"enable"`
	EnableRSS               bool       `json:"enableRss"`
	EnableAutomaticSearch   bool       `json:"enableAutomaticSearch"`
	EnableInteractiveSearch bool       `json:"enableInteractiveSearch"`
	Priority                int        `json:"priority"`
	Tags                    []int64    `json:"tags"`
	Fields                  []arrField `json:"fields"`
	Capabilities            *struct {
		Categories []struct {
			ID            int `json:"id"`
			SubCategories []struct {
				ID int `json:"id"`
			} `json:"subCategories"`
		} `json:"categories"`
	} `json:"capabilities"`
}

func arrIndexers(ctx context.Context, c *client) ([]Indexer, error) {
	var raw []rawProvider
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/indexer", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]Indexer, 0, len(raw))
	for _, r := range raw {
		ix := Indexer{ID: r.ID, Name: r.Name, Implementation: r.Implementation, Protocol: r.Protocol, Enable: r.Enable == nil || *r.Enable,
			EnableRSS: r.EnableRSS, EnableAutomatic: r.EnableAutomaticSearch, EnableInteractive: r.EnableInteractiveSearch,
			Priority: r.Priority, Tags: r.Tags, Fields: fieldMap(r.Fields)}
		if r.Enable == nil {
			ix.Enable = r.EnableRSS || r.EnableAutomaticSearch || r.EnableInteractiveSearch
		}
		if r.Capabilities != nil {
			for _, cat := range r.Capabilities.Categories {
				ix.Categories = append(ix.Categories, cat.ID)
				for _, sub := range cat.SubCategories {
					ix.Categories = append(ix.Categories, sub.ID)
				}
			}
		}
		out = append(out, ix)
	}
	return out, nil
}

func arrDownloadClients(ctx context.Context, c *client) ([]DownloadClient, error) {
	var raw []rawProvider
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/downloadclient", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]DownloadClient, 0, len(raw))
	for _, r := range raw {
		out = append(out, DownloadClient{ID: r.ID, Name: r.Name, Implementation: r.Implementation, Protocol: r.Protocol,
			Enable: r.Enable == nil || *r.Enable, Priority: r.Priority, Tags: r.Tags, Fields: fieldMap(r.Fields)})
	}
	return out, nil
}

func arrPathMappings(ctx context.Context, c *client) ([]RemotePathMapping, error) {
	var out []RemotePathMapping
	err := c.get(ctx, apiBase(c.conn.Kind)+"/remotepathmapping", nil, &out)
	return out, err
}

func arrImportLists(ctx context.Context, c *client) ([]ImportList, error) {
	var raw []struct {
		ID                  int64      `json:"id"`
		Name                string     `json:"name"`
		Implementation      string     `json:"implementation"`
		Enabled             *bool      `json:"enabled"`
		EnableAuto          *bool      `json:"enableAutomaticAdd"`
		RootFolderPath      string     `json:"rootFolderPath"`
		QualityProfileID    int64      `json:"qualityProfileId"`
		MinimumAvailability string     `json:"minimumAvailability"`
		Monitor             string     `json:"monitor"`
		ShouldMonitor       *bool      `json:"shouldMonitor"`
		SearchOnAdd         bool       `json:"searchOnAdd"`
		Tags                []int64    `json:"tags"`
		Fields              []arrField `json:"fields"`
	}
	if err := c.get(ctx, apiBase(c.conn.Kind)+"/importlist", nil, &raw); err != nil {
		return nil, err
	}
	out := make([]ImportList, 0, len(raw))
	for _, r := range raw {
		l := ImportList{ID: r.ID, Name: r.Name, Implementation: r.Implementation, Enabled: true, RootFolderPath: r.RootFolderPath,
			QualityProfileID: r.QualityProfileID, MinimumAvailability: r.MinimumAvailability, Monitored: r.Monitor != "none",
			SearchOnAdd: r.SearchOnAdd, Tags: r.Tags, Fields: fieldMap(r.Fields)}
		if r.Enabled != nil {
			l.Enabled = *r.Enabled
		} else if r.EnableAuto != nil {
			l.Enabled = *r.EnableAuto
		}
		if r.ShouldMonitor != nil {
			l.Monitored = *r.ShouldMonitor
		}
		out = append(out, l)
	}
	return out, nil
}

type pagedRecords[T any] struct {
	Page         int `json:"page"`
	PageSize     int `json:"pageSize"`
	TotalRecords int `json:"totalRecords"`
	Records      []T `json:"records"`
}

type rawBlock struct {
	ID              int64     `json:"id"`
	MovieID         int64     `json:"movieId"`
	SeriesID        int64     `json:"seriesId"`
	ArtistID        int64     `json:"artistId"`
	SourceTitle     string    `json:"sourceTitle"`
	Date            time.Time `json:"date"`
	Protocol        string    `json:"protocol"`
	Indexer         string    `json:"indexer"`
	Message         string    `json:"message"`
	TorrentInfoHash string    `json:"torrentInfoHash"`
}

// arrBlocklist reads the blocklist page by page (at most limit entries).
func arrBlocklist(ctx context.Context, c *client, limit int) ([]BlockItem, error) {
	var out []BlockItem
	for page := 1; len(out) < limit; page++ {
		var res pagedRecords[rawBlock]
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"500"}, "sortKey": {"date"}, "sortDirection": {"descending"}}
		if err := c.get(ctx, apiBase(c.conn.Kind)+"/blocklist", q, &res); err != nil {
			return out, err
		}
		for _, r := range res.Records {
			out = append(out, BlockItem{ID: r.ID, Title: r.SourceTitle, Protocol: strings.ToLower(r.Protocol), Indexer: r.Indexer,
				InfoHash: strings.ToLower(r.TorrentInfoHash), Message: r.Message, Date: r.Date, ItemID: r.MovieID + r.SeriesID + r.ArtistID})
		}
		if len(res.Records) == 0 || page*max(res.PageSize, 1) >= res.TotalRecords {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

type rawHistory struct {
	ID          int64     `json:"id"`
	EventType   string    `json:"eventType"`
	SourceTitle string    `json:"sourceTitle"`
	Date        time.Time `json:"date"`
	MovieID     int64     `json:"movieId"`
	SeriesID    int64     `json:"seriesId"`
	EpisodeID   int64     `json:"episodeId"`
	ArtistID    int64     `json:"artistId"`
	AlbumID     int64     `json:"albumId"`
	Quality     struct {
		Quality QualityRef `json:"quality"`
	} `json:"quality"`
	Data map[string]any `json:"data"`
}

// historyEvents are the history entries worth bringing over.
var historyEvents = map[string]string{"grabbed": "grabbed", "downloadFolderImported": "imported", "trackFileImported": "imported",
	"downloadImported": "imported", "downloadFailed": "failed", "albumImportIncomplete": "failed"}

// arrHistory reads grabs, imports and failures newer than since (at most limit).
func arrHistory(ctx context.Context, c *client, since time.Time, limit int) ([]HistoryItem, error) {
	var out []HistoryItem
	for page := 1; len(out) < limit; page++ {
		var res pagedRecords[rawHistory]
		q := url.Values{"page": {strconv.Itoa(page)}, "pageSize": {"500"}, "sortKey": {"date"}, "sortDirection": {"descending"}}
		if err := c.get(ctx, apiBase(c.conn.Kind)+"/history", q, &res); err != nil {
			return out, err
		}
		old := false
		for _, r := range res.Records {
			if r.Date.Before(since) {
				old = true
				break
			}
			ev, ok := historyEvents[r.EventType]
			if !ok {
				continue
			}
			h := HistoryItem{ID: r.ID, EventType: ev, Title: r.SourceTitle, Date: r.Date, Quality: r.Quality.Quality.Name,
				ItemID: r.MovieID + r.SeriesID + r.ArtistID, EpisodeID: r.EpisodeID, AlbumID: r.AlbumID}
			if v, ok := r.Data["indexer"].(string); ok {
				h.Indexer = v
			}
			if v, ok := r.Data["downloadClientName"].(string); ok {
				h.Client = v
			} else if v, ok := r.Data["downloadClient"].(string); ok {
				h.Client = v
			}
			out = append(out, h)
		}
		if old || len(res.Records) == 0 || page*max(res.PageSize, 1) >= res.TotalRecords {
			break
		}
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// readArrSettings reads the settings categories every *arr shares.
func readArrSettings(ctx context.Context, c *client, s *Snapshot) error {
	var err error
	if s.QualityProfiles, err = arrQualityProfiles(ctx, c); err != nil {
		return err
	}
	if s.QualityDefinitions, err = arrQualityDefinitions(ctx, c); err != nil {
		return err
	}
	if c.conn.Kind != Lidarr {
		if s.CustomFormats, err = arrCustomFormats(ctx, c); err != nil {
			s.warn("custom formats could not be read: %v", err)
		}
	}
	if s.ReleaseProfiles, err = arrReleaseProfiles(ctx, c); err != nil {
		s.warn("release profiles could not be read: %v", err)
	}
	if s.DelayProfiles, err = arrDelayProfiles(ctx, c); err != nil {
		return err
	}
	if s.RootFolders, err = arrRootFolders(ctx, c); err != nil {
		return err
	}
	if s.Naming, err = arrNaming(ctx, c); err != nil {
		s.warn("naming could not be read: %v", err)
	}
	if s.Indexers, err = arrIndexers(ctx, c); err != nil {
		return err
	}
	if s.DownloadClients, err = arrDownloadClients(ctx, c); err != nil {
		return err
	}
	if s.PathMappings, err = arrPathMappings(ctx, c); err != nil {
		return err
	}
	if s.ImportLists, err = arrImportLists(ctx, c); err != nil {
		s.warn("import lists could not be read: %v", err)
	}
	return nil
}

func (s *Snapshot) warn(format string, a ...any) {
	s.Warnings = append(s.Warnings, fmt.Sprintf(format, a...))
}

// tmdbImage turns a TMDb image URL into the file name Locarr's image cache uses.
func tmdbImage(remote string) string {
	u, err := url.Parse(remote)
	if err != nil || !strings.HasSuffix(u.Host, "tmdb.org") {
		return ""
	}
	i := strings.LastIndex(u.Path, "/")
	if i < 0 {
		return ""
	}
	return u.Path[i+1:]
}

type rawImage struct {
	CoverType string `json:"coverType"`
	RemoteURL string `json:"remoteUrl"`
}

func poster(images []rawImage) string {
	for _, im := range images {
		if im.CoverType == "poster" {
			return tmdbImage(im.RemoteURL)
		}
	}
	return ""
}

// date keeps the YYYY-MM-DD part of a source timestamp.
func date(s string) string {
	if len(s) >= 10 {
		return s[:10]
	}
	return ""
}
