package ops

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/tomaszcichy9825/culler/internal/scan"
)

// Halves names which halves of a RAW+JPEG pair survive a verdict, in the same
// vocabulary the decision store records a mask in.
type Halves string

const (
	HalvesBoth Halves = "rj"
	HalvesRAW  Halves = "r"
	HalvesJPEG Halves = "j"
)

// Tokens is what a destination template can name about one frame. Plan is
// pure, so everything a template might ask for arrives here rather than being
// read off the disk while the plan is being built.
//
// An empty field is a question the frame cannot answer, and the segment it
// appears in collapses rather than being filled with a guess.
type Tokens struct {
	Shot   time.Time
	Stem   string
	Ext    string // without the leading dot
	Camera string
	Lens   string
}

// CopyTo copies the surviving files of each group into Dest, leaving the
// originals where they are. This is the import: the card is read, never
// written.
//
// Dest may hold token templates, expanded per frame at plan time — a template
// naming the date puts each frame in its own dated folder from one op.
type CopyTo struct {
	Dest   string
	Halves Halves
	// Metadata answers what EXIF knows about a frame, for {camera} and {lens}.
	// A nil Metadata means nothing is known, and those tokens collapse. It is
	// asked once per frame, and only when the template needs an answer.
	Metadata func(scan.PhotoGroup) (camera, lens string)
}

func (o CopyTo) Plan(groups []scan.PhotoGroup) ([]FileAction, error) {
	return route(groups, o.Dest, o.Halves, o.Metadata, VerbCopy)
}

func (o CopyTo) Describe() string { return "Copy to " + o.Dest }

// MoveTo is CopyTo that does not leave the original behind. It is what an
// import becomes when the user has asked for one, and what routing inside the
// library always is.
type MoveTo struct {
	Dest     string
	Halves   Halves
	Metadata func(scan.PhotoGroup) (camera, lens string)
}

func (o MoveTo) Plan(groups []scan.PhotoGroup) ([]FileAction, error) {
	return route(groups, o.Dest, o.Halves, o.Metadata, VerbMove)
}

func (o MoveTo) Describe() string { return "Move to " + o.Dest }

// route plans one verb over the surviving files of every group.
func route(
	groups []scan.PhotoGroup,
	dest string,
	halves Halves,
	metadata func(scan.PhotoGroup) (string, string),
	verb Verb,
) ([]FileAction, error) {
	if strings.TrimSpace(dest) == "" {
		return nil, fmt.Errorf("ops: %s needs a destination", verb)
	}
	wantsMetadata := strings.Contains(dest, "{camera}") || strings.Contains(dest, "{lens}")

	var actions []FileAction
	for _, g := range groups {
		tokens := Tokens{Shot: g.Shot, Stem: g.Stem, Ext: primaryExt(g)}
		if wantsMetadata && metadata != nil {
			tokens.Camera, tokens.Lens = metadata(g)
		}
		dir, err := ExpandTemplate(dest, tokens)
		if err != nil {
			return nil, fmt.Errorf("ops: destination for %s: %w", g.Stem, err)
		}
		// An expansion that kept no folder at all — every segment died with
		// its token — is not a destination, however absolute the bare root
		// separator looks. Spilling files straight into / would be the plan.
		if strings.Trim(dir, "/") == "" {
			return nil, fmt.Errorf("ops: destination %q for %s lost every folder in its path — the frame answers none of its tokens", dest, g.Stem)
		}
		// A destination that never was absolute would plan actions relative to
		// wherever the process happens to be running from. Refusing here keeps
		// that path out of every plan.
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("ops: destination %q for %s expands to %q, which is not an absolute folder", dest, g.Stem, dir)
		}
		for _, ref := range surviving(g, halves) {
			actions = append(actions, FileAction{
				Verb: verb,
				Src:  ref.Path,
				Dst:  filepath.Join(dir, filepath.Base(ref.Path)),
			})
		}
	}
	return actions, nil
}

// surviving lists the files of g that halves keeps, in the order they should
// be written: the RAW, then the JPEG, then the sidecars.
//
// Sidecars follow the RAW, so they only travel when the RAW does. A frame with
// no RAW at all has nothing else for them to belong to, so there they follow
// the JPEG instead of being stranded.
func surviving(g scan.PhotoGroup, halves Halves) []scan.FileRef {
	var out []scan.FileRef
	raw := g.Raw != nil && halves != HalvesJPEG
	jpeg := g.Jpeg != nil && halves != HalvesRAW
	if raw {
		out = append(out, *g.Raw)
	}
	if jpeg {
		out = append(out, *g.Jpeg)
	}
	if raw || (g.Raw == nil && jpeg) {
		out = append(out, g.Sidecars...)
	}
	return out
}

