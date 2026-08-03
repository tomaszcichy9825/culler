// Package catalog is the library index: what the user has shot, where it
// lives, and how much of the disk it is holding. It answers the three LIBRARY
// questions — search across every indexed folder, which shoots those frames
// belong to, and what is on which volume.
//
// The index is a SQLite database in the app's data directory. It is never
// written to the card being culled, and nothing in it is authoritative: every
// row can be rebuilt by walking the roots again, so a corrupt or deleted
// catalogue costs a reindex and nothing else.
package catalog

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// Verdict values a frame can carry in the catalogue. They mirror the decision
// store's vocabulary, with a name for the absence that facets can ask for.
const (
	VerdictKeep = "keep"
	VerdictCut  = "cut"
	// VerdictNone asks a facet for the frames nobody has judged. It is never
	// stored: an unjudged frame holds the empty string.
	VerdictNone = "undecided"
)

// schemaVersion is what this build writes. A database carrying a higher one
// was written by a newer culler and is not opened: guessing at a schema we do
// not know would be a good way to lose someone's catalogue.
const schemaVersion = 1

const schema = `
CREATE TABLE IF NOT EXISTS frames (
	hash       TEXT PRIMARY KEY,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	kind       TEXT NOT NULL,
	shot       INTEGER NOT NULL,
	raw_path   TEXT NOT NULL,
	jpeg_path  TEXT NOT NULL,
	raw_bytes  INTEGER NOT NULL,
	jpeg_bytes INTEGER NOT NULL,
	rating     INTEGER NOT NULL,
	verdict    TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	indexed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS frames_dir  ON frames(dir);
CREATE INDEX IF NOT EXISTS frames_shot ON frames(shot);

CREATE TABLE IF NOT EXISTS roots (
	path            TEXT PRIMARY KEY,
	added_at        INTEGER NOT NULL,
	last_indexed_at INTEGER NOT NULL
);
`

// Store is the catalogue database.
//
// Frames are keyed on the same content hash the decision store uses, so a
// frame keeps its row through a rename and loses it on an edit. One
// consequence worth knowing: a file copied into two indexed roots is one row,
// filed under whichever root indexed it last.
type Store struct {
	db *sql.DB
}

// Root is one folder the user has asked the catalogue to cover, with what it
// currently holds.
type Root struct {
	Path          string
	Volume        string
	AddedAt       time.Time
	LastIndexedAt time.Time // zero until the first index pass finishes
	Frames        int
	RawBytes      int64
	JpegBytes     int64
}

// Frame is one catalogued frame.
//
// The file paths are held as well as the folder, because the preview route is
// keyed on an exact path: a catalogued frame that cannot name its files cannot
// be drawn. They are what was true at index time and may since have moved,
// which is what a reindex is for.
type Frame struct {
	Hash      string
	Dir       string
	Stem      string
	Kind      string // paired | jpeg-only | raw-only
	Shot      time.Time
	RawPath   string // empty when there is no RAW
	JpegPath  string // empty when there is no JPEG
	RawBytes  int64
	JpegBytes int64
	Rating    int
	Verdict   string // "" | keep | cut
}

// Bytes is everything the frame occupies on disk.
func (f Frame) Bytes() int64 { return f.RawBytes + f.JpegBytes }

// Open opens the catalogue at path, creating the file and the schema if they
// are not there yet.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection, for the same reason the decision store keeps one: the
	// index pass writes in batches while the UI reads, and SQLITE_BUSY between
	// them is not worth the concurrency on a database this small.
	db.SetMaxOpenConns(1)

	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("catalog: %s: %w", pragma, err)
		}
	}
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migrate brings the database up to schemaVersion. It runs on every Open and
// is a no-op on a database that is already current.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return fmt.Errorf("catalog: read schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("catalog: database is at schema %d, this build understands %d — upgrade culler",
			version, schemaVersion)
	}
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("catalog: create schema: %w", err)
	}
	// Steps for future versions go here, each guarded by the version it lifts
	// the database out of, before the version is stamped.
	if version < schemaVersion {
		if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", schemaVersion)); err != nil {
			return fmt.Errorf("catalog: stamp schema version: %w", err)
		}
	}
	return nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// AddRoot registers path as a folder the catalogue covers. Adding a root that
// is already there is not an error and does not disturb when it was added; it
// returns the root either way.
func (s *Store) AddRoot(path string) (Root, error) {
	clean, err := cleanRoot(path)
	if err != nil {
		return Root{}, err
	}
	_, err = s.db.Exec(
		`INSERT INTO roots (path, added_at, last_indexed_at) VALUES (?, ?, 0)
		 ON CONFLICT(path) DO NOTHING`, clean, time.Now().Unix())
	if err != nil {
		return Root{}, fmt.Errorf("catalog: add root %s: %w", clean, err)
	}
	return s.root(clean)
}

