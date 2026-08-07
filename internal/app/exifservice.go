package app

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/journal"
	"github.com/tomaszcichy9825/culler/internal/ops"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// Where the metadata writer keeps its two working directories, both inside the
// app data directory. Nothing to do with a metadata write is ever put on the
// card: the only file this service adds beside a photograph is the XMP sidecar
// the user explicitly asked for.
const (
	stagingDir = "exif-staging"
	backupRoot = "backup"
)

// ExifFieldDTO is one row of the editor form.
type ExifFieldDTO struct {
	// Tag is the EXIF name, and the identity an edit refers to.
	Tag string `json:"tag"`
	// Label is how the row is titled.
	Label string `json:"label"`
	// Section groups rows under one heading in the form.
	Section string `json:"section"`
	// Value is the current value: RFC3339 for the capture time and plain text
	// for everything else, already formatted for the fields nobody can edit.
	Value string `json:"value"`
	// Present is false when the file does not carry the tag at all, which is a
	// different thing from carrying an empty one.
	Present bool `json:"present"`
	// Writable is false for a row the app will not write — every tag it does
	// not know how to produce, and every tag a RAW frame's sidecar cannot
	// carry. The form draws these locked.
	Writable bool `json:"writable"`
}

// FrameExifDTO is everything the editor knows about one frame.
type FrameExifDTO struct {
	Path string `json:"path"`
	Stem string `json:"stem"`
	// Kind is jpeg or raw, which decides how a write reaches the frame.
	Kind string `json:"kind"`
	// Sidecar is where a RAW frame's edits go, and empty for a JPEG.
	Sidecar string         `json:"sidecar"`
	Fields  []ExifFieldDTO `json:"fields"`
	// Error is why this frame could not be read, if it could not be. One
	// unreadable frame never fails the whole read.
	Error string `json:"error"`
}

// ExifEditDTO is what the user changed on one frame. A null field is one they
// did not touch; a field set to the empty string clears the tag. That is the
// batch editor's rule — "a value you type replaces every frame, empty leaves
// them alone" — carried all the way to the writer.
type ExifEditDTO struct {
	Path string `json:"path"`
	// DateTimeOriginal is RFC3339, or the same shape with the offset left off
	// for a frame whose zone is unknown. An offset it carries is written out as
	// OffsetTimeOriginal; a time without one writes no offset tag at all,
	// because the zone the file never recorded is not this app's to invent.
	DateTimeOriginal *string `json:"dateTimeOriginal"`
	Artist           *string `json:"artist"`
	Copyright        *string `json:"copyright"`
	StripGPS         bool    `json:"stripGps"`
	// SetGPS writes a location onto the frame — a pin dropped on the map or a
	// position copied from another photo. JPEG gets it in place; a RAW gets it
	// in a sidecar. It is set, not typed, so it is a coordinate not a string.
	SetGPS *GPSCoordDTO `json:"setGps"`
}

// GPSCoordDTO is a location to write, in signed decimal degrees, with an
// altitude in metres that means something only when HasAltitude says so.
type GPSCoordDTO struct {
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Altitude    float64 `json:"altitude"`
	HasAltitude bool    `json:"hasAltitude"`
}

// ExifWriteRowDTO is one line of the write-plan dialog: what happens, to which
// file, to which tag, with what value, by what method.
type ExifWriteRowDTO struct {
	Sign   string `json:"sign"`   // + adds or replaces, − removes, ! is a warning
	Target string `json:"target"` // the file's name, not its path
	Tag    string `json:"tag"`
	Value  string `json:"value"`
	Method string `json:"method"` // in place | sidecar | skipped
}

// ExifPlanDTO is what a write would do, with nothing done yet.
type ExifPlanDTO struct {
	Description string            `json:"description"`
	Rows        []ExifWriteRowDTO `json:"rows"`
	// Writes is the number of tag changes, Frames the number of frames they
	// are spread across, and Files the number of files that will be touched.
	Writes int `json:"writes"`
	Frames int `json:"frames"`
	Files  int `json:"files"`
	// BackupDir is where the originals are copied before anything is written.
	BackupDir string `json:"backupDir"`
	// Assurances are the green rows of the dialog footer, Warnings the amber
	// ones. Both are sentences, ready to draw.
	Assurances []string `json:"assurances"`
	Warnings   []string `json:"warnings"`
}

