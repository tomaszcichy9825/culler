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

	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/hash"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// The two phases of an index pass. The listing walks the tree and writes rows
// from directory listings alone; the hashing goes back and reads content to
// give those rows their identities.
const (
	PhaseListing = "listing"
	PhaseHashing = "hashing"
)

// Progress is what an index pass reports as it goes. Dirs and Frames are
// cumulative for the pass, so they only ever climb; Done marks the last
// report for a root.
//
// The listing phase has no total — counting the tree before walking it would
// mean reading every directory twice, and on a card reader that is the
// expensive half of the whole operation. The hashing phase knows exactly what
// the listing left it, so Hashed climbs towards Pending and a bar drawn from
// them is honest.
type Progress struct {
	Root   string
	Dir    string
	Dirs   int
	Frames int
	// Phase says which half of the pass is reporting.
	Phase string
	// Hashed and Pending are the hashing phase's progress: how many frames
	// have been read so far, of how many the listing left unidentified.
	Hashed  int
	Pending int
	Done    bool
}

// Stats is what an index pass found.
//
// Frames is everything the pass accounted for, whether it had to read it or
// not. Changed counts the rows the pass read content for and rewrote, and
// Removed the rows it dropped: on a rerun over an untouched card both are
// zero, which is the point of recording sizes and modification times in the
// first place.
type Stats struct {
	Dirs    int
	Frames  int
	Changed int
	Removed int
	// Unreadable counts frames whose primary file could not be hashed. Their
	// existing rows are kept — unreadable is not the same as gone — but what
	// those rows say may be stale until a pass can read the files again.
	Unreadable int
}

