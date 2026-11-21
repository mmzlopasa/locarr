package parser

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type goldenCase struct {
	Name   string         `json:"name"`
	Path   bool           `json:"path,omitempty"`
	Expect map[string]any `json:"expect"`
}

func loadGolden(t testing.TB) map[string][]goldenCase {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("testdata", "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden files: %v", err)
	}
	out := map[string][]goldenCase{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cases []goldenCase
		if err := json.Unmarshal(b, &cases); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = cases
	}
	return out
}

// TestGolden checks every name in testdata/*.json. Only the fields listed in
// "expect" are compared (nested objects partially); an empty list means the
// field must be empty and null means it must be absent.
func TestGolden(t *testing.T) {
	all := loadGolden(t)
	total, failed := 0, 0
	names := make([]string, 0, len(all))
	for k := range all {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, file := range names {
		for _, c := range all[file] {
			total++
			var r Release
			if c.Path {
				r = ParsePath(c.Name)
			} else {
				r = Parse(c.Name)
			}
			b, _ := json.Marshal(r)
			var got map[string]any
			_ = json.Unmarshal(b, &got)
			if diffs := compare("", c.Expect, got); len(diffs) > 0 {
				failed++
				if failed <= 60 {
					t.Errorf("%s: %q\n    %s\n    got %s", file, c.Name, strings.Join(diffs, "\n    "), b)
				}
			}
		}
	}
	if total < 1000 {
		t.Errorf("golden corpus has %d names, want at least 1000", total)
	}
	t.Logf("golden: %d names, %d failed", total, failed)
}

func compare(prefix string, want, got map[string]any) []string {
	var diffs []string
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		w, g := want[k], got[k]
		name := prefix + k
		switch wv := w.(type) {
		case nil:
			if g != nil && !isEmpty(g) {
				diffs = append(diffs, fmt.Sprintf("%s: want absent, got %v", name, g))
			}
		case map[string]any:
			gm, _ := g.(map[string]any)
			diffs = append(diffs, compare(name+".", wv, gm)...)
		case []any:
			if len(wv) == 0 {
				if !isEmpty(g) {
					diffs = append(diffs, fmt.Sprintf("%s: want empty, got %v", name, g))
				}
				continue
			}
			if !reflect.DeepEqual(wv, g) {
				diffs = append(diffs, fmt.Sprintf("%s: want %v, got %v", name, wv, g))
			}
		default:
			if isEmpty(w) && isEmpty(g) {
				continue
			}
			if !reflect.DeepEqual(w, g) {
				diffs = append(diffs, fmt.Sprintf("%s: want %v, got %v", name, w, g))
			}
		}
	}
	return diffs
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case []any:
		return len(x) == 0
	case string:
		return x == ""
	case bool:
		return !x
	case float64:
		return x == 0
	}
	return false
}
