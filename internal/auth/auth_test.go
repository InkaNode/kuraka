package auth

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	ok, err := VerifyPassword("correct horse battery", h)
	if err != nil || !ok {
		t.Fatalf("verify correct: ok=%v err=%v", ok, err)
	}
	ok, err = VerifyPassword("wrong horse battery", h)
	if err != nil || ok {
		t.Fatalf("verify wrong: ok=%v err=%v", ok, err)
	}
	h2, _ := HashPassword("correct horse battery")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	salt := base64.RawStdEncoding.EncodeToString(make([]byte, 16))
	key := base64.RawStdEncoding.EncodeToString(make([]byte, 32))
	tests := []string{
		"",
		"plain",
		"$argon2i$v=19$m=65536,t=3,p=2$" + salt + "$" + key,
		"$argon2id$v=16$m=65536,t=3,p=2$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=0,p=2$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=0$" + salt + "$" + key,
		"$argon2id$v=19$m=99999999,t=3,p=2$" + salt + "$" + key,
		"$argon2id$v=19$m=65536,t=3,p=2$!!!$" + key,
		"$argon2id$v=19$m=65536,t=3,p=2$" + salt + "$",
		"$argon2id$v=19$m=65536,t=3,p=2$" + salt + "$" + key + "$extra",
	}
	for _, enc := range tests {
		if ok, err := VerifyPassword("whatever-password", enc); err == nil || ok {
			t.Errorf("%q: want error, got ok=%v err=%v", enc, ok, err)
		}
	}
}

func TestVerifyPasswordReadsParams(t *testing.T) {
	// Lower-cost params encoded in the string must be honoured.
	salt := []byte("0123456789abcdef")
	enc := "$argon2id$v=19$m=1024,t=1,p=1$" + base64.RawStdEncoding.EncodeToString(salt) + "$"
	p, err := parseHash(enc + base64.RawStdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	if p.memory != 1024 || p.time != 1 || p.threads != 1 {
		t.Fatalf("params not parsed: %+v", p)
	}
}

func TestDummyHashAndBurn(t *testing.T) {
	if _, err := parseHash(DummyHash); err != nil {
		t.Fatalf("DummyHash malformed: %v", err)
	}
	Burn("anything at all")
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		pw   string
		want error
	}{
		{"", ErrWeakPassword},
		{strings.Repeat("a", 11), ErrWeakPassword},
		{strings.Repeat("a", 12), nil},
		{strings.Repeat("ñ", 12), nil}, // runes, not bytes
		{strings.Repeat("a", 256), nil},
		{strings.Repeat("a", 257), ErrWeakPassword},
	}
	for _, tt := range tests {
		if err := ValidatePassword(tt.pw); !errors.Is(err, tt.want) {
			t.Errorf("len %d: got %v want %v", len(tt.pw), err, tt.want)
		}
	}
}

func TestRFC6238Vectors(t *testing.T) {
	key := []byte("12345678901234567890")
	tests := []struct {
		unix int64
		want string
	}{
		{59, "94287082"},
		{1111111109, "07081804"},
		{1111111111, "14050471"},
		{1234567890, "89005924"},
		{2000000000, "69279037"},
		{20000000000, "65353130"},
	}
	for _, tt := range tests {
		if got := totpAt(key, time.Unix(tt.unix, 0), 8); got != tt.want {
			t.Errorf("t=%d: got %s want %s", tt.unix, got, tt.want)
		}
	}
	// The public 6-digit API must agree with the low digits of the vector.
	secret := b32.EncodeToString(key)
	got, err := TOTPCode(secret, time.Unix(59, 0))
	if err != nil || got != "287082" {
		t.Errorf("TOTPCode: got %q err=%v", got, err)
	}
}

func TestVerifyTOTPWindow(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(secret) != 32 || strings.Contains(secret, "=") {
		t.Fatalf("unexpected secret %q", secret)
	}
	now := time.Unix(1_700_000_010, 0)
	code := func(at time.Time) string {
		c, err := TOTPCode(secret, at)
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"current", code(now), true},
		{"previous step", code(now.Add(-30 * time.Second)), true},
		{"next step", code(now.Add(30 * time.Second)), true},
		{"two steps back", code(now.Add(-60 * time.Second)), false},
		{"two steps ahead", code(now.Add(60 * time.Second)), false},
		{"empty", "", false},
		{"short", code(now)[:5], false},
		{"long", code(now) + "0", false},
		{"non-digit", "12a456", false},
	}
	for _, tt := range tests {
		if got := VerifyTOTP(secret, tt.code, now); got != tt.want {
			t.Errorf("%s (%q): got %v want %v", tt.name, tt.code, got, tt.want)
		}
	}
	if VerifyTOTP("not base32!", code(now), now) {
		t.Error("invalid secret accepted")
	}
}

func TestOTPAuthURL(t *testing.T) {
	got := OTPAuthURL("Inka Node", "ana@example.com", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/Inka%20Node:ana@example.com?secret=JBSWY3DPEHPK3PXP&issuer=Inka%20Node&algorithm=SHA1&digits=6&period=30"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if u := OTPAuthURL("a:b", "c", "S"); !strings.HasPrefix(u, "otpauth://totp/a%3Ab:c?") {
		t.Errorf("colon in issuer not escaped: %s", u)
	}
}

func TestToken(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if a == b || len(a) != 43 {
		t.Fatalf("bad tokens %q %q", a, b)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(HashToken(a)) {
		t.Fatalf("bad hash %q", HashToken(a))
	}
	if HashToken("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("HashToken is not sha256 hex")
	}
}

func TestBox(t *testing.T) {
	key := hex.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	box, err := NewBox(key)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal([]byte("totp-secret"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := box.Open(sealed)
	if err != nil || string(plain) != "totp-secret" {
		t.Fatalf("round trip: %q %v", plain, err)
	}
	again, _ := box.Seal([]byte("totp-secret"))
	if again == sealed {
		t.Fatal("nonce reuse")
	}

	raw, _ := base64.RawStdEncoding.DecodeString(sealed)
	for i := range raw {
		tampered := append([]byte(nil), raw...)
		tampered[i] ^= 0x01
		if _, err := box.Open(base64.RawStdEncoding.EncodeToString(tampered)); err == nil {
			t.Fatalf("tamper at byte %d not detected", i)
		}
	}
	for _, bad := range []string{"", "!!", base64.RawStdEncoding.EncodeToString(raw[:20])} {
		if _, err := box.Open(bad); err == nil {
			t.Errorf("Open(%q) succeeded", bad)
		}
	}

	other, _ := NewBox(strings.Repeat("ab", 32))
	if _, err := other.Open(sealed); err == nil {
		t.Fatal("opened with wrong key")
	}
}

func TestNewBoxKeyErrors(t *testing.T) {
	secretish := strings.Repeat("zz", 32)
	for _, k := range []string{"", strings.Repeat("ab", 31), strings.Repeat("ab", 33), secretish} {
		_, err := NewBox(k)
		if err == nil {
			t.Errorf("key len %d accepted", len(k))
			continue
		}
		if k != "" && strings.Contains(err.Error(), k) {
			t.Errorf("error leaks key: %v", err)
		}
	}
}