func (s *Stats) add(other Stats) {
	s.Dirs += other.Dirs
	s.Frames += other.Frames
	s.Changed += other.Changed
	s.Removed += other.Removed
	s.Unreadable += other.Unreadable
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
	Lookup func(hash, dir, stem string) (verdict string, rating int)

	// Progress is called after each directory and once more at the end. It
	// runs on the indexing goroutine, so it must not block for long.
	Progress func(Progress)

	// hashFile replaces the content hasher in tests, which is how a test can
	// count — or refuse — the reads a pass makes. Nil reads the file.
	hashFile func(path string) (string, error)

	// captureTime replaces the EXIF reader in tests. Nil reads the file's
	// metadata, which is the only thing that knows when a photograph was
	// taken. The second result is false for a frame that carries no capture
	// time, and the file's own mtime stands in.
	captureTime func(path string) (time.Time, bool)
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
// The pass runs in two phases. The listing walks the tree and writes a row
// for every frame from the directory listing alone — paths, sizes, times,
// shot-from-mtime, no identity — so the tree, the counts and the search have
// the root within seconds of it being added. The hashing then goes back over
// what the listing could not identify and reads it in, filling each row in
// place. On a cloud-synced folder this is the difference between minutes and
// seconds: listing placeholders is free, and only the read forces a download.
//
// The walk streams. One directory is scanned and written at a time, so a card
// with fifty thousand frames does not have to fit in memory before the first
// row lands.
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
	// The directories the listing left frames unidentified in, for the hashing
	// phase to come back to. Bounded by the number of directories: the groups
	// themselves are re-listed then, not held.
	type pendingDir struct {
		dir    string
		frames int
	}
	var pendingDirs []pendingDir
	totalPending := 0

	err = walkDirFollowingRoot(clean, func(path string, d fs.DirEntry, err error) error {
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
			done, todo, err := s.listDir(path, groups, opts.Lookup)
			if err != nil {
				return err
			}
			stats.add(done)
			if todo > 0 {
				pendingDirs = append(pendingDirs, pendingDir{dir: path, frames: todo})
				totalPending += todo
			}
		} else {
			removed, err := s.pruneDir(path, nil)
			if err != nil {
				return err
			}
			stats.Removed += removed
		}

		if opts.Progress != nil {
			opts.Progress(Progress{
				Root: clean, Dir: path, Dirs: stats.Dirs, Frames: stats.Frames, Phase: PhaseListing,
			})
		}
		return nil
	})
	if err != nil {
		return stats, err
	}

	// Catalogued folders the walk never reached but that are still on disk
	// behind a symlink. WalkDir does not follow a symlinked directory, while
	// UpsertDir happily catalogues one — coverage is lexical — so without
	// this a folder the user opened would flip-flop: catalogued on open,
	// forgotten on the next reindex. The walk still does not descend into it,
	// which is the stance internal/scan takes too (a symlink is resolved,
	// never walked as a directory), so its subtree is unknown rather than
	// current: it joins failed and keeps its rows exactly as they were, and
	// the next folder open refreshes them.
	linked, err := s.symlinkedDirs(clean, walked, failed)
	if err != nil {
		return stats, err
	}
	failed = append(failed, linked...)

	removed, err := s.pruneMissingDirs(clean, walked, failed)
	if err != nil {
		return stats, err
	}
	stats.Removed += removed

	// The hashing phase. The listing has landed and been pruned; everything
	// the tree and the search show is already true, and what follows only
	// fills identities in. The first report goes out before the first read,
	// so a consumer can tell "listed, now reading" from "still listing".
	hashed := 0
	if opts.Progress != nil {
		opts.Progress(Progress{
			Root: clean, Dirs: stats.Dirs, Frames: stats.Frames,
			Phase: PhaseHashing, Hashed: 0, Pending: totalPending,
		})
	}
	for _, pd := range pendingDirs {
		done, read, err := s.hashDir(pd.dir, cfg, workers, opts, func(soFar int) {
			if opts.Progress != nil {
				opts.Progress(Progress{
					Root: clean, Dir: pd.dir, Dirs: stats.Dirs, Frames: stats.Frames,
					Phase: PhaseHashing, Hashed: hashed + soFar, Pending: totalPending,
				})
			}
		})
		stats.add(done)
		hashed += read
		if err != nil {
			return stats, err
		}
	}

	// Stamping only lands when clean is itself a registered root. Given a
	// folder inside one, this pass covered part of that root and not the whole
	// of it, so the root's own last-indexed time is left where it was rather
	// than claiming a walk that did not happen.
	if _, err := s.db.Exec(
		`UPDATE roots SET last_indexed_at = ? WHERE path = ?`, time.Now().Unix(), clean); err != nil {
		return stats, fmt.Errorf("catalog: stamp root %s: %w", clean, err)
	}
	if opts.Progress != nil {
		opts.Progress(Progress{
			Root: clean, Dirs: stats.Dirs, Frames: stats.Frames,
			Phase: PhaseHashing, Hashed: hashed, Pending: totalPending, Done: true,
		})
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
// opening a folder in CULL must not quietly start cataloguing it. A hidden
// directory is refused the same way: the index walk skips dot-folders, so
// cataloguing one here would only flip-flop — present after the open, pruned
// by the next reindex.
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
	// Both phases, back to back: one directory is bounded, and a folder the
	// user has open deserves identities now rather than behind them.
	stats, todo, err := s.listDir(clean, groups, opts.Lookup)
	stats.Dirs = 1
	if err != nil {
		return stats, err
	}
	if todo > 0 {
		done, _, err := s.hashDir(clean, opts.scanConfig(), opts.workers(), opts, nil)
		stats.add(done)
		if err != nil {
			return stats, err
		}
	}
	return stats, nil
}

// covered reports whether dir is one of the registered roots or sits inside
// one — reachably: a folder only addressable through a hidden component is
// one the index walk will never visit, so it does not count as covered.
func (s *Store) covered(dir string) (bool, error) {
	roots, err := rootPaths(s.db)
	if err != nil {
		return false, err
	}
	for _, root := range roots {
		if under(dir, root) && !hiddenUnder(dir, root) {
			return true, nil
		}
	}
	return false, nil
}

