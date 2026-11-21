package app

import (
	"context"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloads"
	"github.com/mmzlopasa/locarr/server/internal/indexers"
)

// releaseSource lets grabs fetch release files through their indexer, with the
// indexer's credentials, grab limit and seeding goals.
type releaseSource struct{ ix *indexers.Service }

var _ downloads.ReleaseSource = releaseSource{}

func (r releaseSource) Indexer(ctx context.Context, id int64) (downloads.IndexerInfo, error) {
	ind, err := r.ix.Get(ctx, id)
	if err != nil {
		return downloads.IndexerInfo{}, err
	}
	info := downloads.IndexerInfo{Name: ind.Name, Tags: ind.Tags, SeedRatio: ind.SeedRatio}
	if ind.SeedTimeMinutes != nil {
		d := time.Duration(*ind.SeedTimeMinutes) * time.Minute
		info.SeedTime = &d
	}
	return info, nil
}

func (r releaseSource) Fetch(ctx context.Context, id int64, title, link string) ([]byte, error) {
	f, err := r.ix.Fetch(ctx, indexers.Release{IndexerID: id, Title: title, DownloadURL: link})
	return f.Data, err
}

func (r releaseSource) FetchToken(ctx context.Context, id int64, token string) ([]byte, error) {
	f, err := r.ix.FetchToken(ctx, id, token)
	return f.Data, err
}
