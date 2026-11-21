// Package contracttest is the one behaviour suite every download client must
// pass, against a real instance (build tag "clients") or a faithful fake.
package contracttest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/downloaders"
	"github.com/mmzlopasa/locarr/server/internal/downloaders/testkit"
)

// Target is one client instance under test.
type Target struct {
	Client downloaders.Client
	// Category used by the suite (created when the client can).
	Category string
	// ClientSeedDir is a folder as the client sees it, LocalSeedDir the same
	// folder as the test sees it. The suite writes a file there and adds a
	// torrent for it, so the client verifies the data and seeds with no network.
	// Empty = the seeding check is skipped (usenet, or not mounted).
	ClientSeedDir string
	LocalSeedDir  string
	// Timeout for asynchronous changes (default 60 s).
	Timeout time.Duration
}

// Run runs the suite.
func Run(t *testing.T, tg Target) {
	t.Helper()
	if tg.Timeout == 0 {
		tg.Timeout = 60 * time.Second
	}
	if tg.Category == "" {
		tg.Category = "locarr-test"
	}
	c := tg.Client
	caps := c.Capabilities()
	ctx := context.Background()

	t.Run("test", func(t *testing.T) {
		st, err := c.Test(ctx)
		if err != nil {
			t.Fatalf("Test: %v", err)
		}
		if st.Version == "" {
			t.Error("Test returned no version")
		}
	})

	t.Run("categories", func(t *testing.T) {
		err := c.EnsureCategory(ctx, tg.Category, "")
		if !caps.CreateCategories {
			if err != nil && !errors.Is(err, downloaders.ErrUnsupported) {
				t.Fatalf("EnsureCategory on a client without category creation: %v", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("EnsureCategory: %v", err)
		}
		// Twice is fine.
		if err := c.EnsureCategory(ctx, tg.Category, ""); err != nil {
			t.Fatalf("EnsureCategory again: %v", err)
		}
		cats, err := c.Categories(ctx)
		if err != nil {
			t.Fatalf("Categories: %v", err)
		}
		if !slices.ContainsFunc(cats, func(x downloaders.Category) bool { return strings.EqualFold(x.Name, tg.Category) }) {
			t.Fatalf("category %q missing from %+v", tg.Category, cats)
		}
	})

	if caps.Protocol == downloaders.Torrent {
		runTorrent(t, tg)
	} else {
		runUsenet(t, tg)
	}
}

func find(t *testing.T, c downloaders.Client, category, id string) (downloaders.Item, bool) {
	t.Helper()
	items, err := c.List(context.Background(), category)
	if err != nil {
		t.Fatalf("List(%q): %v", category, err)
	}
	for _, it := range items {
		if downloaders.SameID(it.ID, id) {
			return it, true
		}
	}
	return downloaders.Item{}, false
}

// eventually polls cond until it holds or the timeout passes.
func eventually(t *testing.T, timeout time.Duration, what string, cond func() (bool, string)) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	last := ""
	for {
		ok, state := cond()
		if ok {
			return
		}
		last = state
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (last: %s)", what, last)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func checkItem(t *testing.T, it downloaders.Item) {
	t.Helper()
	if !it.State.Valid() {
		t.Errorf("item %s: invalid state %q (raw %q)", it.ID, it.State, it.RawState)
	}
	if it.RawState == "" {
		t.Errorf("item %s: raw state missing", it.ID)
	}
	if it.Progress < 0 || it.Progress > 1 {
		t.Errorf("item %s: progress %v out of range", it.ID, it.Progress)
	}
}

func runTorrent(t *testing.T, tg Target) {
	c := tg.Client
	caps := c.Capabilities()
	ctx := context.Background()
	cat := tg.Category
	listCat := cat
	if !caps.Categories && !caps.DirectoryCategories {
		listCat = ""
	}

	t.Run("magnet paused", func(t *testing.T) {
		link, hash := testkit.RandomMagnet("locarr-contract-magnet")
		id, err := c.Add(ctx, downloaders.AddRequest{Magnet: link, Category: cat, Paused: true, Name: "locarr-contract-magnet"})
		if err != nil {
			t.Fatalf("Add magnet: %v", err)
		}
		if !downloaders.SameID(id, hash) {
			t.Fatalf("Add returned %q, want the info hash %q", id, hash)
		}
		t.Cleanup(func() { _ = c.Remove(ctx, id, true) })
		eventually(t, tg.Timeout, "the magnet to be listed as paused or queued", func() (bool, string) {
			it, ok := find(t, c, listCat, id)
			if !ok {
				return false, "not listed"
			}
			checkItem(t, it)
			if (caps.Categories || caps.DirectoryCategories) && !strings.EqualFold(it.Category, cat) {
				return false, "category " + it.Category
			}
			return it.State == downloaders.StatePaused || it.State == downloaders.StateQueued, string(it.State) + "/" + it.RawState
		})
		if caps.Priority {
			if err := c.SetPriority(ctx, id, downloaders.PriorityTop); err != nil {
				t.Errorf("SetPriority top: %v", err)
			}
			if err := c.SetPriority(ctx, id, downloaders.PriorityLast); err != nil {
				t.Errorf("SetPriority last: %v", err)
			}
		}
		if err := c.Remove(ctx, id, true); err != nil && !errors.Is(err, downloaders.ErrUnsupported) {
			t.Fatalf("Remove: %v", err)
		}
		if errors.Is(c.Remove(ctx, "0000000000000000000000000000000000000000", false), nil) {
			t.Log("removing an unknown hash returned no error (accepted)")
		}
		eventually(t, tg.Timeout, "the magnet to disappear", func() (bool, string) {
			_, ok := find(t, c, listCat, id)
			return !ok, "still listed"
		})
	})

	if tg.ClientSeedDir == "" {
		return
	}
	t.Run("torrent file seeds local data", func(t *testing.T) {
		tor := testkit.RandomTorrent("locarr-contract", 64<<10)
		if err := os.WriteFile(filepath.Join(tg.LocalSeedDir, tor.Name), tor.Data, 0o644); err != nil { // #nosec G306 -- the client must read it
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join(tg.LocalSeedDir, tor.Name)) })
		ratio := 1.5
		seed := 2 * time.Hour
		id, err := c.Add(ctx, downloaders.AddRequest{TorrentFile: tor.File, Category: cat, SavePath: tg.ClientSeedDir,
			Name: tor.Name, SeedRatio: &ratio, SeedTime: &seed, Tags: []string{"locarr"}})
		if err != nil {
			t.Fatalf("Add torrent: %v", err)
		}
		if !downloaders.SameID(id, tor.InfoHash) {
			t.Fatalf("Add returned %q, want %q", id, tor.InfoHash)
		}
		t.Cleanup(func() { _ = c.Remove(ctx, id, false) })
		var it downloaders.Item
		eventually(t, tg.Timeout, "the torrent to verify and seed", func() (bool, string) {
			var ok bool
			it, ok = find(t, c, listCat, id)
			if !ok {
				return false, "not listed"
			}
			checkItem(t, it)
			done := it.State == downloaders.StateSeeding || it.State == downloaders.StateCompleted
			return done && it.Progress == 1 && !it.Busy, string(it.State) + "/" + it.RawState
		})
		if it.Size != int64(len(tor.Data)) {
			t.Errorf("size %d, want %d", it.Size, len(tor.Data))
		}
		if filepath.Clean(it.SavePath) != filepath.Clean(tg.ClientSeedDir) {
			t.Errorf("save path %q, want %q", it.SavePath, tg.ClientSeedDir)
		}
		if filepath.Clean(it.ContentPath) != filepath.Join(tg.ClientSeedDir, tor.Name) {
			t.Errorf("content path %q, want %q", it.ContentPath, filepath.Join(tg.ClientSeedDir, tor.Name))
		}
		if it.Name != tor.Name {
			t.Errorf("name %q, want %q", it.Name, tor.Name)
		}
		files, err := c.Files(ctx, id)
		if err != nil {
			t.Fatalf("Files: %v", err)
		}
		if len(files) != 1 || files[0].Size != int64(len(tor.Data)) || filepath.Base(files[0].Path) != tor.Name {
			t.Errorf("files = %+v", files)
		}

		if err := c.Pause(ctx, id); err != nil {
			t.Fatalf("Pause: %v", err)
		}
		eventually(t, tg.Timeout, "pause", func() (bool, string) {
			it, ok := find(t, c, listCat, id)
			// A finished torrent may report paused or completed when stopped.
			return ok && (it.State == downloaders.StatePaused || it.State == downloaders.StateCompleted), string(it.State) + "/" + it.RawState
		})
		if err := c.Resume(ctx, id); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		eventually(t, tg.Timeout, "resume", func() (bool, string) {
			it, ok := find(t, c, listCat, id)
			return ok && (it.State == downloaders.StateSeeding || it.State == downloaders.StateCompleted), string(it.State) + "/" + it.RawState
		})

		if err := c.Remove(ctx, id, false); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		eventually(t, tg.Timeout, "removal", func() (bool, string) {
			_, ok := find(t, c, listCat, id)
			return !ok, "still listed"
		})
		// Without deleteData the files stay.
		if _, err := os.Stat(filepath.Join(tg.LocalSeedDir, tor.Name)); err != nil {
			t.Errorf("data was deleted by Remove(deleteData=false): %v", err)
		}
	})
}

