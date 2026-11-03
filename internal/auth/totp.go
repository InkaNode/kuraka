package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP parameters (RFC 6238 defaults, as expected by authenticator apps).
const (
	totpDigits     = 6
	totpPeriod     = 30
	totpSecretSize = 20
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// ErrInvalidSecret is returned for a TOTP secret that is not valid base32.
var ErrInvalidSecret = errors.New("invalid TOTP secret")

// GenerateTOTPSecret returns a new random 160-bit secret, base32 without padding.
func GenerateTOTPSecret() (string, error) {
	b := make([]byte, totpSecretSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return b32.EncodeToString(b), nil
}

// TOTPCode returns the 6-digit code for secret at time t.
func TOTPCode(secret string, t time.Time) (string, error) {
	key, err := decodeSecret(secret)
	if err != nil {
		return "", err
	}
	if t.Unix() < 0 {
		return "", errors.New("time before Unix epoch")
	}
	return totpAt(key, t, totpDigits), nil
}

// VerifyTOTP reports whether code is valid for secret at t, allowing one step
// of clock drift either way. Input that is not exactly 6 digits is rejected.
func VerifyTOTP(secret, code string, t time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	key, err := decodeSecret(secret)
	if err != nil {
		return false
	}
	counter := t.Unix() / totpPeriod
	ok := 0
	for d := int64(-1); d <= 1; d++ {
		c := counter + d
		if c < 0 {
			continue
		}
		ok |= subtle.ConstantTimeCompare([]byte(hotp(key, uint64(c), totpDigits)), []byte(code))
	}
	return ok == 1
}

// OTPAuthURL returns the otpauth:// provisioning URI for authenticator apps.
func OTPAuthURL(issuer, account, secret string) string {
	label := escapeLabel(issuer) + ":" + escapeLabel(account)
	return "otpauth://totp/" + label +
		"?secret=" + escapeQuery(secret) +
		"&issuer=" + escapeQuery(issuer) +
		"&algorithm=SHA1" +
		"&digits=" + fmt.Sprint(totpDigits) +
		"&period=" + fmt.Sprint(totpPeriod)
}

// totpAt computes an RFC 6238 SHA1 code with a 30 s step and the given digits.
func totpAt(key []byte, t time.Time, digits int) string {
	return hotp(key, uint64(t.Unix()/totpPeriod), digits)
}

// hotp implements RFC 4226 with HMAC-SHA1.
func hotp(key []byte, counter uint64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, bin%mod)
}

func decodeSecret(secret string) ([]byte, error) {
	s := strings.TrimRight(strings.ToUpper(strings.ReplaceAll(secret, " ", "")), "=")
	key, err := b32.DecodeString(s)
	if err != nil || len(key) == 0 {
		return nil, ErrInvalidSecret
	}
	return key, nil
}

// escapeLabel escapes one half of the issuer:account label, including ':'.
func escapeLabel(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), ":", "%3A")
}

// escapeQuery escapes a query value using %20 for spaces, which
// authenticator apps handle more consistently than '+'.
func escapeQuery(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
