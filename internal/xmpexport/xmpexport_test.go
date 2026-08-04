package xmpexport

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// frame builds a group whose files sit in dir. A stem given both a RAW and a
// JPEG is a pair; either one alone is a single-file frame.
func frame(t *testing.T, dir, stem string, raw, jpeg bool) scan.PhotoGroup {
	t.Helper()
	g := scan.PhotoGroup{Dir: dir, Stem: stem}
	if raw {
		path := filepath.Join(dir, stem+".RAF")
		if err := os.WriteFile(path, []byte("raw bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		g.Raw = &scan.FileRef{Path: path}
	}
	if jpeg {
		path := filepath.Join(dir, stem+".JPG")
		if err := os.WriteFile(path, []byte("jpeg bytes"), 0o644); err != nil {
			t.Fatal(err)
		}
		g.Jpeg = &scan.FileRef{Path: path}
	}
	return g
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// wellFormed fails the test if data is not parseable XML. Every sidecar this
// package produces has to be readable by the tools it is written for.
func wellFormed(t *testing.T, data string) {
	t.Helper()
	d := xml.NewDecoder(strings.NewReader(data))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("not well-formed XML: %v\n%s", err, data)
		}
	}
}

func TestSidecarGoesBesideTheRAW(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, true)

	if got, want := SidecarPath(g), filepath.Join(dir, "DSCF0001.RAF.xmp"); got != want {
		t.Errorf("sidecar path: want %q, got %q", want, got)
	}
}

func TestSidecarGoesBesideTheJPEGWhenThereIsNoRAW(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0002", false, true)

	if got, want := SidecarPath(g), filepath.Join(dir, "DSCF0002.JPG.xmp"); got != want {
		t.Errorf("sidecar path: want %q, got %q", want, got)
	}
}

func TestWriteExportsTheRatingAndTheLabel(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)

	res, err := Write(g, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 4})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.Action != ActionWritten {
		t.Errorf("action: want %q, got %q", ActionWritten, res.Action)
	}

	body := read(t, SidecarPath(g))
	wellFormed(t, body)
	if !strings.Contains(body, "<xmp:Rating>4</xmp:Rating>") {
		t.Errorf("rating missing:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:Label>Green</xmp:Label>") {
		t.Errorf("label missing:\n%s", body)
	}
}

func TestWriteLabelsACutRed(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)

	if _, err := Write(g, decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	body := read(t, SidecarPath(g))
	if !strings.Contains(body, "<xmp:Label>Red</xmp:Label>") {
		t.Errorf("a cut must read as a red label:\n%s", body)
	}
	// Unrated is the absence of a rating, not a zero: writing 0 would tell
	// Lightroom the photograph was judged and found worth no stars.
	if strings.Contains(body, "xmp:Rating") {
		t.Errorf("an unrated frame must carry no rating:\n%s", body)
	}
}

func TestWriteLeavesNoFileForAFrameWithNothingToSay(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)

	res, err := Write(g, decide.Record{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.Action != ActionNone {
		t.Errorf("action: want %q, got %q", ActionNone, res.Action)
	}
	if _, err := os.Stat(SidecarPath(g)); !os.IsNotExist(err) {
		t.Error("an undecided, unrated frame must not litter the folder with a sidecar")
	}
}

// Clearing a decision removes what this app exported and nothing else. The
// sidecar itself stays: other tools own the rest of it.
func TestWriteClearsOurFieldsWithoutDeletingTheSidecar(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)
	if _, err := Write(g, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 5}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	res, err := Write(g, decide.Record{})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if res.Action != ActionCleared {
		t.Errorf("action: want %q, got %q", ActionCleared, res.Action)
	}
	body := read(t, SidecarPath(g))
	wellFormed(t, body)
	if strings.Contains(body, "xmp:Rating") || strings.Contains(body, "xmp:Label") {
		t.Errorf("our fields survived the clear:\n%s", body)
	}
}

// A second export of the same frame replaces the values rather than adding a
// second copy of each field.
func TestWriteIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)

	if _, err := Write(g, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 2}); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if _, err := Write(g, decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth, Rating: 5}); err != nil {
		t.Fatalf("second Write: %v", err)
	}

	body := read(t, SidecarPath(g))
	wellFormed(t, body)
	if n := strings.Count(body, "<xmp:Rating>"); n != 1 {
		t.Errorf("want one rating, got %d:\n%s", n, body)
	}
	if !strings.Contains(body, "<xmp:Rating>5</xmp:Rating>") || !strings.Contains(body, "<xmp:Label>Red</xmp:Label>") {
		t.Errorf("the second export did not replace the first:\n%s", body)
	}
}

// Writing the same values twice must not touch the file at all, so an export
// run over an unchanged folder leaves every modification time alone.
func TestWriteSkipsAFileItWouldNotChange(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)
	rec := decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 3}
	if _, err := Write(g, rec); err != nil {
		t.Fatalf("first Write: %v", err)
	}
	before, err := os.Stat(SidecarPath(g))
	if err != nil {
		t.Fatal(err)
	}

	res, err := Write(g, rec)
	if err != nil {
		t.Fatalf("second Write: %v", err)
	}
	if res.Action != ActionNone {
		t.Errorf("action: want %q, got %q", ActionNone, res.Action)
	}
	after, err := os.Stat(SidecarPath(g))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("an export that changes nothing must not rewrite the file")
	}
}

// A sidecar this package cannot parse is left exactly as it was. Refusing is
// the only safe answer: the alternative is overwriting a file whose contents
// another tool owns.
func TestWriteRefusesToTouchASidecarItCannotRead(t *testing.T) {
	dir := t.TempDir()
	g := frame(t, dir, "DSCF0001", true, false)
	path := SidecarPath(g)
	const junk = "this is not XML at all <<<"
	if err := os.WriteFile(path, []byte(junk), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Write(g, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 1}); err == nil {
		t.Fatal("want an error for an unreadable sidecar")
	}
	if got := read(t, path); got != junk {
		t.Errorf("the file was modified: %q", got)
	}
}

func TestWriteNeedsAFrame(t *testing.T) {
	if _, err := Write(scan.PhotoGroup{Dir: t.TempDir(), Stem: "DSCF0001"}, decide.Record{Rating: 3}); err == nil {
		t.Error("a group with no files has nothing to put a sidecar beside")
	}
}

func TestLabelForVerdict(t *testing.T) {
	for _, tc := range []struct {
		verdict decide.Verdict
		want    string
	}{
		{decide.Keep, "Green"},
		{decide.Cut, "Red"},
		{decide.Undecided, ""},
		{decide.Verdict("nonsense"), ""},
	} {
		if got := Label(tc.verdict); got != tc.want {
			t.Errorf("Label(%q): want %q, got %q", tc.verdict, tc.want, got)
		}
	}
}