// ExifService reads and writes frame metadata.
type ExifService struct {
	app *App
}

// NewExifService binds the service to the shared state.
func NewExifService(a *App) *ExifService {
	return &ExifService{app: a}
}

// field describes one row of the form: where it comes from and whether this
// app is willing to write it.
type field struct {
	tag     string
	label   string
	section string
	// writable in a JPEG's own EXIF, and in a RAW frame's XMP sidecar. A tag
	// this app cannot produce is locked everywhere.
	inJPEG   bool
	inXMP    bool
	value    func(exif.Fields) (string, bool)
	document string
}

// fields is the form, in the order it is drawn. Adding a row means adding a
// line here; the editor, the plan and the writer all read this one list.
var exifFields = []field{
	{tag: "DateTimeOriginal", label: "Capture time", section: "Capture", inJPEG: true, inXMP: true, value: func(f exif.Fields) (string, bool) {
		if !f.DateTimeOriginal.Present {
			return "", false
		}
		return formatCaptureTime(f.DateTimeOriginal), true
	}},
	{tag: "Artist", label: "Artist", section: "Rights", inJPEG: true, inXMP: true, value: func(f exif.Fields) (string, bool) {
		return f.Artist.Value, f.Artist.Present
	}},
	{tag: "Copyright", label: "Copyright", section: "Rights", inJPEG: true, inXMP: true, value: func(f exif.Fields) (string, bool) {
		return f.Copyright.Value, f.Copyright.Present
	}},
	{tag: "Make", label: "Camera make", section: "Camera", value: func(f exif.Fields) (string, bool) {
		return f.Make.Value, f.Make.Present
	}},
	{tag: "Model", label: "Camera model", section: "Camera", value: func(f exif.Fields) (string, bool) {
		return f.Model.Value, f.Model.Present
	}},
	{tag: "LensModel", label: "Lens", section: "Camera", value: func(f exif.Fields) (string, bool) {
		return f.LensModel.Value, f.LensModel.Present
	}},
	{tag: "ExposureTime", label: "Shutter", section: "Exposure", value: func(f exif.Fields) (string, bool) {
		return formatShutter(f.ExposureTime), f.ExposureTime.Present
	}},
	{tag: "FNumber", label: "Aperture", section: "Exposure", value: func(f exif.Fields) (string, bool) {
		if !f.FNumber.Present {
			return "", false
		}
		return "ƒ" + trimFloat(f.FNumber.Float()), true
	}},
	{tag: "ISO", label: "ISO", section: "Exposure", value: func(f exif.Fields) (string, bool) {
		return fmt.Sprint(f.ISO.Value), f.ISO.Present
	}},
	{tag: "FocalLength", label: "Focal length", section: "Exposure", value: func(f exif.Fields) (string, bool) {
		if !f.FocalLength.Present {
			return "", false
		}
		return trimFloat(f.FocalLength.Float()) + " mm", true
	}},
	{tag: "Orientation", label: "Orientation", section: "Image", value: func(f exif.Fields) (string, bool) {
		return fmt.Sprint(f.Orientation.Value), f.Orientation.Present
	}},
	{tag: "ImageSize", label: "Pixels", section: "Image", value: func(f exif.Fields) (string, bool) {
		if !f.ImageWidth.Present || !f.ImageHeight.Present {
			return "", false
		}
		return fmt.Sprintf("%d × %d", f.ImageWidth.Value, f.ImageHeight.Value), true
	}},
	{tag: "GPSPosition", label: "Position", section: "Location", value: func(f exif.Fields) (string, bool) {
		if !f.GPS.Present {
			return "", false
		}
		return fmt.Sprintf("%.6f, %.6f", f.GPS.Latitude, f.GPS.Longitude), true
	}},
	{tag: "GPSAltitude", label: "Altitude", section: "Location", value: func(f exif.Fields) (string, bool) {
		if !f.GPS.HasAltitude {
			return "", false
		}
		return trimFloat(f.GPS.Altitude) + " m", true
	}},
}

