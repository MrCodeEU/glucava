package secrets

import (
	"database/sql"
	"errors"

	"github.com/pocketbase/pocketbase/core"
)

// Vault stores encrypted values in the secrets collection.
type Vault struct {
	App    core.App
	Cipher *Cipher
}

// Set encrypts value and stores it under name, replacing any earlier value.
func (v *Vault) Set(name, value string) error {
	sealed, err := v.Cipher.Seal(name, value)
	if err != nil {
		return err
	}
	rec, err := v.App.FindFirstRecordByData("secrets", "name", name)
	if errors.Is(err, sql.ErrNoRows) {
		col, cerr := v.App.FindCollectionByNameOrId("secrets")
		if cerr != nil {
			return cerr
		}
		rec = core.NewRecord(col)
		rec.Set("name", name)
	} else if err != nil {
		return err
	}
	rec.Set("ciphertext", sealed)
	return v.App.Save(rec)
}

// Get returns the decrypted value. ok is false when name is not stored.
func (v *Vault) Get(name string) (value string, ok bool, err error) {
	rec, err := v.App.FindFirstRecordByData("secrets", "name", name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	value, err = v.Cipher.Open(name, rec.GetString("ciphertext"))
	return value, err == nil, err
}

// Delete removes a stored value. Deleting an unknown name is not an error.
func (v *Vault) Delete(name string) error {
	rec, err := v.App.FindFirstRecordByData("secrets", "name", name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return v.App.Delete(rec)
}
