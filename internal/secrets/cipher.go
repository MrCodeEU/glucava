// Package secrets encrypts credentials before they are stored in the database.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var randRead = rand.Read

func encodeKey(key []byte) string       { return base64.StdEncoding.EncodeToString(key) }
func keyFilePath(dataDir string) string { return filepath.Join(dataDir, keyFile) }
func keyFromEnv() bool                  { return os.Getenv(keyEnv) != "" }

const (
	keyEnv   = "GLUCAVA_SECRET_KEY"
	keyFile  = "secret.key"
	keyBytes = 32
)

// Cipher seals and opens values with AES-256-GCM. The value name is bound to the
// ciphertext as additional data, so a value copied under another name fails to open.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher returns a Cipher for a 32 byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != keyBytes {
		return nil, fmt.Errorf("secrets: key must be %d bytes, got %d", keyBytes, len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Seal encrypts plaintext and returns base64(nonce || ciphertext).
func (c *Cipher) Seal(name, plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := c.aead.Seal(nonce, nonce, []byte(plaintext), []byte(name))
	return base64.StdEncoding.EncodeToString(out), nil
}

// Open decrypts a value produced by Seal with the same name.
func (c *Cipher) Open(name, sealed string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(sealed)
	if err != nil {
		return "", fmt.Errorf("secrets: decode: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secrets: ciphertext too short")
	}
	pt, err := c.aead.Open(nil, raw[:ns], raw[ns:], []byte(name))
	if err != nil {
		return "", errors.New("secrets: cannot decrypt (wrong key or corrupted value)")
	}
	return string(pt), nil
}

// LoadKey returns the encryption key. It uses GLUCAVA_SECRET_KEY (base64 of
// 32 bytes) when set. Otherwise it reads <dataDir>/secret.key, creating it with
// mode 0600 on first run. Losing the key makes stored secrets unreadable.
func LoadKey(dataDir string) ([]byte, error) {
	if v := os.Getenv(keyEnv); v != "" {
		key, err := base64.StdEncoding.DecodeString(v)
		if err != nil || len(key) != keyBytes {
			return nil, fmt.Errorf("secrets: %s must be base64 of %d bytes", keyEnv, keyBytes)
		}
		return key, nil
	}

	path := filepath.Join(dataDir, keyFile)
	if b, err := os.ReadFile(path); err == nil {
		key, derr := base64.StdEncoding.DecodeString(string(b))
		if derr != nil || len(key) != keyBytes {
			return nil, fmt.Errorf("secrets: %s is not base64 of %d bytes", path, keyBytes)
		}
		return key, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	key := make([]byte, keyBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	enc := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
		return nil, err
	}
	return key, nil
}