// Read returns the metadata of every path given, keyed by path. A frame that
// cannot be read comes back carrying the reason rather than taking the rest of
// the batch down with it — an editor showing thirteen frames and one error is
// more use than one showing an error.
func (s *ExifService) Read(paths []string) (map[string]FrameExifDTO, error) {
	cfg := s.app.Config().ScanConfig()
	out := make(map[string]FrameExifDTO, len(paths))
	for _, path := range paths {
		if path == "" {
			continue
		}
		out[path] = readFrame(path, cfg)
	}
	return out, nil
}

func readFrame(path string, cfg scan.Config) FrameExifDTO {
	dto := FrameExifDTO{
		Path: path,
		Stem: filepath.Base(path),
		Kind: "jpeg",
	}
	if isRawPath(path, cfg) {
		dto.Kind = "raw"
		dto.Sidecar = sidecarPath(path)
	}

	values, err := exif.Read(path)
	if err != nil {
		dto.Error = err.Error()
		return dto
	}
	dto.Fields = make([]ExifFieldDTO, 0, len(exifFields))
	for _, f := range exifFields {
		value, present := f.value(values)
		writable := f.inJPEG
		if dto.Kind == "raw" {
			writable = f.inXMP
		}
		dto.Fields = append(dto.Fields, ExifFieldDTO{
			Tag:      f.tag,
			Label:    f.label,
			Section:  f.section,
			Value:    value,
			Present:  present,
			Writable: writable,
		})
	}
	return dto
}

// Plan reports what writing the edits would do. It reads the files and works
// out where every byte would go, and writes nothing at all.
func (s *ExifService) Plan(edits []ExifEditDTO) (ExifPlanDTO, error) {
	targets, err := s.resolve(edits)
	if err != nil {
		return ExifPlanDTO{}, err
	}
	return s.describe(targets), nil
}

// Apply writes the edits and journals what it did. Every file it replaces is
// moved into the backup directory first, in the same batch, so a single undo
// puts every original back exactly as it was — see the internal/exif package
// documentation for why that is a move and a copy rather than a new verb.
func (s *ExifService) Apply(edits []ExifEditDTO) (BatchDTO, error) {
	targets, err := s.resolve(edits)
	if err != nil {
		return BatchDTO{}, err
	}
	plan := s.describe(targets)

	staging := filepath.Join(s.app.dataDir, stagingDir, fmt.Sprintf("%d", time.Now().UnixNano()))
	// Staging is scratch space: whatever happens, it does not outlive the call.
	defer os.RemoveAll(staging)

	backup := s.backupDir()
	var actions []ops.FileAction
	var installs []string
	for i, t := range targets {
		if t.skip != "" {
			continue
		}
		body, err := t.render()
		if err != nil {
			return BatchDTO{}, fmt.Errorf("%s: %w", filepath.Base(t.path), err)
		}
		staged := filepath.Join(staging, fmt.Sprintf("%03d-%s", i, filepath.Base(t.write)))
		if err := exif.WriteFile(staged, body, t.mode); err != nil {
			return BatchDTO{}, fmt.Errorf("stage %s: %w", filepath.Base(t.write), err)
		}
		// A file that is being replaced is moved aside first; a sidecar that
		// does not exist yet has no original to keep. The install is tied to
		// its backup move: if the original could not be moved aside, installing
		// anyway would file the edit beside it under a numbered name, in the
		// photo folder — possibly the card — so the executor skips it instead.
		if t.exists {
			actions = append(actions, ops.FileAction{
				Verb: ops.VerbMove,
				Src:  t.write,
				Dst:  filepath.Join(backup, filepath.Base(t.write)),
			})
		}
		actions = append(actions, ops.FileAction{
			Verb: ops.VerbCopy, Src: staged, Dst: t.write, NeedsPrior: t.exists,
		})
		installs = append(installs, t.write)
	}
	if len(actions) == 0 {
		return BatchDTO{}, nil
	}
	if err := os.MkdirAll(backup, 0o755); err != nil {
		return BatchDTO{}, fmt.Errorf("create backup directory: %w", err)
	}

	jrnl, err := s.app.openJournal()
	if err != nil {
		return BatchDTO{}, err
	}
	// No trasher: a metadata write never deletes anything.
	executor := &ops.Executor{Journal: jrnl}
	batch, applyErr := executor.Apply(plan.Description, actions)
	dto := batchDTO(batch)
	if applyErr != nil {
		return dto, applyErr
	}
	return dto, checkInstalled(batch, installs)
}

