package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

func TestGroupDTOPaired(t *testing.T) {
	g := pairedGroup("DSCF0001")
	g.Warnings = []string{"duplicate files for this frame"}

	dto := groupDTO(g, "abc123", time.Time{}, decide.Record{Verdict: decide.Keep, Mask: decide.MaskJPEG, Rating: 4})

	if dto.Kind != "paired" {
		t.Errorf("Kind = %q, want paired", dto.Kind)
	}
	if !dto.HasRaw || !dto.HasJpeg {
		t.Errorf("HasRaw/HasJpeg = %v/%v, want both true", dto.HasRaw, dto.HasJpeg)
	}
	if dto.RawPath != g.Raw.Path || dto.JpegPath != g.Jpeg.Path {
		t.Errorf("paths = %q/%q, want %q/%q", dto.RawPath, dto.JpegPath, g.Raw.Path, g.Jpeg.Path)
	}
	if dto.Sidecars != 1 {
		t.Errorf("Sidecars = %d, want 1", dto.Sidecars)
	}
	if dto.Shot != "2026-05-01T09:30:00Z" {
		t.Errorf("Shot = %q, want RFC3339", dto.Shot)
	}
	if dto.Verdict != "keep" || dto.Mask != "j" {
		t.Errorf("Verdict/Mask = %q/%q, want keep/j", dto.Verdict, dto.Mask)
	}
	if dto.Rating != 4 {
		t.Errorf("Rating = %d, want 4", dto.Rating)
	}
	// The pre-verdict field the current grid still renders.
	if dto.Decision != "drop_raw" {
		t.Errorf("Decision = %q, want the drop_raw the verdict maps onto", dto.Decision)
	}
	if dto.Hash != "abc123" {
		t.Errorf("Hash = %q", dto.Hash)
	}
	if len(dto.Warnings) != 1 {
		t.Errorf("Warnings = %v, want the scan's own warning only", dto.Warnings)
	}
	g.Warnings[0] = "mutated"
	if dto.Warnings[0] == "mutated" {
		t.Error("DTO shares the group's warnings slice")
	}
}

func TestGroupDTORawOnly(t *testing.T) {
	g := scan.PhotoGroup{
		Dir:  "/card/DCIM",
		Stem: "DSCF0002",
		Kind: scan.KindRAWOnly,
		Raw:  &scan.FileRef{Path: "/card/DCIM/DSCF0002.RAF"},
		Shot: time.Date(2026, 5, 1, 9, 31, 0, 0, time.UTC),
	}
	dto := groupDTO(g, "def456", time.Time{}, decide.Record{})
	if dto.HasJpeg || dto.JpegPath != "" {
		t.Errorf("RAW-only frame reports a JPEG: %+v", dto)
	}
	if dto.Kind != "raw-only" {
		t.Errorf("Kind = %q, want raw-only", dto.Kind)
	}
	if dto.Verdict != "" || dto.Rating != 0 {
		t.Errorf("undecided frame reports %q/%d", dto.Verdict, dto.Rating)
	}
	if dto.Decision != "none" {
		t.Errorf("Decision = %q, want none", dto.Decision)
	}
}

func TestGroupDTOUnhashableFrameIsWarned(t *testing.T) {
	dto := groupDTO(pairedGroup("DSCF0003"), "", time.Time{}, decide.Record{})
	if len(dto.Warnings) != 1 || !strings.Contains(dto.Warnings[0], "will not be remembered") {
		t.Errorf("Warnings = %v, want a warning about the missing identity", dto.Warnings)
	}
}

func TestPrimaryRefPrefersJPEG(t *testing.T) {
	g := pairedGroup("DSCF0004")
	if got := primaryRef(g); got != g.Jpeg {
		t.Errorf("primary of a paired frame is %v, want the JPEG", got)
	}
	g.Jpeg = nil
	if got := primaryRef(g); got != g.Raw {
		t.Errorf("primary of a RAW-only frame is %v, want the RAW", got)
	}
	if got := primaryRef(scan.PhotoGroup{}); got != nil {
		t.Errorf("primary of an empty frame is %v, want nil", got)
	}
}

