package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestPutUsesSACStoreWhenStoreFlagOmitted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	src := filepath.Join(dir, "artifact.txt")
	if err := os.WriteFile(src, []byte("from env store"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("go", "run", ".", "put", src, "--no-record")
	cmd.Env = append(os.Environ(), "SAC_STORE="+store, "SAC_LOG=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . put failed: %v\n%s", err, out)
	}
	if entries, err := os.ReadDir(filepath.Join(store, "sha256")); err != nil || len(entries) == 0 {
		t.Fatalf("expected blob under SAC_STORE, entries=%v err=%v\noutput:%s", entries, err, out)
	}
}

func TestVerifyUsesSACLogWhenLogFlagOmitted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "lineage.db")

	cmd := exec.Command("go", "run", ".", "verify")
	cmd.Env = append(os.Environ(), "SAC_LOG="+logPath, "SAC_STORE=")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run . verify failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(logPath); err != nil {
		t.Fatalf("expected verify to use SAC_LOG path: %v\noutput:%s", err, out)
	}
}