// RemoveRoot forgets a root and the frames only it covered. Frames that
// another registered root still covers stay, because the user has not stopped
// asking for them. Removing a root that was never added is not an error.
func (s *Store) RemoveRoot(path string) error {
	clean, err := cleanRoot(path)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM roots WHERE path = ?`, clean); err != nil {
		return fmt.Errorf("catalog: remove root %s: %w", clean, err)
	}

	survivors, err := rootPaths(tx)
	if err != nil {
		return err
	}
	where, args := underRoot(clean)
	for _, other := range survivors {
		clause, extra := underRoot(other)
		where += " AND NOT " + clause
		args = append(args, extra...)
	}
	if _, err := tx.Exec(`DELETE FROM frames WHERE `+where, args...); err != nil {
		return fmt.Errorf("catalog: prune frames under %s: %w", clean, err)
	}
	return tx.Commit()
}

// Roots returns every registered root, oldest first, with what it holds.
func (s *Store) Roots() ([]Root, error) {
	paths, err := rootPaths(s.db)
	if err != nil {
		return nil, err
	}
	out := make([]Root, 0, len(paths))
	for _, p := range paths {
		r, err := s.root(p)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// root reads one root and totals what sits under it.
func (s *Store) root(path string) (Root, error) {
	r := Root{Path: path, Volume: volumeOf(path)}
	var added, indexed int64
	err := s.db.QueryRow(
		`SELECT added_at, last_indexed_at FROM roots WHERE path = ?`, path).Scan(&added, &indexed)
	if err == sql.ErrNoRows {
		return Root{}, fmt.Errorf("catalog: %s is not a registered root", path)
	}
	if err != nil {
		return Root{}, err
	}
	r.AddedAt = time.Unix(added, 0)
	if indexed > 0 {
		r.LastIndexedAt = time.Unix(indexed, 0)
	}

	where, args := underRoot(path)
	err = s.db.QueryRow(
		`SELECT COUNT(*), COALESCE(SUM(raw_bytes),0), COALESCE(SUM(jpeg_bytes),0)
		 FROM frames WHERE `+where, args...).Scan(&r.Frames, &r.RawBytes, &r.JpegBytes)
	if err != nil {
		return Root{}, fmt.Errorf("catalog: total root %s: %w", path, err)
	}
	return r, nil
}

// querier is the part of *sql.DB and *sql.Tx this package reads through.
type querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func rootPaths(q querier) ([]string, error) {
	rows, err := q.Query(`SELECT path FROM roots ORDER BY added_at, path`)
	if err != nil {
		return nil, fmt.Errorf("catalog: read roots: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// cleanRoot normalises a root path. Roots must be absolute: everything the
// catalogue does with them — prefix matching, volume rollups, pruning — is
// wrong the moment two roots can mean different folders from different working
// directories.
func cleanRoot(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("catalog: no root given")
	}
	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("catalog: root %s is not an absolute path", path)
	}
	return filepath.Clean(path), nil
}

// underRoot builds the predicate for "this frame's directory is the root or
// sits inside it".
//
// It compares a prefix rather than using LIKE or GLOB, because a real folder
// name is allowed to contain every wildcard either of those understands.
// SQLite's substr counts characters, so the length is measured in runes.
func underRoot(root string) (string, []any) {
	prefix := root + string(filepath.Separator)
	return "(dir = ? OR substr(dir, 1, ?) = ?)",
		[]any{root, utf8.RuneCountInString(prefix), prefix}
}

// volumeOf names the volume a path lives on, lexically — no mount table is
// consulted, and no build-tagged platform code is needed for a rollup whose
// only job is to group roots the user recognises as separate disks.
//
// Removable media and network shares mount under a known parent on each
// platform (/Volumes on macOS, /media and /mnt on Linux, a drive letter on
// Windows); anything else is on the system volume.
func volumeOf(path string) string {
	if v := filepath.VolumeName(path); v != "" {
		return v + string(filepath.Separator)
	}
	clean := filepath.Clean(path)
	for _, parent := range []string{"/Volumes", "/media", "/mnt", "/run/media"} {
		prefix := parent + "/"
		if !strings.HasPrefix(clean, prefix) {
			continue
		}
		rest := clean[len(prefix):]
		if rest == "" {
			continue
		}
		// /media/<user>/<card> on Linux puts the user in the way of the volume.
		parts := strings.Split(rest, "/")
		if (parent == "/media" || parent == "/run/media") && len(parts) > 1 {
			return prefix + parts[0] + "/" + parts[1]
		}
		return prefix + parts[0]
	}
	return "/"
}
