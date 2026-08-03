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
type Stats struct {
	Dirs   int
	Frames int
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

	err = filepath.WalkDir(clean, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A directory that cannot be read is skipped rather than failing
			// the pass: one unreadable folder on a card should not cost the
			// user the other forty.
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
			return nil
		}
		walked = append(walked, path)
		stats.Dirs++

		if len(groups) > 0 {
			written, err := s.writeDir(path, groups, workers, opts.Lookup)
			if err != nil {
				return err
			}
			stats.Frames += written
		} else if err := s.pruneDir(path, nil); err != nil {
			return err
		}

		if opts.Progress != nil {
			opts.Progress(Progress{Root: clean, Dir: path, Dirs: stats.Dirs, Frames: stats.Frames})
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	if err := s.pruneMissingDirs(clean, walked); err != nil {
		return stats, err
	}
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
		total.Dirs += stats.Dirs
		total.Frames += stats.Frames
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

const upsertFrameSQL = `
INSERT INTO frames
	(hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes, rating, verdict, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hash) DO UPDATE SET
	dir = excluded.dir,
	stem = excluded.stem,
	kind = excluded.kind,
	shot = excluded.shot,
	raw_path = excluded.raw_path,
	jpeg_path = excluded.jpeg_path,
	raw_bytes = excluded.raw_bytes,
	jpeg_bytes = excluded.jpeg_bytes,
	rating = excluded.rating,
	verdict = excluded.verdict,
	indexed_at = excluded.indexed_at
`

// writeDir hashes one directory's frames and writes them in a single
// transaction, then drops any row still filed under that directory that the
// scan did not find. It returns how many frames it wrote.
func (s *Store) writeDir(dir string, groups []scan.PhotoGroup, workers int, lookup func(string) (string, int)) (int, error) {
	hashes := hashGroups(groups, workers)
	now := time.Now().Unix()

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	kept := make([]string, 0, len(groups))
	written := 0
	for i, g := range groups {
		// A frame whose primary file cannot be read has no identity, so there
		// is nothing to key a row on. It is skipped rather than guessed at.
		if hashes[i] == "" {
			continue
		}
		verdict, rating := "", 0
		if lookup != nil {
			verdict, rating = lookup(hashes[i])
		}
		if verdict != VerdictKeep && verdict != VerdictCut {
			verdict = ""
		}
		if rating < 0 {
			rating = 0
		}
		var rawBytes, jpegBytes int64
		var rawPath, jpegPath string
		if g.Raw != nil {
			rawBytes = g.Raw.Size
			rawPath = g.Raw.Path
		}
		if g.Jpeg != nil {
			jpegBytes = g.Jpeg.Size
			jpegPath = g.Jpeg.Path
		}
		if _, err := tx.Exec(upsertFrameSQL,
			hashes[i], g.Dir, g.Stem, g.Kind.String(), g.Shot.Unix(),
			rawPath, jpegPath, rawBytes, jpegBytes, rating, verdict, now); err != nil {
			return 0, fmt.Errorf("catalog: write %s: %w", g.Stem, err)
		}
		kept = append(kept, hashes[i])
		written++
	}
	if err := pruneDirTx(tx, dir, kept); err != nil {
		return 0, err
	}
	return written, tx.Commit()
}

// pruneDir drops every row filed under dir except the hashes given.
func (s *Store) pruneDir(dir string, keep []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := pruneDirTx(tx, dir, keep); err != nil {
		return err
	}
	return tx.Commit()
}

func pruneDirTx(tx *sql.Tx, dir string, keep []string) error {
	query := `DELETE FROM frames WHERE dir = ?`
	args := []any{dir}
	if len(keep) > 0 {
		query += ` AND hash NOT IN (?` + strings.Repeat(`,?`, len(keep)-1) + `)`
		for _, h := range keep {
			args = append(args, h)
		}
	}
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("catalog: prune %s: %w", dir, err)
	}
	return nil
}

// pruneMissingDirs drops the frames of directories that were under root and
// are not any more. The walked list is bounded by the number of directories
// rather than the number of frames, so it goes into a temporary table instead
// of an ever-growing IN clause.
func (s *Store) pruneMissingDirs(root string, walked []string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`CREATE TEMP TABLE IF NOT EXISTS pass_dirs (dir TEXT PRIMARY KEY)`); err != nil {
		return fmt.Errorf("catalog: prepare prune: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM pass_dirs`); err != nil {
		return fmt.Errorf("catalog: prepare prune: %w", err)
	}
	for _, dir := range walked {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO pass_dirs (dir) VALUES (?)`, dir); err != nil {
			return fmt.Errorf("catalog: prepare prune: %w", err)
		}
	}
	where, args := underRoot(root)
	if _, err := tx.Exec(
		`DELETE FROM frames WHERE `+where+` AND dir NOT IN (SELECT dir FROM pass_dirs)`, args...); err != nil {
		return fmt.Errorf("catalog: prune folders that left %s: %w", root, err)
	}
	return tx.Commit()
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
