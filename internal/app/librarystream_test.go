package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/hash"
)

// recorder captures what a streamed open emits, in order.
type recorder struct {
	mu     sync.Mutex
	events []recorded
}

type recorded struct {
	name string
	data any
}

func (r *recorder) emit(name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recorded{name, data})
}

func (r *recorder) snapshot() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded{}, r.events...)
}

// frames returns every frame carried by scan:frames events for a token, in
// arrival order.
func (r *recorder) frames(token string) []GroupDTO {
	var out []GroupDTO
	for _, e := range r.snapshot() {
		if p, ok := e.data.(ScanFrames); ok && e.name == EventScanFrames && p.Token == token {
			out = append(out, p.Frames...)
		}
	}
	return out
}

// hashed returns every frame identity carried by scan:hashed events.
func (r *recorder) hashed(token string) []FrameHash {
	var out []FrameHash
	for _, e := range r.snapshot() {
		if p, ok := e.data.(ScanHashed); ok && e.name == EventScanHashed && p.Token == token {
			out = append(out, p.Frames...)
		}
	}
	return out
}

func (r *recorder) done(token string) (ScanDone, bool) {
	for _, e := range r.snapshot() {
		if p, ok := e.data.(ScanDone); ok && e.name == EventScanDone && p.Token == token {
			return p, true
		}
	}
	return ScanDone{}, false
}

func (r *recorder) count(name string) int {
	n := 0
	for _, e := range r.snapshot() {
		if e.name == name {
			n++
		}
	}
	return n
}

// waitFor polls until cond holds, failing the test if it never does. Polling
// keeps the assertions readable without a channel per event kind.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// streamService returns a library service whose emissions are captured and
// whose hashing is the given function.
func streamService(t *testing.T, a *App, rec *recorder, hashFn func(string) (string, error)) *LibraryService {
	t.Helper()
	s := NewLibraryService(a)
	s.emit = rec.emit
	if hashFn != nil {
		s.hashFn = hashFn
	}
	return s
}

// A blocked hash must not hold up the grid: the frames land while every hash
// is still stuck.
func TestOpenFolderStreamPaintsFramesBeforeHashing(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	rec := &recorder{}

	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(2)
	s := streamService(t, a, rec, func(path string) (string, error) {
		started.Done()
		<-release
		return hash.Content(path)
	})

	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatalf("OpenFolderStream: %v", err)
	}
	if ticket.Token == "" || ticket.Dir != dir {
		t.Fatalf("ticket = %+v, want a token and the resolved dir", ticket)
	}

	waitFor(t, "the frames batch", func() bool { return len(rec.frames(ticket.Token)) == 2 })
	// Both workers are inside the blocked hash, so nothing could have been
	// resolved behind them.
	started.Wait()
	if n := len(rec.hashed(ticket.Token)); n != 0 {
		t.Fatalf("%d frames were hashed while every hash was blocked", n)
	}
	if _, ok := rec.done(ticket.Token); ok {
		t.Fatal("the open reported done before hashing finished")
	}
	for _, f := range rec.frames(ticket.Token) {
		if f.Hash != "" {
			t.Errorf("frame %s arrived carrying a hash before it was computed", f.Stem)
		}
		if f.Verdict != "" || f.Rating != 0 || f.Destination != "" {
			t.Errorf("frame %s arrived with a decision that could not be known yet: %+v", f.Stem, f)
		}
	}

	close(release)
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })
	if n := len(rec.hashed(ticket.Token)); n != 2 {
		t.Errorf("%d frames hashed, want 2", n)
	}
}

// The walk must not be paced by the hashers at any depth: a folder far larger
// than any queue between the two still paints in full while every hash is
// stuck.
func TestOpenFolderStreamPaintsEveryFrameWhileHashingIsBlocked(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	const frames = 300
	for i := 0; i < frames; i++ {
		name := filepath.Join(dir, fmt.Sprintf("IMG_%04d.jpg", i))
		if err := os.WriteFile(name, []byte("jpeg bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	rec := &recorder{}
	release := make(chan struct{})
	s := streamService(t, a, rec, func(path string) (string, error) {
		<-release
		return hash.Content(path)
	})
	s.workers = 2

	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "every frame to be painted", func() bool { return len(rec.frames(ticket.Token)) == frames })
	if n := len(rec.hashed(ticket.Token)); n != 0 {
		t.Errorf("%d identities resolved while every hash was blocked", n)
	}

	close(release)
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })
	if n := len(rec.hashed(ticket.Token)); n != frames {
		t.Errorf("%d identities resolved, want %d", n, frames)
	}
}

