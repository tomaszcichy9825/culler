package app

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/platform"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// resultQueue is how many hashed frames may wait to be resolved against the
// store. Resolving is a local database read and hashing is a file read over
// whatever the card is plugged into, so this only ever buffers a hiccup.
const resultQueue = 256

// progressEvery throttles the progress event to one per this many identities,
// matching what the unstreamed open reports.
const progressEvery = 16

// streamBatching bounds how much a streamed open holds back before it emits.
type streamBatching struct {
	Frames int           // frames per scan:frames batch
	Hashes int           // identities per scan:hashed batch
	Delay  time.Duration // longest a partly filled batch is held
}

func (b streamBatching) withDefaults() streamBatching {
	if b.Frames <= 0 {
		b.Frames = scan.DefaultBatchSize
	}
	if b.Hashes <= 0 {
		b.Hashes = scan.DefaultBatchSize
	}
	if b.Delay <= 0 {
		b.Delay = scan.DefaultMaxDelay
	}
	return b
}

// ScanTicket names one streamed open. Every event the open produces carries
// the same token, and a token is only ever issued once, so the frontend can
// drop anything that does not belong to the open it is currently showing.
type ScanTicket struct {
	Token   string `json:"token"`
	Seq     int64  `json:"seq"`     // rises with every open; higher wins
	Dir     string `json:"dir"`     // resolved absolute path
	Network bool   `json:"network"` // lives on a network volume
}

// OpenFolderStream starts an open and returns as soon as the folder has been
// checked, without waiting for the walk. Frames arrive on scan:frames as the
// walk finds them, their identities and recorded decisions follow on
// scan:hashed, and scan:done closes the open.
//
// Opening another folder abandons whatever is still in flight: the abandoned
// open stops emitting entirely, so a slow scan can never land on top of the
// folder the user has moved to.
func (s *LibraryService) OpenFolderStream(dir string) (ScanTicket, error) {
	resolved, err := resolveFolder(dir)
	if err != nil {
		return ScanTicket{}, err
	}
	// The store opens before the ticket is issued so that an unusable data
	// directory is an error the caller can show, not a failed open that only
	// turns up in the middle of a scan.
	store, err := s.app.decisions()
	if err != nil {
		return ScanTicket{}, err
	}

	network := platform.IsNetwork(resolved)
	workers := s.workers
	if workers <= 0 {
		workers = s.app.hashWorkers(network)
	}

	ctx, cancel, ticket := s.begin(resolved, network)
	st := &folderStream{
		ticket:  ticket,
		cfg:     s.app.Config().ScanConfig(),
		store:   store,
		hashFn:  s.hashFn,
		capture: s.captureFn,
		emit:    s.emit,
		workers: workers,
		batch:   s.batch.withDefaults(),
	}
	go st.run(ctx, cancel)
	return ticket, nil
}

// begin abandons the open in flight and issues the next ticket.
func (s *LibraryService) begin(dir string, network bool) (context.Context, context.CancelFunc, ScanTicket) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
	s.seq++
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	return ctx, cancel, ScanTicket{
		Token:   fmt.Sprintf("scan-%d", s.seq),
		Seq:     s.seq,
		Dir:     dir,
		Network: network,
	}
}

// folderStream is one open in flight.
type folderStream struct {
	ticket  ScanTicket
	cfg     scan.Config
	store   *decide.Store
	hashFn  func(path string) (string, error)
	capture func(path string) (time.Time, bool)
	emit    func(name string, data any)
	workers int
	batch   streamBatching

	// discovered is how many frames the walk has handed over so far, which is
	// all the total a progress bar can honestly show mid-walk.
	discovered atomic.Int64
	// storeErr is the first decision-store read that failed. The frames still
	// arrive; they simply show as undecided, and the open says so when it ends.
	storeErr error
}

// frameResult is one frame with its identity read.
type frameResult struct {
	group scan.PhotoGroup
	hash  string
	// shot is the capture time out of the file's metadata; taken is false when
	// the file carried none and the walk's mtime stands.
	shot  time.Time
	taken bool
}

// run walks the folder, hashes behind the walk and reports. It emits nothing
// once the open has been abandoned, including its own completion.
func (f *folderStream) run(ctx context.Context, release context.CancelFunc) {
	defer release()

	frames := newFrameQueue()
	results := make(chan frameResult, resultQueue)
	// An abandoned open has nothing left to hash; waking the hashers is what
	// lets them notice.
	go func() {
		<-ctx.Done()
		frames.close()
	}()

	var hashers sync.WaitGroup
	for i := 0; i < f.workers; i++ {
		hashers.Add(1)
		go func() {
			defer hashers.Done()
			f.hash(ctx, frames, results)
		}()
	}
	collected := make(chan struct{})
	go f.collect(ctx, results, collected)

	total := 0
	opts := scan.StreamOptions{BatchSize: f.batch.Frames, MaxDelay: f.batch.Delay}
	err := scan.ScanDirStreamContext(ctx, f.ticket.Dir, f.cfg, opts, func(batch []scan.PhotoGroup) {
		total += len(batch)
		f.discovered.Store(int64(total))

		// The grid paints from this, so it goes out before the frames are
		// queued for hashing rather than after.
		dtos := make([]GroupDTO, 0, len(batch))
		for _, g := range batch {
			dtos = append(dtos, frameDTO(g))
		}
		f.send(ctx, EventScanFrames, ScanFrames{
			Token:  f.ticket.Token,
			Seq:    f.ticket.Seq,
			Dir:    f.ticket.Dir,
			Frames: dtos,
		})

		for _, g := range batch {
			frames.push(g)
		}
	})

	frames.close()
	hashers.Wait()
	close(results)
	<-collected

	if ctx.Err() != nil {
		return
	}
	done := ScanDone{Token: f.ticket.Token, Seq: f.ticket.Seq, Dir: f.ticket.Dir, Total: total}
	switch {
	case err != nil:
		done.Error = fmt.Sprintf("scan %s: %v", f.ticket.Dir, err)
	case f.storeErr != nil:
		done.Error = fmt.Sprintf("read decisions: %v", f.storeErr)
	}
	f.emit(EventScanDone, done)
}

