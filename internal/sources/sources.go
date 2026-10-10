// Package sources turns the glucose source chosen in Settings into a live
// glucose.Source. It is the one place that reads the stored settings and
// secrets of a source; the sources themselves (internal/glucose) only describe
// their fields and build a client from values.
package sources

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
)

// DefaultID is the source used when neither Settings nor the environment name one.
const DefaultID = "dexcom"

// Manager is a glucose.Source that follows the Settings choice: it reads the
// active source and its values on every call and rebuilds the client only when
// something changed, so edits apply without a restart.
type Manager struct {
	Store *store.PB
	Vault *secrets.Vault
	// Fallback is the source to use while Settings names none (GLUCAVA_SOURCE).
	Fallback string

	mu  sync.Mutex
	cur glucose.Source
	key string
}

var (
	_ glucose.Source = (*Manager)(nil)
	_ glucose.Named  = (*Manager)(nil)
)

// ActiveID returns the id of the source in use.
func (m *Manager) ActiveID() string {
	cfg, err := m.Store.LoadConfig()
	if err == nil && cfg.GlucoseSource != "" {
		return cfg.GlucoseSource
	}
	if m.Fallback != "" {
		return m.Fallback
	}
	return DefaultID
}

// Name implements glucose.Named: readings are stored under the source's id.
func (m *Manager) Name() string { return m.ActiveID() }

// Samples implements glucose.Source.
func (m *Manager) Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	src, err := m.build()
	if err != nil {
		return nil, err
	}
	return src.Samples(ctx, from, to)
}

// mapValues is the glucose.Values of one source.
type mapValues map[string]string

func (v mapValues) Get(key string) string { return v[key] }

func (m *Manager) build() (glucose.Source, error) {
	cfg, err := m.Store.LoadConfig()
	if err != nil {
		return nil, err
	}
	id := cfg.GlucoseSource
	if id == "" {
		id = m.Fallback
	}
	if id == "" {
		id = DefaultID
	}
	d, ok := glucose.Lookup(id)
	if !ok {
		return nil, fmt.Errorf("glucose source %q is not known (known: %v)", id, glucose.IDs())
	}

	vals := make(map[string]string, len(d.Fields))
	for _, f := range d.Fields {
		if !f.Secret() {
			vals[f.Key] = cfg.SourceValue(id, f)
			continue
		}
		v, _, err := m.Vault.Get(glucose.SecretName(id, f))
		if err != nil {
			return nil, err
		}
		vals[f.Key] = v
	}
	get := func(f glucose.Field) string { return vals[f.Key] }
	if f, missing := glucose.MissingRequired(d, get); missing {
		return nil, fmt.Errorf("%w: %s: %q is not set", glucose.ErrAuth, id, f.Key)
	}

	key := id
	for _, f := range d.Fields {
		key += "\x00" + vals[f.Key]
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cur == nil || m.key != key {
		src, err := d.Build(mapValues(vals))
		if err != nil {
			return nil, err
		}
		m.cur, m.key = src, key
	}
	return m.cur, nil
}
