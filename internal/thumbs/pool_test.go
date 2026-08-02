package thumbs

import (
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// result collects pool callbacks in completion order.
type result struct {
	mu    sync.Mutex
	order []string
	errs  map[string]error
	paths map[string]string
}

func newResult() *result {
	return &result{errs: map[string]error{}, paths: map[string]string{}}
}

func (r *result) record(name string) func(string, error) {
	return func(path string, err error) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.order = append(r.order, name)
		r.errs[name] = err
		r.paths[name] = path
	}
}

func (r *result) snapshot() ([]string, map[string]error, map[string]string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...), maps(r.errs), strs(r.paths)
}

func maps(in map[string]error) map[string]error {
	out := make(map[string]error, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func strs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func newTestPool(t *testing.T, workers int) (*Store, *Pool) {
	t.Helper()
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := NewPool(s, workers)
	t.Cleanup(p.Close)
	return s, p
}

func TestPoolRunsHigherPriorityFirst(t *testing.T) {
	_, p := newTestPool(t, 1)
	src := makeJPEG(t, 400, 300)
	res := newResult()

	// Occupy the single worker so the next two requests are certain to be
	// queued together and ordered by priority rather than arrival.
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(3)
	blocking := func() ([]byte, error) {
		close(started)
		<-release
		return src, nil
	}
	done := func(name string) func(string, error) {
		rec := res.record(name)
		return func(path string, err error) {
			rec(path, err)
			wg.Done()
		}
	}

	p.Request(context.Background(), key("p0"), SizeGrid, 0, blocking, done("blocker"))
	<-started
	p.Request(context.Background(), key("p1"), SizeGrid, 1, func() ([]byte, error) { return src, nil }, done("low"))
	p.Request(context.Background(), key("p2"), SizeGrid, 9, func() ([]byte, error) { return src, nil }, done("high"))
	close(release)
	wg.Wait()

	order, errs, _ := res.snapshot()
	for name, err := range errs {
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	want := []string{"blocker", "high", "low"}
	if len(order) != len(want) {
		t.Fatalf("got %v completions, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("completion order %v, want %v", order, want)
		}
	}
}

func TestPoolDropsCancelledRequest(t *testing.T) {
	_, p := newTestPool(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var sourced atomic.Bool
	done := make(chan error, 1)
	p.Request(ctx, key("q1"), SizeGrid, 0, func() ([]byte, error) {
		sourced.Store(true)
		return makeJPEG(t, 400, 300), nil
	}, func(_ string, err error) { done <- err })

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled request never reported")
	}
	if sourced.Load() {
		t.Error("cancelled request decoded its source anyway")
	}
}

func TestPoolReportsSourceError(t *testing.T) {
	_, p := newTestPool(t, 2)
	want := errors.New("no preview in this RAW")
	done := make(chan error, 1)
	p.Request(context.Background(), key("r1"), SizeGrid, 0,
		func() ([]byte, error) { return nil, want },
		func(_ string, err error) { done <- err })

	select {
	case err := <-done:
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want %v", err, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request never reported")
	}
}

func TestPoolRejectsNilSource(t *testing.T) {
	_, p := newTestPool(t, 2)
	done := make(chan error, 1)
	p.Request(context.Background(), key("r2"), SizeGrid, 0, nil,
		func(_ string, err error) { done <- err })

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("nil source reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request never reported")
	}
}

func TestPoolServesCacheHitWithoutSource(t *testing.T) {
	s, p := newTestPool(t, 2)
	src := makeJPEG(t, 400, 300)
	cached, err := s.Put(key("s1"), SizeGrid, src)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	var sourced atomic.Bool
	type outcome struct {
		path string
		err  error
	}
	done := make(chan outcome, 1)
	p.Request(context.Background(), key("s1"), SizeGrid, 0, func() ([]byte, error) {
		sourced.Store(true)
		return src, nil
	}, func(path string, err error) { done <- outcome{path, err} })

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("err = %v", got.err)
		}
		if got.path != cached {
			t.Fatalf("path = %q, want %q", got.path, cached)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request never reported")
	}
	if sourced.Load() {
		t.Error("cache hit read the source anyway")
	}
}

func TestPoolCloseWaitsForInFlightWork(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := NewPool(s, 1)
	src := makeJPEG(t, 400, 300)

	started := make(chan struct{})
	var finished atomic.Int32
	p.Request(context.Background(), key("t1"), SizeGrid, 0, func() ([]byte, error) {
		close(started)
		time.Sleep(50 * time.Millisecond)
		return src, nil
	}, func(string, error) { finished.Add(1) })
	<-started
	// Queued behind the in-flight request; Close must drain it too.
	p.Request(context.Background(), key("t2"), SizeGrid, 0,
		func() ([]byte, error) { return src, nil },
		func(string, error) { finished.Add(1) })

	p.Close()
	if got := finished.Load(); got != 2 {
		t.Fatalf("%d requests finished when Close returned, want 2", got)
	}
	for _, label := range []string{"t1", "t2"} {
		if _, ok := s.Path(key(label), SizeGrid); !ok {
			t.Errorf("%s not in the cache after Close", label)
		}
	}
}

func TestPoolRejectsRequestsAfterClose(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := NewPool(s, 2)
	p.Close()

	done := make(chan error, 1)
	p.Request(context.Background(), key("u1"), SizeGrid, 0,
		func() ([]byte, error) { return makeJPEG(t, 400, 300), nil },
		func(_ string, err error) { done <- err })
	select {
	case err := <-done:
		if !errors.Is(err, ErrClosed) {
			t.Fatalf("err = %v, want ErrClosed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request after Close never reported")
	}
}

func TestPoolHandlesConcurrentRequests(t *testing.T) {
	s, err := NewStore(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	p := NewPool(s, 0) // runtime.NumCPU()
	src := makeJPEG(t, 320, 240)

	const n = 64
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	paths := make([]string, n)
	for i := range n {
		go func() {
			ctx := context.Background()
			size := SizeGrid
			if i%2 == 0 {
				size = SizeLoupe
			}
			p.Request(ctx, key(string(rune('a'+i%26))+string(rune('a'+i/26))), size, i%5,
				func() ([]byte, error) { return src, nil },
				func(path string, err error) {
					paths[i], errs[i] = path, err
					wg.Done()
				})
		}()
	}
	wg.Wait()
	p.Close()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("request %d: %v", i, errs[i])
		}
		if _, err := os.Stat(paths[i]); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
}