// primaryExt is the extension of the file a frame is really about: the RAW
// when there is one, the JPEG otherwise. Lowercase and without the dot, so it
// reads as a folder name.
func primaryExt(g scan.PhotoGroup) string {
	ref := g.Raw
	if ref == nil {
		ref = g.Jpeg
	}
	if ref == nil {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(ref.Path), "."))
}

// ExpandTemplate turns a destination template into a real directory path for
// one frame.
//
// A token the frame cannot answer takes its whole path segment with it, so
// `/library/{camera}/{stem}` on a frame with no EXIF is `/library/DSCF0001`
// and never `/library//DSCF0001` or a folder literally called {camera}. The
// segment goes rather than just the token because a segment written around a
// token — `shot-on-{camera}` — is about that token and means nothing without
// it.
//
// Separators inside a token's value are the one thing that does not pass
// through: a camera that calls itself Nikon/Z8 must not quietly add a folder
// level. Separators the user wrote, including inside a date layout, do.
//
// A backslash in the template is a separator too, so a template written with
// Windows separators splits into the same segments it would with forward
// slashes — a dead token takes its own segment, never a whole backslash-joined
// path that then collapses to nothing.
func ExpandTemplate(template string, tok Tokens) (string, error) {
	template = strings.ReplaceAll(template, `\`, "/")
	// A UNC destination opens with a doubled separator naming a host, not an
	// empty folder. It has to survive the split, or \\server\share would come
	// out as /server/share — a path on the local volume that nothing can use.
	unc := strings.HasPrefix(template, "//")
	absolute := strings.HasPrefix(template, "/")
	rest := strings.TrimLeft(template, "/")

	// Each segment carries whether a token in it went unanswered, which is
	// what decides between an empty segment worth dropping and a whole
	// segment worth dropping.
	var segments []string
	var dead []bool
	segments = append(segments, "")
	dead = append(dead, false)

	addLiteral := func(s string) {
		parts := strings.Split(s, "/")
		segments[len(segments)-1] += parts[0]
		for _, p := range parts[1:] {
			segments = append(segments, p)
			dead = append(dead, false)
		}
	}

	for rest != "" {
		open := strings.IndexByte(rest, '{')
		if open == -1 {
			addLiteral(rest)
			break
		}
		addLiteral(rest[:open])
		shut := strings.IndexByte(rest[open:], '}')
		if shut == -1 {
			return "", fmt.Errorf("unclosed { in destination %q", template)
		}
		name := rest[open+1 : open+shut]
		rest = rest[open+shut+1:]

		value, ok := resolveToken(name, tok)
		if !ok {
			dead[len(dead)-1] = true
			continue
		}
		addLiteral(value)
	}

	var kept []string
	for i, s := range segments {
		if dead[i] || s == "" || s == "." || s == ".." {
			continue
		}
		kept = append(kept, s)
	}
	joined := strings.Join(kept, "/")
	if unc {
		return "//" + joined, nil
	}
	if absolute {
		return "/" + joined, nil
	}
	return joined, nil
}

// resolveToken answers one token, reporting false when the frame has nothing
// to fill it with — which includes tokens this build has never heard of, so an
// unknown one degrades to a missing folder level rather than to a literal.
func resolveToken(name string, tok Tokens) (string, bool) {
	if layout, ok := strings.CutPrefix(name, "date:"); ok {
		if tok.Shot.IsZero() || layout == "" {
			return "", false
		}
		return tok.Shot.Format(layout), true
	}
	var value string
	switch name {
	case "stem":
		value = tok.Stem
	case "ext":
		value = tok.Ext
	case "camera":
		value = tok.Camera
	case "lens":
		value = tok.Lens
	}
	value = sanitiseValue(value)
	return value, value != ""
}

// sanitiseValue makes a metadata string safe to be one path segment.
func sanitiseValue(v string) string {
	v = strings.NewReplacer("/", "-", `\`, "-", ":", "-").Replace(v)
	v = strings.TrimSpace(v)
	if v == "." || v == ".." {
		return ""
	}
	return v
}
