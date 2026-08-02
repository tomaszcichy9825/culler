// Package decide stores the culling decisions made in the UI. Decisions are
// short-lived — they exist for the minutes between marking frames and applying
// the operation — but they must survive a rename, a reopened folder and a
// crash, so they live in a small SQLite database in the app's config dir and
// never on the card being culled.
package decide

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Decision is what the user asked for on one frame.
type Decision string

const (
	None     Decision = "none"
	KeepAll  Decision = "keep_all"
	DropRAW  Decision = "drop_raw"
	DropJPEG Decision = "drop_jpeg"
	DropAll  Decision = "drop_all"
)

// valid reports whether d is a decision the store will accept. None is valid
// and means "undecided": storing it removes the record.
func (d Decision) valid() bool {
	switch d {
	case None, KeepAll, DropRAW, DropJPEG, DropAll:
		return true
	}
	return false
}

// Item is one decision in a batch.
type Item struct {
	Hash string
	Dir  string
	Stem string
	D    Decision
}

// Store is the decision database.
//
// Rows are keyed on the content hash rather than the path, which is what makes
// a decision survive a rename and correctly not survive an edit. Dir and stem
// come along so the grid can load a folder's decisions in one query.
type Store struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS decisions (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	decision   TEXT NOT NULL CHECK (decision IN ('keep_all','drop_raw','drop_jpeg','drop_all')),
	updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_dir ON decisions(dir);
`

// Open opens the decision database at path, creating the file and schema if
// they are not there yet.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection: the writes are tiny and always from the UI, and this
	// removes SQLITE_BUSY between the ticker batch and a folder load.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("decide: %s: %w", pragma, err)
		}
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("decide: create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

const upsertSQL = `
INSERT INTO decisions (hash, dir, stem, decision, updated_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	decision = excluded.decision,
	updated_at = excluded.updated_at
`

// Set records decision d for the frame whose primary file hashes to hash,
// remembering the directory and stem it was last seen at. Setting None clears
// the decision; clearing an undecided frame is not an error.
func (s *Store) Set(hash, dir, stem string, d Decision) error {
	return s.inTx(func(tx *sql.Tx) error {
		return apply(tx, Item{Hash: hash, Dir: dir, Stem: stem, D: d})
	})
}

// SetBatch applies many decisions in one transaction. The UI marks frames far
// faster than it should touch the disk, so it collects them on a ticker and
// flushes through here. Either the whole batch lands or none of it does.
func (s *Store) SetBatch(items []Item) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, it := range items {
			if err := apply(tx, it); err != nil {
				return err
			}
		}
		return nil
	})
}

// apply writes one item inside a transaction.
func apply(tx *sql.Tx, it Item) error {
	if !it.D.valid() {
		return fmt.Errorf("decide: unknown decision %q for %s", it.D, it.Stem)
	}
	if it.D == None {
		_, err := tx.Exec(`DELETE FROM decisions WHERE hash = ?`, it.Hash)
		return err
	}
	_, err := tx.Exec(upsertSQL, it.Hash, it.Dir, it.Stem, string(it.D), time.Now().Unix())
	return err
}

// Get returns the decision recorded for a content hash. The second result is
// false when the frame is undecided.
func (s *Store) Get(hash string) (Decision, bool, error) {
	var d string
	err := s.db.QueryRow(`SELECT decision FROM decisions WHERE hash = ?`, hash).Scan(&d)
	if err == sql.ErrNoRows {
		return None, false, nil
	}
	if err != nil {
		return None, false, err
	}
	return Decision(d), true, nil
}

// ForDir returns stem to decision for every decided frame last seen in dir.
// This is the one query the grid runs when a folder is opened.
func (s *Store) ForDir(dir string) (map[string]Decision, error) {
	rows, err := s.db.Query(
		`SELECT stem, decision FROM decisions WHERE dir = ? ORDER BY updated_at`, dir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]Decision)
	for rows.Next() {
		var stem, d string
		if err := rows.Scan(&stem, &d); err != nil {
			return nil, err
		}
		// Ordered by updated_at, so if an edited file left a stale row under
		// its old hash the newest decision for that stem wins.
		out[stem] = Decision(d)
	}
	return out, rows.Err()
}

// Clear wipes every decision. Used when the user discards a session rather
// than applying it.
func (s *Store) Clear() error {
	_, err := s.db.Exec(`DELETE FROM decisions`)
	return err
}

// inTx runs fn in a transaction, rolling back on any error.
func (s *Store) inTx(fn func(*sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
