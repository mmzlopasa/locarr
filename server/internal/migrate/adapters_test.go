package migrate

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/migrate/fake"
)

var loopback = httpx.Policy{Allowed: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8"), netip.MustParsePrefix("::1/128")}}

func startFake(t *testing.T, kind string, lib *fake.Library) (*fake.Server, Conn) {
	t.Helper()
	f := fake.New(kind, "key-"+kind, lib)
	f.Prefix = "/base"
	if kind == "qbittorrent" {
		f.Username, f.Password = "admin", "secret"
	}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := Conn{Kind: Kind(kind), URL: srv.URL + "/base", APIKey: "key-" + kind}
	if kind == "qbittorrent" {
		c.APIKey, c.Username, c.Password = "", "admin", "secret"
	}
	return f, c
}

func all3() ReadOptions {
	return ReadOptions{Scopes: Scopes{Settings: true, Library: true, Users: true, History: true}, HistorySince: time.Now().AddDate(0, 0, -90)}
}

func TestReadEverySource(t *testing.T) {
	ctx := context.Background()
	lib := fake.Default()
	for _, k := range Kinds {
		t.Run(string(k), func(t *testing.T) {
			f, c := startFake(t, string(k), lib)
			s, err := Read(ctx, c, loopback, all3())
			if err != nil {
				t.Fatal(err)
			}
			if s.Version == "" {
				t.Fatal("no version")
			}
			if n := f.Mutations.Load(); n != 0 {
				t.Fatalf("%d changing requests were sent", n)
			}
			switch k {
			case Radarr:
				if len(s.Movies) != 3 || s.Movies[0].File == nil || s.Movies[0].Poster != "poster10378.jpg" || s.Movies[2].File != nil {
					t.Fatalf("movies %+v", s.Movies)
				}
				if len(s.Collections) != 1 || len(s.Exclusions) != 1 || len(s.ImportLists) != 2 || len(s.CustomFormats) != 3 {
					t.Fatalf("settings %d %d %d %d", len(s.Collections), len(s.Exclusions), len(s.ImportLists), len(s.CustomFormats))
				}
				if s.QualityProfiles[0].Language != "English" || len(s.QualityProfiles[0].FormatScores) != 2 {
					t.Fatalf("profile %+v", s.QualityProfiles[0])
				}
				// History stops at the 90-day window.
				if len(s.History) != 3 || len(s.Blocklist) != 1 {
					t.Fatalf("history %d blocklist %d", len(s.History), len(s.Blocklist))
				}
			case Sonarr:
				if len(s.Series) != 2 || len(s.Series[0].Episodes) != 4 || len(s.Series[0].Files) != 2 || s.Series[0].Episodes[1].FileID != 101 {
					t.Fatalf("series %+v", s.Series)
				}
				if len(s.Tags) != 2 || len(s.DownloadClients) != 3 || len(s.Indexers) != 4 || len(s.PathMappings) != 1 {
					t.Fatal("settings missing")
				}
				if s.History[0].EpisodeID != 12 {
					t.Fatalf("history %+v", s.History[0])
				}
			case Lidarr:
				if len(s.Artists) != 1 || len(s.Artists[0].Albums) != 2 || len(s.Artists[0].Albums[0].Files) != 2 {
					t.Fatalf("artists %+v", s.Artists)
				}
				f := s.Artists[0].Albums[0].Files[1]
				if f.Position != 2 || f.Disc != 1 || f.Title != "Second Light" || f.Quality != "FLAC" {
					t.Fatalf("track %+v", f)
				}
				if len(s.MetadataProfiles) != 1 || len(s.MetadataProfiles[0].Primary) != 2 {
					t.Fatalf("metadata profiles %+v", s.MetadataProfiles)
				}
			case Prowlarr:
				if len(s.Indexers) != 2 || len(s.Indexers[0].Categories) != 3 || len(s.Apps) != 1 {
					t.Fatalf("prowlarr %+v %+v", s.Indexers, s.Apps)
				}
			case Bazarr:
				if len(s.SubtitleProfiles) != 2 || !s.SubtitleProfiles[0].Languages[1].HI || !s.SubtitleProfiles[1].Languages[0].Forced ||
					!s.SubtitleProfiles[1].Cutoff || s.SubtitleProfiles[0].Cutoff {
					t.Fatalf("profiles %+v", s.SubtitleProfiles)
				}
				if len(s.SubtitleProviders) != 4 || s.SubtitleProviders[1].Fields["api_key"] != "subdl-key-123" {
					t.Fatalf("providers %+v", s.SubtitleProviders)
				}
				if len(s.SubtitleAssignments) != 4 {
					t.Fatalf("assignments %+v", s.SubtitleAssignments)
				}
			case Overseerr, Seerr:
				if len(s.Users) != 3 || len(s.Requests) != 4 || len(s.Issues) != 1 || len(s.Issues[0].Comments) != 2 {
					t.Fatalf("users %d requests %d issues %d", len(s.Users), len(s.Requests), len(s.Issues))
				}
				if s.Requests[0].Title != "Big Buck Bunny" || s.Requests[2].Title != "Cosmos Laundromat" || s.Requests[2].Year != 2015 {
					t.Fatalf("titles %+v", s.Requests)
				}
				if s.Users[1].Quotas["movie"].Limit != 3 || s.DefaultQuotas["series"].Limit != 2 {
					t.Fatal("quotas")
				}
			case QBittorrent:
				if s.Client == nil || len(s.Client.Categories) != 3 || s.Client.Categories[2].SavePath != "/downloads/tv-sonarr" {
					t.Fatalf("client %+v", s.Client)
				}
			case SABnzbd:
				if s.Client == nil || len(s.Client.Categories) != 2 || s.Client.Categories[0].SavePath != "/downloads/complete/movies" {
					t.Fatalf("client %+v", s.Client)
				}
			}
		})
	}
}

func TestDetectRefusals(t *testing.T) {
	ctx := context.Background()
	lib := fake.Default()
	_, c := startFake(t, "radarr", lib)
	bad := c
	bad.APIKey = "wrong"
	if _, err := Detect(ctx, bad, loopback); err == nil || code(err) != "unauthorized" {
		t.Fatalf("wrong key: %v", err)
	}
	wrong := c
	wrong.Kind = Sonarr
	wrong.APIKey = "key-radarr"
	if _, err := Detect(ctx, wrong, loopback); err == nil || code(err) != "wrong_app" {
		t.Fatalf("wrong app: %v", err)
	}
	f, c3 := startFake(t, "sonarr", lib)
	f.Version = "2.0.0.5344"
	if _, err := Detect(ctx, c3, loopback); err == nil || code(err) != "unsupported_version" {
		t.Fatalf("old version: %v", err)
	}
	_, q := startFake(t, "qbittorrent", lib)
	q.Password = "nope"
	if _, err := Detect(ctx, q, loopback); err == nil || code(err) != "unauthorized" {
		t.Fatalf("qbit password: %v", err)
	}
	// The loopback address is refused unless allowed (the indexer policy).
	if _, err := Detect(ctx, c, httpx.Policy{}); err == nil {
		t.Fatal("loopback allowed without the policy")
	}
}

func TestSonarrV3LanguageProfiles(t *testing.T) {
	lib := fake.Default()
	f, c := startFake(t, "sonarr", lib)
	f.Version = "3.0.10.1567"
	s, err := Read(context.Background(), c, loopback, all3())
	if err != nil {
		t.Fatal(err)
	}
	if len(s.LanguageProfiles) != 1 || len(s.LanguageProfiles[0].Languages) != 2 {
		t.Fatalf("%+v", s.LanguageProfiles)
	}
	langs := profileLanguages(s, s.QualityProfiles[0])
	conv := ConvertProfile(s.QualityProfiles[0], kindOf(Sonarr), langs)
	if len(conv.Profile.Languages) != 2 || conv.Profile.Languages[1] != "hr" {
		t.Fatalf("languages %+v", conv.Profile.Languages)
	}
}

func code(err error) string {
	var se *SourceError
	if errors.As(err, &se) {
		return se.Code
	}
	return ""
}