func TestHashGroupsSkipsUnreadableFiles(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(good, []byte("\xff\xd8 jpeg bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	groups := []scan.PhotoGroup{
		{Stem: "DSCF0001", Jpeg: &scan.FileRef{Path: good}},
		{Stem: "DSCF0002", Jpeg: &scan.FileRef{Path: filepath.Join(dir, "gone.JPG")}},
		{Stem: "DSCF0003"},
	}
	hashes := hashGroups(groups, 4, nil)
	if len(hashes) != 3 {
		t.Fatalf("%d hashes for 3 groups", len(hashes))
	}
	if hashes[0] == "" {
		t.Error("readable file produced no hash")
	}
	if hashes[1] != "" || hashes[2] != "" {
		t.Errorf("missing files produced hashes: %q, %q", hashes[1], hashes[2])
	}
}

func TestExpandPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}

	got, err := expandPath("~/Pictures")
	if err != nil {
		t.Fatalf("expandPath(~): %v", err)
	}
	if want := filepath.Join(home, "Pictures"); got != want {
		t.Errorf("expandPath(~/Pictures) = %q, want %q", got, want)
	}

	if got, err := expandPath("~"); err != nil || got != home {
		t.Errorf("expandPath(~) = %q, %v, want %q", got, err, home)
	}

	// A leading tilde that is part of a name is not a home directory. The
	// fixture is absolute for the platform, so nothing rebases it either.
	odd := filepath.FromSlash(filepath.VolumeName(os.TempDir()) + "/card/~odd")
	got, err = expandPath(odd)
	if err != nil || got != odd {
		t.Errorf("expandPath(%s) = %q, %v", odd, got, err)
	}

	if _, err := expandPath(""); err == nil {
		t.Error("empty path accepted")
	}
}

func TestParseVerdict(t *testing.T) {
	tests := []struct {
		verdict, mask string
		want          decide.Record
	}{
		{"keep", "rj", decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth}},
		{"keep", "r", decide.Record{Verdict: decide.Keep, Mask: decide.MaskRAW}},
		{"cut", "j", decide.Record{Verdict: decide.Cut, Mask: decide.MaskJPEG}},
		{"", "", decide.Record{}},
	}
	for _, tt := range tests {
		v, m, err := parseVerdict(tt.verdict, tt.mask)
		if err != nil {
			t.Errorf("parseVerdict(%q, %q): %v", tt.verdict, tt.mask, err)
			continue
		}
		if v != tt.want.Verdict || m != tt.want.Mask {
			t.Errorf("parseVerdict(%q, %q) = %q/%q, want %q/%q",
				tt.verdict, tt.mask, v, m, tt.want.Verdict, tt.want.Mask)
		}
	}
	for _, tt := range []struct{ verdict, mask string }{
		{"burn", "rj"}, {"KEEP", "rj"}, {"keep", "raw"}, {"keep", "jr"}, {"cut", ""},
	} {
		if _, _, err := parseVerdict(tt.verdict, tt.mask); err == nil {
			t.Errorf("parseVerdict(%q, %q) accepted an unknown value", tt.verdict, tt.mask)
		}
	}
}

// Both directions of the compatibility mapping, which is the only thing
// keeping the pre-verdict frontend rendering.
func TestLegacyDecisionMapping(t *testing.T) {
	tests := []struct {
		decision string
		record   decide.Record
	}{
		{"none", decide.Record{}},
		{"keep_all", decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth}},
		{"drop_raw", decide.Record{Verdict: decide.Keep, Mask: decide.MaskJPEG}},
		{"drop_jpeg", decide.Record{Verdict: decide.Keep, Mask: decide.MaskRAW}},
		{"drop_all", decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth}},
	}
	for _, tt := range tests {
		got, err := parseDecision(tt.decision)
		if err != nil {
			t.Errorf("parseDecision(%q): %v", tt.decision, err)
			continue
		}
		if got != tt.record {
			t.Errorf("parseDecision(%q) = %+v, want %+v", tt.decision, got, tt.record)
		}
		if back := legacyDecision(tt.record); back != tt.decision {
			t.Errorf("legacyDecision(%+v) = %q, want %q", tt.record, back, tt.decision)
		}
	}
	// A cut keeps its name whatever the mask says, and a rating alone is not
	// a decision the old model could express.
	if got := legacyDecision(decide.Record{Verdict: decide.Cut, Mask: decide.MaskRAW}); got != "drop_all" {
		t.Errorf("a masked cut reads as %q, want drop_all", got)
	}
	if got := legacyDecision(decide.Record{Rating: 3}); got != "none" {
		t.Errorf("a rated but undecided frame reads as %q, want none", got)
	}
	for _, s := range []string{"", "delete", "DROP_RAW", "copy_to"} {
		if _, err := parseDecision(s); err == nil {
			t.Errorf("parseDecision(%q) accepted an unknown decision", s)
		}
	}
}

