// Package cas implements a content-addressed blob store: files are written
// under a directory keyed by their SHA-256 digest, deduplicated on write,
// and made read-only once stored.
package cas

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
)

// Store is a content-addressed store rooted at a directory on disk.
type Store struct {
	root string
}

// Open prepares (creating if needed) a content-addressed store rooted at dir.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(filepath.Join(dir, "sha256"), 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(dir, ".tmp"), 0o755); err != nil {
		return nil, err
	}
	return &Store{root: dir}, nil
}

// Path returns the on-disk path a blob with the given hex digest would
// occupy, whether or not it currently exists. Two levels of 2-hex-char
// sharding (65,536 leaf directories) keep any single directory small even
// at millions of stored blobs.
func (s *Store) Path(digest string) string {
	return filepath.Join(s.root, "sha256", digest[:2], digest[2:4], digest)
}

// Has reports whether a blob with the given digest is already stored.
func (s *Store) Has(digest string) bool {
	_, err := os.Stat(s.Path(digest))
	return err == nil
}

// Size returns the byte size of a stored blob.
func (s *Store) Size(digest string) (int64, error) {
	info, err := os.Stat(s.Path(digest))
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Write streams src into the store, hashing as it goes, and returns the hex
// digest and byte size. deduped is true if a blob with that digest was
// already present, in which case the existing copy is kept and src's bytes
// are discarded after hashing.
func (s *Store) Write(src io.Reader) (digest string, size int64, deduped bool, err error) {
	tmp, err := os.CreateTemp(filepath.Join(s.root, ".tmp"), "")
	if err != nil {
		return "", 0, false, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once renamed into place

	h := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(tmp, h), src)
	closeErr := tmp.Close()
	if copyErr != nil {
		return "", 0, false, copyErr
	}
	if closeErr != nil {
		return "", 0, false, closeErr
	}

	digest = hex.EncodeToString(h.Sum(nil))
	final := s.Path(digest)

	if s.Has(digest) {
		return digest, n, true, nil
	}
	if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
		return "", 0, false, err
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return "", 0, false, err
	}
	_ = os.Chmod(final, 0o444) // best-effort; dedup relies on presence, not perms

	return digest, n, false, nil
}

// WriteFile is a convenience wrapper over Write for an on-disk source file.
func (s *Store) WriteFile(path string) (digest string, size int64, deduped bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, false, err
	}
	defer f.Close()
	return s.Write(f)
}

// Get opens a stored blob for reading.
func (s *Store) Get(digest string) (*os.File, error) {
	return os.Open(s.Path(digest))
}