// checkInstalled confirms every rewritten file landed on the path it was meant
// to. The executor renames around a collision rather than overwriting, which
// is right for a cull and wrong here: it would leave the edited frame beside
// the original under a numbered name. It cannot happen once the backup move
// has succeeded, so this is the assertion that says so out loud rather than a
// case anyone is expected to hit.
func checkInstalled(batch journal.Batch, installs []string) error {
	wanted := make(map[string]bool, len(installs))
	for _, path := range installs {
		wanted[path] = true
	}
	var stray []string
	for _, a := range batch.Actions {
		if a.Outcome != journal.OutcomeOK || a.Dst == "" || wanted[a.Dst] {
			continue
		}
		if strings.Contains(a.Src, stagingDir) {
			stray = append(stray, a.Dst)
		}
	}
	if len(stray) > 0 {
		return fmt.Errorf("edited metadata landed beside the original rather than on it: %s", strings.Join(stray, ", "))
	}
	return nil
}

// target is one frame's edit, resolved against the disk.
type target struct {
	path   string // the frame itself
	write  string // the file that will be written: the frame, or its sidecar
	method string // in place | sidecar | skipped
	raw    bool
	exists bool // whether write already exists and so needs backing up
	mode   os.FileMode
	change exif.Changes
	rows   []ExifWriteRowDTO
	skip   string // why this frame is being left alone, if it is
}

// render produces the bytes that will replace the target's file.
func (t target) render() ([]byte, error) {
	if t.raw {
		return exif.RenderXMP(t.change), nil
	}
	data, err := os.ReadFile(t.path)
	if err != nil {
		return nil, err
	}
	return exif.RewriteJPEG(data, t.change)
}

// resolve turns the edits into targets, deciding for each one where its bytes
// go and whether they can go there at all.
func (s *ExifService) resolve(edits []ExifEditDTO) ([]target, error) {
	cfg := s.app.Config().ScanConfig()
	targets := make([]target, 0, len(edits))
	for _, edit := range edits {
		t, err := resolveOne(edit, cfg)
		if err != nil {
			return nil, err
		}
		if t.change.Empty() {
			continue
		}
		targets = append(targets, t)
	}
	return targets, nil
}

