// Package secret encrypts sensitive values (address book passwords) at rest.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
)

const prefix = "v1:"

// Box encrypts with AES-256-GCM. The key is derived from the server's session
// secret, so keep DATA_DIR/session.secret (or SESSION_SECRET) with your backups.
type Box struct{ aead cipher.AEAD }

func New(masterSecret string) *Box {
	key := sha256.Sum256([]byte("rustdesk-manager/secret-box/v1\x00" + masterSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}
	return &Box{aead: aead}
}

// Seal encrypts s. Empty stays empty so "no password" is still visible.
func (b *Box) Seal(s string) string {
	if s == "" {
		return ""
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	out := b.aead.Seal(nonce, nonce, []byte(s), nil)
	return prefix + base64.RawStdEncoding.EncodeToString(out)
}

var ErrDecrypt = errors.New("cannot decrypt value (was the session secret changed?)")

// Open decrypts a value from Seal.
func (b *Box) Open(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	enc, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return "", ErrDecrypt
	}
	raw, err := base64.RawStdEncoding.DecodeString(enc)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrDecrypt
	}
	plain, err := b.aead.Open(nil, raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():], nil)
	if err != nil {
		return "", ErrDecrypt
	}
	return string(plain), nil
}
