package app

import (
	"path/filepath"
	"testing"
)

// The owner's setup: qBittorrent has the downloads folder at /downloads,
// Locarr sees it at <data>/downloads. Locarr finds the finished download,
// suggests the mapping in the queue, the suggestion list and the health
// checks, adds nothing by itself, and imports once the user adds it.
func TestPathMappingSuggestionToImport(t *testing.T) {
	le := newLibraryEnv(t)
	le.client.remote = "/downloads"
	m := le.addMovie(10378, true)
	waitFor(t, "the automatic search to grab", func() bool { return le.downloadCount(m.ID) == 1 })
	le.client.finish(t)
	le.poll()

	type suggestion struct {
		Host, RemotePath, LocalPath, ClientPath, FoundAt, Source string
		DownloadIds                                              []int64
	}
	var queue struct {
		Items []struct {
			ID             int64
			Status         string
			Message        string
			PathSuggestion *suggestion
		}
	}
	le.expect(le.do("GET", "/api/v1/queue", nil), 200).json(&queue)
	if len(queue.Items) != 1 || queue.Items[0].Status != "active" || queue.Items[0].PathSuggestion == nil {
		t.Fatalf("queue: %+v", queue.Items)
	}
	local := filepath.Join(le.downloads)
	sg := *queue.Items[0].PathSuggestion
	if sg.Host != "flow.lan" || sg.RemotePath != "/downloads" || sg.LocalPath != local || sg.Source != "download" ||
		sg.ClientPath != "/downloads/Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR" ||
		sg.FoundAt != filepath.Join(local, "Big.Buck.Bunny.2008.1080p.BluRay.x264-LOCARR") {
		t.Fatalf("suggestion: %+v", sg)
	}
	var list []suggestion
	le.expect(le.do("GET", "/api/v1/remotepathmappings/suggestions", nil), 200).json(&list)
	if len(list) != 1 || list[0].RemotePath != "/downloads" || len(list[0].DownloadIds) != 1 || list[0].DownloadIds[0] != queue.Items[0].ID {
		t.Fatalf("suggestions: %+v", list)
	}
	var health struct {
		Issues []struct {
			Code   string
			Params map[string]any
		}
	}
	le.expect(le.do("POST", "/api/v1/system/health/run", nil), 200).json(&health)
	found := false
	for _, is := range health.Issues {
		if is.Code == "mapping_suggested" && is.Params["remotePath"] == "/downloads" && is.Params["localPath"] == local {
			found = true
		}
	}
	if !found {
		t.Fatalf("health: %+v", health.Issues)
	}
	var maps []map[string]any
	le.expect(le.do("GET", "/api/v1/remotepathmappings", nil), 200).json(&maps)
	if len(maps) != 0 {
		t.Fatalf("a mapping was added without the user: %+v", maps)
	}

	le.expect(le.do("POST", "/api/v1/remotepathmappings", map[string]any{"host": sg.Host, "remotePath": sg.RemotePath, "localPath": sg.LocalPath}), 201)
	le.poll()
	waitFor(t, "the import", func() bool { return le.movie(m.ID).File != nil })
	le.expect(le.do("GET", "/api/v1/remotepathmappings/suggestions", nil), 200).json(&list)
	if len(list) != 0 {
		t.Fatalf("suggestions after adding: %+v", list)
	}
}