// hiddenUnder reports whether any folder between root and dir — dir itself
// included, root excluded — is hidden. It is the lexical twin of the index
// walk's dot-folder skip: what the walk will not enter, a folder open must
// not catalogue, or the folder flip-flops between the two passes.
func hiddenUnder(dir, root string) bool {
	rel := strings.TrimPrefix(dir, childPrefix(root))
	if rel == dir || rel == "" {
		return false // dir is root itself, or not under it at all
	}
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

const upsertFrameSQL = `
INSERT INTO frames
	(hash, dir, stem, kind, shot, raw_path, jpeg_path, raw_bytes, jpeg_bytes,
	 raw_mtime, jpeg_mtime, shot_source, rating, verdict, indexed_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(hash, dir, stem) DO UPDATE SET
	kind = excluded.kind,
	shot = excluded.shot,
	shot_source = excluded.shot_source,
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
// Where a row's shot time came from. A row written before the catalogue read
// capture times holds neither value, and that emptiness is what marks it as
// still owing one read.
const (
	sourceCaptureTime = "exif"
	sourceFileTime    = "mtime"
)

type rowState struct {
	hash    string
	raw     fileState
	jpeg    fileState
	verdict string
	rating  int
	// shotSource is empty on a row from a build that recorded the file's time
	// as the shot time without saying so. Such a row is re-read once, however
	// well its sizes and mtimes still match, because the time it holds is the
	// day the file was written rather than the day the photograph was taken.
	shotSource string
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

// sameRecordedContent reports whether both files' sizes and modification
// times still match what the row recorded. Paths are left out on purpose: a
// rename moves no bytes, and it is the bytes a verdict judged.
func sameRecordedContent(row rowState, raw, jpeg fileState) bool {
	return row.raw.bytes == raw.bytes && row.raw.mtime == raw.mtime &&
		row.jpeg.bytes == jpeg.bytes && row.jpeg.mtime == jpeg.mtime
}

// dirState reads back what the catalogue holds for one directory, keyed on the
// path each frame is identified by. One query per directory, and the rows are
// bounded by what fits in a folder.
func (s *Store) dirState(dir string) (map[string]rowState, error) {
	rows, err := s.db.Query(
		`SELECT hash, raw_path, jpeg_path, raw_bytes, jpeg_bytes, raw_mtime, jpeg_mtime, verdict, rating, shot_source
		 FROM frames WHERE dir = ?`, dir)
	if err != nil {
		return nil, fmt.Errorf("catalog: read %s: %w", dir, err)
	}
	defer rows.Close()

	out := map[string]rowState{}
	for rows.Next() {
		var r rowState
		if err := rows.Scan(&r.hash, &r.raw.path, &r.jpeg.path, &r.raw.bytes, &r.jpeg.bytes,
			&r.raw.mtime, &r.jpeg.mtime, &r.verdict, &r.rating, &r.shotSource); err != nil {
			return nil, err
		}
		if key := primaryPath(r.raw, r.jpeg); key != "" {
			out[key] = r
		}
	}
	return out, rows.Err()
}

// currentRow reports whether row already describes this exact state of the
// frame's files, identity included — the one case a pass has nothing to read.
//
// A row that never had its capture time read is not current however well its
// files match: it holds the day the file was written where the day the
// photograph was taken belongs, and only the file can settle that. Those rows
// are re-read once, and the row written in their place says where its time
// came from, so it is never re-read for this reason again.
func currentRow(row rowState, raw, jpeg fileState) bool {
	return row.hash != "" && row.shotSource != "" && row.raw == raw && row.jpeg == jpeg
}

// listDir is the listing phase over one directory: every frame the scan found
// becomes a row on the strength of the listing alone, and rows for files that
// have left are dropped. No file content is read here, which is the point —
// on a cloud-synced folder a read is a download.
//
// It returns how many frames still need an identity, for hashDir to come back
// to. Three kinds of frame land in that count: a new one, whose row is written
// here with an empty hash; one whose pending row a previous pass never filled;
// and one whose files changed under a row that has an identity — that row is
// left exactly as it was, because a stale row beats an unhashed one until the
// new bytes have actually been read.
func (s *Store) listDir(dir string, groups []scan.PhotoGroup, lookup func(hash, dir, stem string) (string, int)) (Stats, int, error) {
	held, err := s.dirState(dir)
	if err != nil {
		return Stats{}, 0, err
	}

	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return Stats{}, 0, err
	}
	defer tx.Rollback()

	var stats Stats
	pending := 0
	kept := make([]FrameKey, 0, len(groups))
	for _, g := range groups {
		raw, jpeg := stateOf(g)
		row, ok := held[primaryPath(raw, jpeg)]
		stats.Frames++

		switch {
		case ok && currentRow(row, raw, jpeg):
			// The files are as they were. Only a judgement that has moved
			// since the last pass is worth a write.
			kept = append(kept, FrameKey{Hash: row.hash, Dir: g.Dir, Stem: g.Stem})
			verdict, rating := judgement(lookup, row.hash, g.Dir, g.Stem)
			if row.verdict != verdict || row.rating != rating {
				if _, err := tx.Exec(
					`UPDATE frames SET verdict = ?, rating = ? WHERE hash = ? AND dir = ? AND stem = ?`,
					verdict, rating, row.hash, g.Dir, g.Stem); err != nil {
					return stats, 0, fmt.Errorf("catalog: refresh %s: %w", g.Stem, err)
				}
			}
		case ok && row.hash != "":
			// Changed under a row that has an identity. The row stays as it
			// was until the new bytes are read: its verdict judged content
			// that is known to have existed, which is more than the listing
			// can say about what replaced it.
			kept = append(kept, FrameKey{Hash: row.hash, Dir: g.Dir, Stem: g.Stem})
			pending++
		default:
			// New, or a pending row from a pass that never got to read it.
			// The listing alone makes it a row — no identity, no judgement,
			// but a frame the tree can count and the search can find.
			if !ok || row.raw != raw || row.jpeg != jpeg {
				// The listing reads no file, so all it can offer is the file's
				// own time — said plainly, so the hashing phase knows to come
				// back for the photograph's.
				if _, err := tx.Exec(upsertFrameSQL,
					"", g.Dir, g.Stem, g.Kind.String(), g.Shot.Unix(),
					raw.path, jpeg.path, raw.bytes, jpeg.bytes, raw.mtime, jpeg.mtime,
					sourceFileTime, 0, "", now); err != nil {
					return stats, 0, fmt.Errorf("catalog: list %s: %w", g.Stem, err)
				}
			}
			kept = append(kept, FrameKey{Hash: "", Dir: g.Dir, Stem: g.Stem})
			pending++
		}
	}

	if _, err := pruneDirTx(tx, dir, kept); err != nil {
		return stats, 0, err
	}
	// Counted from the files rather than from the rows the prune touched: a
	// frame that was rewritten in place will get a new identity and so lose
	// its old row, and reporting that as a removal would make an edit look
	// like a deletion.
	onDisk := map[string]bool{}
	for _, g := range groups {
		raw, jpeg := stateOf(g)
		onDisk[primaryPath(raw, jpeg)] = true
	}
	for path := range held {
		if !onDisk[path] {
			stats.Removed++
		}
	}
	return stats, pending, tx.Commit()
}

// hashBatch is how many frames the hashing phase reads between writes and
// progress reports. Small enough that rows land and the count moves while a
// cloud folder is still downloading, large enough that the workers stay busy.
const hashBatch = 32

// hashDir is the hashing phase over one directory: the frames the listing
// could not identify are read, and their rows filled in place. The directory
// is re-listed rather than carried over from the listing phase, so the pass
// holds directories in memory, never frames — and a file that moved between
// the phases is classified against what is true now.
//
// It returns how many frames it read (or tried to). A directory that can no
// longer be listed keeps its rows and reports nothing: it may be a card
// mid-unplug, and the next pass settles it either way.
func (s *Store) hashDir(dir string, cfg scan.Config, workers int, opts IndexOptions, onHashed func(int)) (Stats, int, error) {
	groups, err := scan.ScanDir(dir, cfg)
	if err != nil {
		return Stats{}, 0, nil
	}
	held, err := s.dirState(dir)
	if err != nil {
		return Stats{}, 0, err
	}

	// Two kinds of work, and they cost very different amounts. A frame whose
	// files the catalogue does not recognise needs its whole content read, to
	// hash it. A frame it does recognise, but whose row predates capture
	// times, needs only the head of the file — the identity in the row is
	// still good, because the files have not changed. Re-hashing those would
	// mean pulling an entire library back down over a network share to correct
	// a timestamp, which is the difference between seconds and an afternoon.
	todo := make([]scan.PhotoGroup, 0, len(groups))
	repair := make([]scan.PhotoGroup, 0)
	for _, g := range groups {
		raw, jpeg := stateOf(g)
		row, ok := held[primaryPath(raw, jpeg)]
		switch {
		case ok && currentRow(row, raw, jpeg):
			// Nothing to do: identity and shot time are both settled.
		case ok && row.hash != "" && row.shotSource == "" && row.raw == raw && row.jpeg == jpeg:
			repair = append(repair, g)
		default:
			todo = append(todo, g)
		}
	}

	var stats Stats
	done := 0
	if err := s.repairShotTimes(repair, workers, opts.captureTime, &stats); err != nil {
		return stats, done, err
	}
	for start := 0; start < len(todo); start += hashBatch {
		batch := todo[start:min(start+hashBatch, len(todo))]
		fresh := identifyGroups(batch, workers, opts.hashFile, opts.captureTime)
		if err := s.fillRows(held, batch, fresh, opts.Lookup, &stats); err != nil {
			return stats, done, err
		}
		done += len(batch)
		if onHashed != nil {
			onHashed(done)
		}
	}
	return stats, done, nil
}

// repairShotTimes corrects rows that hold a file's mtime where a photograph's
// capture time belongs, which is every row written before the catalogue read
// capture times at all.
//
// It reads the head of each file and nothing else: these frames are already
// identified and their files have not moved, so the hash in the row still
// stands. Each row then records where its new time came from, and is never
// repaired again.
func (s *Store) repairShotTimes(groups []scan.PhotoGroup, workers int, captureTime func(string) (time.Time, bool), stats *Stats) error {
	if len(groups) == 0 {
		return nil
	}
	for start := 0; start < len(groups); start += hashBatch {
		batch := groups[start:min(start+hashBatch, len(groups))]
		// The hash is not wanted here, so the reader that would compute it is
		// replaced by one that reads nothing.
		times := identifyGroups(batch, workers,
			func(string) (string, error) { return "", nil }, captureTime)

		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		for i, g := range batch {
			shot, source := g.Shot, sourceFileTime
			if times[i].taken {
				shot, source = times[i].shot, sourceCaptureTime
			}
			if _, err := tx.Exec(
				`UPDATE frames SET shot = ?, shot_source = ? WHERE dir = ? AND stem = ?`,
				shot.Unix(), source, g.Dir, g.Stem); err != nil {
				tx.Rollback()
				return fmt.Errorf("catalog: repair shot time for %s: %w", g.Stem, err)
			}
			stats.Changed++
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// fillRows writes one hashed batch: each frame that got an identity replaces
// whatever row stood for it — the pending listing row, or the old identity
// its content outgrew.
func (s *Store) fillRows(held map[string]rowState, groups []scan.PhotoGroup, fresh []identity, lookup func(hash, dir, stem string) (string, int), stats *Stats) error {
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for i, g := range groups {
		raw, jpeg := stateOf(g)
		row, ok := held[primaryPath(raw, jpeg)]
		hash := fresh[i].hash
		if hash == "" {
			// The file is on disk — the listing saw it — but its bytes cannot
			// be read, so the frame keeps whatever row it has: pending, or the
			// old identity, because stale beats silently gone. A verdict
			// judged bytes; when the recorded sizes and times no longer match
			// the row, those bytes have moved, and the verdict goes rather
			// than sentencing content nobody has seen. A frame whose recorded
			// state still matches was only unreadable, not changed, and keeps
			// its verdict.
			stats.Unreadable++
			if ok && row.hash != "" && row.verdict != "" && !sameRecordedContent(row, raw, jpeg) {
				if _, err := tx.Exec(
					`UPDATE frames SET verdict = '' WHERE hash = ? AND dir = ? AND stem = ?`,
					row.hash, g.Dir, g.Stem); err != nil {
					return fmt.Errorf("catalog: clear stale verdict for %s: %w", g.Stem, err)
				}
			}
			continue
		}

		verdict, rating := judgement(lookup, hash, g.Dir, g.Stem)
		// One frame on disk, one row: the superseded row — pending, or the
		// old identity — goes as the filled one lands. A twin under another
		// stem or in another folder is a different frame and is not touched.
		if _, err := tx.Exec(
			`DELETE FROM frames WHERE dir = ? AND stem = ? AND hash <> ?`,
			g.Dir, g.Stem, hash); err != nil {
			return fmt.Errorf("catalog: supersede %s: %w", g.Stem, err)
		}
		// When the photograph was taken, and where that answer came from. The
		// file's own time is the fallback and is recorded as such, so a row
		// standing on it is not mistaken for one standing on the EXIF.
		shot, shotSource := g.Shot, sourceFileTime
		if fresh[i].taken {
			shot, shotSource = fresh[i].shot, sourceCaptureTime
		}
		if _, err := tx.Exec(upsertFrameSQL,
			hash, g.Dir, g.Stem, g.Kind.String(), shot.Unix(),
			raw.path, jpeg.path, raw.bytes, jpeg.bytes, raw.mtime, jpeg.mtime,
			shotSource, rating, verdict, now); err != nil {
			return fmt.Errorf("catalog: write %s: %w", g.Stem, err)
		}
		stats.Changed++
	}
	return tx.Commit()
}

// judgement asks the lookup what has been decided about a frame and clamps
// the answer to what a row can hold.
func judgement(lookup func(hash, dir, stem string) (string, int), hash, dir, stem string) (string, int) {
	verdict, rating := "", 0
	if lookup != nil {
		verdict, rating = lookup(hash, dir, stem)
	}
	if verdict != VerdictKeep && verdict != VerdictCut {
		verdict = ""
	}
	if rating < 0 {
		rating = 0
	}
	return verdict, rating
}

// pruneDir drops every row filed under dir except the frames given, and
// returns how many it dropped.
func (s *Store) pruneDir(dir string, keep []FrameKey) (int, error) {
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

// pruneDirTx works out the doomed rows in Go and deletes them one key at a
// time inside the transaction, rather than binding the whole keep list into
// one NOT IN: a flat folder can hold more frames than the driver has
// parameter slots in a statement. Rows are told apart by (hash, stem) within
// the dir, so one of two same-content twins can be pruned without the other.
func pruneDirTx(tx *sql.Tx, dir string, keep []FrameKey) (int, error) {
	if len(keep) == 0 {
		res, err := tx.Exec(`DELETE FROM frames WHERE dir = ?`, dir)
		if err != nil {
			return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
		}
		n, err := res.RowsAffected()
		return int(n), err
	}

	type rowKey struct{ hash, stem string }
	keepSet := make(map[rowKey]bool, len(keep))
	for _, k := range keep {
		keepSet[rowKey{k.Hash, k.Stem}] = true
	}
	rows, err := tx.Query(`SELECT hash, stem FROM frames WHERE dir = ?`, dir)
	if err != nil {
		return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
	}
	var doomed []rowKey
	for rows.Next() {
		var k rowKey
		if err := rows.Scan(&k.hash, &k.stem); err != nil {
			rows.Close()
			return 0, err
		}
		if !keepSet[k] {
			doomed = append(doomed, k)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	// Closed before the deletes: the store holds one connection, and nothing
	// can execute on it while a result set is still open.
	rows.Close()

	for _, k := range doomed {
		if _, err := tx.Exec(
			`DELETE FROM frames WHERE dir = ? AND hash = ? AND stem = ?`,
			dir, k.hash, k.stem); err != nil {
			return 0, fmt.Errorf("catalog: prune %s: %w", dir, err)
		}
	}
	return len(doomed), nil
}

// symlinkedDirs returns the directories that keep catalogued folders under
// root reachable through a symlink: for every catalogued directory the walk
// did not reach and no failed directory already covers, the ancestor that is
// a symlink still resolving to a directory on disk. The result is
// deduplicated — a rescued ancestor covers everything beneath it.
func (s *Store) symlinkedDirs(root string, walked, failed []string) ([]string, error) {
	where, args := underRoot(root)
	rows, err := s.db.Query(`SELECT DISTINCT dir FROM frames WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("catalog: read folders under %s: %w", root, err)
	}
	defer rows.Close()
	var dirs []string
	for rows.Next() {
		var d string
		if err := rows.Scan(&d); err != nil {
			return nil, err
		}
		dirs = append(dirs, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	reached := make(map[string]bool, len(walked))
	for _, d := range walked {
		reached[d] = true
	}
	underAny := func(dir string, list []string) bool {
		for _, l := range list {
			if under(dir, l) {
				return true
			}
		}
		return false
	}

	var out []string
	for _, dir := range dirs {
		if reached[dir] || underAny(dir, failed) || underAny(dir, out) {
			continue
		}
		// Climb towards root. The first component that is itself a symlink
		// decides: resolving to a directory means everything beneath it is
		// present and only reachable through it; a dangling link means the
		// folder really has left. Lstat follows every component but the last,
		// so a failure here says this level is gone and the verdict, if any,
		// lives further up.
		for p := dir; p != root && len(p) > len(root); p = filepath.Dir(p) {
			fi, err := os.Lstat(p)
			if err != nil || fi.Mode()&os.ModeSymlink == 0 {
				continue
			}
			if target, err := os.Stat(p); err == nil && target.IsDir() {
				out = append(out, p)
			}
			break
		}
	}
	return out, nil
}

// pruneMissingDirs drops the frames of directories that were under root and
// are not any more. The walked and failed lists are bounded by the number of
// directories rather than the number of frames, so both go into temporary
// tables: an ever-growing IN clause runs out of parameter slots, and one AND
// NOT term per failed directory runs out of expression depth near a thousand
// unreadable folders.
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

	for _, stmt := range []string{
		`CREATE TEMP TABLE IF NOT EXISTS pass_dirs (dir TEXT PRIMARY KEY)`,
		`DELETE FROM pass_dirs`,
		`CREATE TEMP TABLE IF NOT EXISTS failed_dirs (dir TEXT PRIMARY KEY, prefix TEXT NOT NULL)`,
		`DELETE FROM failed_dirs`,
	} {
		if _, err := tx.Exec(stmt); err != nil {
			return 0, fmt.Errorf("catalog: prepare prune: %w", err)
		}
	}
	for _, dir := range walked {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO pass_dirs (dir) VALUES (?)`, dir); err != nil {
			return 0, fmt.Errorf("catalog: prepare prune: %w", err)
		}
	}
	for _, dir := range failed {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO failed_dirs (dir, prefix) VALUES (?, ?)`,
			dir, childPrefix(dir)); err != nil {
			return 0, fmt.Errorf("catalog: prepare prune: %w", err)
		}
	}
	where, args := underRoot(root)
	// length() counts characters on SQLite text, matching the rune count
	// underRoot binds, so the two prefix tests agree on non-ASCII names.
	query := `DELETE FROM frames WHERE ` + where + `
		AND dir NOT IN (SELECT dir FROM pass_dirs)
		AND NOT EXISTS (
			SELECT 1 FROM failed_dirs f
			WHERE frames.dir = f.dir OR substr(frames.dir, 1, length(f.prefix)) = f.prefix
		)`
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

