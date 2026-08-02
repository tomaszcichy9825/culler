package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

func TestGroupDTOPaired(t *testing.T) {
	g := pairedGroup("DSCF0001")
	g.Warnings = []string{"duplicate files for this frame"}

	dto := groupDTO(g, "abc123", decide.DropRAW)

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
	if dto.Decision != "drop_raw" {
		t.Errorf("Decision = %q", dto.Decision)
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
	dto := groupDTO(g, "def456", decide.None)
	if dto.HasJpeg || dto.JpegPath != "" {
		t.Errorf("RAW-only frame reports a JPEG: %+v", dto)
	}
	if dto.Kind != "raw-only" {
		t.Errorf("Kind = %q, want raw-only", dto.Kind)
	}
	if dto.Decision != "none" {
		t.Errorf("Decision = %q, want none", dto.Decision)
	}
}

func TestGroupDTOUnhashableFrameIsWarned(t *testing.T) {
	dto := groupDTO(pairedGroup("DSCF0003"), "", decide.None)
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
	hashes := hashGroups(groups)
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

	// A leading tilde that is part of a name is not a home directory.
	got, err = expandPath("/card/~odd")
	if err != nil || got != "/card/~odd" {
		t.Errorf("expandPath(/card/~odd) = %q, %v", got, err)
	}

	if _, err := expandPath(""); err == nil {
		t.Error("empty path accepted")
	}
}

func TestParseDecision(t *testing.T) {
	for _, s := range []string{"none", "keep_all", "drop_raw", "drop_jpeg", "drop_all"} {
		if _, err := parseDecision(s); err != nil {
			t.Errorf("parseDecision(%q): %v", s, err)
		}
	}
	for _, s := range []string{"", "delete", "DROP_RAW", "copy_to"} {
		if _, err := parseDecision(s); err == nil {
			t.Errorf("parseDecision(%q) accepted an unknown decision", s)
		}
	}
}

func TestToItemRequiresHash(t *testing.T) {
	if _, err := toItem(DecisionItem{Stem: "DSCF0001", Decision: "drop_raw"}); err == nil {
		t.Error("decision without a frame identity accepted")
	}
	item, err := toItem(DecisionItem{Hash: "h", Dir: "/card", Stem: "DSCF0001", Decision: "drop_all"})
	if err != nil {
		t.Fatalf("toItem: %v", err)
	}
	if item.D != decide.DropAll {
		t.Errorf("decision = %q, want drop_all", item.D)
	}
}
