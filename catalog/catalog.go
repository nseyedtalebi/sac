// Package catalog tracks blobs known to have been durably written to a SAC
// content-addressed store. The filesystem remains the authority for bytes;
// this SQLite database is an inventory that can be reconciled with it.
package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
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
CREATE TABLE IF NOT EXISTS artifact_locator (
    digest      TEXT NOT NULL REFERENCES artifact(digest),
    locator     TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    PRIMARY KEY (digest, locator)
);
CREATE INDEX IF NOT EXISTS artifact_locator_by_locator ON artifact_locator(locator);
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

// LocatedArtifact is one artifact observed at a URI locator.
type LocatedArtifact struct {
	Digest     string
	Size       int64
	Locator    string
	ObservedAt string
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

// Record adds a successfully written blob and optional observed URI locators to
// the inventory. Re-recording the same digest and size is a no-op; a different
// size is a corruption signal.
func (c *Catalog) Record(digest string, size int64, locators ...string) (fresh bool, err error) {
	if !cas.ValidDigest(digest) {
		return false, fmt.Errorf("catalog: invalid digest %q", digest)
	}
	if size < 0 {
		return false, fmt.Errorf("catalog: size must not be negative")
	}
	locators, err = normalizeLocators(locators)
	if err != nil {
		return false, err
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
		for _, locator := range locators {
			if _, err := tx.Exec(
				`INSERT INTO artifact_locator(digest, locator, observed_at) VALUES (?, ?, ?)
				 ON CONFLICT(digest, locator) DO NOTHING`,
				digest, locator, now,
			); err != nil {
				return err
			}
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

// ListLocators returns every artifact with a locator that starts with prefix.
// An empty prefix lists all locator observations.
func (c *Catalog) ListLocators(prefix string) ([]LocatedArtifact, error) {
	if prefix != "" {
		if _, err := parseLocator(prefix); err != nil {
			return nil, err
		}
	}
	pattern := escapeLike(prefix) + "%"
	rows, err := c.db.Query(
		`SELECT a.digest, a.byte_size, l.locator, l.observed_at
		 FROM artifact_locator AS l
		 JOIN artifact AS a ON a.digest = l.digest
		 WHERE l.locator LIKE ? ESCAPE '\'
		 ORDER BY l.locator, a.digest`,
		pattern,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	artifacts := make([]LocatedArtifact, 0)
	for rows.Next() {
		var artifact LocatedArtifact
		if err := rows.Scan(&artifact.Digest, &artifact.Size, &artifact.Locator, &artifact.ObservedAt); err != nil {
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

// Remove deletes an artifact and its locator observations from the inventory.
func (c *Catalog) Remove(digest string) error {
	if !cas.ValidDigest(digest) {
		return fmt.Errorf("catalog: invalid digest %q", digest)
	}
	return retryBusy(func() error {
		tx, err := c.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		if _, err := tx.Exec(`DELETE FROM artifact_locator WHERE digest = ?`, digest); err != nil {
			return err
		}
		result, err := tx.Exec(`DELETE FROM artifact WHERE digest = ?`, digest)
		if err != nil {
			return err
		}
		if removed, err := result.RowsAffected(); err != nil {
			return err
		} else if removed != 1 {
			return fmt.Errorf("catalog: unknown digest %s", digest)
		}
		return tx.Commit()
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

func normalizeLocators(locators []string) ([]string, error) {
	seen := make(map[string]struct{}, len(locators))
	result := make([]string, 0, len(locators))
	for _, locator := range locators {
		if _, err := parseLocator(locator); err != nil {
			return nil, err
		}
		if _, exists := seen[locator]; !exists {
			seen[locator] = struct{}{}
			result = append(result, locator)
		}
	}
	return result, nil
}

func parseLocator(locator string) (*url.URL, error) {
	u, err := url.ParseRequestURI(locator)
	if err != nil || u.Scheme == "" {
		return nil, fmt.Errorf("catalog: locator must be an absolute URI, got %q", locator)
	}
	return u, nil
}

func escapeLike(s string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(s)
}
