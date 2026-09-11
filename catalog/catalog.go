// Package catalog tracks blobs known to have been durably written to a SAC
// content-addressed store. The filesystem remains the authority for bytes;
// this SQLite database is an inventory that can be reconciled with it.
package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/nseyedtalebi/sac/cas"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

const schema = `
CREATE TABLE IF NOT EXISTS artifact (
    digest            TEXT PRIMARY KEY,
    byte_size         INTEGER NOT NULL CHECK (byte_size >= 0),
    first_seen_utc    TEXT NOT NULL,
    last_verified_utc TEXT NOT NULL
);
`

const (
	busyTimeout  = time.Second
	busyAttempts = 8
)

// Catalog is a SQLite inventory of known blobs.
type Catalog struct {
	db *sql.DB
}

// Artifact is one known blob and its most recent successful verification.
type Artifact struct {
	Digest          string
	Size            int64
	FirstSeenUTC    string
	LastVerifiedUTC string
}

// Open connects to (creating if needed) the catalog database at path.
func Open(path string) (*Catalog, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection per process plus WAL lets independent CLI invocations
	// serialize their short catalog writes instead of failing with SQLITE_BUSY.
	db.SetMaxOpenConns(1)
	if err := retryBusy(func() error {
		_, err := db.Exec(fmt.Sprintf(`PRAGMA busy_timeout = %d; PRAGMA journal_mode = WAL;`, busyTimeout.Milliseconds()))
		return err
	}); err != nil {
		db.Close()
		return nil, err
	}
	if err := retryBusy(func() error {
		_, err := db.Exec(schema)
		return err
	}); err != nil {
		db.Close()
		return nil, err
	}
	return &Catalog{db: db}, nil
}

// Close closes the underlying database.
func (c *Catalog) Close() error { return c.db.Close() }

// Record adds a successfully written blob to the inventory. Re-recording the
// same digest and size is a no-op; a different size is a corruption signal.
func (c *Catalog) Record(digest string, size int64) (fresh bool, err error) {
	if !cas.ValidDigest(digest) {
		return false, fmt.Errorf("catalog: invalid digest %q", digest)
	}
	if size < 0 {
		return false, fmt.Errorf("catalog: size must not be negative")
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	err = retryBusy(func() error {
		fresh = false
		tx, err := c.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()

		result, err := tx.Exec(
			`INSERT INTO artifact(digest, byte_size, first_seen_utc, last_verified_utc) VALUES (?, ?, ?, ?)
			 ON CONFLICT(digest) DO NOTHING`,
			digest, size, now, now,
		)
		if err != nil {
			return err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if inserted == 0 {
			var existing int64
			if err := tx.QueryRow(`SELECT byte_size FROM artifact WHERE digest = ?`, digest).Scan(&existing); err != nil {
				return err
			}
			if existing != size {
				return fmt.Errorf("catalog: digest %s has size %d, not %d", digest, existing, size)
			}
		} else {
			fresh = true
		}
		return tx.Commit()
	})
	return fresh, err
}

// List returns every known artifact in digest order.
func (c *Catalog) List() ([]Artifact, error) {
	rows, err := c.db.Query(`SELECT digest, byte_size, first_seen_utc, last_verified_utc FROM artifact ORDER BY digest`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var artifacts []Artifact
	for rows.Next() {
		var artifact Artifact
		if err := rows.Scan(&artifact.Digest, &artifact.Size, &artifact.FirstSeenUTC, &artifact.LastVerifiedUTC); err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact)
	}
	return artifacts, rows.Err()
}

// MarkVerified records that the filesystem verified a known blob just now.
func (c *Catalog) MarkVerified(digest string) error {
	return retryBusy(func() error {
		result, err := c.db.Exec(`UPDATE artifact SET last_verified_utc = ? WHERE digest = ?`, time.Now().UTC().Format(time.RFC3339Nano), digest)
		if err != nil {
			return err
		}
		if updated, err := result.RowsAffected(); err != nil {
			return err
		} else if updated != 1 {
			return fmt.Errorf("catalog: unknown digest %s", digest)
		}
		return nil
	})
}

func retryBusy(op func() error) error {
	delay := 10 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err := op()
		if err == nil || !isBusy(err) || attempt == busyAttempts-1 {
			return err
		}
		time.Sleep(delay)
		delay *= 2
	}
}

func isBusy(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_BUSY
}
