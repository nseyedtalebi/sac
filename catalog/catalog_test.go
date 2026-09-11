package catalog

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordListsKnownArtifactAndRejectsSizeConflict(t *testing.T) {
	c, err := Open(t.TempDir() + "/catalog.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })

	digest := strings.Repeat("a", 64)
	fresh, err := c.Record(digest, 12)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh {
		t.Fatal("first record should be fresh")
	}
	fresh, err = c.Record(digest, 12)
	if err != nil {
		t.Fatal(err)
	}
	if fresh {
		t.Fatal("same artifact should not be fresh")
	}
	if _, err := c.Record(digest, 13); err == nil {
		t.Fatal("expected a size conflict")
	}

	artifacts, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 || artifacts[0].Digest != digest || artifacts[0].Size != 12 {
		t.Fatalf("artifacts = %#v, want one recorded artifact", artifacts)
	}
}

func TestConcurrentRecordsDoNotLoseArtifacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	initial, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := initial.Close(); err != nil {
		t.Fatal(err)
	}

	const writers = 10
	errs := make(chan error, writers)
	for i := range writers {
		go func() {
			c, err := Open(path)
			if err == nil {
				_, err = c.Record(fmt.Sprintf("%064x", i), int64(i))
				closeErr := c.Close()
				if err == nil {
					err = closeErr
				}
			}
			errs <- err
		}()
	}
	for range writers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	c, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	artifacts, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != writers {
		t.Fatalf("artifact count = %d, want %d", len(artifacts), writers)
	}
}
