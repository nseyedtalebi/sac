package cas

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteGetDedup(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	content := []byte("hello, content-addressed world")
	digest1, size1, deduped1, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if deduped1 {
		t.Fatal("first write should not be deduped")
	}
	if size1 != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size1, len(content))
	}

	digest2, _, deduped2, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if !deduped2 {
		t.Fatal("second write of identical content should be deduped")
	}
	if digest1 != digest2 {
		t.Fatalf("digests differ: %s != %s", digest1, digest2)
	}

	f, err := store.Get(digest1)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("read back %q, want %q", got, content)
	}
}

func TestReadOnly(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest, _, _, err := store.Write(bytes.NewReader([]byte("x")))
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path(digest))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o200 != 0 {
		t.Fatalf("blob is writable: mode %v", info.Mode())
	}
}

func TestPathSharding(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest := "abcdef0123456789"
	want := filepath.Join(store.root, "sha256", "ab", digest)
	if got := store.Path(digest); got != want {
		t.Fatalf("Path = %s, want %s", got, want)
	}
}

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	if err := os.WriteFile(src, []byte("from a file"), 0o644); err != nil {
		t.Fatal(err)
	}

	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest, size, deduped, err := store.WriteFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if deduped {
		t.Fatal("should not be deduped")
	}
	if size != 11 {
		t.Fatalf("size = %d, want 11", size)
	}
	if !store.Has(digest) {
		t.Fatal("store should have digest after write")
	}
}
