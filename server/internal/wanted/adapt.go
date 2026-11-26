package wanted

import (
	"context"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/decision"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// indexerFlag maps an indexer's marker to a custom format indexer flag.
var indexerFlag = map[string]string{
	"freeleech": "freeleech", "halfleech": "halfleech", "doubleupload": "double_upload", "double_upload": "double_upload",
	"golden": "golden", "approved": "approved", "internal": "internal", "scene": "scene", "nuked": "nuked",
	"freeleech75": "freeleech75", "freeleech25": "freeleech25",
}

// Flags turns indexer markers into customformats.IndexerFlags names.
func Flags(r *indexers.Release) []string {
	var out []string
	seen := map[string]bool{}
	add := func(f string) {
		if f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, f := range r.Flags {
		add(indexerFlag[strings.ToLower(strings.ReplaceAll(f, " ", ""))])
	}
	// Volume factors say the same as the flags when an indexer sends only those.
	if r.DownloadVolumeFactor != nil {
		switch *r.DownloadVolumeFactor {
		case 0:
			add("freeleech")
		case 0.5:
			add("halfleech")
		case 0.25:
			add("freeleech75")
		case 0.75:
			add("freeleech25")
		}
	}
	if r.UploadVolumeFactor != nil && *r.UploadVolumeFactor == 2 {
		add("double_upload")
	}
	return out
}

// Candidate adapts an indexer result to the decision engine's input. ind may
// be nil when the indexer is unknown (deleted since).
func Candidate(r *indexers.Release, ind *indexers.Indexer) decision.Candidate {
	c := decision.Candidate{GUID: r.GUID, Title: r.Title, Size: r.Size, Protocol: string(r.Protocol), IndexerID: r.IndexerID, Categories: r.Categories,
		IndexerName: r.IndexerName, IndexerPriority: r.IndexerPriority, IndexerFlags: Flags(r), Seeders: r.Seeders, Peers: r.Peers,
		PublishDate: r.PublishDate, InfoURL: r.InfoURL, InfoHash: r.InfoHash,
		IDs: decision.IDs{TMDb: int(r.TMDBID), TVDb: int(r.TVDBID), IMDb: r.IMDBID}}
	c.DownloadURL = r.DownloadURL
	if c.DownloadURL == "" {
		c.DownloadURL = r.MagnetURL
	}
	if c.Protocol == "" {
		c.Protocol = decision.Torrent
	}
	if ind != nil {
		c.IndexerDisabled = !ind.Enabled
		c.IndexerRSSDisabled = !ind.EnableRSS
		c.IndexerAutomaticSearchDisabled = !ind.EnableAutomatic
		c.IndexerInteractiveSearchDisabled = !ind.EnableInteractive
		c.MinimumSeeders = ind.MinimumSeeders
		if c.IndexerPriority == 0 {
			c.IndexerPriority = ind.Priority
		}
		if c.IndexerName == "" {
			c.IndexerName = ind.Name
		}
	}
	return c
}

// Blocklist adapts the downloads blocklist to the decision engine.
type Blocklist struct{ Checker downloads.BlocklistChecker }

var _ decision.Blocklist = Blocklist{}

// Blocked implements decision.Blocklist.
func (b Blocklist) Blocked(ctx context.Context, c *decision.Candidate, _ *parser.Release) (string, bool) {
	if b.Checker == nil {
		return "", false
	}
	var id *int64
	if c.IndexerID > 0 {
		v := c.IndexerID
		id = &v
	}
	blocked, reason := b.Checker.IsBlocklisted(ctx, downloads.Candidate{Title: c.Title, Protocol: c.Protocol, IndexerID: id,
		InfoHash: strings.ToLower(c.InfoHash), GUID: c.GUID})
	return reason, blocked
}
