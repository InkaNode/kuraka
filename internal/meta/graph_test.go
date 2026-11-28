package meta

import "testing"

func TestValidSignature(t *testing.T) {
	body := []byte(`{"object":"page"}`)
	good := Sign(body, "secret")
	for _, c := range []struct {
		header, secret string
		want           bool
	}{
		{good, "secret", true},
		{good, "other", false},
		{good, "", false},
		{"sha1=" + good[7:], "secret", false},
		{"sha256=zz", "secret", false},
		{"", "secret", false},
	} {
		if got := ValidSignature(body, c.header, c.secret); got != c.want {
			t.Errorf("ValidSignature(%q, %q) = %v", c.header, c.secret, got)
		}
	}
}