func TestToItemRequiresHash(t *testing.T) {
	if _, err := toVerdictItem(VerdictItem{Stem: "DSCF0001", Verdict: "keep", Mask: "rj"}); err == nil {
		t.Error("verdict without a frame identity accepted")
	}
	if _, err := toRatingItem(RatingItem{Stem: "DSCF0001", Rating: 3}); err == nil {
		t.Error("rating without a frame identity accepted")
	}
	if _, err := toLegacyItem(DecisionItem{Stem: "DSCF0001", Decision: "drop_raw"}); err == nil {
		t.Error("decision without a frame identity accepted")
	}

	item, err := toVerdictItem(VerdictItem{Hash: "h", Dir: "/card", Stem: "DSCF0001", Verdict: "cut", Mask: "rj"})
	if err != nil {
		t.Fatalf("toVerdictItem: %v", err)
	}
	if item.Verdict != decide.Cut || item.Mask != decide.MaskBoth {
		t.Errorf("verdict = %q/%q, want cut/rj", item.Verdict, item.Mask)
	}
	legacy, err := toLegacyItem(DecisionItem{Hash: "h", Dir: "/card", Stem: "DSCF0001", Decision: "drop_all"})
	if err != nil {
		t.Fatalf("toLegacyItem: %v", err)
	}
	if legacy.Verdict != decide.Cut || legacy.Mask != decide.MaskBoth {
		t.Errorf("drop_all became %q/%q, want cut/rj", legacy.Verdict, legacy.Mask)
	}
}

// The final progress report must arrive however many groups could not be
// hashed: a group with no file to read still counts towards done, or the bar
// sticks below the total forever.
func TestHashGroupsProgressReachesTheTotalWithFilelessGroups(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "DSCF0001.JPG")
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	groups := []scan.PhotoGroup{
		{Dir: dir, Stem: "DSCF0001", Jpeg: &scan.FileRef{Path: path}},
		{Dir: dir, Stem: "GHOST"}, // neither half: nothing to read
	}

	last := 0
	hashes := hashGroups(groups, 2, func(done int) { last = done })
	if hashes[0] == "" {
		t.Error("the readable frame was not hashed")
	}
	if hashes[1] != "" {
		t.Error("a file-less group grew a hash")
	}
	if last != len(groups) {
		t.Errorf("final progress = %d, want %d — the bar would stick below the total", last, len(groups))
	}
}

// Progress over a folder must only ever climb: the workers hash in parallel,
// and a report from a slower worker landing after a faster one's would rewind
// the bar in front of the user.
func TestHashGroupsReportsMonotonicProgress(t *testing.T) {
	dir := t.TempDir()
	groups := make([]scan.PhotoGroup, 0, 128)
	for i := 0; i < 128; i++ {
		stem := fmt.Sprintf("IMG_%04d", i)
		path := filepath.Join(dir, stem+".JPG")
		if err := os.WriteFile(path, []byte("frame "+stem), 0o644); err != nil {
			t.Fatal(err)
		}
		groups = append(groups, scan.PhotoGroup{
			Dir:  dir,
			Stem: stem,
			Jpeg: &scan.FileRef{Path: path},
		})
	}

	var mu sync.Mutex
	var got []int
	hashes := hashGroups(groups, 8, func(done int) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, done)
	})
	for i, h := range hashes {
		if h == "" {
			t.Fatalf("frame %d was not hashed", i)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("progress went backwards: %v", got)
		}
	}
	if len(got) == 0 || got[len(got)-1] != len(groups) {
		t.Fatalf("final report %v, want it to end at %d", got, len(groups))
	}
}
