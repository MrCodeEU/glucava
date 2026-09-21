// Package tokens manages the bearer tokens that authorise the trigger endpoint.
// Only a SHA-256 hash is stored, so a token is shown once when created.
package tokens

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const prefix = "gst_"

// Manager reads and writes the api_tokens collection.
type Manager struct {
	App core.App
}

// Info describes a stored token without its secret.
type Info struct {
	Name     string
	Revoked  bool
	LastUsed time.Time // zero if never used
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create makes a new token named name and returns the plain value. Names are unique.
func (m *Manager) Create(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("tokens: name is required")
	}
	if _, err := m.App.FindFirstRecordByData("api_tokens", "name", name); err == nil {
		return "", errors.New("tokens: a token with this name already exists")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := prefix + base64.RawURLEncoding.EncodeToString(raw)

	col, err := m.App.FindCollectionByNameOrId("api_tokens")
	if err != nil {
		return "", err
	}
	rec := core.NewRecord(col)
	rec.Set("name", name)
	rec.Set("token_hash", hash(token))
	rec.Set("revoked", false)
	if err := m.App.Save(rec); err != nil {
		return "", err
	}
	return token, nil
}

// Verify reports whether token is valid and not revoked, and records its use.
func (m *Manager) Verify(token string) (bool, error) {
	if !strings.HasPrefix(token, prefix) {
		return false, nil
	}
	rec, err := m.App.FindFirstRecordByData("api_tokens", "token_hash", hash(token))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if rec.GetBool("revoked") {
		return false, nil
	}
	rec.Set("last_used", time.Now().UTC())
	if err := m.App.Save(rec); err != nil {
		return false, err
	}
	return true, nil
}

// Revoke disables the token with the given name.
func (m *Manager) Revoke(name string) error {
	rec, err := m.App.FindFirstRecordByData("api_tokens", "name", name)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("tokens: no token with this name")
	}
	if err != nil {
		return err
	}
	rec.Set("revoked", true)
	return m.App.Save(rec)
}

// List returns all tokens, oldest first.
func (m *Manager) List() ([]Info, error) {
	recs, err := m.App.FindRecordsByFilter("api_tokens", "", "created", 0, 0)
	if err != nil {
		return nil, err
	}
	out := make([]Info, len(recs))
	for i, r := range recs {
		out[i] = Info{Name: r.GetString("name"), Revoked: r.GetBool("revoked"), LastUsed: r.GetDateTime("last_used").Time()}
	}
	return out, nil
}
