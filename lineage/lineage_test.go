package lineage

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Log {
	t.Helper()
	l, err := Open(filepath.Join(t.TempDir(), "lineage.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestRecordIdempotent(t *testing.T) {
	l := open(t)

	ev, err := BuildEvent("ingest", "proc", "1.0",
		[]Item{{Hash: "sha256:aaaa", ByteSize: 10}},
		[]Item{{Hash: "sha256:bbbb", ByteSize: 20}},
		"nima", "host1", "")
	if err != nil {
		t.Fatal(err)
	}

	fresh1, err := l.Record(ev)
	if err != nil {
		t.Fatal(err)
	}
	if !fresh1 {
		t.Fatal("first record should be fresh")
	}

	// Re-emitting the same logical event (same EventID) must be a no-op.
	fresh2, err := l.Record(ev)
	if err != nil {
		t.Fatal(err)
	}
	if fresh2 {
		t.Fatal("re-recording the same event should not be fresh")
	}

	count, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}
}

func TestRecordChains(t *testing.T) {
	l := open(t)

	for i := 0; i < 3; i++ {
		ev, err := BuildEvent("ingest", "proc", "1.0",
			nil,
			[]Item{{Hash: "sha256:" + string(rune('a'+i)), ByteSize: int64(i)}},
			"nima", "host1", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Record(ev); err != nil {
			t.Fatal(err)
		}
	}

	count, err := l.Verify()
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	var seqs []int64
	rows, err := l.db.Query(`SELECT seq FROM event_log ORDER BY seq`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		if err := rows.Scan(&seq); err != nil {
			t.Fatal(err)
		}
		seqs = append(seqs, seq)
	}
	if len(seqs) != 3 || seqs[0] != 1 || seqs[1] != 2 || seqs[2] != 3 {
		t.Fatalf("seqs = %v, want [1 2 3]", seqs)
	}
}

func TestVerifyDetectsTampering(t *testing.T) {
	l := open(t)

	ev, err := BuildEvent("ingest", "proc", "1.0", nil,
		[]Item{{Hash: "sha256:cccc", ByteSize: 5}}, "nima", "host1", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Record(ev); err != nil {
		t.Fatal(err)
	}

	if _, err := l.db.Exec(`UPDATE event_log SET payload = REPLACE(payload, 'cccc', 'dddd') WHERE seq = 1`); err != nil {
		t.Fatal(err)
	}

	if _, err := l.Verify(); err == nil {
		t.Fatal("expected Verify to detect tampering")
	}
}
