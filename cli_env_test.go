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

func TestPruneMissingDryRunThenApplyRestoresVerify(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	catalog := filepath.Join(dir, "catalog.sqlite")
	src := filepath.Join(dir, "deleted-artifact.txt")
	if err := os.WriteFile(src, []byte("delete this blob"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	put := exec.Command("go", "run", ".", "put", "--locator", "file:///deleted-artifact.txt", src)
	put.Env = env
	putOut, err := put.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . put failed: %v\n%s", err, putOut)
	}
	digest := strings.Fields(string(putOut))[0]
	if err := os.Remove(filepath.Join(store, "sha256", digest[:2], digest[2:4], digest)); err != nil {
		t.Fatal(err)
	}

	dryRun := exec.Command("go", "run", ".", "prune-missing")
	dryRun.Env = env
	dryRunOut, err := dryRun.CombinedOutput()
	if err != nil || !strings.Contains(string(dryRunOut), "dry-run: 1 missing artifacts") {
		t.Fatalf("prune-missing dry-run = %v\n%s", err, dryRunOut)
	}

	verify := exec.Command("go", "run", ".", "verify")
	verify.Env = env
	if out, err := verify.CombinedOutput(); err == nil || !strings.Contains(string(out), digest) {
		t.Fatalf("verify after dry-run = %v\n%s", err, out)
	}

	apply := exec.Command("go", "run", ".", "prune-missing", "--apply")
	apply.Env = env
	applyOut, err := apply.CombinedOutput()
	if err != nil || !strings.Contains(string(applyOut), "ok: 1 missing artifacts removed") {
		t.Fatalf("prune-missing --apply = %v\n%s", err, applyOut)
	}

	verify = exec.Command("go", "run", ".", "verify")
	verify.Env = env
	if out, err := verify.CombinedOutput(); err != nil || !strings.Contains(string(out), "ok: 0 artifacts verified") {
		t.Fatalf("verify after apply = %v\n%s", err, out)
	}
	locate := exec.Command("go", "run", ".", "locate", "--prefix", "file:///deleted-artifact.txt")
	locate.Env = env
	if out, err := locate.CombinedOutput(); err != nil || string(out) != "[]\n" {
		t.Fatalf("locate after apply = %v\n%s", err, out)
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

func TestLocateListsLocatorRecordedByPut(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	catalog := filepath.Join(dir, "catalog.sqlite")
	src := filepath.Join(dir, "checkpoint.bin")
	locator := "ml://checkpoint/meta%2Fllama-3.1-8b/step-000120"
	if err := os.WriteFile(src, []byte("checkpoint bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	put := exec.Command("go", "run", ".", "put", "--locator", locator, src)
	put.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	if out, err := put.CombinedOutput(); err != nil {
		t.Fatalf("go run . put failed: %v\n%s", err, out)
	}

	locate := exec.Command("go", "run", ".", "locate", "--prefix", "ml://checkpoint/meta%2Fllama-3.1-8b/")
	locate.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_CATALOG="+catalog)
	out, err := locate.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . locate failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), locator) {
		t.Fatalf("locate output = %q, want locator %q", out, locator)
	}
}
