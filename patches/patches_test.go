package patches

import (
	"io/fs"
	"path"
	"testing"
)

func TestSeries(t *testing.T) {
	apps, err := fs.Glob(files, "*/series")
	if err != nil || len(apps) == 0 {
		t.Fatalf("no series: %v", err)
	}
	for _, s := range apps {
		app := path.Dir(s)
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
		}
		all, _ := fs.Glob(files, app+"/*.patch")
		for _, f := range all {
			if !listed[path.Base(f)] {
				t.Errorf("%s is not in %s/series", f, app)
			}
		}
	}
}
