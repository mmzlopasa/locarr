package indexers

import (
	"sync"
	"time"
)

// cache keeps one indexer's answer to an identical request for a short time,
// so repeating a search (or paging back) does not hit the indexer again.
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	at       time.Time
	releases []Release
}

const cacheMaxEntries = 500

func newCache() *cache { return &cache{entries: map[string]cacheEntry{}} }

func (c *cache) get(key string, ttl time.Duration, now time.Time) ([]Release, bool) {
	if ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || now.Sub(e.at) > ttl {
		return nil, false
	}
	return e.releases, true
}

func (c *cache) put(key string, releases []Release, ttl time.Duration, now time.Time) {
	if ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= cacheMaxEntries {
		for k, e := range c.entries {
			if now.Sub(e.at) > ttl {
				delete(c.entries, k)
			}
		}
		// Still full: drop the oldest entry.
		for len(c.entries) >= cacheMaxEntries {
			var oldest string
			var at time.Time
			for k, e := range c.entries {
				if oldest == "" || e.at.Before(at) {
					oldest, at = k, e.at
				}
			}
			delete(c.entries, oldest)
		}
	}
	c.entries[key] = cacheEntry{at: now, releases: releases}
}
