package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/stats"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/strava"
)

// openVault loads the encryption key and returns the vault.
func openVault(app core.App) (*secrets.Vault, error) {
	key, err := secrets.LoadKey(app.DataDir())
	if err != nil {
		return nil, err
	}
	c, err := secrets.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return &secrets.Vault{App: app, Cipher: c}, nil
}

// newStravaWriter builds the browser writer. Cookies live in the vault, and the
// browser's refreshed cookies are written back after each successful run.
func newStravaWriter(vault *secrets.Vault, tun tuning) *strava.Writer {
	return strava.NewWriter(strava.Config{
		Selectors: tun.Selectors,
		UserAgent: tun.UserAgent,
		BaseURL:   os.Getenv("GLUCAVA_STRAVA_URL"), // empty means https://www.strava.com; set for tests
		NoSandbox: os.Geteuid() == 0 || os.Getenv("GLUCAVA_NO_SANDBOX") == "1",
		LoadCookies: func() ([]strava.Cookie, error) {
			raw, ok, err := vault.Get(secrets.NameStravaCookies)
			if err != nil || !ok {
				return nil, err
			}
			return strava.DecodeCookies(raw)
		},
		SaveCookies: func(c []strava.Cookie) error {
			enc, err := strava.EncodeCookies(c)
			if err != nil {
				return err
			}
			return vault.Set(secrets.NameStravaCookies, enc)
		},
		Pause: func() { time.Sleep(time.Duration(400+rand.IntN(800)) * time.Millisecond) },
	})
}

// defaultSource is the glucose source used when GLUCAVA_SOURCE is unset.
const defaultSource = "dexcom"

// liveSource is a glucose source and the key its readings are stored under.
type liveSource struct {
	name   string
	source glucose.Source
}

// sourceBuilders maps GLUCAVA_SOURCE values to constructors. A new live source
// (Nightscout, LibreLinkUp, ...) is one entry here plus its glucose.Source.
var sourceBuilders = map[string]func(st *store.PB, vault *secrets.Vault) glucose.Source{
	"dexcom": func(st *store.PB, vault *secrets.Vault) glucose.Source { return &dexcomSource{st: st, vault: vault} },
}

// newSource returns the live glucose source named name (default "dexcom").
func newSource(name string, st *store.PB, vault *secrets.Vault) (liveSource, error) {
	if name == "" {
		name = defaultSource
	}
	build, ok := sourceBuilders[name]
	if !ok {
		known := make([]string, 0, len(sourceBuilders))
		for k := range sourceBuilders {
			known = append(known, k)
		}
		sort.Strings(known)
		return liveSource{}, fmt.Errorf("GLUCAVA_SOURCE %q is not a known glucose source (known: %s)", name, strings.Join(known, ", "))
	}
	return liveSource{name: name, source: build(st, vault)}, nil
}

// dexcomSource reads region, username and password on each call and rebuilds the
// Share client when they change, so settings edits apply without a restart.
type dexcomSource struct {
	st    *store.PB
	vault *secrets.Vault

	mu  sync.Mutex
	cur *glucose.DexcomShare
	key [32]byte
}

func (d *dexcomSource) Samples(ctx context.Context, from, to time.Time) ([]stats.Sample, error) {
	c, err := d.client()
	if err != nil {
		return nil, err
	}
	return c.Samples(ctx, from, to)
}

func (d *dexcomSource) client() (*glucose.DexcomShare, error) {
	region, user, err := d.st.DexcomSettings()
	if err != nil {
		return nil, err
	}
	pass, _, err := d.vault.Get(secrets.NameDexcomPassword)
	if err != nil {
		return nil, err
	}
	if user == "" || pass == "" {
		return nil, fmt.Errorf("%w: Dexcom username and password are not set", glucose.ErrAuth)
	}
	base := map[string]string{"us": glucose.RegionUS, "jp": glucose.RegionJP}[region]
	if base == "" {
		base = glucose.RegionOUS
	}
	if u := os.Getenv("GLUCAVA_DEXCOM_URL"); u != "" { // set for tests
		base = u
	}

	key := sha256.Sum256([]byte(base + "\x00" + user + "\x00" + pass))
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur == nil || d.key != key {
		d.cur, d.key = glucose.NewDexcomShare(base, user, pass), key
	}
	return d.cur, nil
}
