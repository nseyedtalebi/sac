// Package lineage is a durable, hash-chained, local-first event log: writes
// land in SQLite immediately (so callers survive an outage of whatever
// remote store the log eventually drains to), each event chains to the one
// before it, and re-recording the same logical event is a no-op.
package lineage

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS event_log (
    seq        INTEGER PRIMARY KEY,
    event_id   TEXT NOT NULL UNIQUE,
    event_hash TEXT NOT NULL UNIQUE,
    prev_hash  TEXT,
    event_utc  TEXT NOT NULL,
    run_kind   TEXT NOT NULL,
    payload    TEXT NOT NULL,
    synced     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS artifact (
    artifact_hash  TEXT PRIMARY KEY,
    byte_size      INTEGER,
    hash_algo      TEXT NOT NULL DEFAULT 'SHA2_256',
    first_seen_utc TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS artifact_location (
    artifact_hash TEXT NOT NULL REFERENCES artifact(artifact_hash),
    locator       TEXT NOT NULL,
    observed_utc  TEXT NOT NULL,
    observed_by   TEXT NOT NULL,
    UNIQUE(artifact_hash, locator)
);
CREATE TABLE IF NOT EXISTS run (
    run_id          INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id        TEXT NOT NULL UNIQUE REFERENCES event_log(event_id),
    run_kind        TEXT NOT NULL,
    process_name    TEXT NOT NULL,
    process_version TEXT,
    actor           TEXT NOT NULL,
    host            TEXT,
    idempotency_key TEXT,
    event_utc       TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS run_input (
    run_id        INTEGER NOT NULL REFERENCES run(run_id),
    artifact_hash TEXT NOT NULL REFERENCES artifact(artifact_hash),
    role          TEXT,
    PRIMARY KEY (run_id, artifact_hash)
);
CREATE TABLE IF NOT EXISTS run_output (
    run_id        INTEGER NOT NULL REFERENCES run(run_id),
    artifact_hash TEXT NOT NULL REFERENCES artifact(artifact_hash),
    PRIMARY KEY (run_id, artifact_hash)
);
`

// Log is a hash-chained event log backed by a local SQLite file.
type Log struct {
	db *sql.DB
}

// Open connects to (creating if needed) the lineage database at path.
func Open(path string) (*Log, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection: Record's read-last-seq-then-insert sequence is only
	// safe for a single writer, same invariant a flock enforces elsewhere.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Log{db: db}, nil
}

// Close closes the underlying database.
func (l *Log) Close() error { return l.db.Close() }

// Item is an input or output artifact reference within an Event.
type Item struct {
	Hash     string   `json:"hash"` // "sha256:<hex>" or bare hex
	ByteSize int64    `json:"byte_size,omitempty"`
	Role     string   `json:"role,omitempty"` // inputs only
	Locators []string `json:"locators,omitempty"`
}

// Event is one entry in the log: a run of some process against some inputs,
// producing some outputs.
type Event struct {
	Seq            int64  `json:"seq"`
	EventID        string `json:"event_id"`
	EventHash      string `json:"event_hash"`
	PrevHash       string `json:"prev_hash,omitempty"`
	EventUTC       string `json:"event_utc"`
	RunKind        string `json:"run_kind"`
	ProcessName    string `json:"process_name"`
	ProcessVersion string `json:"process_version"`
	Actor          string `json:"actor"`
	Host           string `json:"host"`
	IdempotencyKey string `json:"idempotency_key"`
	Inputs         []Item `json:"inputs"`
	Outputs        []Item `json:"outputs"`
}

// coreFields is the subset of an Event that determines its identity: it
// excludes EventUTC/Seq/EventHash/PrevHash, so replaying the same logical
// operation yields the same EventID and Record treats it as a no-op.
type coreFields struct {
	RunKind        string `json:"run_kind"`
	ProcessName    string `json:"process_name"`
	ProcessVersion string `json:"process_version"`
	Actor          string `json:"actor"`
	Host           string `json:"host"`
	IdempotencyKey string `json:"idempotency_key"`
	Inputs         []Item `json:"inputs"`
	Outputs        []Item `json:"outputs"`
}

// canonicalJSON encodes v deterministically: map keys sort automatically,
// struct fields follow declaration order, and HTML-escaping is disabled so
// the same value always produces the same bytes.
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// hexHash strips an optional "algo:" prefix, e.g. "sha256:ab…" -> "ab…".
func hexHash(h string) string {
	if i := strings.LastIndex(h, ":"); i >= 0 {
		return h[i+1:]
	}
	return h
}

func sortedHashes(items []Item) []string {
	hs := make([]string, len(items))
	for i, it := range items {
		hs[i] = hexHash(it.Hash)
	}
	sort.Strings(hs)
	return hs
}

// BuildEvent constructs an Event ready for Record. If idempotencyKey is
// empty, one is derived from processName/processVersion and the sorted
// input/output hashes.
func BuildEvent(runKind, processName, processVersion string, inputs, outputs []Item, actor, host, idempotencyKey string) (Event, error) {
	if idempotencyKey == "" {
		idempotencyKey = fmt.Sprintf("%s:%s:%s=>%s",
			processName, processVersion,
			strings.Join(sortedHashes(inputs), ","),
			strings.Join(sortedHashes(outputs), ","))
	}

	core := coreFields{
		RunKind: runKind, ProcessName: processName, ProcessVersion: processVersion,
		Actor: actor, Host: host, IdempotencyKey: idempotencyKey,
		Inputs: inputs, Outputs: outputs,
	}
	b, err := canonicalJSON(core)
	if err != nil {
		return Event{}, err
	}
	sum := sha256.Sum256(b)

	return Event{
		EventID:        "sha256:" + hex.EncodeToString(sum[:]),
		EventUTC:       time.Now().UTC().Format(time.RFC3339),
		RunKind:        runKind,
		ProcessName:    processName,
		ProcessVersion: processVersion,
		Actor:          actor,
		Host:           host,
		IdempotencyKey: idempotencyKey,
		Inputs:         inputs,
		Outputs:        outputs,
	}, nil
}

// Record appends ev to the log idempotently: if an event with the same
// EventID has already been recorded, Record is a no-op and returns
// fresh=false. Otherwise it assigns Seq, chains PrevHash to the previous
// entry's EventHash, computes its own EventHash, and returns fresh=true.
func (l *Log) Record(ev Event) (fresh bool, err error) {
	tx, err := l.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	var existing int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM event_log WHERE event_id = ?`, ev.EventID).Scan(&existing); err != nil {
		return false, err
	}
	if existing > 0 {
		return false, tx.Commit()
	}

	var lastSeq sql.NullInt64
	var lastHash sql.NullString
	err = tx.QueryRow(`SELECT seq, event_hash FROM event_log ORDER BY seq DESC LIMIT 1`).Scan(&lastSeq, &lastHash)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	ev.Seq = lastSeq.Int64 + 1
	ev.PrevHash = lastHash.String

	ev.EventHash = ""
	envelope, err := canonicalJSON(ev)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(envelope)
	ev.EventHash = "sha256:" + hex.EncodeToString(sum[:])

	payload, err := canonicalJSON(ev)
	if err != nil {
		return false, err
	}

	if _, err := tx.Exec(
		`INSERT INTO event_log(seq, event_id, event_hash, prev_hash, event_utc, run_kind, payload) VALUES (?,?,?,?,?,?,?)`,
		ev.Seq, ev.EventID, ev.EventHash, nullIfEmpty(ev.PrevHash), ev.EventUTC, ev.RunKind, string(payload),
	); err != nil {
		return false, err
	}

	if _, err := tx.Exec(
		`INSERT INTO run(event_id, run_kind, process_name, process_version, actor, host, idempotency_key, event_utc) VALUES (?,?,?,?,?,?,?,?)`,
		ev.EventID, ev.RunKind, ev.ProcessName, ev.ProcessVersion, ev.Actor, ev.Host, ev.IdempotencyKey, ev.EventUTC,
	); err != nil {
		return false, err
	}
	var runID int64
	if err := tx.QueryRow(`SELECT run_id FROM run WHERE event_id = ?`, ev.EventID).Scan(&runID); err != nil {
		return false, err
	}

	upsert := func(it Item, isInput bool) error {
		h := hexHash(it.Hash)
		if _, err := tx.Exec(
			`INSERT OR IGNORE INTO artifact(artifact_hash, byte_size, first_seen_utc) VALUES (?,?,?)`,
			h, it.ByteSize, ev.EventUTC,
		); err != nil {
			return err
		}
		for _, loc := range it.Locators {
			if loc == "" {
				continue
			}
			if _, err := tx.Exec(
				`INSERT OR IGNORE INTO artifact_location(artifact_hash, locator, observed_utc, observed_by) VALUES (?,?,?,?)`,
				h, loc, ev.EventUTC, ev.Actor,
			); err != nil {
				return err
			}
		}
		if isInput {
			_, err := tx.Exec(`INSERT OR IGNORE INTO run_input(run_id, artifact_hash, role) VALUES (?,?,?)`, runID, h, it.Role)
			return err
		}
		_, err := tx.Exec(`INSERT OR IGNORE INTO run_output(run_id, artifact_hash) VALUES (?,?)`, runID, h)
		return err
	}
	for _, in := range ev.Inputs {
		if err := upsert(in, true); err != nil {
			return false, err
		}
	}
	for _, out := range ev.Outputs {
		if err := upsert(out, false); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// Verify walks the event log from the beginning and confirms every entry's
// EventHash matches its recomputed value and correctly chains to the
// previous entry's hash. It returns the number of events verified; on a
// mismatch it returns the count verified so far alongside the error.
func (l *Log) Verify() (count int64, err error) {
	rows, err := l.db.Query(`SELECT seq, payload, event_hash, prev_hash FROM event_log ORDER BY seq ASC`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var expectedPrev sql.NullString
	for rows.Next() {
		var seq int64
		var payload, eventHash string
		var prevHash sql.NullString
		if err := rows.Scan(&seq, &payload, &eventHash, &prevHash); err != nil {
			return count, err
		}
		if prevHash.Valid != expectedPrev.Valid || prevHash.String != expectedPrev.String {
			return count, fmt.Errorf("seq %d: prev_hash mismatch: got %q, want %q", seq, prevHash.String, expectedPrev.String)
		}

		var ev Event
		if err := json.Unmarshal([]byte(payload), &ev); err != nil {
			return count, err
		}
		storedHash := ev.EventHash
		ev.EventHash = ""
		envelope, err := canonicalJSON(ev)
		if err != nil {
			return count, err
		}
		sum := sha256.Sum256(envelope)
		recomputed := "sha256:" + hex.EncodeToString(sum[:])
		if recomputed != eventHash || recomputed != storedHash {
			return count, fmt.Errorf("seq %d: event_hash mismatch: stored %q, recomputed %q", seq, eventHash, recomputed)
		}

		expectedPrev = sql.NullString{String: eventHash, Valid: true}
		count++
	}
	return count, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
