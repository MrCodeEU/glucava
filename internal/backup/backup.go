// Package backup writes and restores a Glucava backup: one zip with a
// consistent copy of the database, a small manifest and, only when asked for,
// the encryption key. The secrets inside the database stay encrypted, so a
// backup without the key exposes health data but no credentials.
package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/pocketbase/dbx"
)

const (
	dbName       = "data.db"
	keyName      = "secret.key"
	manifestName = "glucava-backup.json"

	// maxRestoreSize bounds what a restore extracts, so a crafted zip cannot fill the disk.
	maxRestoreSize = 4 << 30
)

// Manifest describes a backup. It is the first entry of the zip.
type Manifest struct {
	Format      int       `json:"format"`
	Build       string    `json:"build"`
	Created     time.Time `json:"created"`
	IncludesKey bool      `json:"includes_key"`
}

// DB is the part of the PocketBase app Create needs.
type DB interface {
	ConcurrentDB() dbx.Builder
	DataDir() string
}

// Create writes a backup of app's database to w. The database is copied with
// VACUUM INTO, which gives a consistent snapshot while the server keeps running
// and only briefly blocks writers. With includeKey the key file in the data
// dir is added too; a key held in GLUCAVA_SECRET_KEY has no file and is never
// included.
func Create(ctx context.Context, app DB, w io.Writer, build string, includeKey bool) error {
	tmp, err := os.MkdirTemp(app.DataDir(), "backup-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmp) }()
	snapshot := filepath.Join(tmp, dbName)
	if _, err := app.ConcurrentDB().NewQuery("VACUUM INTO {:path}").Bind(dbx.Params{"path": snapshot}).WithContext(ctx).Execute(); err != nil {
		return fmt.Errorf("backup: copy database: %w", err)
	}

	keyPath := filepath.Join(app.DataDir(), keyName)
	hasKey := false
	if includeKey {
		if _, err := os.Stat(keyPath); err == nil {
			hasKey = true
		}
	}

	zw := zip.NewWriter(w)
	mw, err := zw.Create(manifestName)
	if err != nil {
		return err
	}
	if err := json.NewEncoder(mw).Encode(Manifest{Format: 1, Build: build, Created: time.Now().UTC(), IncludesKey: hasKey}); err != nil {
		return err
	}
	if err := addFile(zw, snapshot, dbName); err != nil {
		return err
	}
	if hasKey {
		if err := addFile(zw, keyPath, keyName); err != nil {
			return err
		}
	}
	return zw.Close()
}

func addFile(zw *zip.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate, Modified: time.Now()})
	if err != nil {
		return err
	}
	_, err = io.Copy(w, f)
	return err
}

// Restore unpacks the backup zip at src into dataDir. The server must not be
// running. An existing database makes it fail unless overwrite is set, in which
// case the old one is kept as data.db.before-restore. The key file in the
// backup (if any) is restored too, but never replaces an existing one unless
// overwrite is set. It returns the manifest.
func Restore(src, dataDir string, overwrite bool) (Manifest, error) {
	var m Manifest
	zr, err := zip.OpenReader(src)
	if err != nil {
		return m, fmt.Errorf("backup: not a zip file: %w", err)
	}
	defer func() { _ = zr.Close() }()

	files := map[string]*zip.File{}
	var total uint64
	for _, f := range zr.File {
		files[f.Name] = f // names are only compared, never joined into a path
		total += f.UncompressedSize64
	}
	if total > maxRestoreSize {
		return m, errors.New("backup: archive is too large")
	}
	mf, ok := files[manifestName]
	if !ok {
		return m, errors.New("backup: not a Glucava backup (no " + manifestName + ")")
	}
	if err := readJSON(mf, &m); err != nil {
		return m, fmt.Errorf("backup: read manifest: %w", err)
	}
	if m.Format != 1 {
		return m, fmt.Errorf("backup: format %d is not supported by this version", m.Format)
	}
	dbf, ok := files[dbName]
	if !ok {
		return m, errors.New("backup: the archive has no database")
	}

	if err := checkSQLite(dbf); err != nil {
		return m, err
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return m, err
	}
	dbPath := filepath.Join(dataDir, dbName)
	if _, err := os.Stat(dbPath); err == nil {
		if !overwrite {
			return m, errors.New("backup: " + dbPath + " exists; stop the server and use --force to replace it (the old one is kept as " + dbName + ".before-restore)")
		}
		if err := os.Rename(dbPath, dbPath+".before-restore"); err != nil {
			return m, err
		}
	}
	// A stale write-ahead log from the old database would corrupt the restored one.
	for _, ext := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + ext); err != nil && !os.IsNotExist(err) {
			return m, err
		}
	}
	if err := extract(dbf, dbPath); err != nil {
		return m, err
	}
	if kf, ok := files[keyName]; ok {
		keyPath := filepath.Join(dataDir, keyName)
		if _, err := os.Stat(keyPath); err != nil || overwrite {
			if err := extract(kf, keyPath); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

// checkSQLite refuses an archive whose database entry is not a SQLite file,
// before anything in the data dir is touched.
func checkSQLite(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	head := make([]byte, 16)
	if _, err := io.ReadFull(rc, head); err != nil || string(head) != "SQLite format 3\x00" {
		return errors.New("backup: the database in the archive is not a SQLite file")
	}
	return nil
}

func readJSON(f *zip.File, v any) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	return json.NewDecoder(io.LimitReader(rc, 1<<16)).Decode(v)
}

func extract(f *zip.File, dst string) error {
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, io.LimitReader(rc, maxRestoreSize)); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}
