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

// Verdict is what the user asked for on one frame: keep it, cut it, or nothing
// yet. Which halves of a pair a keep applies to is the mask's business.
type Verdict string

const (
	Undecided Verdict = ""
	Keep      Verdict = "keep"
	Cut       Verdict = "cut"
)

// Mask says which halves of a RAW+JPEG pair survive a keep. A cut reads it the
// other way round when the user has asked for cuts to remove only the masked
// out halves.
type Mask string

const (
	MaskBoth Mask = "rj"
	MaskRAW  Mask = "r"
	MaskJPEG Mask = "j"
)

// MaxRating is the top of the star scale. Zero means unrated.
const MaxRating = 5

// Record is everything the store remembers about one frame. A record with no
// verdict, no rating and no destination is not stored at all.
type Record struct {
	Verdict Verdict
	Mask    Mask
	Rating  int
	// Destination is where the frame is routed on apply: a library-relative or
	// absolute directory, possibly holding token templates. Empty means the
	// frame goes nowhere and stays where it is.
	Destination string
}

// VerdictItem is one frame's verdict in a batch.
type VerdictItem struct {
	Hash    string
	Dir     string
	Stem    string
	Verdict Verdict
	Mask    Mask
}

// RatingItem is one frame's rating in a batch.
type RatingItem struct {
	Hash   string
	Dir    string
	Stem   string
	Rating int
}

// DestinationItem is one frame's destination in a batch.
type DestinationItem struct {
	Hash        string
	Dir         string
	Stem        string
	Destination string
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
	hash        TEXT PRIMARY KEY,
	dir         TEXT NOT NULL,
	stem        TEXT NOT NULL,
	verdict     TEXT NOT NULL CHECK (verdict IN ('','keep','cut')),
	mask        TEXT NOT NULL CHECK (mask IN ('rj','r','j')),
	rating      INTEGER NOT NULL CHECK (rating BETWEEN 0 AND 5),
	destination TEXT NOT NULL DEFAULT '',
	updated_at  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_dir ON decisions(dir);
CREATE TABLE IF NOT EXISTS destinations (
	path         TEXT PRIMARY KEY,
	label        TEXT NOT NULL DEFAULT '',
	last_used_at INTEGER NOT NULL DEFAULT 0,
	use_count    INTEGER NOT NULL DEFAULT 0,
	pinned       INTEGER NOT NULL DEFAULT 0,
	slot         INTEGER
);
`

// Open opens the decision database at path, creating the file and schema if
// they are not there yet and migrating a database written by an older build.
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
	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("decide: create schema: %w", err)
	}
	return &Store{db: db}, nil
}

// migrate brings a database written by an older build up to the current
// schema, oldest step first. Every step is a no-op on a database that has
// already been through it, so the chain runs on every Open.
func migrate(db *sql.DB) error {
	if err := migrateToVerdicts(db); err != nil {
		return err
	}
	return migrateToDestinations(db)
}

// migrateToVerdicts rewrites a database that still holds the single-column
// decision model into verdicts, masks and ratings.
//
// The four old decisions each describe a keep with a mask, except drop_all
// which is a cut. Anything else — an undecided row, or a value from a
// hand-edited database — has no verdict to migrate to and is dropped.
func migrateToVerdicts(db *sql.DB) error {
	cols, err := columns(db, "decisions")
	if err != nil {
		return err
	}
	if len(cols) == 0 || cols["verdict"] {
		return nil // fresh database, or already migrated
	}
	if !cols["decision"] {
		return fmt.Errorf("decide: decisions table has neither a decision nor a verdict column")
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// The index follows the table through a rename, so it has to go before the
	// new schema can claim its name.
	stmts := []string{
		`DROP INDEX IF EXISTS decisions_dir`,
		`ALTER TABLE decisions RENAME TO decisions_old`,
		schema,
		`INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, destination, updated_at)
		 SELECT hash, dir, stem,
		        CASE decision WHEN 'drop_all' THEN 'cut' ELSE 'keep' END,
		        CASE decision
		             WHEN 'drop_raw'  THEN 'j'
		             WHEN 'drop_jpeg' THEN 'r'
		             ELSE 'rj'
		        END,
		        0, '', updated_at
		 FROM decisions_old
		 WHERE decision IN ('keep_all','drop_raw','drop_jpeg','drop_all')`,
		`DROP TABLE decisions_old`,
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("decide: migrate decisions: %w", err)
		}
	}
	return tx.Commit()
}

// migrateToDestinations adds the per-frame destination to a verdict-era
// database. The destinations table itself needs no migration: Open creates it
// if it is missing, and there is nothing to carry over into it.
func migrateToDestinations(db *sql.DB) error {
	cols, err := columns(db, "decisions")
	if err != nil {
		return err
	}
	if len(cols) == 0 || cols["destination"] {
		return nil // fresh database, or already migrated
	}
	if _, err := db.Exec(`ALTER TABLE decisions ADD COLUMN destination TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("decide: add destination column: %w", err)
	}
	return nil
}

