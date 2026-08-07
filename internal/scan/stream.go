package scan

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Batching defaults. A batch is small enough that the first tiles paint almost
// as soon as the directory has been read, and large enough that a folder of a
// few thousand frames does not cross the bridge to the webview a frame at a
// time.
const (
	DefaultBatchSize = 64
	DefaultMaxDelay  = 40 * time.Millisecond
)

// readdirChunk is how many directory entries are asked for at once. Reading
// the names is cheap next to the stat of each file, so the chunk only exists
// to keep a huge directory from being held in one allocation.
const readdirChunk = 512

// StreamOptions bounds how long frames are held back before they are handed
// over. The zero value takes the defaults.
type StreamOptions struct {
	// BatchSize is the most frames one batch carries.
	BatchSize int
	// MaxDelay is the longest a partly filled batch is held. On a network
	// volume where each stat is slow this is what keeps tiles arriving.
	MaxDelay time.Duration
}

func (o StreamOptions) batchSize() int {
	if o.BatchSize > 0 {
		return o.BatchSize
	}
	return DefaultBatchSize
}

func (o StreamOptions) maxDelay() time.Duration {
	if o.MaxDelay > 0 {
		return o.MaxDelay
	}
	return DefaultMaxDelay
}

// ScanDirStream walks dir and hands its frames to emit in batches as they are
// resolved, rather than returning the whole folder at once. Grouping is
// identical to ScanDir's, down to the order the frames come out in: what
// arrives first is the start of the same sorted list, not a different one.
// Junk symlinks are skipped exactly as ScanDir skips them. The one remaining
// divergence is a directory whose regular entries cannot be statted — the
// listable-but-untraversable folder — where ScanDir fails outright and the
// stream paints what it can while dropping the rest; see resolve for why.
//
// Each batch is freshly allocated and belongs to emit, which is called on the
// caller's goroutine and must not block for long — the walk is stalled while
// it runs. An empty folder emits nothing.
func ScanDirStream(dir string, cfg Config, emit func(batch []PhotoGroup)) error {
	return ScanDirStreamContext(context.Background(), dir, cfg, StreamOptions{}, emit)
}

// ScanDirStreamContext is ScanDirStream with cancellation and explicit
// batching. A cancelled walk stops emitting and returns the context's error,
// which is what a folder switch wants: the frames still in flight belong to a
// folder the user has already left.
func ScanDirStreamContext(ctx context.Context, dir string, cfg Config, opts StreamOptions, emit func(batch []PhotoGroup)) error {
	buckets, order, err := readEntries(ctx, dir, cfg)
	if err != nil {
		return err
	}

	batchSize, maxDelay := opts.batchSize(), opts.maxDelay()
	batch := make([]PhotoGroup, 0, batchSize)
	last := time.Now()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		emit(batch)
		batch = make([]PhotoGroup, 0, batchSize)
		last = time.Now()
	}

	// Names are known, so grouping is already settled; what is left is the
	// stat of each file, which is the slow half on a network share and the
	// half worth streaming.
	for _, key := range order {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := buckets[key].resolve()
		g, ok := b.group(dir)
		if !ok {
			continue
		}
		batch = append(batch, g)
		if len(batch) >= batchSize || time.Since(last) >= maxDelay {
			flush()
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	flush()
	return nil
}

// entrySlot is a file that has been classified by name but not yet stat'd.
type entrySlot struct {
	entry    os.DirEntry
	name     string
	class    string
	priority int
}

// entryBucket is a stemBucket before any of its files have been stat'd.
type entryBucket struct {
	dir   string
	stem  string
	slots []entrySlot
}

func (b *entryBucket) noteStem(stem string) {
	if b.stem == "" || stem < b.stem {
		b.stem = stem
	}
}

// resolve stats the bucket's files and turns it into a stemBucket. A file
// whose stat fails is dropped rather than failing the whole walk: the stream
// paints what it can read for a person to look at, and nothing downstream of
// it prunes a catalogue. ScanDir, which the catalogue does trust, fails
// instead when a stat error hides files that are still there.
func (b *entryBucket) resolve() stemBucket {
	out := stemBucket{stem: b.stem}
	for _, s := range b.slots {
		info, err := s.entry.Info()
		if err != nil {
			continue
		}
		path := filepath.Join(b.dir, s.name)
		if info.Mode()&os.ModeSymlink != 0 {
			// The link's own metadata is not the frame's. Stat the target the
			// way ScanDir does, and drop a link that leads nowhere or to a
			// directory.
			target, err := os.Stat(path)
			if err != nil || target.IsDir() {
				continue
			}
			info = target
		}
		out.add(s.class, fileSlot{
			file: FileRef{
				Path:    path,
				Size:    info.Size(),
				ModTime: info.ModTime(),
			},
			name:     s.name,
			priority: s.priority,
		})
	}
	return out
}

// readEntries reads the directory in chunks and buckets the recognised files
// by stem, without stat'ing any of them. It returns the buckets and their keys
// in the order ScanDir would sort the finished groups into.
//
// The whole listing is read before any frame is emitted because grouping is
// not final until it has been: a JPEG handed over on its own would have to be
// taken back when the RAW that pairs with it turns up later in the directory.
func readEntries(ctx context.Context, dir string, cfg Config) (map[string]*entryBucket, []string, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	buckets := make(map[string]*entryBucket)
	for {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		entries, readErr := f.ReadDir(readdirChunk)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			// Hidden files are never frames. macOS writes AppleDouble
			// companions (._NAME.RAF) onto SMB and exFAT volumes that carry
			// real image extensions but hold resource-fork metadata.
			if strings.HasPrefix(name, ".") {
				continue
			}
			class, prio := cfg.class(strings.ToLower(filepath.Ext(name)))
			if class == "" {
				continue
			}
			key, stem := stemKey(name, cfg)
			b, ok := buckets[key]
			if !ok {
				b = &entryBucket{dir: dir}
				buckets[key] = b
			}
			b.noteStem(stem)
			b.slots = append(b.slots, entrySlot{entry: e, name: name, class: class, priority: prio})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, nil, readErr
		}
	}

	order := make([]string, 0, len(buckets))
	for key := range buckets {
		order = append(order, key)
	}
	// The key is the lowercased stem, which is what ScanDir sorts its finished
	// groups by, so batches arrive as a prefix of that same order.
	sort.Strings(order)
	return buckets, order, nil
}