// walkDirFollowingRoot is filepath.WalkDir with one difference: a root that
// is itself a symlink to a directory is followed rather than visited as the
// file the link is. A root is an address the user gave, and a linked path is
// as good an address as a real one — internal/scan takes the same stance for
// linked files, resolving them rather than reading the link. Without this,
// the walk lstats the root, enters nothing, and the prune afterwards empties
// everything under the link on every reindex. Directories inside the walk
// are still never entered through links, exactly as filepath.WalkDir has it.
func walkDirFollowingRoot(root string, fn fs.WalkDirFunc) error {
	info, err := os.Stat(root)
	if err != nil {
		err = fn(root, nil, err)
	} else {
		err = walkDir(root, fs.FileInfoToDirEntry(info), fn)
	}
	if err == fs.SkipDir || err == fs.SkipAll {
		return nil
	}
	return err
}

// walkDir mirrors filepath.WalkDir's descent, entry for entry, so the walk
// behaves identically below the root.
func walkDir(path string, d fs.DirEntry, fn fs.WalkDirFunc) error {
	if err := fn(path, d, nil); err != nil || !d.IsDir() {
		if err == fs.SkipDir && d.IsDir() {
			err = nil
		}
		return err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		// Second call on the same directory reports the listing failure, as
		// filepath.WalkDir does.
		err = fn(path, d, err)
		if err != nil {
			if err == fs.SkipDir {
				err = nil
			}
			return err
		}
	}
	for _, e := range entries {
		if err := walkDir(filepath.Join(path, e.Name()), e, fn); err != nil {
			if err == fs.SkipDir {
				break
			}
			return err
		}
	}
	return nil
}

