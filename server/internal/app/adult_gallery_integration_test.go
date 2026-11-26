package app

import (
	"archive/zip"
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	afake "github.com/mmzlopasa/locarr/server/internal/adult/meta/fake"
	ifake "github.com/mmzlopasa/locarr/server/internal/indexers/fake"
)

// Synthetic, non-explicit release names.
const (
	galleryRelease      = "StudioA.24.02.01.Performer.One.Second.Scene.XXX.IMAGESET-GRP"
	galleryVideoRelease = "StudioA.24.02.01.Performer.One.Second.Scene.1080p.WEB-DL.x264-GRP"
)

// genPNG is a generated picture (a colour gradient, no real content).
func genPNG(t *testing.T, n int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for y := 0; y < 48; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{R: uint8(n * 40), G: uint8(x * 4), B: uint8(y * 5), A: 255}) // #nosec G115 -- small test values
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// writeGalleryZip is what the fake client "downloads" for an image set: a
// zip of generated pictures (out of order, in a subfolder), a text file and
// a system file, next to a loose .nfo.
func writeGalleryZip(t *testing.T, folder, name string) {
	t.Helper()
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	for _, f := range []struct {
		name string
		data []byte
	}{
		{"set/10.png", genPNG(t, 3)}, {"set/2.png", genPNG(t, 2)}, {"set/1.png", genPNG(t, 1)},
		{"set/readme.txt", []byte("synthetic test set")}, {"__MACOSX/set/._1.png", []byte("x")},
	} {
		fw, err := w.Create(f.name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = fw.Write(f.data)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, name+".zip"), b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "release.nfo"), []byte("nfo"), 0o644); err != nil {
		t.Fatal(err)
	}
}

type apiGallery struct {
	ID      int64
	Kind    string
	Ref     string
	Title   string
	Studio  string
	SceneID int64
	Status  string
	Image   string
	Path    string
	File    *struct {
		Path       string
		Size       int64
		ImageCount int
	}
}

