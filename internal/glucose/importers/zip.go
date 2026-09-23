package importers

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
)

// maxZipMember caps a zip entry's decompressed size. Bounds a zip bomb's
// blast radius; real CGM exports are a few MB at most.
const maxZipMember = 64 << 20

// openZipMember opens the first file in a zip archive whose name pred
// accepts, fully decoded in memory. This never writes an extracted path to
// the filesystem — the classic zip-slip path-traversal vulnerability (a
// crafted "../../etc/..." entry name) cannot apply, because entry names are
// only ever compared as strings, never used to build a filesystem path.
// Decompression is capped at maxZipMember to bound a zip bomb; other
// entries, directories and nested zips are ignored.
func openZipMember(data []byte, pred func(name string) bool) (content []byte, name string, err error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, "", err
	}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !pred(f.Name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, "", err
		}
		limited := io.LimitReader(rc, maxZipMember+1)
		content, err = io.ReadAll(limited)
		_ = rc.Close()
		if err != nil {
			return nil, "", err
		}
		if len(content) > maxZipMember {
			return nil, "", fmt.Errorf("importers: %q is larger than %d bytes decompressed", f.Name, maxZipMember)
		}
		return content, f.Name, nil
	}
	return nil, "", fmt.Errorf("importers: no matching file found in the zip")
}

var zipMagic = []byte("PK\x03\x04")

func looksLikeZip(data []byte) bool {
	return bytes.HasPrefix(data, zipMagic)
}
