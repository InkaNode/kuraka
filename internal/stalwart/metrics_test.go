package stalwart

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestParseMetrics(t *testing.T) {
	data := `
# HELP stalwart_queue_messages Queue size
# TYPE stalwart_queue_messages gauge
stalwart_queue_messages{queue="a"} 3
stalwart_queue_messages{queue="b"} 4
stalwart_auth_failed_total 2 1793664000000
spaced{label="a } b \" c"} 1.5
scientific 2e2
negative -3
missing
invalid value
nonfinite NaN
infinite +Inf
overflow 1e999
broken{label="oops} 3
unclosed{label="oops" 3
empty{}
joined{}3
bad-name 3
badTimestamp 2 nope
extraTokens 2 123 trailing
`
	want := map[string]float64{"stalwart_queue_messages": 7, "stalwart_auth_failed_total": 2, "spaced": 1.5, "scientific": 200, "negative": -3}
	if got := parseMetrics(data); !reflect.DeepEqual(got, want) {
		t.Fatalf("metrics = %v, want %v", got, want)
	}
	if got := parseMetrics("\t# comment\r\n\n"); len(got) != 0 {
		t.Fatalf("empty metrics = %v", got)
	}
}

func TestMetrics(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/metrics/prometheus" {
					t.Errorf("request = %s %s", r.Method, r.URL.Path)
				}
				user, secret, ok := r.BasicAuth()
				if !ok || user != MetricsUser || secret != "test-secret" {
					t.Error("missing metrics credentials")
				}
				w.WriteHeader(status)
				fmt.Fprint(w, "stalwart_queue_messages 5\n")
			}))
			defer srv.Close()
			got, err := New(srv.URL, testKey).Metrics(context.Background(), MetricsUser, "test-secret")
			if status == http.StatusOK {
				if err != nil || got["stalwart_queue_messages"] != 5 {
					t.Fatalf("Metrics = %v, %v", got, err)
				}
			} else if err == nil {
				t.Fatal("HTTP error ignored")
			}
		})
	}
}
