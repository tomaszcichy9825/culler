package catalog

import (
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tomaszcichy9825/culler/internal/hash"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// Progress is what an index pass reports as it goes. Dirs and Frames are
// cumulative for the pass, so they only ever climb; Done marks the last
// report for a root.
//
// There is no total. Counting the tree before walking it would mean reading
// every directory twice, and on a card reader that is the expensive half of
// the whole operation.
type Progress struct {
	Root   string
	Dir    string
	Dirs   int
	Frames int
	Done   bool
}

// Stats is what an index pass found.
//
// Frames is everything the pass accounted for, whether it had to read it or
// not. Changed and Removed are what the pass actually did: on a rerun over an
// untouched card both are zero, which is the point of recording sizes and
// modification times in the first place.
type Stats struct {
	Dirs    int
	Frames  int
	Changed int
	Removed int
}

func (s *Stats) add(other Stats) {
	s.Dirs += other.Dirs
	s.Frames += other.Frames
	s.Changed += other.Changed
	s.Removed += other.Removed
}

// IndexOptions tunes one index pass.
type IndexOptions struct {
	// Scan is the extension configuration. The zero value takes the defaults.
	Scan scan.Config

	// Workers caps concurrent identity hashes. Zero means one per CPU, which
	// is right for a local disk and wrong for a network share, where parallel
	// head reads stall each other — pass the configured network cap there,
	// the same way a folder open does.
	Workers int

	// Lookup returns the verdict and rating already recorded for a frame, so
	// the catalogue can show what was decided without owning the decisions.
	// Nil leaves every frame unjudged.
	Lookup func(hash string) (verdict string, rating int)

	// Progress is called after each directory and once more at the end. It
	// runs on the indexing goroutine, so it must not block for long.
	Progress func(Progress)
}

func (o IndexOptions) scanConfig() scan.Config {
	if len(o.Scan.RawExts) == 0 && len(o.Scan.JpegExts) == 0 {
		return scan.DefaultConfig()
	}
	return o.Scan
}

func (o IndexOptions) workers() int {
	if o.Workers < 1 {
		return runtime.NumCPU()
	}
	return o.Workers
}

// Index walks root and brings the catalogue in line with what is on disk:
// frames that are new arrive, frames that changed are rewritten, and frames
// that have left are dropped. It registers root if it is not registered yet,
// so opening a folder in the library is enough to start covering it.
//
// The walk streams. One directory is scanned, hashed and written at a time, so
// a card with fifty thousand frames does not have to fit in memory before the
// first row lands.
//
// It is also incremental. A frame whose files still carry the size and
// modification time the catalogue recorded is left alone — not re-read, not
// rewritten — so a rerun over a card that has not moved costs a directory
// listing per folder and nothing else. The cost of that is a file rewritten
// with the same length and the same timestamp, which the pass will not notice;
// nothing else the app does can produce one, and a reindex after emptying the
// catalogue rebuilds it from scratch either way.
func (s *Store) Index(root string, opts IndexOptions) (Stats, error) {
	clean, err := cleanRoot(root)
	if err != nil {
		return Stats{}, err
	}
	info, err := os.Stat(clean)
	if err != nil {
		return Stats{}, fmt.Errorf("catalog: index %s: %w", clean, err)
	}
	if !info.IsDir() {
		return Stats{}, fmt.Errorf("catalog: %s is not a folder", clean)
	}
	if _, err := s.AddRoot(clean); err != nil {
		return Stats{}, err
	}

	cfg := opts.scanConfig()
	workers := opts.workers()
	var stats Stats
	// Every directory the walk reached, so the prune afterwards can tell a
	// directory that is now empty from one that is no longer there.
	walked := make([]string, 0, 64)
	// Every directory the walk could not list. Their subtrees are unknown, not
	// gone, and the prune must leave them exactly as they were.
	var failed []string

	err = filepath.WalkDir(clean, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is skipped rather than failing
			// the pass: one unreadable folder on a card should not cost the
			// user the other forty. Its frames stay catalogued — unreadable is
			// not the same as gone.
			failed = append(failed, path)
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		// Hidden directories hold no photographs, and .Trashes on a card holds
		// files the user has already thrown away.
		if path != clean && strings.HasPrefix(d.Name(), ".") {
			return fs.SkipDir
		}

		groups, err := scan.ScanDir(path, cfg)
		if err != nil {
			// A listing that failed keeps its frames, for the same reason
			// UpsertDir leaves an unreadable directory alone: forgetting a
			// folder on the strength of one failed read would be the wrong
			// guess. The walk cannot see inside it either, so the whole
			// subtree is off limits to the prune.
			failed = append(failed, path)
			return fs.SkipDir
		}
		walked = append(walked, path)
		stats.Dirs++

		if len(groups) > 0 {
			done, err := s.writeDir(path, groups, workers, opts.Lookup)
			if err != nil {
				return err
			}
			stats.add(done)
		} else {
			removed, err := s.pruneDir(path, nil)
			if err != nil {
				return err
			}
			stats.Removed += removed
		}

		if opts.Progress != nil {
			opts.Progress(Progress{Root: clean, Dir: path, Dirs: stats.Dirs, Frames: stats.Frames})
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	removed, err := s.pruneMissingDirs(clean, walked, failed)
	if err != nil {
		return stats, err
	}
	stats.Removed += removed
	// Stamping only lands when clean is itself a registered root. Given a
	// folder inside one, this pass covered part of that root and not the whole
	// of it, so the root's own last-indexed time is left where it was rather
	// than claiming a walk that did not happen.
	if _, err := s.db.Exec(
		`UPDATE roots SET last_indexed_at = ? WHERE path = ?`, time.Now().Unix(), clean); err != nil {
		return stats, fmt.Errorf("catalog: stamp root %s: %w", clean, err)
	}
	if opts.Progress != nil {
		opts.Progress(Progress{Root: clean, Dirs: stats.Dirs, Frames: stats.Frames, Done: true})
	}
	return stats, nil
}

// IndexAll indexes each root in turn and returns the totals. Passing no roots
// indexes the registered ones, which is what a refresh does. A root that
// cannot be walked — an unplugged card, most often — stops the pass, with the
// roots before it already written.
func (s *Store) IndexAll(roots []string, opts IndexOptions) (Stats, error) {
	if len(roots) == 0 {
		known, err := rootPaths(s.db)
		if err != nil {
			return Stats{}, err
		}
		roots = known
	}
	var total Stats
	for _, root := range roots {
		stats, err := s.Index(root, opts)
		total.add(stats)
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// UpsertDir brings one directory in line with what is on disk without
// descending into it: the frames in it are added or refreshed, and rows for
// files that have left are dropped. It is what a folder open calls, so a card
// the user has just culled is current in the library without a walk.
//
// A directory no registered root covers is not the catalogue's business, so
// the call does nothing and says so with a zero result rather than an error:
// opening a folder in CULL must not quietly start cataloguing it.
func (s *Store) UpsertDir(dir string, opts IndexOptions) (Stats, error) {
	clean, err := cleanRoot(dir)
	if err != nil {
		return Stats{}, err
	}
	covered, err := s.covered(clean)
	if err != nil || !covered {
		return Stats{}, err
	}
	groups, err := scan.ScanDir(clean, opts.scanConfig())
	if err != nil {
		// A directory that cannot be read is left as it was. It may be a card
		// that has been unplugged since, and forgetting its frames on the
		// strength of one failed listing would be the wrong guess.
		return Stats{}, nil
	}
	if len(groups) == 0 {
		removed, err := s.pruneDir(clean, nil)
		return Stats{Dirs: 1, Removed: removed}, err
	}
	stats, err := s.writeDir(clean, groups, opts.workers(), opts.Lookup)
	stats.Dirs = 1
	return stats, err
}

// covered reports whether dir is one of the registered roots or sits inside
// one.
func (s *Store) covered(dir string) (bool, error) {
	roots, err := rootPaths(s.db)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		if under(dir, root) {
			return true, nil
		}
	}
	return false, nil
}

const upsertFrameSQL = `
INSERT INTO frames
	(hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes,
	 raw_mtime, jpeg_mtime, rating, verdict, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	kind = excluded.kind,
	shot = excluded.shot,
	raw_path = excluded.raw_path,
	jpeg_path = excluded.jpeg_path,
	raw_bytes = excluded.raw_bytes,
	jpeg_bytes = excluded.jpeg_bytes,
	raw_mtime = excluded.raw_mtime,
	jpeg_mtime = excluded.jpeg_mtime,
	rating = excluded.rating,
	verdict = excluded.verdict,
	indexed_at = excluded.indexed_at
`

// fileState is what a row remembers about one of a frame's two files. Equal
// states mean the file has not been touched since it was indexed, which is
// what lets a pass skip reading it.
type fileState struct {
	path  string
	bytes int64
	mtime int64 // unix nanoseconds, zero when there is no such file
}

// rowState is one catalogued frame as the last pass left it.
type rowState struct {
	hash    string
	raw     fileState
	jpeg    fileState
	verdict string
	rating  int
}

// stateOf reads a group's files the way a row records them.
func stateOf(g scan.PhotoGroup) (raw, jpeg fileState) {
	if g.Raw != nil {
		raw = fileState{path: g.Raw.Path, bytes: g.Raw.Size, mtime: g.Raw.ModTime.UnixNano()}
	}
	if g.Jpeg != nil {
		jpeg = fileState{path: g.Jpeg.Path, bytes: g.Jpeg.Size, mtime: g.Jpeg.ModTime.UnixNano()}
	}
	return raw, jpeg
}

// primaryPath is the file a frame is identified by, which is what a row is
// found again under. It is empty for a group with no files at all.
func primaryPath(raw, jpeg fileState) string {
	if jpeg.path != "" {
		return jpeg.path
	}
	return raw.path
}

// dirState reads back what the catalogue holds for one directory, keyed on the
// path each frame is identified by. One query per directory, and the rows are
// bounded by what fits in a folder.
func (s *Store) dirState(dir string) (map[string]rowState, error) {
	rows, err := s.db.Query(
		`SELECT hash, raw_path, jpeg_path, raw_bytes, jpeg_bytes, raw_mtime, jpeg_mtime, verdict, rating
		 FROM frames WHERE dir = ?`, dir)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", dir, err)
	}
	defer rows.Close()

	out := map[string]rowState{}
	for rows.Next() {
		var r rowState
		if err := rows.Scan(&r.hash, &r.raw.path, &r.jpeg.path, &r.raw.bytes, &r.jpeg.bytes,
			&r.raw.mtime, &r.jpeg.mtime, &r.verdict, &r.rating); err != nil {
			return nil, err
		}
		if key := primaryPath(r.raw, r.jpeg); key != "" {
			out[key] = r
		}
	}
	return out, rows.Err()
}

// writeDir brings one directory's rows in line with the groups the scan found
// and drops any row the scan did not find. Frames whose files still match what
// was recorded are neither hashed nor rewritten; when only their judgement has
// moved, the judgement alone is written.
func (s *Store) writeDir(dir string, groups []scan.PhotoGroup, workers int, lookup func(string) (string, int)) (Stats, error) {
	held, err := s.dirState(dir)
	if err != nil {
		return Stats{}, err
	}

	// Which groups still need an identity, and therefore a read of the file.
	states := make([][2]fileState, len(groups))
	known := make([]string, len(groups))
	stale := make([]scan.PhotoGroup, 0, len(groups))
	staleAt := make([]int, 0, len(groups))
	for i, g := range groups {
		raw, jpeg := stateOf(g)
		states[i] = [2]fileState{raw, jpeg}
		if row, ok := held[primaryPath(raw, jpeg)]; ok && row.raw == raw && row.jpeg == jpeg {
			known[i] = row.hash
			continue
		}
		stale = append(stale, g)
		staleAt = append(staleAt, i)
	}
	fresh := hashGroups(stale, workers)
	for i, at := range staleAt {
		known[at] = fresh[i]
	}

	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return Stats{}, err
	}
	defer tx.Rollback()

	var stats Stats
	kept := make([]string, 0, len(groups))
	rewritten := make([]bool, len(groups))
	for _, at := range staleAt {
		rewritten[at] = true
	}

	for i, g := range groups {
		hash := known[i]
		// A frame whose primary file cannot be read has no identity, so there
		// is nothing to key a row on. It is skipped rather than guessed at.
		if hash == "" {
			continue
		}
		verdict, rating := "", 0
		if lookup != nil {
			verdict, rating = lookup(hash)
		}
		if verdict != VerdictKeep && verdict != VerdictCut {
			verdict = ""
		}
		if rating < 0 {
			rating = 0
		}
		kept = append(kept, hash)
		stats.Frames++

		if !rewritten[i] {
			// The files are as they were. Only a judgement that has moved since
			// the last pass is worth a write.
			row := held[primaryPath(states[i][0], states[i][1])]
			if row.verdict != verdict || row.rating != rating {
				if _, err := tx.Exec(
					`UPDATE frames SET verdict = ?, rating = ? WHERE hash = ?`,
					verdict, rating, hash); err != nil {
					return stats, fmt.Errorf("catalog: refresh %s: %w", g.Stem, err)
				}
			}
			continue
		}

		raw, jpeg := states[i][0], states[i][1]
		if _, err := tx.Exec(upsertFrameSQL,
			hash, g.Dir, g.Stem, g.Kind.String(), g.Shot.Unix(),
			raw.path, jpeg.path, raw.bytes, jpeg.bytes, raw.mtime, jpeg.mtime,
			rating, verdict, now); err != nil {
			return stats, fmt.Errorf("catalog: write %s: %w", g.Stem, err)
		}
		stats.Changed++
	}

	if _, err := pruneDirTx(tx, dir, kept); err != nil {
		return stats, err
	}
	// Counted from the files rather than from the rows the prune touched: a
	// frame that was rewritten in place gets a new identity and so loses its
	// old row, and reporting that as a removal would make an edit look like a
	// deletion.
	onDisk := map[string]bool{}
	for i := range groups {
		onDisk[primaryPath(states[i][0], states[i][1])] = true
	}
	for path := range held {
		if !onDisk[path] {
			stats.Removed++
		}
	}
	return stats, tx.Commit()
}

// pruneDir drops every row filed under dir except the hashes given, and
// returns how many it dropped.
func (s *Store) pruneDir(dir string, keep []string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	removed, err := pruneDirTx(tx, dir, keep)
	if err != nil {
		return 0, err
	}
	return removed, tx.Commit()
}

// pruneDirTx works out the doomed rows in Go and deletes them in batches,
// rather than binding the whole keep list into one NOT IN: a flat folder can
// hold more frames than the driver has parameter slots in a statement.
func pruneDirTx(tx *sql.Tx, dir string, keep []string) (int, error) {
	if len(keep) == 0 {
		res, err := tx.Exec(`DELETE FROM frames WHERE dir = ?`, dir)
		if err != nil {
			return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
		}
		n, err := res.RowsAffected()
		return int(n), err
	}

	keepSet := make(map[string]bool, len(keep))
	for _, h := range keep {
		keepSet[h] = true
	}
	rows, err := tx.Query(`SELECT hash FROM frames WHERE dir = ?`, dir)
	if err != nil {
		return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
	}
	var doomed []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			rows.Close()
			return 0, err
		}
		if !keepSet[h] {
			doomed = append(doomed, h)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	// Closed before the deletes: the store holds one connection, and nothing
	// can execute on it while a result set is still open.
	rows.Close()

	for start := 0; start < len(doomed); start += chunk {
		batch := doomed[start:min(start+chunk, len(doomed))]
		args := make([]any, len(batch))
		for i, h := range batch {
			args[i] = h
		}
		query := `DELETE FROM frames WHERE hash IN (?` + strings.Repeat(`,?`, len(batch)-1) + `)`
		if _, err := tx.Exec(query, args...); err != nil {
			return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
		}
	}
	return len(doomed), nil
}

// pruneMissingDirs drops the frames of directories that were under root and
// are not any more. The walked list is bounded by the number of directories
// rather than the number of frames, so it goes into a temporary table instead
// of an ever-growing IN clause.
//
// A directory in failed could not be listed, which is not the same as not
// being there: everything under it — itself and the subtree the walk never
// reached — keeps its rows.
func (s *Store) pruneMissingDirs(root string, walked, failed []string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS pass_dirs (dir TEXT PRIMARY KEY)`); err != nil {
		return 0, fmt.Errorf("catalog: prepare prune: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM pass_dirs`); err != nil {
		return 0, fmt.Errorf("catalog: prepare prune: %w", err)
	}
	for _, dir := range walked {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO pass_dirs (dir) VALUES (?)`, dir); err != nil {
			return 0, fmt.Errorf("catalog: prepare prune: %w", err)
		}
	}
	where, args := underRoot(root)
	query := `DELETE FROM frames WHERE ` + where + ` AND dir NOT IN (SELECT dir FROM pass_dirs)`
	for _, dir := range failed {
		fw, fa := underRoot(dir)
		query += ` AND NOT ` + fw
		args = append(args, fa...)
	}
	res, err := tx.Exec(query, args...)
	if err != nil {
		return 0, fmt.Errorf("catalog: prune folders that left %s: %w", root, err)
	}
	removed, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(removed), tx.Commit()
}

// hashGroups returns the identity hash of every group's primary file, aligned
// with groups and empty where the file could not be read. The worker count is
// the caller's: all CPUs for a local disk, the configured low cap for a
// network volume.
func hashGroups(groups []scan.PhotoGroup, workers int) []string {
	if workers < 1 {
		workers = 1
	}
	hashes := make([]string, len(groups))
	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, g := range groups {
		ref := primaryRef(g)
		if ref == nil {
			continue
		}
		wg.Add(1)
		go func(i int, path string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if h, err := hash.Content(path); err == nil {
				hashes[i] = h
			}
		}(i, ref.Path)
	}
	wg.Wait()
	return hashes
}

// primaryRef is the file a frame is identified by: the JPEG when there is one,
// because that is the frame the user is looking at, otherwise the RAW.
func primaryRef(g scan.PhotoGroup) *scan.FileRef {
	if g.Jpeg != nil {
		return g.Jpeg
	}
	return g.Raw
}