func resolveOne(edit ExifEditDTO, cfg scan.Config) (target, error) {
	if strings.TrimSpace(edit.Path) == "" {
		return target{}, errors.New("no frame to write to")
	}
	path, err := expandPath(edit.Path)
	if err != nil {
		return target{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return target{}, fmt.Errorf("open frame: %w", err)
	}

	t := target{path: path, raw: isRawPath(path, cfg), mode: info.Mode().Perm()}
	if t.raw {
		t.write, t.method = sidecarPath(path), "sidecar"
		// A sidecar inherits sensible permissions rather than the RAW's, which
		// cameras sometimes write read-only.
		t.mode = 0o644
	} else {
		t.write, t.method = path, "in place"
	}
	if _, err := os.Lstat(t.write); err == nil {
		t.exists = true
	}

	if err := t.applyEdit(edit); err != nil {
		return target{}, err
	}
	if reason := writeBarrier(t); reason != "" {
		t.skip, t.method = reason, "skipped"
	}
	return t, nil
}

// applyEdit fills in the changes and the plan rows they produce. A tag the
// frame's method cannot carry is dropped here rather than silently written
// somewhere it will never be read from.
func (t *target) applyEdit(edit ExifEditDTO) error {
	name := filepath.Base(t.write)
	row := func(sign, tag, value string) {
		t.rows = append(t.rows, ExifWriteRowDTO{Sign: sign, Target: name, Tag: tag, Value: value, Method: t.method})
	}

	if edit.DateTimeOriginal != nil {
		if *edit.DateTimeOriginal == "" {
			// The capture time is the one tag the app will not remove: a frame
			// with no time at all sorts nowhere and cannot be found again.
			return errors.New("the capture time cannot be cleared, only changed")
		}
		when, err := parseCaptureTime(*edit.DateTimeOriginal)
		if err != nil {
			return fmt.Errorf("capture time %q: %w", *edit.DateTimeOriginal, err)
		}
		t.change.DateTimeOriginal = &when
		row("+", "DateTimeOriginal", renderCaptureTime(when))
	}
	if edit.Artist != nil {
		t.change.Artist = edit.Artist
		row(signFor(*edit.Artist), "Artist", *edit.Artist)
	}
	if edit.Copyright != nil {
		t.change.Copyright = edit.Copyright
		row(signFor(*edit.Copyright), "Copyright", *edit.Copyright)
	}
	if edit.SetGPS != nil {
		g := *edit.SetGPS
		if math.Abs(g.Latitude) > 90 || math.Abs(g.Longitude) > 180 {
			return fmt.Errorf("location %.6f, %.6f is off the earth", g.Latitude, g.Longitude)
		}
		// The altitude encodes as hundredths of a metre in a uint32, so an absurd
		// value would silently wrap to a wrong one. Anywhere a photograph is taken
		// sits well inside this bound; a value past it is a mistake, not a place.
		if g.HasAltitude && math.Abs(g.Altitude) > 100_000 {
			return fmt.Errorf("altitude %.1f m is not a place on earth", g.Altitude)
		}
		// Setting a location works for a RAW too: unlike stripping, a sidecar can
		// carry the coordinate the RAW itself never will.
		t.change.SetGPS = &exif.GPSCoord{
			Latitude:    g.Latitude,
			Longitude:   g.Longitude,
			Altitude:    g.Altitude,
			HasAltitude: g.HasAltitude,
		}
		row("+", "GPSPosition", fmt.Sprintf("%.6f, %.6f", g.Latitude, g.Longitude))
	} else if edit.StripGPS && !t.raw {
		// A sidecar cannot remove coordinates from inside a RAW, and pretending
		// otherwise is the one lie a metadata editor must not tell.
		t.change.StripGPS = true
		row("−", "GPSPosition", "removed")
	}
	return nil
}

func signFor(value string) string {
	if value == "" {
		return "−"
	}
	return "+"
}

// writeBarrier reports why a frame cannot be written, or the empty string.
// Opening the file for writing and closing it again is the only check that
// catches a read-only volume without writing anything to find out.
func writeBarrier(t target) string {
	if !t.exists {
		// A new sidecar needs a writable directory, which is not knowable
		// without creating something. The write itself will say.
		return ""
	}
	if t.mode&0o200 == 0 {
		return "the file is read-only"
	}
	f, err := os.OpenFile(t.write, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return "the volume is read-only"
	}
	if err := f.Close(); err != nil {
		return err.Error()
	}
	return ""
}

// describe turns resolved targets into the write-plan dialog's contents.
func (s *ExifService) describe(targets []target) ExifPlanDTO {
	plan := ExifPlanDTO{BackupDir: s.backupDir()}
	files := map[string]bool{}
	var skipped int

	for _, t := range targets {
		plan.Frames++
		plan.Rows = append(plan.Rows, t.rows...)
		if t.skip != "" {
			skipped++
			continue
		}
		plan.Writes += len(t.rows)
		files[t.write] = true
	}
	plan.Files = len(files)

	plan.Description = fmt.Sprintf("Write metadata to %d %s", plan.Frames, pluralFrames(plan.Frames))
	if plan.Writes == 0 {
		plan.Description = "Nothing to write"
	}
	plan.Assurances = []string{
		"back up originals to " + plan.BackupDir,
		"RAW via sidecar · JPEG in place",
	}
	if skipped > 0 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"%d %s cannot be written to and will be skipped", skipped, pluralFrames(skipped)))
	}
	return plan
}