// frameQueue hands frames from the walk to the hashers without ever making the
// walk wait. Hashing a card over SMB is slower than listing it by orders of
// magnitude, and the walk's own listing is already in memory, so holding the
// frames costs nothing next to stalling the paint behind the slowest hash.
type frameQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	items  []scan.PhotoGroup
	closed bool
}

func newFrameQueue() *frameQueue {
	q := &frameQueue{}
	q.ready = sync.NewCond(&q.mu)
	return q
}

func (q *frameQueue) push(g scan.PhotoGroup) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	q.items = append(q.items, g)
	q.ready.Signal()
}

// pop blocks until a frame is available, reporting false once the queue has
// been drained and closed.
func (q *frameQueue) pop() (scan.PhotoGroup, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.items) == 0 && !q.closed {
		q.ready.Wait()
	}
	if len(q.items) == 0 {
		return scan.PhotoGroup{}, false
	}
	g := q.items[0]
	q.items = q.items[1:]
	return g, true
}

// close is safe to call more than once: both the end of the walk and an
// abandoned open reach it.
func (q *frameQueue) close() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.closed = true
	q.ready.Broadcast()
}

// hash reads the identity of each frame it is given. A frame whose primary
// file cannot be read yields an empty hash rather than stopping the open.
func (f *folderStream) hash(ctx context.Context, frames *frameQueue, results chan<- frameResult) {
	for {
		if ctx.Err() != nil {
			return
		}
		g, ok := frames.pop()
		if !ok {
			return
		}
		var h string
		var shot time.Time
		var taken bool
		if ref := primaryRef(g); ref != nil {
			if v, err := f.hashFn(ref.Path); err == nil {
				h = v
			}
			// Read on the file this pass already opened. A frame whose bytes
			// would not hash may still say when it was taken, and a tile with
			// the right date beats one with the day of the copy.
			if f.capture != nil {
				shot, taken = f.capture(ref.Path)
			}
		}
		select {
		case results <- frameResult{group: g, hash: h, shot: shot, taken: taken}:
		case <-ctx.Done():
			return
		}
	}
}

// collect batches finished identities, resolves what the store remembers about
// each and reports progress. It is the only goroutine that touches the store,
// so the reads stay serialised behind the hashing.
func (f *folderStream) collect(ctx context.Context, results <-chan frameResult, done chan<- struct{}) {
	defer close(done)

	ticker := time.NewTicker(f.batch.Delay)
	defer ticker.Stop()

	pending := make([]FrameHash, 0, f.batch.Hashes)
	completed := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		f.send(ctx, EventScanHashed, ScanHashed{
			Token:  f.ticket.Token,
			Seq:    f.ticket.Seq,
			Dir:    f.ticket.Dir,
			Frames: pending,
		})
		pending = make([]FrameHash, 0, f.batch.Hashes)
	}
	progress := func() {
		f.send(ctx, EventScanProgress, ScanProgress{
			Token: f.ticket.Token,
			Dir:   f.ticket.Dir,
			Done:  completed,
			Total: int(f.discovered.Load()),
		})
	}

	for {
		select {
		case r, ok := <-results:
			if !ok {
				flush()
				progress()
				return
			}
			completed++
			pending = append(pending, f.identity(r))
			if len(pending) >= f.batch.Hashes {
				flush()
			}
			if completed%progressEvery == 0 {
				progress()
			}
		case <-ticker.C:
			flush()
		case <-ctx.Done():
			return
		}
	}
}

// identity resolves what has been recorded against a hashed frame.
func (f *folderStream) identity(r frameResult) FrameHash {
	var rec decide.Record
	if r.hash != "" {
		recorded, ok, err := f.store.Get(r.hash, r.group.Dir, r.group.Stem)
		switch {
		case err != nil:
			if f.storeErr == nil {
				f.storeErr = err
			}
		case ok:
			rec = recorded
		}
	}
	return frameIdentity(r.group, r.hash, r.shot, rec)
}

// send drops the payload if the open has been abandoned. The frontend filters
// on the token as well: an emission already on its way out cannot be recalled.
func (f *folderStream) send(ctx context.Context, name string, data any) {
	if ctx.Err() != nil {
		return
	}
	f.emit(name, data)
}
