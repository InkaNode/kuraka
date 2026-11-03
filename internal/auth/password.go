// Package auth provides password hashing, TOTP, random tokens and an
// authenticated secret box.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for new hashes. VerifyPassword reads the parameters
// from the encoded hash, so these can be raised without breaking old hashes.
const (
	argonMemory  = 64 * 1024 // KiB
	argonTime    = 3
	argonThreads = 2
	argonSaltLen = 16
	argonKeyLen  = 32
)

// Bounds accepted when parsing an encoded hash, to keep a corrupt or hostile
// row from requesting absurd resources.
const (
	maxArgonMemory = 1024 * 1024 // KiB (1 GiB)
	maxArgonTime   = 64
	minSaltLen     = 8
	minKeyLen      = 16
	maxKeyLen      = 128
)

// Password length limits, in runes.
const (
	MinPasswordLen = 12
	MaxPasswordLen = 256
)

// ErrWeakPassword is returned by ValidatePassword when a password is outside
// the allowed length range.
var ErrWeakPassword = fmt.Errorf("password must be %d to %d characters", MinPasswordLen, MaxPasswordLen)

// ErrMalformedHash is returned by VerifyPassword for an unparseable hash.
var ErrMalformedHash = errors.New("malformed password hash")

// DummyHash is a valid argon2id hash of a random, discarded password. Burn
// verifies against it so that "user not found" costs the same as a real check.
var DummyHash = "$argon2id$v=19$m=65536,t=3,p=2$HHIT/N5k40qo1R5OALIEeQ$zn3G22Yb4eU2INHLmNzsFL1DCqkOsgGro77qOfwMCRg"

// ValidatePassword reports whether pw satisfies the length policy.
func ValidatePassword(pw string) error {
	n := utf8.RuneCountInString(pw)
	if n < MinPasswordLen || n > MaxPasswordLen {
		return ErrWeakPassword
	}
	return nil
}

// HashPassword returns an argon2id PHC string for pw.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword reports whether pw matches the encoded argon2id hash.
// A malformed hash returns an error.
func VerifyPassword(pw, encoded string) (bool, error) {
	p, err := parseHash(encoded)
	if err != nil {
		return false, err
	}
	key := argon2.IDKey([]byte(pw), p.salt, p.time, p.memory, p.threads, uint32(len(p.key)))
	return subtle.ConstantTimeCompare(key, p.key) == 1, nil
}

// Burn spends the same work as a real password check. Use it when the user
// does not exist so response timing does not reveal that.
func Burn(pw string) {
	_, _ = VerifyPassword(pw, DummyHash)
}

type argonParams struct {
	memory  uint32
	time    uint32
	threads uint8
	salt    []byte
	key     []byte
}

func parseHash(encoded string) (argonParams, error) {
	var p argonParams
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return p, ErrMalformedHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return p, ErrMalformedHash
	}
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &p.memory, &p.time, &threads); err != nil {
		return p, ErrMalformedHash
	}
	if fmt.Sprintf("m=%d,t=%d,p=%d", p.memory, p.time, threads) != parts[3] {
		return p, ErrMalformedHash
	}
	if threads < 1 || threads > 255 || p.time < 1 || p.time > maxArgonTime ||
		p.memory < 8*threads || p.memory > maxArgonMemory {
		return p, ErrMalformedHash
	}
	p.threads = uint8(threads)
	var err error
	if p.salt, err = base64.RawStdEncoding.Strict().DecodeString(parts[4]); err != nil || len(p.salt) < minSaltLen {
		return p, ErrMalformedHash
	}
	if p.key, err = base64.RawStdEncoding.Strict().DecodeString(parts[5]); err != nil ||
		len(p.key) < minKeyLen || len(p.key) > maxKeyLen {
		return p, ErrMalformedHash
	}
	return p, nil
}
