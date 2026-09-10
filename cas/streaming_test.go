package cas

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPathMalformedDigestDoesNotPanic(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, digest := range []string{"", "a", "ab", "abc"} {
		_ = store.Path(digest) // must not panic
	}
}

func TestWriteWithRelativeRoot(t *testing.T) {
	parent := t.TempDir()
	t.Chdir(parent)
	store, err := Open("./cas/")
	if err != nil {
		t.Fatal(err)
	}
	digest, _, _, err := store.Write(bytes.NewReader([]byte("relative root")))
	if err != nil {
		t.Fatal(err)
	}
	if !store.Has(digest) {
		t.Fatal("written blob missing")
	}
	if _, err := os.Stat(filepath.Join(parent, "cas", "sha256")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteExpectedMismatchRejected(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" // sha256("")
	_, _, err = store.WriteExpected(bytes.NewReader([]byte("actual content")), wantDigest)
	if !errors.Is(err, ErrDigestMismatch) {
		t.Fatalf("err = %v, want ErrDigestMismatch", err)
	}
	if store.Has(wantDigest) {
		t.Fatal("mismatched content must not become visible under the declared digest")
	}
}

func TestWriteExpectedInvalidDigestRejected(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = store.WriteExpected(bytes.NewReader([]byte("x")), "not-a-digest")
	if !errors.Is(err, ErrInvalidDigest) {
		t.Fatalf("err = %v, want ErrInvalidDigest", err)
	}
}

func TestWriteExpectedSuccess(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("known content")
	digest, _, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	// Remove it and redo via WriteExpected to test the primitive standalone.
	store2, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	size, deduped, err := store2.WriteExpected(bytes.NewReader(content), digest)
	if err != nil {
		t.Fatal(err)
	}
	if deduped {
		t.Fatal("first write should not be deduped")
	}
	if size != int64(len(content)) {
		t.Fatalf("size = %d, want %d", size, len(content))
	}
	if !store2.Has(digest) {
		t.Fatal("expected digest should be visible after WriteExpected")
	}
}

func TestReadVerifiedNotFound(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	_, err = store.ReadVerified(digest, 1024)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestReadVerifiedExceedsMaxBytes(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("this content is bigger than the limit")
	digest, _, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadVerified(digest, 4); err == nil {
		t.Fatal("expected an error when blob exceeds maxBytes")
	}
}

func TestReadVerifiedRoundTrip(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("round trip content")
	digest, _, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.ReadVerified(digest, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("got %q, want %q", got, content)
	}
}

func TestReadVerifiedDetectsCorruption(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("original content")
	digest, _, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	path := store.Path(digest)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("tampered content!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReadVerified(digest, 1024); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestVerifyRoundTrip(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("verify me")
	digest, size, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(digest, size); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyNotFound(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	digest := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if err := store.Verify(digest, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestVerifyDetectsSizeMismatch(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("size check")
	digest, size, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(digest, size+1); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

// TestWriteDedupVerifiesExistingContent guards against trusting mere
// presence at the digest path: if the on-disk blob was corrupted after
// being stored, a second Write of the same original content must surface
// that corruption instead of silently reporting a clean dedup.
func TestWriteDedupVerifiesExistingContent(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("dedup me")
	digest, _, _, err := store.Write(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	path := store.Path(digest)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("corrupted!"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, _, err = store.Write(bytes.NewReader(content))
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}
