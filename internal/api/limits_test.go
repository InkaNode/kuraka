package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/InkaNode/kuraka/internal/auth"
	"github.com/InkaNode/kuraka/internal/store"
)

func loginFromIP(t *testing.T, s *Server, ip, email, password string, want int) {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)))
	r.RemoteAddr = ip + ":1234"
	r.Header.Set("Origin", "http://kuraka.test")
	r.Header.Set("Content-Type", "application/json")
	// A direct public client cannot change its limiter key with this header.
	r.Header.Set("X-Forwarded-For", "192.0.2.200")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != want {
		t.Fatalf("login %s from %s: HTTP %d, want %d: %s", email, ip, w.Code, want, w.Body.String())
	}
	if want == http.StatusTooManyRequests {
		assertCode(t, w.Body.Bytes(), "rate_limited")
	}
}

func TestIPLoginSprayingLimit(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now().UTC()
	h.s.now = func() time.Time { return now }
	for i := 0; i < 30; i++ {
		want := http.StatusUnauthorized
		if i == 29 {
			want = http.StatusTooManyRequests
		}
		loginFromIP(t, h.s, "198.51.100.10", fmt.Sprintf("missing%d@example.test", i), "bad", want)
	}
	loginFromIP(t, h.s, "198.51.100.10", "root@example.test", rootPassword, http.StatusTooManyRequests)
	loginFromIP(t, h.s, "198.51.100.11", "root@example.test", rootPassword, http.StatusOK)
	now = now.Add(15*time.Minute - time.Nanosecond)
	loginFromIP(t, h.s, "198.51.100.10", "root@example.test", rootPassword, http.StatusTooManyRequests)
	now = now.Add(time.Nanosecond)
	loginFromIP(t, h.s, "198.51.100.10", "root@example.test", rootPassword, http.StatusOK)
}

func TestSuccessfulLoginClearsOnlyPair(t *testing.T) {
	h := newHarness(t, true)
	ip := "198.51.100.12"
	loginFromIP(t, h.s, ip, "root@example.test", "bad", http.StatusUnauthorized)
	loginFromIP(t, h.s, ip, "root@example.test", rootPassword, http.StatusOK)
	if _, ok := h.s.limiter.entries[ip+"\x00root@example.test"]; ok {
		t.Fatal("successful login retained the pair counter")
	}
	if got := h.s.ipLimiter.entries[ip].failures; got != 1 {
		t.Fatalf("successful login changed IP failures to %d, want 1", got)
	}
}

