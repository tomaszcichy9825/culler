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
	"database/sql/driver"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
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
const schemaVersion = 2

const schema = `
CREATE TABLE IF NOT EXISTS frames (
	hash       TEXT NOT NULL,
	dir        TEXT NOT NULL,
	stem       TEXT NOT NULL,
	kind       TEXT NOT NULL,
	shot       INTEGER NOT NULL,
	raw_path   TEXT NOT NULL,
	jpeg_path  TEXT NOT NULL,
	raw_bytes  INTEGER NOT NULL,
	jpeg_bytes INTEGER NOT NULL,
	raw_mtime  INTEGER NOT NULL DEFAULT 0,
	jpeg_mtime INTEGER NOT NULL DEFAULT 0,
	rating     INTEGER NOT NULL,
	verdict    TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	indexed_at INTEGER NOT NULL,
	PRIMARY KEY (hash, dir, stem)
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
// Frames are keyed on (hash, dir, stem), the same identity the decision
// store uses: the content and the place it was seen. A file copied into two
// indexed roots is two rows,
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

// registerULower gives every connection a Unicode-aware lower(). SQLite's own
// folds ASCII and nothing else, which would leave a stem like MÜNCHEN_001
// unfindable by any casing of its own name. Registration is per-driver and
// once per process; it has to land before the first connection is made,
// because a connection picks its functions up as it opens.
var registerULower = sync.OnceValue(func() error {
	return sqlite.RegisterDeterministicScalarFunction("ulower", 1,
		func(ctx *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if s, ok := args[0].(string); ok {
				return strings.ToLower(s), nil
			}
			return args[0], nil
		})
})

// Open opens the catalogue at path, creating the file and the schema if they
// are not there yet.
func Open(path string) (*Store, error) {
	if err := registerULower(); err != nil {
		return nil, fmt.Errorf("catalog: register ulower: %w", err)
	}
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
	// Version 2 gave every frame the modification times of its files, so a
	// rerun can tell an untouched file from a rewritten one without reading it.
	// A database from version 1 gets the columns holding zero, which matches no
	// real file: the next pass re-reads everything once and is incremental
	// after that. The columns are added by inspection rather than by version,
	// because a fresh database has already got them from the schema above.
	held, err := columns(db, "frames")
	if err != nil {
		return err
	}
	for _, name := range []string{"raw_mtime", "jpeg_mtime"} {
		if held[name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE frames ADD COLUMN ` + name + ` INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("catalog: add %s: %w", name, err)
		}
	}
	if err := migrateFramesToCompositeKey(db); err != nil {
		return err
	}
	if err := collapseNestedRoots(db); err != nil {
		return err
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

// migrateFramesToCompositeKey rebuilds a frames table whose primary key was
// the hash alone onto (hash, dir, stem). Rows come across unchanged — the old
// key guaranteed one row per hash, so no two can collide under the wider key.
func migrateFramesToCompositeKey(db *sql.DB) error {
	var pkCols int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('frames') WHERE pk > 0`).Scan(&pkCols); err != nil {
		return fmt.Errorf("catalog: read frames key: %w", err)
	}
	if pkCols != 1 {
		return nil // fresh database, or already on the composite key
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmts := []string{
		`DROP INDEX IF EXISTS frames_dir`,
		`DROP INDEX IF EXISTS frames_shot`,
		`ALTER TABLE frames RENAME TO frames_old`,
		schema,
		`INSERT INTO frames (hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes,
		                     raw_mtime, jpeg_mtime, rating, verdict, indexed_at)
		 SELECT hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes,
		        raw_mtime, jpeg_mtime, rating, verdict, indexed_at FROM frames_old`,
		`DROP TABLE frames_old`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("catalog: migrate frames to composite key: %w", err)
		}
	}
	return tx.Commit()
}

// collapseNestedRoots forgets any root that another root already contains.
//
// It runs on every Open rather than behind a version, because the rows it
// cleans up are not tied to a schema: a catalogue written before AddRoot kept
// the roots apart can hold a folder and its parent at once, and the user who
// is already in that state sees both at the top of the tree, one a superset of
// the other. On a catalogue that is already in order it does nothing.
//
// Only the root rows go. Every frame the forgotten roots covered sits inside
// the root that absorbed them, so nothing is uncatalogued by this.
func collapseNestedRoots(db *sql.DB) error {
	rows, err := rootPaths(db)
	if err != nil {
		return err
	}
	for _, path := range rows {
		for _, other := range rows {
			if other == path || !under(path, other) {
				continue
			}
			if _, err := db.Exec(`DELETE FROM roots WHERE path = ?`, path); err != nil {
				return fmt.Errorf("catalog: collapse root %s into %s: %w", path, other, err)
			}
			break
		}
	}
	return nil
}

// columns returns the column names of table, empty when the table does not
// exist.
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s columns: %w", table, err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// chunk is how many hashes go into one statement. The driver's parameter
// ceiling is 32766, and a batch is kept far below it rather than risking the
// one statement that trips it.
const chunk = 500

// FrameKey names one catalogued frame: the content and the place it was seen,
// which is the identity rows are keyed on.
type FrameKey struct {
	Hash string
	Dir  string
	Stem string
}

