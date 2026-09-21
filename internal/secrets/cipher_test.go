package secrets

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testCipher(t *testing.T) *Cipher {
	t.Helper()
	c, err := NewCipher([]byte(strings.Repeat("k", keyBytes)))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRoundTrip(t *testing.T) {
	c := testCipher(t)
	sealed, err := c.Seal("dexcom_password", "hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "hunter2") {
		t.Fatal("ciphertext leaks plaintext")
	}
	got, err := c.Open("dexcom_password", sealed)
	if err != nil || got != "hunter2" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestNonceUnique(t *testing.T) {
	c := testCipher(t)
	a, _ := c.Seal("n", "same")
	b, _ := c.Seal("n", "same")
	if a == b {
		t.Fatal("two seals of the same value must differ")
	}
}

func TestWrongNameFails(t *testing.T) {
	c := testCipher(t)
	sealed, _ := c.Seal("strava_cookies", "x")
	if _, err := c.Open("dexcom_password", sealed); err == nil {
		t.Fatal("expected failure when opened under another name")
	}
}

func TestWrongKeyAndGarbage(t *testing.T) {
	c := testCipher(t)
	other, _ := NewCipher([]byte(strings.Repeat("z", keyBytes)))
	sealed, _ := c.Seal("n", "x")
	if _, err := other.Open("n", sealed); err == nil {
		t.Error("expected failure with wrong key")
	}
	for _, bad := range []string{"", "!!!", "AAAA"} {
		if _, err := c.Open("n", bad); err == nil {
			t.Errorf("expected failure for %q", bad)
		}
	}
}

func TestBadKeyLength(t *testing.T) {
	if _, err := NewCipher([]byte("short")); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadKeyFileCreatedAndStable(t *testing.T) {
	t.Setenv(keyEnv, "")
	dir := t.TempDir()
	k1, err := LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	k2, err := LoadKey(dir)
	if err != nil {
		t.Fatal(err)
	}
	if string(k1) != string(k2) {
		t.Fatal("key changed between loads")
	}
	st, err := os.Stat(filepath.Join(dir, keyFile))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("key file mode = %v, want 0600", st.Mode().Perm())
	}
}

func TestLoadKeyEnv(t *testing.T) {
	key := []byte(strings.Repeat("e", keyBytes))
	t.Setenv(keyEnv, base64.StdEncoding.EncodeToString(key))
	got, err := LoadKey(t.TempDir())
	if err != nil || string(got) != string(key) {
		t.Fatalf("got %q, %v", got, err)
	}
	t.Setenv(keyEnv, "not-base64-32-bytes")
	if _, err := LoadKey(t.TempDir()); err == nil {
		t.Error("expected error for invalid env key")
	}
}