func runUsenet(t *testing.T, tg Target) {
	c := tg.Client
	caps := c.Capabilities()
	ctx := context.Background()
	cat := tg.Category

	t.Run("nzb paused", func(t *testing.T) {
		name := "Locarr.Contract.Test." + time.Now().Format("150405.000")
		id, err := c.Add(ctx, downloaders.AddRequest{NZB: testkit.NZB(name), Name: name, Category: cat, Paused: true})
		if err != nil {
			t.Fatalf("Add NZB: %v", err)
		}
		if id == "" {
			t.Fatal("Add returned no ID")
		}
		t.Cleanup(func() { _ = c.Remove(ctx, id, true) })
		eventually(t, tg.Timeout, "the NZB to be listed as paused", func() (bool, string) {
			it, ok := find(t, c, cat, id)
			if !ok {
				return false, "not listed"
			}
			checkItem(t, it)
			if !strings.EqualFold(it.Category, cat) {
				return false, "category " + it.Category
			}
			return it.State == downloaders.StatePaused, string(it.State) + "/" + it.RawState
		})
		if caps.Priority {
			for _, p := range []downloaders.Priority{downloaders.PriorityTop, downloaders.PriorityLast, downloaders.PriorityNormal} {
				if err := c.SetPriority(ctx, id, p); err != nil {
					t.Errorf("SetPriority %s: %v", p, err)
				}
			}
		}
		if err := c.Resume(ctx, id); err != nil {
			t.Fatalf("Resume: %v", err)
		}
		eventually(t, tg.Timeout, "the NZB to leave the paused state", func() (bool, string) {
			it, ok := find(t, c, cat, id)
			// With no news server it may queue, try, or fail straight away.
			return ok && it.State != downloaders.StatePaused, string(it.State) + "/" + it.RawState
		})
		if err := c.Pause(ctx, id); err != nil && !errors.Is(err, downloaders.ErrItemNotFound) {
			t.Logf("Pause after resume: %v (accepted: the job may have finished)", err)
		}
		if err := c.Remove(ctx, id, true); err != nil {
			t.Fatalf("Remove: %v", err)
		}
		eventually(t, tg.Timeout, "removal", func() (bool, string) {
			_, ok := find(t, c, cat, id)
			return !ok, "still listed"
		})
	})

	t.Run("unknown id", func(t *testing.T) {
		err := c.Pause(ctx, "999999999")
		if err == nil {
			t.Log("pausing an unknown ID returned no error (accepted)")
		}
	})
}