// columns returns the column names of table, empty when the table does not
// exist.
func columns(db *sql.DB, table string) (map[string]bool, error) {
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, fmt.Errorf("decide: read %s columns: %w", table, err)
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

// upsertVerdictSQL writes a verdict. Clearing one takes the destination with
// it: a frame nobody is keeping is not being routed anywhere either.
const upsertVerdictSQL = `
INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, destination, updated_at)
VALUES (?, ?, ?, ?, ?, 0, '', ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	verdict = excluded.verdict,
	mask = excluded.mask,
	destination = CASE WHEN excluded.verdict = '' THEN '' ELSE decisions.destination END,
	updated_at = excluded.updated_at
`

const upsertRatingSQL = `
INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, destination, updated_at)
VALUES (?, ?, ?, '', 'rj', ?, '', ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	rating = excluded.rating,
	updated_at = excluded.updated_at
`

// upsertDestinationSQL routes a frame. Naming a destination is a way of saying
// the frame is worth keeping, so an undecided frame becomes a keep — the same
// implication a mask toggle carries. A verdict the user has actually typed is
// left exactly as it is, including a cut.
const upsertDestinationSQL = `
INSERT INTO decisions (hash, dir, stem, verdict, mask, rating, destination, updated_at)
VALUES (?, ?, ?, ?, 'rj', 0, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	verdict = CASE
		WHEN decisions.verdict = '' AND excluded.destination <> '' THEN 'keep'
		ELSE decisions.verdict
	END,
	destination = excluded.destination,
	updated_at = excluded.updated_at
`

// pruneSQL drops a row that now says nothing: no verdict, no rating and
// nowhere to go.
const pruneSQL = `DELETE FROM decisions WHERE hash = ? AND verdict = '' AND rating = 0 AND destination = ''`

// SetVerdict records verdict v with mask m for the frame whose primary file
// hashes to hash, remembering the directory and stem it was last seen at.
// Passing Undecided clears the verdict; the frame keeps its rating, and the
// row goes only when there is no rating left to hold it.
func (s *Store) SetVerdict(hash, dir, stem string, v Verdict, m Mask) error {
	return s.inTx(func(tx *sql.Tx) error {
		return applyVerdict(tx, VerdictItem{Hash: hash, Dir: dir, Stem: stem, Verdict: v, Mask: m})
	})
}

// SetVerdictBatch applies many verdicts in one transaction. The UI marks
// frames far faster than it should touch the disk, so it collects them on a
// ticker and flushes through here. Either the whole batch lands or none of it
// does.
func (s *Store) SetVerdictBatch(items []VerdictItem) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, it := range items {
			if err := applyVerdict(tx, it); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetRating records a 1–5 star rating, or 0 to clear it. A rating is a
// judgement about the photograph rather than about the cull, so it is stored
// and cleared independently of the verdict: rating an undecided frame is
// legal and keeps its row.
func (s *Store) SetRating(hash, dir, stem string, rating int) error {
	return s.inTx(func(tx *sql.Tx) error {
		return applyRating(tx, RatingItem{Hash: hash, Dir: dir, Stem: stem, Rating: rating})
	})
}

// SetRatingBatch applies many ratings in one transaction, all or nothing.
func (s *Store) SetRatingBatch(items []RatingItem) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, it := range items {
			if err := applyRating(tx, it); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetDestination routes a frame to dest, or clears its routing when dest is
// empty. Clearing the destination leaves the verdict alone: the frame is still
// being kept, it just stays where it is.
func (s *Store) SetDestination(hash, dir, stem, dest string) error {
	return s.inTx(func(tx *sql.Tx) error {
		return applyDestination(tx, DestinationItem{Hash: hash, Dir: dir, Stem: stem, Destination: dest})
	})
}

// SetDestinationBatch routes many frames in one transaction, all or nothing.
// Routing a whole selection is one keystroke, so it arrives as one batch.
func (s *Store) SetDestinationBatch(items []DestinationItem) error {
	return s.inTx(func(tx *sql.Tx) error {
		for _, it := range items {
			if err := applyDestination(tx, it); err != nil {
				return err
			}
		}
		return nil
	})
}

// applyVerdict writes one verdict inside a transaction.
func applyVerdict(tx *sql.Tx, it VerdictItem) error {
	if err := validVerdict(it.Verdict, it.Mask, it.Stem); err != nil {
		return err
	}
	mask := it.Mask
	if mask == "" {
		mask = MaskBoth
	}
	if _, err := tx.Exec(upsertVerdictSQL,
		it.Hash, it.Dir, it.Stem, string(it.Verdict), string(mask), time.Now().Unix()); err != nil {
		return err
	}
	_, err := tx.Exec(pruneSQL, it.Hash)
	return err
}

// applyDestination writes one destination inside a transaction. A frame with
// no identity hash is refused rather than stored: every such frame would share
// the one empty key and so route the rest of them wherever the last one went.
func applyDestination(tx *sql.Tx, it DestinationItem) error {
	if it.Hash == "" {
		return fmt.Errorf("decide: no frame identity for %s: it cannot be routed", it.Stem)
	}
	verdict := ""
	if it.Destination != "" {
		verdict = string(Keep)
	}
	if _, err := tx.Exec(upsertDestinationSQL,
		it.Hash, it.Dir, it.Stem, verdict, it.Destination, time.Now().Unix()); err != nil {
		return err
	}
	_, err := tx.Exec(pruneSQL, it.Hash)
	return err
}

// applyRating writes one rating inside a transaction.
func applyRating(tx *sql.Tx, it RatingItem) error {
	if it.Rating < 0 || it.Rating > MaxRating {
		return fmt.Errorf("decide: rating %d for %s is off the 0-%d scale", it.Rating, it.Stem, MaxRating)
	}
	if _, err := tx.Exec(upsertRatingSQL,
		it.Hash, it.Dir, it.Stem, it.Rating, time.Now().Unix()); err != nil {
		return err
	}
	_, err := tx.Exec(pruneSQL, it.Hash)
	return err
}

// validVerdict reports whether the store will accept this verdict and mask.
// Undecided needs no mask, because it is on its way to being deleted; keep and
// cut both carry one, since a cut reads the mask too when the user has asked
// for cuts to remove only the masked out halves.
func validVerdict(v Verdict, m Mask, stem string) error {
	switch v {
	case Undecided:
		if m != "" && !m.valid() {
			return fmt.Errorf("decide: unknown mask %q for %s", m, stem)
		}
		return nil
	case Keep, Cut:
		if !m.valid() {
			return fmt.Errorf("decide: unknown mask %q for %s: want %q, %q or %q",
				m, stem, MaskBoth, MaskRAW, MaskJPEG)
		}
		return nil
	}
	return fmt.Errorf("decide: unknown verdict %q for %s", v, stem)
}

func (m Mask) valid() bool {
	switch m {
	case MaskBoth, MaskRAW, MaskJPEG:
		return true
	}
	return false
}

// Get returns the record held for a content hash. The second result is false
// when the frame is neither decided nor rated.
func (s *Store) Get(hash string) (Record, bool, error) {
	var r Record
	err := s.db.QueryRow(
		`SELECT verdict, mask, rating, destination FROM decisions WHERE hash = ?`, hash,
	).Scan(&r.Verdict, &r.Mask, &r.Rating, &r.Destination)
	if err == sql.ErrNoRows {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, err
	}
	return r, true, nil
}

// ForDir returns stem to record for every decided or rated frame last seen in
// dir. This is the one query the grid runs when a folder is opened.
func (s *Store) ForDir(dir string) (map[string]Record, error) {
	rows, err := s.db.Query(
		`SELECT stem, verdict, mask, rating, destination FROM decisions WHERE dir = ? ORDER BY updated_at`, dir)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]Record)
	for rows.Next() {
		var stem string
		var r Record
		if err := rows.Scan(&stem, &r.Verdict, &r.Mask, &r.Rating, &r.Destination); err != nil {
			return nil, err
		}
		// Ordered by updated_at, so if an edited file left a stale row under
		// its old hash the newest record for that stem wins.
		out[stem] = r
	}
	return out, rows.Err()
}

// Clear wipes every record. Used when the user discards a session rather than
// applying it.
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
