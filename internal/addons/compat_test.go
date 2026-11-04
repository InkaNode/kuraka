package addons

import "testing"

func TestAllows(t *testing.T) {
	for _, tc := range []struct {
		component, tag string
		want           bool
	}{
		{"stalwart", "v1.0.0", true},
		{"stalwart", "v1.0.999", true},
		{"stalwart", "v0.99.99", true},
		{"stalwart", "v1.1.0", false},
		{"stalwart", "v2.0.0", false},
		{"webui", "v2.0.4-rc.1", true},
		{"webui", "2.0.4", true},
		{"webui", "v2.1.0-pre", false},
		{"libredesk", "v2.9.99", true},
		{"libredesk", "v2.10.0", false},
		{"libredesk", "v10.0.0", false},
		{"libredesk", "v1.10.0", true},
		{"unknown", "unparseable", true},
		{"unknown", "", true},
		{"stalwart", "", false},
		{"stalwart", "latest", false},
		{"stalwart", "v1.0", false},
		{"stalwart", "v1.0.0-", false},
		{"stalwart", "v1.0.0-rc..1", false},
		{"stalwart", "v1.0.0+build", false},
		{"stalwart", "v01.0.0", false},
		{"stalwart", "v1.0.0\n", false},
		{"stalwart", "v999999999999999999999.0.0", false},
		{"stalwart", "v1.999999999999999999999.0", false},
		{"stalwart", "v1.0.999999999999999999999", false},
	} {
		t.Run(tc.component+"/"+tc.tag, func(t *testing.T) {
			if got := Allows(tc.component, tc.tag); got != tc.want {
				t.Errorf("Allows(%q, %q) = %v, want %v", tc.component, tc.tag, got, tc.want)
			}
		})
	}
}