func TestIPLimitSharedByLoginAndTOTP(t *testing.T) {
	h := newHarness(t, true)
	now := time.Now().UTC()
	h.s.now = func() time.Time { return now }
	ip := "198.51.100.13"
	loginFromIP(t, h.s, ip, "missing@example.test", "bad", http.StatusUnauthorized)
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	enc, err := h.s.box.Seal([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	check := func(ip, email, code string) error {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/totp", nil)
		r.RemoteAddr = ip + ":1234"
		q := &requestState{user: store.User{Email: email, TOTPSecretEnc: enc}}
		return h.s.checkTOTP(r.WithContext(context.WithValue(r.Context(), stateKey{}, q)), code)
	}
	code, err := auth.TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(ip, "totp@example.test", "bad"); err != invalidCredentials {
		t.Fatalf("first TOTP failure: %v", err)
	}
	if err := check(ip, "totp@example.test", code); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.s.limiter.entries[ip+"\x00totp@example.test"]; ok {
		t.Fatal("successful TOTP retained the pair counter")
	}
	if got := h.s.ipLimiter.entries[ip].failures; got != 2 {
		t.Fatalf("successful TOTP changed IP failures to %d, want 2", got)
	}
	for i := 0; i < 28; i++ {
		want := invalidCredentials
		if i == 27 {
			want = rateLimited
		}
		if err := check(ip, fmt.Sprintf("totp%d@example.test", i), "bad"); err != want {
			t.Fatalf("TOTP failure %d: %v, want %v", i, err, want)
		}
	}
	if err := check(ip, "totp@example.test", code); err != rateLimited {
		t.Fatalf("valid TOTP from locked IP: %v", err)
	}
	loginFromIP(t, h.s, ip, "root@example.test", rootPassword, http.StatusTooManyRequests)
	if err := check("198.51.100.14", "totp@example.test", code); err != nil {
		t.Fatalf("another IP affected: %v", err)
	}
	now = now.Add(15 * time.Minute)
	code, err = auth.TOTPCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := check(ip, "totp@example.test", code); err != nil {
		t.Fatalf("TOTP lock did not expire: %v", err)
	}
}

func TestFailureLimiterBoundsAndPruning(t *testing.T) {
	for _, limit := range []int{5, 30} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			l := failureLimiter{limit: limit}
			now := time.Now().UTC()
			for i := 0; i < maxFailureKeys; i++ {
				l.fail(fmt.Sprint(i), now.Add(time.Duration(i)*time.Nanosecond))
			}
			// Updating a window does not evict another key or make it newer.
			l.fail("0", now.Add(time.Second))
			l.fail("overflow", now.Add(time.Second))
			if len(l.entries) != maxFailureKeys || l.oldest.Len() != maxFailureKeys {
				t.Fatalf("unbounded storage: map=%d list=%d", len(l.entries), l.oldest.Len())
			}
			if _, ok := l.entries["0"]; ok {
				t.Fatal("oldest window was not evicted")
			}
			if _, ok := l.entries["1"]; !ok {
				t.Fatal("newer window was evicted")
			}
			l.clear("1")
			if len(l.entries) != maxFailureKeys-1 || l.oldest.Len() != maxFailureKeys-1 {
				t.Fatal("clear retained window storage")
			}
			// fail prunes without requiring blocked to have been called first.
			l.fail("fresh", now.Add(16*time.Minute))
			if len(l.entries) != 1 || l.oldest.Len() != 1 {
				t.Fatal("expired windows were not pruned")
			}
			l.blocked("fresh", now.Add(31*time.Minute))
			if len(l.entries) != 0 || l.oldest.Len() != 0 {
				t.Fatal("blocked did not prune expired storage")
			}
		})
	}
}

func TestFailureLimiterLockOutlivesWindow(t *testing.T) {
	for _, limit := range []int{5, 30} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			l := failureLimiter{limit: limit}
			now := time.Now().UTC()
			for i := 0; i < limit-1; i++ {
				if l.fail("key", now) {
					t.Fatal("locked before reaching limit")
				}
			}
			if !l.fail("key", now.Add(14*time.Minute)) {
				t.Fatal("did not lock at limit")
			}
			if !l.blocked("key", now.Add(28*time.Minute)) {
				t.Fatal("lock expired with failure window")
			}
			if l.blocked("key", now.Add(29*time.Minute)) {
				t.Fatal("lock did not expire after fifteen minutes")
			}
			if l.fail("key", now.Add(29*time.Minute)) || l.entries["key"].failures != 1 {
				t.Fatal("expired lock did not reset failures")
			}
		})
	}
}

func TestPasswordRecheckLimited(t *testing.T) {
	h := newHarness(t, true)
	u, err := h.s.st.GetUserByEmail(context.Background(), "root@example.test")
	if err != nil {
		t.Fatal(err)
	}
	check := func(password string) error {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/password", nil)
		r.RemoteAddr = "198.51.100.20:1234"
		q := &requestState{user: u}
		return h.s.checkPassword(r.WithContext(context.WithValue(r.Context(), stateKey{}, q)), password)
	}
	for i := 0; i < 5; i++ {
		want := wrongCredentials
		if i == 4 {
			want = rateLimited
		}
		if err := check("bad"); err != want {
			t.Fatalf("failure %d: %v, want %v", i, err, want)
		}
	}
	if err := check(rootPassword); err != rateLimited {
		t.Fatalf("valid password while locked: %v", err)
	}
}
