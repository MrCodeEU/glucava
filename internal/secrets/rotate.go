package secrets

import (
	"fmt"

	"github.com/pocketbase/pocketbase/core"
)

// Reencrypt re-seals every stored value with next. It runs in one transaction,
// so either all values move to the new key or none do.
func (v *Vault) Reencrypt(next *Cipher) error {
	return v.App.RunInTransaction(func(tx core.App) error {
		recs, err := tx.FindAllRecords("secrets")
		if err != nil {
			return err
		}
		for _, r := range recs {
			name := r.GetString("name")
			plain, err := v.Cipher.Open(name, r.GetString("ciphertext"))
			if err != nil {
				return fmt.Errorf("secrets: %s: %w", name, err)
			}
			sealed, err := next.Seal(name, plain)
			if err != nil {
				return err
			}
			r.Set("ciphertext", sealed)
			if err := tx.Save(r); err != nil {
				return err
			}
		}
		return nil
	})
}

// NewKey returns a random key for NewCipher.
func NewKey() ([]byte, error) {
	key := make([]byte, keyBytes)
	if _, err := randRead(key); err != nil {
		return nil, err
	}
	return key, nil
}

// EncodeKey formats a key the way GLUCAVA_SECRET_KEY and secret.key store it.
func EncodeKey(key []byte) string { return encodeKey(key) }

// KeyFilePath returns where LoadKey keeps the key when GLUCAVA_SECRET_KEY is unset.
func KeyFilePath(dataDir string) string { return keyFilePath(dataDir) }

// KeyFromEnv reports whether the key comes from GLUCAVA_SECRET_KEY.
func KeyFromEnv() bool { return keyFromEnv() }
