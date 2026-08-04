package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/config"
	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/hash"
)

// exportApp returns an app with the sidecar export turned on.
func exportApp(t *testing.T) *App {
	t.Helper()
	a := testApp(t)
	cfg := a.Config()
	cfg.Behaviour.XMPExport = true
	if err := a.setConfig(cfg); err != nil {
		t.Fatalf("turn the export on: %v", err)
	}
	return a
}

// exportCard writes a RAW+JPEG pair and a JPEG-only frame, with no sidecars.
func exportCard(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range map[string]string{
		"DSCF0001.RAF": "raw bytes",
		"DSCF0001.JPG": "jpeg bytes",
		"DSCF0002.JPG": "another frame",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// decideFrame records a verdict and rating against the identity of path, which
// is what the grid keys a decision on.
func decideFrame(t *testing.T, a *App, dir, path, stem string, v decide.Verdict, rating int) {
	t.Helper()
	h, err := hash.Content(path)
	if err != nil {
		t.Fatal(err)
	}
	store, err := a.decisions()
	if err != nil {
		t.Fatal(err)
	}
	if v != decide.Undecided {
		if err := store.SetVerdict(h, dir, stem, v, decide.MaskBoth); err != nil {
			t.Fatal(err)
		}
	}
	if rating > 0 {
		if err := store.SetRating(h, dir, stem, rating); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExportFolderIsExplicitAndNeedsNoSetting(t *testing.T) {
	a := testApp(t)
	if a.Config().Behaviour.XMPExport {
		t.Fatal("the fixture must start with the auto-export setting off")
	}
	dir := exportCard(t)
	decideFrame(t, a, dir, filepath.Join(dir, "DSCF0001.JPG"), "DSCF0001", decide.Keep, 4)

	// The call is the consent: a manual export writes even with the setting
	// off, because the setting only governs whether an apply does it too.
	res, err := NewXMPExportService(a).ExportFolder(dir)
	if err != nil {
		t.Fatalf("an explicit export must run regardless of the setting: %v", err)
	}
	if res.Written == 0 {
		t.Error("a kept, rated frame should have produced a sidecar")
	}
	if !exists(t, filepath.Join(dir, "DSCF0001.RAF.xmp")) {
		t.Error("the sidecar was not written")
	}
}

// A pair is identified by its JPEG and its sidecar belongs to its RAW, so this
// covers both halves of the mapping in one frame.
func TestExportFolderWritesSidecarsForDecidedFrames(t *testing.T) {
	a := exportApp(t)
	dir := exportCard(t)
	decideFrame(t, a, dir, filepath.Join(dir, "DSCF0001.JPG"), "DSCF0001", decide.Keep, 4)

	res, err := NewXMPExportService(a).ExportFolder(dir)
	if err != nil {
		t.Fatalf("ExportFolder: %v", err)
	}
	if res.Written != 1 {
		t.Errorf("written: want 1, got %d (%v)", res.Written, res.Errors)
	}
	if res.Frames != 2 {
		t.Errorf("frames: want 2, got %d", res.Frames)
	}
	if res.Failed != 0 {
		t.Errorf("failed: want 0, got %d (%v)", res.Failed, res.Errors)
	}

	body, err := os.ReadFile(filepath.Join(dir, "DSCF0001.RAF.xmp"))
	if err != nil {
		t.Fatalf("the pair's sidecar belongs beside its RAW: %v", err)
	}
	if !strings.Contains(string(body), "<xmp:Rating>4</xmp:Rating>") {
		t.Errorf("rating missing:\n%s", body)
	}
	if !strings.Contains(string(body), "<xmp:Label>Green</xmp:Label>") {
		t.Errorf("label missing:\n%s", body)
	}
	// The undecided frame is left with no file beside it.
	if exists(t, filepath.Join(dir, "DSCF0002.JPG.xmp")) {
		t.Error("an undecided frame must not get a sidecar")
	}
	if res.Skipped != 1 {
		t.Errorf("skipped: want 1 for the undecided frame, got %d", res.Skipped)
	}
}

// Clearing a decision has to reach the sidecar the earlier export wrote, and
// take nothing else with it.
func TestExportFolderClearsOurFieldsFromAnExistingSidecar(t *testing.T) {
	a := exportApp(t)
	dir := exportCard(t)
	sidecar := filepath.Join(dir, "DSCF0002.JPG.xmp")
	const existing = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:xmp="http://ns.adobe.com/xap/1.0/">
   <dc:title>Harbour</dc:title>
   <xmp:Rating>3</xmp:Rating>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`
	if err := os.WriteFile(sidecar, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := NewXMPExportService(a).ExportFolder(dir)
	if err != nil {
		t.Fatalf("ExportFolder: %v", err)
	}
	if res.Cleared != 1 {
		t.Errorf("cleared: want 1, got %d (%v)", res.Cleared, res.Errors)
	}

	body, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("the sidecar must stay: %v", err)
	}
	if strings.Contains(string(body), "xmp:Rating") {
		t.Errorf("our field survived:\n%s", body)
	}
	if !strings.Contains(string(body), "<dc:title>Harbour</dc:title>") {
		t.Errorf("someone else's field was taken with it:\n%s", body)
	}
}

// One sidecar this app cannot read is reported and left alone; the rest of the
// folder still exports.
func TestExportFolderReportsASidecarItCannotRead(t *testing.T) {
	a := exportApp(t)
	dir := exportCard(t)
	junk := filepath.Join(dir, "DSCF0001.RAF.xmp")
	const body = "not xml <<<"
	if err := os.WriteFile(junk, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	decideFrame(t, a, dir, filepath.Join(dir, "DSCF0001.JPG"), "DSCF0001", decide.Keep, 4)
	decideFrame(t, a, dir, filepath.Join(dir, "DSCF0002.JPG"), "DSCF0002", decide.Cut, 0)

	res, err := NewXMPExportService(a).ExportFolder(dir)
	if err != nil {
		t.Fatalf("one bad sidecar must not fail the run: %v", err)
	}
	if res.Failed != 1 || len(res.Errors) != 1 {
		t.Errorf("failed: want 1 with a reason, got %d %v", res.Failed, res.Errors)
	}
	if !strings.Contains(res.Errors[0], "DSCF0001.RAF.xmp") {
		t.Errorf("the reason must name the file, got %q", res.Errors[0])
	}
	if got, err := os.ReadFile(junk); err != nil || string(got) != body {
		t.Errorf("the unreadable sidecar was modified: %q", got)
	}
	if res.Written != 1 {
		t.Errorf("the other frame must still export, written = %d", res.Written)
	}
	if !strings.Contains(res.Description, "could not be written") {
		t.Errorf("the summary must mention the failure, got %q", res.Description)
	}
}

// Running the export twice over an unchanged folder writes nothing the second
// time: the sidecars already say what it would say.
func TestExportFolderIsQuietWhenNothingChanged(t *testing.T) {
	a := exportApp(t)
	dir := exportCard(t)
	decideFrame(t, a, dir, filepath.Join(dir, "DSCF0001.JPG"), "DSCF0001", decide.Keep, 4)
	s := NewXMPExportService(a)

	if _, err := s.ExportFolder(dir); err != nil {
		t.Fatalf("first run: %v", err)
	}
	res, err := s.ExportFolder(dir)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if res.Written != 0 || res.Cleared != 0 {
		t.Errorf("want a quiet second run, got written %d cleared %d", res.Written, res.Cleared)
	}
	if res.Description != "No decisions to export" {
		t.Errorf("description: got %q", res.Description)
	}
}

func TestExportFolderNeedsAFolder(t *testing.T) {
	a := exportApp(t)
	file := filepath.Join(t.TempDir(), "DSCF0001.JPG")
	if err := os.WriteFile(file, []byte("jpeg bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := NewXMPExportService(a).ExportFolder(file); err == nil {
		t.Error("a file is not a folder to export")
	}
	if _, err := NewXMPExportService(a).ExportFolder(filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Error("a folder that is not there cannot be exported")
	}
}

// The service reads the running configuration, not the one it was built with,
// so turning the setting off stops the next call.
func TestExportFolderRunsRegardlessOfTheSetting(t *testing.T) {
	a := exportApp(t)
	dir := exportCard(t)
	s := NewXMPExportService(a)
	if _, err := s.ExportFolder(dir); err != nil {
		t.Fatalf("with the setting on: %v", err)
	}

	cfg := a.Config()
	cfg.Behaviour.XMPExport = false
	if err := a.setConfig(cfg); err != nil {
		t.Fatal(err)
	}
	// The setting is off, but an explicit export still runs — it only ever
	// gated the automatic one.
	if _, err := s.ExportFolder(dir); err != nil {
		t.Errorf("an explicit export must run with the setting off: %v", err)
	}
	if a.Config().Behaviour.CollisionPolicy != config.CollisionRenameSuffix {
		t.Error("the rest of the configuration was disturbed")
	}
}