// What the stream ends up with has to be what the batch open would have
// returned, field for field.
func TestOpenFolderStreamMatchesOpenFolder(t *testing.T) {
	a := testApp(t)
	dir := card(t)

	// A decision on the paired frame so the resolved fields are not all empty.
	jpegHash, err := hash.Content(filepath.Join(dir, "DSCF0001.JPG"))
	if err != nil {
		t.Fatal(err)
	}
	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetVerdict(jpegHash, dir, "DSCF0001", decide.Keep, decide.MaskJPEG); err != nil {
		t.Fatal(err)
	}
	if err := store.SetRating(jpegHash, dir, "DSCF0001", 4); err != nil {
		t.Fatal(err)
	}
	if err := store.SetDestination(jpegHash, dir, "DSCF0001", "/library/keepers", decide.VerbDefault); err != nil {
		t.Fatal(err)
	}

	want, err := NewLibraryService(a).OpenFolder(dir)
	if err != nil {
		t.Fatalf("OpenFolder: %v", err)
	}

	rec := &recorder{}
	s := streamService(t, a, rec, nil)
	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatalf("OpenFolderStream: %v", err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	if ticket.Network != want.Network {
		t.Errorf("ticket network = %v, want %v", ticket.Network, want.Network)
	}
	got := rec.frames(ticket.Token)
	if len(got) != len(want.Groups) {
		t.Fatalf("streamed %d frames, the batch open found %d", len(got), len(want.Groups))
	}
	// Fold the identities in, which is what the frontend does with the two
	// event kinds.
	byStem := make(map[string]*GroupDTO, len(got))
	for i := range got {
		byStem[got[i].Dir+"\x00"+got[i].Stem] = &got[i]
	}
	for _, h := range rec.hashed(ticket.Token) {
		g, ok := byStem[h.Dir+"\x00"+h.Stem]
		if !ok {
			t.Fatalf("an identity arrived for %s/%s, which was never handed over as a frame", h.Dir, h.Stem)
		}
		g.Hash = h.Hash
		g.Verdict = h.Verdict
		g.Mask = h.Mask
		g.Rating = h.Rating
		g.Destination = h.Destination
		g.Decision = h.Decision
		g.Warnings = h.Warnings
	}
	for i, w := range want.Groups {
		if !equalGroupDTO(got[i], w) {
			t.Errorf("frame %d:\n streamed %+v\n   batch %+v", i, got[i], w)
		}
	}
	if d, _ := rec.done(ticket.Token); d.Total != len(want.Groups) || d.Error != "" {
		t.Errorf("done = %+v, want %d frames and no error", d, len(want.Groups))
	}
}

func equalGroupDTO(a, b GroupDTO) bool {
	// An empty warning list and an absent one describe the same frame.
	if len(a.Warnings) == 0 && len(b.Warnings) == 0 {
		a.Warnings, b.Warnings = nil, nil
	}
	return reflect.DeepEqual(a, b)
}

// A frame whose primary file cannot be read still arrives, and says so.
func TestOpenFolderStreamWarnsOnUnreadableFrame(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	rec := &recorder{}

	s := streamService(t, a, rec, func(path string) (string, error) {
		if filepath.Base(path) == "DSCF0002.JPG" {
			return "", errors.New("vanished")
		}
		return hash.Content(path)
	})
	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	var found bool
	for _, h := range rec.hashed(ticket.Token) {
		if h.Stem != "DSCF0002" {
			continue
		}
		found = true
		if h.Hash != "" {
			t.Errorf("unreadable frame reported hash %q", h.Hash)
		}
		if len(h.Warnings) == 0 {
			t.Error("unreadable frame carries no warning about its lost identity")
		}
	}
	if !found {
		t.Error("the unreadable frame never had its identity reported")
	}
}

// Opening another folder abandons the one in flight: nothing more from the old
// open reaches the frontend, not even its completion.
func TestOpenFolderStreamSupersedesThePreviousOpen(t *testing.T) {
	a := testApp(t)
	first, second := card(t), card(t)
	rec := &recorder{}

	release := make(chan struct{})
	var blocked sync.Once
	reached := make(chan struct{})
	s := streamService(t, a, rec, func(path string) (string, error) {
		blocked.Do(func() { close(reached) })
		<-release
		return hash.Content(path)
	})

	old, err := s.OpenFolderStream(first)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the first open's frames", func() bool { return len(rec.frames(old.Token)) == 2 })
	<-reached

	fresh, err := s.OpenFolderStream(second)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Token == old.Token {
		t.Fatal("the second open reused the first open's token")
	}
	if fresh.Seq <= old.Seq {
		t.Errorf("second open's seq %d does not follow the first's %d", fresh.Seq, old.Seq)
	}
	before := len(rec.frames(old.Token)) + len(rec.hashed(old.Token))

	close(release)
	waitFor(t, "the second open to finish", func() bool { _, ok := rec.done(fresh.Token); return ok })
	time.Sleep(20 * time.Millisecond) // give an abandoned emission a chance to land

	if _, ok := rec.done(old.Token); ok {
		t.Error("the abandoned open reported done")
	}
	if after := len(rec.frames(old.Token)) + len(rec.hashed(old.Token)); after != before {
		t.Errorf("the abandoned open emitted %d more payloads after being superseded", after-before)
	}
}

// The identity-hash concurrency is capped, which is the whole point on a
// network share where parallel head reads stall the mount.
func TestOpenFolderStreamRespectsWorkerCap(t *testing.T) {
	a := testApp(t)
	dir := t.TempDir()
	for i := 0; i < 24; i++ {
		name := filepath.Join(dir, "IMG_"+string(rune('a'+i))+".jpg")
		if err := os.WriteFile(name, []byte("jpeg bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var mu sync.Mutex
	live, peak := 0, 0
	rec := &recorder{}
	s := streamService(t, a, rec, func(path string) (string, error) {
		mu.Lock()
		live++
		if live > peak {
			peak = live
		}
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		live--
		mu.Unlock()
		return hash.Content(path)
	})
	s.workers = 2

	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	mu.Lock()
	defer mu.Unlock()
	if peak > 2 {
		t.Errorf("%d hashes ran at once, over the cap of 2", peak)
	}
	if peak < 2 {
		t.Errorf("peak concurrency was %d; the cap is not being used at all", peak)
	}
}

// A network volume gets the configured low cap rather than every CPU.
func TestStreamWorkersFollowTheSource(t *testing.T) {
	a := testApp(t)
	if got, want := a.hashWorkers(true), a.Config().Behaviour.NetworkHashWorkers; got != want {
		t.Errorf("network hash workers = %d, want the configured %d", got, want)
	}
	if a.hashWorkers(false) <= 0 {
		t.Errorf("local hash workers = %d", a.hashWorkers(false))
	}
}

func TestOpenFolderStreamRejectsBadPaths(t *testing.T) {
	a := testApp(t)
	s := NewLibraryService(a)

	if _, err := s.OpenFolderStream(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Error("a missing folder was accepted")
	}
	file := filepath.Join(t.TempDir(), "a.jpg")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenFolderStream(file); err == nil {
		t.Error("a file was accepted as a folder")
	}
	if _, err := s.OpenFolderStream(""); err == nil {
		t.Error("an empty path was accepted")
	}
}

// Progress keeps reporting while the hashes come in, so the existing indicator
// still has something to show.
func TestOpenFolderStreamReportsProgress(t *testing.T) {
	a := testApp(t)
	dir := card(t)
	rec := &recorder{}

	s := streamService(t, a, rec, nil)
	ticket, err := s.OpenFolderStream(dir)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })

	if rec.count(EventScanProgress) == 0 {
		t.Fatal("no progress was reported")
	}
	var last ScanProgress
	for _, e := range rec.snapshot() {
		if p, ok := e.data.(ScanProgress); ok {
			if p.Token != ticket.Token || p.Dir != dir {
				t.Fatalf("progress %+v does not belong to the open", p)
			}
			last = p
		}
	}
	if last.Done != 2 || last.Total != 2 {
		t.Errorf("last progress = %d/%d, want 2/2", last.Done, last.Total)
	}
}

// An empty folder still completes, so the frontend can stop waiting on it.
func TestOpenFolderStreamOnEmptyFolder(t *testing.T) {
	a := testApp(t)
	rec := &recorder{}
	s := streamService(t, a, rec, nil)

	ticket, err := s.OpenFolderStream(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the open to finish", func() bool { _, ok := rec.done(ticket.Token); return ok })
	if n := len(rec.frames(ticket.Token)); n != 0 {
		t.Errorf("%d frames in an empty folder", n)
	}
	if d, _ := rec.done(ticket.Token); d.Total != 0 || d.Error != "" {
		t.Errorf("done = %+v, want an empty, clean finish", d)
	}
}
