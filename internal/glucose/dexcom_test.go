package glucose

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func dexDate(t time.Time) string { return fmt.Sprintf("Date(%d)", t.UnixMilli()) }

// fakeShare serves the three Share endpoints.
type fakeShare struct {
	logins, reads atomic.Int32
	expireOnce    atomic.Bool
	lastMinutes   atomic.Value
	badPassword   bool
}

func (f *fakeShare) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/General/AuthenticatePublisherAccount", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if f.badPassword {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Code":"AccountPasswordInvalid"}`))
			return
		}
		_, _ = w.Write([]byte(`"acct-1"`))
	})
	mux.HandleFunc("/General/LoginPublisherAccountById", func(w http.ResponseWriter, r *http.Request) {
		n := f.logins.Add(1)
		_, _ = fmt.Fprintf(w, `"session-%d"`, n)
	})
	mux.HandleFunc("/Publisher/ReadPublisherLatestGlucoseValues", func(w http.ResponseWriter, r *http.Request) {
		f.reads.Add(1)
		f.lastMinutes.Store(r.URL.Query().Get("minutes"))
		if f.expireOnce.CompareAndSwap(true, false) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"Code":"SessionIdNotFound"}`))
			return
		}
		out := []map[string]any{
			{"ST": dexDate(t0.Add(-10 * time.Minute)), "Value": 150},
			{"ST": dexDate(t0.Add(-5 * time.Minute)), "Value": 140},
			{"WT": dexDate(t0.Add(-15 * time.Minute)), "Value": 130}, // no ST: falls back to WT
			{"ST": dexDate(t0.Add(-3 * time.Hour)), "Value": 99},     // outside window
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	return mux
}

func newClient(f *fakeShare) (*DexcomShare, func()) {
	srv := httptest.NewServer(f.handler())
	d := NewDexcomShare(srv.URL, "user", "pw")
	d.Now = func() time.Time { return t0 }
	return d, srv.Close
}

func TestSamplesFiltersAndSorts(t *testing.T) {
	f := &fakeShare{}
	d, done := newClient(f)
	defer done()

	got, err := d.Samples(context.Background(), t0.Add(-20*time.Minute), t0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d samples, want 3: %+v", len(got), got)
	}
	want := []float64{130, 150, 140}
	for i, w := range want {
		if got[i].Value != w {
			t.Errorf("sample %d = %v, want %v", i, got[i].Value, w)
		}
	}
	if m := f.lastMinutes.Load(); m != "21" {
		t.Errorf("minutes param = %v, want 21", m)
	}
}

func TestSessionCachedAndRefreshed(t *testing.T) {
	f := &fakeShare{}
	d, done := newClient(f)
	defer done()
	ctx := context.Background()
	from := t0.Add(-20 * time.Minute)

	for range 2 {
		if _, err := d.Samples(ctx, from, t0); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.logins.Load(); n != 1 {
		t.Errorf("logins = %d, want 1 (session cached)", n)
	}

	f.expireOnce.Store(true)
	if _, err := d.Samples(ctx, from, t0); err != nil {
		t.Fatalf("expected transparent relogin: %v", err)
	}
	if n := f.logins.Load(); n != 2 {
		t.Errorf("logins = %d, want 2 after expiry", n)
	}
}

func TestTooOld(t *testing.T) {
	d, done := newClient(&fakeShare{})
	defer done()
	_, err := d.Samples(context.Background(), t0.Add(-25*time.Hour), t0)
	if !errors.Is(err, ErrTooOld) {
		t.Errorf("err = %v, want ErrTooOld", err)
	}
}

func TestBadPassword(t *testing.T) {
	d, done := newClient(&fakeShare{badPassword: true})
	defer done()
	_, err := d.Samples(context.Background(), t0.Add(-time.Hour), t0)
	if !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v, want ErrAuth", err)
	}
}

func TestNullUUIDIsAuthFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`"` + nullUUID + `"`))
	}))
	defer srv.Close()
	d := NewDexcomShare(srv.URL, "u", "p")
	d.Now = func() time.Time { return t0 }
	if _, err := d.Samples(context.Background(), t0.Add(-time.Hour), t0); !errors.Is(err, ErrAuth) {
		t.Errorf("err = %v, want ErrAuth", err)
	}
}
