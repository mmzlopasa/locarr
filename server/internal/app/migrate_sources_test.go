//go:build sources

package app

// Imports from a real application in Docker (scripts/test-sources.sh starts
// one, seeds it through its own public API and sets the variables below).
//
//	LOCARR_TEST_SOURCE_KIND   sonarr, radarr, lidarr, prowlarr, bazarr, overseerr, seerr, qbittorrent, sabnzbd
//	LOCARR_TEST_SOURCE_URL    its address
//	LOCARR_TEST_SOURCE_KEY    API key (qBittorrent: the password)
//	LOCARR_TEST_SOURCE_USER   qBittorrent user
//	LOCARR_TEST_SOURCE_ROOT   the media folder as the application sees it ("" = none)
//	LOCARR_TEST_SOURCE_LOCAL  the same folder on this machine (added to the data roots)
//	LOCARR_TEST_SOURCE_EXPECT category=minimum,…: items created or merged per category
//	LOCARR_TEST_SOURCE_FILES  files that must be linked in place

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/config"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/migrate"
)

// settingsView is what must not change on the source while importing.
func settingsView(s *migrate.Snapshot) string {
	v := map[string]any{"tags": s.Tags, "profiles": s.QualityProfiles, "formats": len(s.CustomFormats), "delay": s.DelayProfiles,
		"roots": s.RootFolders, "naming": s.Naming, "indexers": s.Indexers, "clients": s.DownloadClients, "mappings": s.PathMappings,
		"movies": len(s.Movies), "series": len(s.Series), "artists": len(s.Artists), "users": len(s.Users), "requests": len(s.Requests),
		"subtitleProfiles": s.SubtitleProfiles, "client": s.Client}
	b, _ := json.Marshal(v)
	return string(b)
}

func TestRealSourceImport(t *testing.T) {
	kind := os.Getenv("LOCARR_TEST_SOURCE_KIND")
	if kind == "" {
		t.Skip("no source (scripts/test-sources.sh)")
	}
	url, key, user := os.Getenv("LOCARR_TEST_SOURCE_URL"), os.Getenv("LOCARR_TEST_SOURCE_KEY"), os.Getenv("LOCARR_TEST_SOURCE_USER")
	root, local := os.Getenv("LOCARR_TEST_SOURCE_ROOT"), os.Getenv("LOCARR_TEST_SOURCE_LOCAL")
	if local != "" {
		local, _ = filepath.EvalSymlinks(local)
	}
	e := newEnvWith(t, func(c *config.Config) {
		if local != "" {
			c.DataRoots = append(c.DataRoots, local)
		}
	}, noLimits)
	e.setup()
	e.expect(e.do("PATCH", "/api/v1/indexers/settings", map[string]any{"allowedHosts": []string{"127.0.0.1"}}), 200)
	me := &migrateEnv{env: e, sources: map[string]int64{}, wait: 5 * time.Minute}

	conn := migrate.Conn{Kind: migrate.Kind(kind), URL: url, APIKey: key, Username: user}
	if kind == "qbittorrent" {
		conn.APIKey, conn.Password = "", key
	}
	pol := httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}}
	ro := migrate.ReadOptions{Scopes: migrate.Scopes{Settings: true, Library: true, Users: true, History: true}, HistorySince: time.Now().AddDate(-1, 0, 0)}
	before, err := migrate.Read(context.Background(), conn, pol, ro)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s %s: %s", before.AppName, before.Version, settingsView(before))

	body := map[string]any{"kind": kind, "url": url, "secret": key}
	if user != "" {
		body["username"] = user
	}
	var src struct {
		ID      int64
		Version string
	}
	e.expect(e.do("POST", "/api/v1/migrate/sources", body), 201).json(&src)
	sup := migrate.Kind(kind).Supported()
	opts := map[string]any{"id": src.ID, "settings": sup.Settings, "library": sup.Library, "users": sup.Users, "history": sup.History}
	if kind == "prowlarr" {
		opts["prowlarrMode"] = "direct"
	}
	run := map[string]any{"sources": []any{opts}}
	if root != "" {
		run["pathMappings"] = []any{map[string]any{"sourceId": 0, "from": root, "to": local}}
	}
	var r apiMigrateRun
	e.expect(e.do("POST", "/api/v1/migrate/runs", run), 202).json(&r)
	r = me.waitRun(r.ID, "planned")
	for _, pc := range r.Summary.PathChecks {
		t.Logf("path check %s → %s: %d of %d", pc.SourcePath, pc.LocalPath, pc.Found, pc.Files)
	}
	done := me.importRun(r.ID)
	for _, it := range me.items(r.ID, "") {
		t.Logf("%-8s %-7s %-18s %s %s %v %s", it.Status, it.Action, it.Category, it.Name, it.Reason, it.Attention, it.Error)
		if it.Status == "failed" {
			t.Errorf("failed: %s %s: %s", it.Category, it.Name, it.Error)
		}
	}
	for _, exp := range strings.Split(os.Getenv("LOCARR_TEST_SOURCE_EXPECT"), ",") {
		cat, n, ok := strings.Cut(strings.TrimSpace(exp), "=")
		if !ok {
			continue
		}
		want, _ := strconv.Atoi(n)
		got := done.Summary.Counts[cat]["create"] + done.Summary.Counts[cat]["update"]
		if got < want {
			t.Errorf("%s: %d created or merged, want at least %d (%v)", cat, got, want, done.Summary.Counts[cat])
		}
	}
	if n, _ := strconv.Atoi(os.Getenv("LOCARR_TEST_SOURCE_FILES")); n > 0 {
		var files int
		_ = e.deps.DB.R.QueryRow(`SELECT count(*) FROM media_files`).Scan(&files)
		if files < n {
			t.Errorf("%d files linked, want %d", files, n)
		}
	}
	// Running again duplicates nothing.
	var r2 apiMigrateRun
	e.expect(e.do("POST", "/api/v1/migrate/runs", run), 202).json(&r2)
	me.waitRun(r2.ID, "planned")
	for _, it := range me.items(r2.ID, "&action=create") {
		t.Errorf("second run would create %s %s", it.Category, it.Name)
	}
	// Nothing changed on the application.
	after, err := migrate.Read(context.Background(), conn, pol, ro)
	if err != nil {
		t.Fatal(err)
	}
	if a, b := settingsView(before), settingsView(after); a != b {
		t.Fatalf("the application changed while importing:\n%s\n%s", a, b)
	}
	if !reflect.DeepEqual(before.Users, after.Users) {
		t.Fatal("users changed")
	}
	fmt.Println("imported", done.Summary.Counts)
}
