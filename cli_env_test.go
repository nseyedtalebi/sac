package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPutUsesSACEnvironmentPathsAndCatalogsBlob(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	catalog := filepath.Join(dir, "catalog.sqlite")
	src := filepath.Join(dir, "artifact.txt")
	if err := os.WriteFile(src, []byte("from env store"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".", "put", src)
	cmd.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . put failed: %v\n%s", err, out)
	}
	if entries, err := os.ReadDir(filepath.Join(store, "sha256")); err != nil || len(entries) == 0 {
		t.Fatalf("expected blob under SAC_STORE, entries=%v err=%v\noutput:%s", entries, err, out)
	}
	if _, err := os.Stat(catalog); err != nil {
		t.Fatalf("expected SQLite catalog at SAC_CATALOG: %v\noutput:%s", err, out)
	}
}

func TestVerifyUsesSACEnvironmentPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	catalog := filepath.Join(dir, "catalog.sqlite")
	src := filepath.Join(dir, "artifact.txt")
	if err := os.WriteFile(src, []byte("verify from env"), 0o644); err != nil {
		t.Fatal(err)
	}
	put := exec.Command("go", "run", ".", "put", src)
	put.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	if out, err := put.CombinedOutput(); err != nil {
		t.Fatalf("go run . put failed: %v\n%s", err, out)
	}

	cmd := exec.Command("go", "run", ".", "verify")
	cmd.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . verify failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok: 1 artifacts verified") {
		t.Fatalf("verify output = %q, want one verified artifact", out)
	}
}

func TestConcurrentPutsCatalogEveryBlob(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	catalog := filepath.Join(dir, "catalog.sqlite")
	bin := filepath.Join(dir, "sac")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}

	const writers = 64
	errs := make(chan error, writers)
	for i := range writers {
		go func() {
			src := filepath.Join(dir, fmt.Sprintf("artifact-%d.txt", i))
			if err := os.WriteFile(src, []byte(fmt.Sprintf("concurrent artifact %d", i)), 0o644); err != nil {
				errs <- err
				return
			}
			put := exec.Command(bin, "put", src)
			put.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
			if out, err := put.CombinedOutput(); err != nil {
				errs <- fmt.Errorf("put %d: %w\n%s", i, err, out)
				return
			}
			errs <- nil
		}()
	}
	for range writers {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}

	verify := exec.Command(bin, "verify")
	verify.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	out, err := verify.CombinedOutput()
	if err != nil {
		t.Fatalf("verify failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ok: 64 artifacts verified") {
		t.Fatalf("verify output = %q, want 64 verified artifacts", out)
	}
}