// backupDir is where originals go before they are replaced: inside the app
// data directory, in a folder named for today, so a week of edits stays
// separable. The design's dialog quotes a path relative to the card; the card
// is exactly where this must not be.
func (s *ExifService) backupDir() string {
	return filepath.Join(s.app.dataDir, backupRoot, time.Now().Format("2006-01-02"))
}

// ShotTime is when the frame was taken: the capture time the camera recorded,
// falling back to the modification time the scan already found. The scan reads
// no EXIF — it walks thousands of files and cannot afford to — so this is how
// a frame's real capture time reaches the grid.
//
// LibraryService.groupDTO currently formats g.Shot directly. Wiring this in is
// one line there:
//
//	Shot: ShotTime(g).Format(time.RFC3339),
func ShotTime(g scan.PhotoGroup) time.Time {
	if ref := primaryRef(g); ref != nil {
		if when, ok := exif.ShotTime(ref.Path); ok {
			return when
		}
	}
	return g.Shot
}

// --- small helpers ----------------------------------------------------------

func isRawPath(path string, cfg scan.Config) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range cfg.RawExts {
		if e == ext {
			return true
		}
	}
	return false
}

// sidecarPath is where a RAW frame's XMP lives: the whole filename plus .xmp,
// which is the form the scanner already groups back onto its frame.
func sidecarPath(path string) string {
	return path + ".xmp"
}

// formatCaptureTime renders the capture time for the form: RFC3339, with the
// offset left off when the file never recorded one, so that what the editor
// shows is what an edit can send straight back — a frame whose zone is unknown
// round-trips as exactly that rather than gaining a "Z" nobody stated.
func formatCaptureTime(t exif.Timestamp) string {
	layout := "2006-01-02T15:04:05"
	if t.HasSubSec {
		layout += ".999999999"
	}
	if t.HasOffset {
		layout += "Z07:00"
	}
	return t.Value.Format(layout)
}

// parseCaptureTime reads an edit's time back in: RFC3339 when it names its
// zone, the same shape without the suffix when it does not. The offset-less
// form is parsed as a wall clock in UTC, mirroring how the reader holds a
// zone-unknown time.
func parseCaptureTime(s string) (exif.CaptureTime, error) {
	if when, err := time.Parse(time.RFC3339, s); err == nil {
		return exif.CaptureTime{Value: when, HasOffset: true}, nil
	}
	when, err := time.ParseInLocation("2006-01-02T15:04:05", s, time.UTC)
	if err != nil {
		return exif.CaptureTime{}, err
	}
	return exif.CaptureTime{Value: when, HasOffset: false}, nil
}

// renderCaptureTime is the plan dialog's rendering of the time being written,
// in the same offset-or-nothing form the field itself uses.
func renderCaptureTime(t exif.CaptureTime) string {
	if t.HasOffset {
		return t.Value.Format(time.RFC3339)
	}
	return t.Value.Format("2006-01-02T15:04:05")
}

// formatShutter renders an exposure the way a photographer reads it: 1/250 for
// a fraction, 2.5s for a long exposure.
func formatShutter(r exif.Rational) string {
	if !r.Present || r.Den == 0 {
		return ""
	}
	if v := r.Float(); v >= 1 {
		return trimFloat(v) + "s"
	}
	return fmt.Sprintf("1/%d", int64(float64(r.Den)/float64(r.Num)+0.5))
}

// trimFloat drops a trailing ".0" so 35mm does not read as 35.0mm.
func trimFloat(v float64) string {
	s := fmt.Sprintf("%.1f", v)
	return strings.TrimSuffix(s, ".0")
}