// hashGroups returns the identity hash of every group's primary file, aligned
// with groups and empty where the file could not be read. The worker count is
// the caller's: all CPUs for a local disk, the configured low cap for a
// network volume. hashFile replaces the reader in tests; nil reads the file.
func hashGroups(groups []scan.PhotoGroup, workers int, hashFile func(string) (string, error)) []string {
	ids := identifyGroups(groups, workers, hashFile, func(string) (time.Time, bool) { return time.Time{}, false })
	hashes := make([]string, len(ids))
	for i, id := range ids {
		hashes[i] = id.hash
	}
	return hashes
}

// identity is what one read of a frame's primary file yields: what the frame
// is, and when the photograph was taken.
type identity struct {
	hash string
	// shot is the capture time out of the file's metadata; taken is false for
	// a frame that carries none, and the file's own time stands in.
	shot  time.Time
	taken bool
}

// identifyGroups reads each frame's primary file once and answers with both
// the content hash and the capture time.
//
// They are read together on purpose. The hash needs the whole file and the
// capture time needs its head, so a pass that read them separately would touch
// every photograph twice — which on a network share or a cloud-synced folder
// is the difference between one download and two.
func identifyGroups(
	groups []scan.PhotoGroup,
	workers int,
	hashFile func(string) (string, error),
	captureTime func(string) (time.Time, bool),
) []identity {
	if workers < 1 {
		workers = 1
	}
	if hashFile == nil {
		hashFile = hash.Content
	}
	if captureTime == nil {
		captureTime = captureTimeOf
	}
	out := make([]identity, len(groups))
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
			if h, err := hashFile(path); err == nil {
				out[i].hash = h
			}
			// The capture time is read whether or not the hash came back: a
			// file that will not hash may still say when it was taken, and a
			// frame in the grid with the right date beats one with none.
			if shot, ok := captureTime(path); ok {
				out[i].shot, out[i].taken = shot, true
			}
		}(i, ref.Path)
	}
	wg.Wait()
	return out
}

// captureTimeOf reads when a photograph was taken. A file whose metadata will
// not parse, or which carries no capture time, answers false — there is no
// guess to make, and the caller falls back to the file's own time.
func captureTimeOf(path string) (time.Time, bool) {
	fields, err := exif.Read(path)
	if err != nil {
		return time.Time{}, false
	}
	if !fields.DateTimeOriginal.Present || fields.DateTimeOriginal.Value.IsZero() {
		return time.Time{}, false
	}
	return fields.DateTimeOriginal.Value, true
}

// primaryRef is the file a frame is identified by: the JPEG when there is one,
// because that is the frame the user is looking at, otherwise the RAW.
func primaryRef(g scan.PhotoGroup) *scan.FileRef {
	if g.Jpeg != nil {
		return g.Jpeg
	}
	return g.Raw
}
