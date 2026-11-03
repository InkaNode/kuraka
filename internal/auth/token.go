package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

const tokenSize = 32

// NewToken returns 32 random bytes encoded with base64.RawURLEncoding.
func NewToken() (string, error) {
	b := make([]byte, tokenSize)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the lowercase hex SHA-256 of tok, for storage.
func HashToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}