// TestAdultGalleryEndToEnd: a scene added with "also look for the photo
// set" gets a linked gallery → the gallery search takes the image set (not
// the video) → the fake client finishes a zip → it is extracted safely to
// "<root>/<gallery folder>/0001.png …" with a cover, the zip stays for
// seeding → images are served only to permitted users → nothing leaks.
func TestAdultGalleryEndToEnd(t *testing.T) {
	ae := newAdultEnv(t)
	saved := ifake.Catalog
	ifake.Catalog = append(append([]ifake.Release{}, saved...),
		ifake.Release{Title: galleryRelease, Cat: 6060, Size: 80 << 20, Seeders: 30, Peers: 31, Hash: "7ab2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour},
		ifake.Release{Title: galleryVideoRelease, Cat: 6010, Size: 900 << 20, Seeders: 90, Peers: 91, Hash: "7bb2c3d4e5f60718293a4b5c6d7e8f9012345678", Age: 5 * time.Hour})
	t.Cleanup(func() { ifake.Catalog = saved })
	ae.client.write = func(t *testing.T, folder, name string) {
		if strings.Contains(name, "IMAGESET") {
			writeGalleryZip(t, folder, name)
			return
		}
		writeMovieFolder(t, folder, name)
	}

	me := ae.me()
	ae.expect(ae.do("PUT", "/api/v1/adult/settings", map[string]any{"enabled": true, "confirmAdults": true, "stashdbKey": afake.StashKey,
		"defaultProfileId": ae.profileID}), 200)
	ae.grant(me.ID, "admin", true)
	ae.expect(ae.do("POST", "/api/v1/adult/rootfolders", map[string]any{"path": ae.adultRoot}), 201)
	var mgr apiUser
	ae.expect(ae.do("POST", "/api/v1/users", map[string]any{"username": "mgr", "password": "manager-pw1", "role": "manager"}), 201).json(&mgr)
	mgrTok := ae.login("mgr", "manager-pw1")

	// The scene with its photo set.
	var scene apiAdultItem
	ae.expect(ae.do("POST", "/api/v1/adult/items", map[string]any{"source": "stashdb", "id": "sc-2", "search": false, "gallery": true}), 201).json(&scene)
	var page struct{ Items []apiGallery }
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items?kind=gallery&sceneId=%d", scene.ID), nil), 200).json(&page)
	if len(page.Items) != 1 {
		t.Fatalf("galleries %+v", page.Items)
	}
	g := page.Items[0]
	if g.Kind != "gallery" || g.SceneID != scene.ID || g.Title != "Second Scene" || g.Studio != "Studio A" ||
		!strings.HasPrefix(g.Ref, "adult_gallery:") || g.Status != "wanted" {
		t.Fatalf("gallery %+v", g)
	}
	// A second photo set for the same scene is refused; a hand-made one needs a title.
	ae.expect(ae.do("POST", "/api/v1/adult/galleries", map[string]any{"sceneId": scene.ID}), 409)
	ae.expect(ae.do("POST", "/api/v1/adult/galleries", map[string]any{"studio": "Studio A"}), 400)
	// Wanted lists it.
	var wanted struct{ Items []apiGallery }
	ae.expect(ae.do("GET", "/api/v1/adult/items?missing=true", nil), 200).json(&wanted)
	if !slices.ContainsFunc(wanted.Items, func(i apiGallery) bool { return i.ID == g.ID }) {
		t.Fatal("wanted lacks the gallery")
	}

	// Search: the image set is taken, never the scene's video.
	if _, err := ae.deps.DB.W.Exec(`UPDATE indexers SET categories = json_set(categories, '$.adult', json('[6000]'))`); err != nil {
		t.Fatal(err)
	}
	ae.expect(ae.do("POST", fmt.Sprintf("/api/v1/adult/items/%d/search", g.ID), nil), 202)
	var title string
	waitFor(t, "the gallery grab", func() bool {
		return ae.deps.DB.R.QueryRow(`SELECT title FROM downloads WHERE json_extract(item_ref, '$.ref') = ?`, g.Ref).Scan(&title) == nil
	})
	if title != galleryRelease {
		t.Fatalf("grabbed %q", title)
	}
	ae.poll()
	ae.client.finish(t)
	ae.poll()
	waitFor(t, "the gallery import", func() bool {
		ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d", g.ID), nil), 200).json(&g)
		return g.File != nil
	})
	wantFolder := filepath.Join(ae.adultRoot, "Studio A - 2024-02-01 - Second Scene [Performer One, Performer Two]")
	if g.File.Path != wantFolder || g.File.ImageCount != 3 || g.Status != "downloaded" {
		t.Fatalf("file %+v, want %s with 3 images", g.File, wantFolder)
	}
	entries, err := os.ReadDir(wantFolder)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"0001.png", "0002.png", "0003.png", "cover.png"}) {
		t.Fatalf("folder holds %v", names)
	}
	// Natural order: 1, 2, 10.
	if b, _ := os.ReadFile(filepath.Join(wantFolder, "0003.png")); !bytes.Equal(b, genPNG(t, 3)) {
		t.Fatal("0003.png is not set/10.png")
	}
	// The archive stays in downloads for seeding.
	if _, err := os.Stat(filepath.Join(ae.downloads, galleryRelease, galleryRelease+".zip")); err != nil {
		t.Fatalf("archive gone: %v", err)
	}
	if g.Image == "" {
		t.Fatal("no cover image")
	}

	// Images: list, full size and thumbnail, private.
	var imgs []struct {
		Index int
		Name  string
	}
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/images", g.ID), nil), 200).json(&imgs)
	if len(imgs) != 3 || imgs[0].Name != "0001.png" {
		t.Fatalf("images %+v", imgs)
	}
	for _, q := range []string{"", "?thumb=true"} {
		r := ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/images/1%s", g.ID, q), nil), 200)
		if cc := r.Header.Get("Cache-Control"); !strings.Contains(cc, "private") || !strings.HasPrefix(r.Header.Get("Content-Type"), "image/") {
			t.Fatalf("image %q: %s %s", q, cc, r.Header.Get("Content-Type"))
		}
	}
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/images/4", g.ID), nil), 404)
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/images", scene.ID), nil), 404)
	var tl apiTimeline
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items/%d/timeline", g.ID), nil), 200).json(&tl)
	if !strings.Contains(tl.types(), "file_imported") {
		t.Fatalf("timeline %s", tl.types())
	}

	// Nothing reaches a manager without view_adult, nor the shared lists.
	for _, p := range []string{fmt.Sprintf("/api/v1/adult/items/%d", g.ID), fmt.Sprintf("/api/v1/adult/items/%d/images", g.ID),
		fmt.Sprintf("/api/v1/adult/items/%d/images/1", g.ID), fmt.Sprintf("/api/v1/adult/items/%d/images/1?thumb=true", g.ID),
		"/api/v1/adult/items?kind=gallery"} {
		if r := ae.as(mgrTok, "GET", p, nil); r.StatusCode != 404 {
			t.Fatalf("%s for the manager: %d", p, r.StatusCode)
		}
	}
	if r := ae.as(mgrTok, "POST", "/api/v1/adult/galleries", map[string]any{"title": "X"}); r.StatusCode != 404 {
		t.Fatalf("add gallery for the manager: %d", r.StatusCode)
	}
	ae.checkLeaks(t, mgrTok, ae.login("mgr", "manager-pw1"), apiAdultItem{ID: g.ID, Ref: g.Ref, Title: g.Title, Image: g.Image}, 1)
	for _, tok := range []string{mgrTok, ae.token} {
		for _, p := range []string{"/api/v1/history", "/api/v1/activity/stats", "/api/v1/board/items", "/api/v1/queue", "/api/v1/recyclebin",
			"/api/v1/calendar?start=2024-01-01&end=2024-12-31", "/api/stats", "/compat/radarr/api/v3/movie"} {
			r := ae.as(tok, "GET", p, nil)
			if strings.Contains(string(r.body), "Second Scene") || strings.Contains(string(r.body), "IMAGESET") || strings.Contains(string(r.body), g.Ref) {
				if tok == ae.token && (p == "/api/v1/queue" || p == "/api/v1/recyclebin") {
					continue // the owner holds view_adult: the queue and bin show their own adult entries
				}
				t.Fatalf("%s leaked the gallery: %s", p, r.body)
			}
		}
	}

	// Removal with "not interested": the scene's photo set is not added again.
	ae.expect(ae.do("DELETE", fmt.Sprintf("/api/v1/adult/items/%d?deleteFiles=true&exclude=true", g.ID), nil), 204)
	if _, err := os.Stat(wantFolder); err == nil {
		t.Fatal("the folder is still there (it goes to the recycle bin)")
	}
	var ex []struct{ Kind, Title string }
	ae.expect(ae.do("GET", "/api/v1/adult/exclusions", nil), 200).json(&ex)
	if len(ex) != 1 || ex[0].Kind != "gallery" {
		t.Fatalf("exclusions %+v", ex)
	}
	it := mustItem(t, ae, scene.ID)
	ae.deps.Adult.AutoGallery(t.Context(), it, false)
	ae.expect(ae.do("GET", fmt.Sprintf("/api/v1/adult/items?kind=gallery&sceneId=%d", scene.ID), nil), 200).json(&page)
	if len(page.Items) != 0 {
		t.Fatal("an excluded photo set came back")
	}
	// By hand it can still be added, and that lifts "not interested".
	ae.expect(ae.do("POST", "/api/v1/adult/galleries", map[string]any{"sceneId": scene.ID, "search": false}), 201)
	ae.expect(ae.do("GET", "/api/v1/adult/exclusions", nil), 200).json(&ex)
	if len(ex) != 0 {
		t.Fatalf("exclusion kept after adding by hand: %+v", ex)
	}
}
