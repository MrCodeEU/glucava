package sources

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	_ "github.com/pocketbase/pocketbase/migrations" // registers the system migrations

	"github.com/MrCodeEU/glucava/internal/glucose"
	_ "github.com/MrCodeEU/glucava/internal/migrations"
	"github.com/MrCodeEU/glucava/internal/secrets"
	"github.com/MrCodeEU/glucava/internal/store"
	"github.com/MrCodeEU/glucava/internal/testutil"
)

var migrated = testutil.NewTemplate(nil)

func newManager(t *testing.T) (*Manager, *store.PB, *secrets.Vault) {
	t.Helper()
	app := migrated.App(t)
	cipher, err := secrets.NewCipher([]byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}
	st := &store.PB{App: app}
	v := &secrets.Vault{App: app, Cipher: cipher}
	return &Manager{Store: st, Vault: v}, st, v
}

func TestManagerFollowsSettings(t *testing.T) {
	m, st, vault := newManager(t)
	ctx := context.Background()
	now := time.Now()

	// Nothing chosen: Dexcom, and it is not set up.
	if m.Name() != "dexcom" {
		t.Errorf("default name = %q", m.Name())
	}
	if _, err := m.Samples(ctx, now.Add(-time.Hour), now); !errors.Is(err, glucose.ErrAuth) {
		t.Errorf("unconfigured dexcom = %v, want ErrAuth", err)
	}

	// Switch to Nightscout in Settings: it takes effect without a restart.
	var gotToken string
	ns := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("token")
		_, _ = w.Write([]byte(`[{"type":"sgv","sgv":111,"date":` + itoa(now.Add(-10*time.Minute).UnixMilli()) + `}]`))
	}))
	defer ns.Close()
	cfg, _ := st.LoadConfig()
	cfg.GlucoseSource = "nightscout"
	_ = cfg.Set("source_nightscout_url", ns.URL)
	if err := st.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if m.Name() != "nightscout" {
		t.Errorf("name after switch = %q", m.Name())
	}
	got, err := m.Samples(ctx, now.Add(-time.Hour), now)
	if err != nil || len(got) != 1 || got[0].Value != 111 {
		t.Fatalf("nightscout samples = %+v, %v", got, err)
	}

	// A secret from the vault reaches the source, and a change rebuilds it.
	if err := vault.Set("source_nightscout_token", "abc"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Samples(ctx, now.Add(-time.Hour), now); err != nil || gotToken != "abc" {
		t.Errorf("token not used: %q, %v", gotToken, err)
	}
}

func TestManagerFallbackAndUnknown(t *testing.T) {
	m, st, _ := newManager(t)
	m.Fallback = "librelinkup"
	if m.Name() != "librelinkup" {
		t.Errorf("env fallback not used: %q", m.Name())
	}
	cfg, _ := st.LoadConfig()
	cfg.GlucoseSource = "dexcom"
	_ = st.SaveConfig(cfg)
	if m.Name() != "dexcom" {
		t.Errorf("Settings must beat the env fallback: %q", m.Name())
	}
	// A stored id that no longer exists (a removed source) is an error, not a silent Dexcom.
	rec, _ := st.App.FindFirstRecordByFilter("settings", "")
	rec.Set("glucose_source", "gone")
	if err := st.App.SaveNoValidate(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Samples(context.Background(), time.Now().Add(-time.Hour), time.Now()); err == nil || !strings.Contains(err.Error(), "gone") {
		t.Errorf("unknown stored source = %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
