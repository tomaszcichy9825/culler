// Package xmpexport writes a frame's verdict and rating into an XMP sidecar
// beside it, for Lightroom, Bridge and anything else that reads one.
//
// This is an export and nothing more. The decision database is the source of
// truth (see docs/DESIGN.md §3.3); a sidecar is a copy handed to other tools,
// written when the user asks for one and never on a keystroke. Nothing here
// reads a sidecar back into a decision, and nothing here runs unless both the
// configuration flag and an explicit action say so.
//
// # What is written
//
//	xmp:Rating   the star rating, 1-5. An unrated frame carries no rating at
//	             all: 0 would say the photograph was judged and found worth no
//	             stars, which is a different fact from not having judged it.
//	xmp:Label    the verdict, as a colour name: keep is "Green", cut is "Red".
//
// The label mapping is the one convention the two readers agree on. Lightroom
// ships a label set whose members are the colour names themselves — Red,
// Yellow, Green, Blue, Purple — and matches an incoming label against them by
// string, so "Green" and "Red" land as real colour labels rather than as
// custom text. Green for a keep and red for a cut is also what the swatches
// have meant in a light table since long before either application. Bridge's
// stock set names the same colours differently ("Approved", "Select"), so a
// Bridge user sees the colour name as a custom label; that is the cost of
// picking one, and picking the colour names keeps the file legible to a human
// reading the XML as well.
//
// A cut is deliberately not exported as xmp:Rating="-1", the rejected value
// the XMP specification allows. Culler's rating and verdict are independent —
// a frame can be cut and still be worth four stars to the person cutting it —
// so writing the verdict into the rating would destroy the other fact.
//
// # Merging
//
// A sidecar that already exists belongs to whoever wrote it. Write never
// replaces one: it locates our two fields in the existing bytes, in element
// or attribute form, removes them, splices ours in, and reproduces every other
// byte of the file exactly. A sidecar this package cannot parse is refused and
// left alone rather than overwritten. Clearing a frame's decision removes our
// fields from the sidecar and leaves the file itself in place, because the
// rest of it was never ours to delete.
//
// Every value written comes from a closed set — an integer 0-5 and two fixed
// colour names — so no user text ever reaches the XML and nothing here needs
// to escape anything.
package xmpexport

import (
	"bytes"
	"errors"
	"io/fs"
	"os"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/exif"
	"github.com/tomaszcichy9825/culler/internal/scan"
)

// Sidecar permissions. A RAW is often written read-only by the camera, and a
// sidecar that inherited that could not be updated by the tool it is for.
const sidecarMode fs.FileMode = 0o644

// Action is what a Write did to the disk.
type Action string

const (
	// ActionNone means the file was not touched: the frame had nothing to
	// export and no sidecar, or the sidecar already said what we would write.
	ActionNone Action = "none"
	// ActionWritten means a sidecar was created or updated with our fields.
	ActionWritten Action = "written"
	// ActionCleared means our fields were removed from a sidecar that stays.
	ActionCleared Action = "cleared"
)

// Result is what happened to one frame.
type Result struct {
	// Path is the sidecar the frame's decision belongs in, whether or not this
	// call wrote to it.
	Path   string
	Action Action
}

// ErrNoFrame is returned for a group with no file to put a sidecar beside.
var ErrNoFrame = errors.New("xmpexport: this frame has no file to write a sidecar beside")

// SidecarPath is where a frame's sidecar lives: the whole filename plus .xmp,
// on the RAW when there is one and on the JPEG otherwise. The full filename
// rather than the stem is what the scanner groups back onto the frame, and
// what keeps a RAW+JPEG pair's sidecar attached to the RAW half — see
// docs/DESIGN.md §3.1.
func SidecarPath(g scan.PhotoGroup) string {
	if g.Raw != nil {
		return g.Raw.Path + ".xmp"
	}
	if g.Jpeg != nil {
		return g.Jpeg.Path + ".xmp"
	}
	return ""
}

// Label is the colour name a verdict exports as, empty for a frame nobody has
// judged. See the package documentation for why these two colours.
func Label(v decide.Verdict) string {
	switch v {
	case decide.Keep:
		return "Green"
	case decide.Cut:
		return "Red"
	}
	return ""
}

// Write puts rec into the frame's sidecar and reports what it did.
//
// A frame with neither a rating nor a verdict gets no sidecar; if one is
// already there, our fields come out of it and the file stays. The write is
// atomic — the bytes land in a temporary file beside the target and are
// renamed over it — so an interrupted export leaves the old sidecar or the new
// one and never half of either.
func Write(g scan.PhotoGroup, rec decide.Record) (Result, error) {
	path := SidecarPath(g)
	if path == "" {
		return Result{}, ErrNoFrame
	}

	existing, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if len(fields(rec)) == 0 {
			return Result{Path: path, Action: ActionNone}, nil
		}
		if err := exif.WriteFile(path, render(rec), sidecarMode); err != nil {
			return Result{Path: path}, err
		}
		return Result{Path: path, Action: ActionWritten}, nil
	case err != nil:
		return Result{Path: path}, err
	}

	merged, err := Merge(existing, rec)
	if err != nil {
		return Result{Path: path}, err
	}
	if bytes.Equal(merged, existing) {
		return Result{Path: path, Action: ActionNone}, nil
	}
	if err := exif.WriteFile(path, merged, sidecarMode); err != nil {
		return Result{Path: path}, err
	}
	if len(fields(rec)) == 0 {
		return Result{Path: path, Action: ActionCleared}, nil
	}
	return Result{Path: path, Action: ActionWritten}, nil
}

// render builds a fresh sidecar carrying only our fields. It follows the shape
// of exif.RenderXMP — the same packet wrapper, the same one description — so a
// folder culled with this app looks the same whichever of the two wrote the
// file.
func render(rec decide.Record) []byte {
	var b bytes.Buffer
	b.WriteString("<?xpacket begin=\"\uFEFF\" id=\"W5M0MpCehiHzreSzNTczkc9d\"?>\n")
	b.WriteString(`<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="culler">` + "\n")
	b.WriteString(`  <rdf:RDF xmlns:rdf="` + rdfNS + `">` + "\n")
	b.WriteString(`    <rdf:Description rdf:about=""` + "\n")
	b.WriteString(`        xmlns:` + defaultPrefix + `="` + xmpNS + `">` + "\n")
	for _, f := range fields(rec) {
		b.WriteString("      " + f.element(defaultPrefix) + "\n")
	}
	b.WriteString("    </rdf:Description>\n  </rdf:RDF>\n</x:xmpmeta>\n")
	b.WriteString(`<?xpacket end="w"?>` + "\n")
	return b.Bytes()
}
