package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
)

const boxKeySize = 32

// ErrBoxOpen is returned when a sealed value is malformed or fails
// authentication.
var ErrBoxOpen = errors.New("secretbox: cannot open sealed value")

// Box encrypts small secrets with AES-256-GCM.
type Box struct {
	aead cipher.AEAD
}

// NewBox builds a Box from a hex-encoded 32-byte key. Errors never include
// the key material.
func NewBox(keyHex string) (*Box, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != boxKeySize {
		return nil, fmt.Errorf("secretbox: key must be %d hex-encoded bytes", boxKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secretbox: %w", err)
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain and returns base64(nonce || ciphertext).
func (b *Box) Seal(plain []byte) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("secretbox: generate nonce: %w", err)
	}
	out := b.aead.Seal(nonce, nonce, plain, nil)
	return base64.RawStdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal.
func (b *Box) Open(s string) ([]byte, error) {
	raw, err := base64.RawStdEncoding.DecodeString(s)
	ns := b.aead.NonceSize()
	if err != nil || len(raw) < ns+b.aead.Overhead() {
		return nil, ErrBoxOpen
	}
	plain, err := b.aead.Open(nil, raw[:ns], raw[ns:], nil)
	if err != nil {
		return nil, ErrBoxOpen
	}
	return plain, nil
}
