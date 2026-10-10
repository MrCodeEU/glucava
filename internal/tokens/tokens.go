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
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"
)

const prefix = "gst_"

// Manager reads and writes the api_tokens collection.
type Manager struct {
	App core.App
}

// Scope is one thing a token may be used for.
type Scope string

// A token carries one or more scopes; an endpoint accepts it only with the scope it needs.
const (
	ScopeTrigger Scope = "trigger" // POST /api/trigger and the Tasker profile
	ScopeMetrics Scope = "metrics" // GET /metrics
	ScopeBackup  Scope = "backup"  // GET /export/backup.zip
)

// AllScopes lists every scope, in the order the UI shows them.
var AllScopes = []Scope{ScopeTrigger, ScopeMetrics, ScopeBackup}

// DefaultScopes is what a token gets when none are asked for: what every token
// could do before scopes existed. Backups are never part of it.
var DefaultScopes = []Scope{ScopeTrigger, ScopeMetrics}

// ParseScopes turns "trigger,backup" into scopes, rejecting unknown names. An
// empty list gives DefaultScopes.
func ParseScopes(list string) ([]Scope, error) {
	var out []Scope
	for _, name := range strings.Split(list, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		sc := Scope(name)
		if !slices.Contains(AllScopes, sc) {
			return nil, fmt.Errorf("tokens: unknown scope %q (known: trigger, metrics, backup)", name)
		}
		if !slices.Contains(out, sc) {
			out = append(out, sc)
		}
	}
	if len(out) == 0 {
		return slices.Clone(DefaultScopes), nil
	}
	return out, nil
}

func joinScopes(scopes []Scope) string {
	parts := make([]string, len(scopes))
	for i, sc := range scopes {
		parts[i] = string(sc)
	}
	return strings.Join(parts, ",")
}

// Info describes a stored token without its secret.
type Info struct {
	Name     string
	Revoked  bool
	LastUsed time.Time // zero if never used
	Scopes   []Scope
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Create makes a new token named name and returns the plain value. Names are
// unique. Without scopes the token gets DefaultScopes.
func (m *Manager) Create(name string, scopes ...Scope) (string, error) {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
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
	rec.Set("scopes", joinScopes(scopes))
	rec.Set("revoked", false)
	if err := m.App.Save(rec); err != nil {
		return "", err
	}
	return token, nil
}

// Verify reports whether token may fire the push trigger. See VerifyScope.
func (m *Manager) Verify(token string) (bool, error) { return m.VerifyScope(token, ScopeTrigger) }

// VerifyScope reports whether token is valid, not revoked and carries scope,
// and records its use. A token without the scope is simply not valid for it.
func (m *Manager) VerifyScope(token string, scope Scope) (bool, error) {
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
	if rec.GetBool("revoked") || !slices.Contains(scopesOf(rec), scope) {
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
		out[i] = Info{Name: r.GetString("name"), Revoked: r.GetBool("revoked"), LastUsed: r.GetDateTime("last_used").Time(), Scopes: scopesOf(r)}
	}
	return out, nil
}

// scopesOf reads a record's scopes. A stored value that fails to parse grants
// nothing rather than the defaults.
func scopesOf(r *core.Record) []Scope {
	var out []Scope
	for _, name := range strings.Split(r.GetString("scopes"), ",") {
		if sc := Scope(strings.TrimSpace(name)); slices.Contains(AllScopes, sc) {
			out = append(out, sc)
		}
	}
	return out
}
