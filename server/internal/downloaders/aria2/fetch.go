package aria2

import (
	"context"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
)

// fetchTorrent downloads a .torrent through the guarded HTTP client (size cap,
// timeout, same-host redirects only) and validates it, so the info hash is
// known before the client sees it.
func fetchTorrent(ctx context.Context, hc *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, downloaders.ClassifyTransportError(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, downloaders.ClientError("torrent URL answered HTTP %d", resp.StatusCode)
	}
	b, err := downloaders.ReadLimited(resp.Body, limit)
	if err != nil {
		return nil, err
	}
	if _, err := downloaders.ParseTorrent(b); err != nil {
		return nil, err
	}
	return b, nil
}
