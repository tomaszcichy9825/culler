package thumbs

import (
	"container/heap"
	"context"
	"errors"
	"runtime"
	"sync"
)

// ErrClosed is reported to requests made after the pool has been closed.
var ErrClosed = errors.New("thumbs: pool closed")

var errNoSource = errors.New("thumbs: request has no source function")

// Pool decodes thumbnails into a Store on a fixed set of workers, highest
// priority first. Priority follows the viewport: visible tiles outrank
// overscan, and a tile that scrolled away cancels its context rather than
// holding a worker.
type Pool struct {
	store *Store

	mu     sync.Mutex
	cond   *sync.Cond
	queue  requestQueue
	seq    uint64
	closed bool
	wg     sync.WaitGroup
}

// NewPool starts a pool of workers writing into store. workers of 0 or less
// means runtime.NumCPU().
func NewPool(store *Store, workers int) *Pool {
	if workers <= 0 {
		workers = runtime.NumCPU()
	}
	p := &Pool{store: store}
	p.cond = sync.NewCond(&p.mu)
	p.wg.Add(workers)
	for range workers {
		go p.work()
	}
	return p
}

// Request schedules a thumbnail for (key, size). source is called only when
// the cache misses, and returns the JPEG bytes to resize — an external JPEG,
// an embedded RAW preview, whichever the caller decided on. done is called
// exactly once, with the cache path or with the error that stopped it:
// ctx.Err() if the request was cancelled before it ran, ErrClosed if the pool
// has shut down. Higher priority runs first.
func (p *Pool) Request(ctx context.Context, key string, size Size, priority int, source func() ([]byte, error), done func(path string, err error)) {
	// Caught here rather than on a worker: a panic in a worker goroutine takes
	// the whole application down mid-cull.
	if source == nil {
		if done != nil {
			done("", errNoSource)
		}
		return
	}

	r := &request{
		ctx:      ctx,
		key:      key,
		size:     size,
		priority: priority,
		source:   source,
		done:     done,
	}

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		r.report("", ErrClosed)
		return
	}
	p.seq++
	r.seq = p.seq
	heap.Push(&p.queue, r)
	p.mu.Unlock()

	p.cond.Signal()
}

// Close stops the pool once every queued and in-flight request has been
// reported. It is safe to call more than once.
func (p *Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.cond.Broadcast()
	p.wg.Wait()
}

func (p *Pool) work() {
	defer p.wg.Done()
	for {
		p.mu.Lock()
		for p.queue.Len() == 0 && !p.closed {
			p.cond.Wait()
		}
		if p.queue.Len() == 0 {
			p.mu.Unlock()
			return
		}
		r := heap.Pop(&p.queue).(*request)
		p.mu.Unlock()

		r.run(p.store)
	}
}

// request is one queued thumbnail job.
type request struct {
	ctx      context.Context
	key      string
	size     Size
	priority int
	seq      uint64 // arrival order, breaks ties within a priority
	source   func() ([]byte, error)
	done     func(string, error)
}

func (r *request) run(s *Store) {
	if err := r.ctx.Err(); err != nil {
		r.report("", err)
		return
	}
	if path, ok := s.Path(r.key, r.size); ok {
		s.Touch(r.key, r.size)
		r.report(path, nil)
		return
	}

	src, err := r.source()
	if err != nil {
		r.report("", err)
		return
	}
	// Reading the source can take a while on a card reader; check again
	// before paying for the decode.
	if err := r.ctx.Err(); err != nil {
		r.report("", err)
		return
	}

	path, err := s.Put(r.key, r.size, src)
	if err != nil {
		r.report("", err)
		return
	}
	r.report(path, nil)
}

func (r *request) report(path string, err error) {
	if r.done != nil {
		r.done(path, err)
	}
}

// requestQueue is a max-heap on priority, FIFO within a priority.
type requestQueue []*request

func (q requestQueue) Len() int { return len(q) }

func (q requestQueue) Less(i, j int) bool {
	if q[i].priority != q[j].priority {
		return q[i].priority > q[j].priority
	}
	return q[i].seq < q[j].seq
}

func (q requestQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *requestQueue) Push(x any) { *q = append(*q, x.(*request)) }

func (q *requestQueue) Pop() any {
	old := *q
	n := len(old)
	r := old[n-1]
	old[n-1] = nil
	*q = old[:n-1]
	return r
}
