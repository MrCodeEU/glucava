package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	"github.com/pocketbase/pocketbase/core"

	"github.com/MrCodeEU/glucava/internal/glucose"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/sources"
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

// newSource returns the live glucose source, which follows the choice in
// Settings. name is GLUCAVA_SOURCE: it applies while Settings names none, and an
// unknown one stops the server at start.
func newSource(name string, st *store.PB, vault *secrets.Vault) (*sources.Manager, error) {
	if name != "" {
		if _, ok := glucose.Lookup(name); !ok {
			return nil, fmt.Errorf("GLUCAVA_SOURCE %q is not a known glucose source (known: %s)", name, strings.Join(glucose.IDs(), ", "))
		}
	}
	return &sources.Manager{Store: st, Vault: vault, Fallback: name}, nil
}
