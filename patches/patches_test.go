package patches

import (
	"io/fs"
	"path"
	"regexp"
	"testing"
)

func TestSeries(t *testing.T) {
	apps, err := fs.Glob(files, "*/series")
	if err != nil || len(apps) == 0 {
		t.Fatalf("no series: %v", err)
	}
	for _, s := range apps {
		app := path.Dir(s)
		if ref := Upstream(app); !regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`).MatchString(ref) {
			t.Errorf("%s: UPSTREAM REF = %q", app, ref)
		}
		series, err := Series(app)
		if err != nil {
			t.Fatal(err)
		}
		listed := map[string]bool{}
		for _, p := range series {
			if listed[p.Name] {
				t.Errorf("%s: %s is listed twice", app, p.Name)
			}
			listed[p.Name] = true
			if p.Title == "" {
				t.Errorf("%s: %s has no title", app, p.Name)
			}
		}
		all, _ := fs.Glob(files, app+"/*.patch")
		for _, f := range all {
			if !listed[path.Base(f)] {
				t.Errorf("%s is not in %s/series", f, app)
			}
		}
	}
}

func TestPatchTitle(t *testing.T) {
	for in, want := range map[string]string{
		"[PATCH] feat(tags): optional tag colors": "Tags: optional tag colors",
		"[PATCH] fix: keep drafts":                "keep drafts",
		"[PATCH] plain subject":                   "plain subject",
	} {
		if got := patchTitle(in); got != want {
			t.Errorf("patchTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