// RemoveFrames forgets these frames. Keys the catalogue does not hold are
// ignored, so a caller does not have to know what was catalogued.
//
// This is what an apply calls once the files are in the trash. Waiting for the
// next index pass would leave rows describing files that are gone, and a
// search result the user cannot open is worse than one that is missing. The
// removal is scoped to the exact frame — a byte-identical twin in another
// folder keeps its row, because its files never moved.
func (s *Store) RemoveFrames(keys []FrameKey) error {
	if len(keys) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, k := range keys {
		if _, err := tx.Exec(
			`DELETE FROM frames WHERE hash = ? AND dir = ? AND stem = ?`,
			k.Hash, k.Dir, k.Stem); err != nil {
			return fmt.Errorf("catalog: forget %s: %w", k.Stem, err)
		}
	}
	return tx.Commit()
}

// Decision is one frame's judgement as the decision store currently holds it,
// named by the frame it belongs to.
type Decision struct {
	Hash    string
	Dir     string
	Stem    string
	Verdict string // "" | keep | cut
	Rating  int
}

// SetDecisions writes fresh verdicts and ratings over what the last index pass
// recorded. Hashes the catalogue does not hold are ignored.
//
// The catalogue does not own decisions and never asks for these: the caller
// that has just read the decision store hands back what it found, so the facet
// counts and the session table stop describing a judgement the user has since
// changed. What is on screen is overlaid at read time regardless; this is what
// keeps the aggregates that are counted in SQL honest.
func (s *Store) SetDecisions(items []Decision) error {
	if len(items) == 0 {
		return nil
	}
	for _, it := range items {
		switch it.Verdict {
		case "", VerdictKeep, VerdictCut:
		default:
			return fmt.Errorf("catalog: %s cannot hold verdict %q", it.Hash, it.Verdict)
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, it := range items {
		rating := it.Rating
		if rating < 0 {
			rating = 0
		}
		if _, err := tx.Exec(
			`UPDATE frames SET verdict = ?, rating = ? WHERE hash = ? AND dir = ? AND stem = ?`,
			it.Verdict, rating, it.Hash, it.Dir, it.Stem); err != nil {
			return fmt.Errorf("catalog: record decision for %s: %w", it.Hash, err)
		}
	}
	return tx.Commit()
}

// AddRoot registers path as a folder the catalogue covers, and keeps the roots
// from overlapping. No two registered roots ever contain one another, because
// a folder that appeared both at the top of the tree and inside another root
// would be two different answers to the same question.
//
// Three things can happen, and none of them is an error:
//
//   - The path is already registered, or sits inside a root that is: nothing
//     changes and the root that covers it comes back. Re-adding does not
//     disturb when a root was added.
//   - The path contains roots that are already registered: it absorbs them.
//     Their rows go and it takes their place. The frames stay exactly as they
//     are — they are keyed on content and filed by directory, so they are
//     already under the new root and nothing has to move.
//   - Neither: it is registered beside what is there.
//
// An absorbing root starts out never indexed, even though most of what is
// under it has been. That is the truth: it also covers ground no pass has
// walked, and the walk is cheap now that an unchanged file is not re-read.
func (s *Store) AddRoot(path string) (Root, error) {
	clean, err := cleanRoot(path)
	if err != nil {
		return Root{}, err
	}

	covering, err := s.addRootTx(clean)
	if err != nil {
		return Root{}, err
	}
	// Read back outside the transaction: the store holds one connection, and
	// querying through it while a transaction is open would wait on itself.
	return s.root(covering)
}

// addRootTx does the write half of AddRoot and returns the path of the root
// that now covers clean.
func (s *Store) addRootTx(clean string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	existing, err := rootPaths(tx)
	if err != nil {
		return "", err
	}
	for _, root := range existing {
		if under(clean, root) {
			return root, nil // already covered; the deferred rollback writes nothing
		}
	}
	for _, root := range existing {
		if !under(root, clean) {
			continue
		}
		if _, err := tx.Exec(`DELETE FROM roots WHERE path = ?`, root); err != nil {
			return "", fmt.Errorf("catalog: absorb root %s into %s: %w", root, clean, err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO roots (path, added_at, last_indexed_at) VALUES (?, ?, 0)
		 ON CONFLICT(path) DO NOTHING`, clean, time.Now().Unix()); err != nil {
		return "", fmt.Errorf("catalog: add root %s: %w", clean, err)
	}
	return clean, tx.Commit()
}

// RemoveRoot forgets a root and the frames under it. Nothing on disk is
// touched, and removing a root that was never added is not an error.
//
// No surviving root can be holding those frames up: registered roots never
// contain one another, so a frame under this one is under no other.
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
	where, args := underRoot(clean)
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

// under reports whether path is root or sits inside it.
//
// The comparison is on whole path segments, so /Volumes/CardTwo is not treated
// as living inside /Volumes/Card. It is the same rule the sidebar's tree uses,
// and the two have to agree: a folder that counted as covered in one place and
// not the other would be a root here and a child there.
func under(path, root string) bool {
	if path == root {
		return true
	}
	sep := string(filepath.Separator)
	if root == sep {
		return strings.HasPrefix(path, sep)
	}
	return strings.HasPrefix(path, root+sep)
}

// underRoot builds the predicate for "this frame's directory is the root or
// sits inside it".
//
// It compares a prefix rather than using LIKE or GLOB, because a real folder
// name is allowed to contain every wildcard either of those understands.
// SQLite's substr counts characters, so the length is measured in runes.
func underRoot(root string) (string, []any) {
	sep := string(filepath.Separator)
	prefix := root + sep
	// The filesystem root already ends in the separator; doubling it would
	// build a prefix no path carries. It is the same special case under makes.
	if root == sep {
		prefix = sep
	}
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
