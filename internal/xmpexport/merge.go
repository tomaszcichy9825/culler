package xmpexport

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/tomaszcichy9825/culler/internal/decide"
	"github.com/tomaszcichy9825/culler/internal/exif"
)

const (
	xmpNS = "http://ns.adobe.com/xap/1.0/"
	rdfNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	// defaultPrefix is what our namespace is bound to in a sidecar this app
	// writes, and in one that does not bind it at all. A sidecar that already
	// has a prefix for it keeps that prefix.
	defaultPrefix = "xmp"
)

var (
	// ErrNotXMP means the file did not parse as XML. It is not an invitation
	// to replace it: a file this package cannot read is a file whose contents
	// it cannot promise to preserve.
	ErrNotXMP = errors.New("xmpexport: this sidecar is not readable XML, so it will not be written over")
	// ErrNoDescription means there is nowhere in the packet our fields belong.
	ErrNoDescription = errors.New("xmpexport: this sidecar has no rdf:Description to write into")
)

// field is one value this package owns, by its local name in the XMP basic
// namespace. Nothing outside this list is ever removed or written.
type field struct {
	name  string
	value string
}

// element renders the field under a prefix. Both values come from a closed set
// — an integer and one of two colour names — so there is nothing to escape.
func (f field) element(prefix string) string {
	return fmt.Sprintf("<%s:%s>%s</%s:%s>", prefix, f.name, f.value, prefix, f.name)
}

// Property is one XMP property a merge owns, identified by namespace URI and
// local name. Every owned property is removed from the existing sidecar
// wherever it appears — element form or attribute form — and Render, when it
// is not nil, produces the inner XML of the element written in its place.
// Render's prefix argument reports what the target block binds a namespace
// to, declaring a fresh binding when the file has none, so nested containers
// like rdf:Seq come out in the file's own prefixes.
type Property struct {
	Space string
	Local string
	Render func(prefix func(uri string) string) string
}

// fields is what a record exports as, in the order it is written. A record
// with nothing to say produces none, which is what makes an undecided frame
// leave no file behind.
func fields(rec decide.Record) []field {
	var out []field
	if rec.Rating > 0 {
		rating := rec.Rating
		if rating > decide.MaxRating {
			rating = decide.MaxRating
		}
		out = append(out, field{name: "Rating", value: strconv.Itoa(rating)})
	}
	if label := Label(rec.Verdict); label != "" {
		out = append(out, field{name: "Label", value: label})
	}
	return out
}

// edit replaces the bytes in [at, end) with text. A deletion carries no text.
type edit struct {
	at, end int
	text    string
}

// description is one rdf:Description block, located in the raw bytes.
type description struct {
	start, startEnd int // the span of its start tag
	endAt           int // where its end tag begins, -1 when self-closing
	selfClosing     bool
	// scope is the namespace bindings visible inside this block, innermost
	// last, so a splice can write fields in the prefixes the file itself uses.
	scope []map[string]string
	// taken is every prefix bound in that scope, so a new binding does not
	// steal a name the file is already using for something else.
	taken map[string]bool
}

// Merge returns existing with our two decision fields replaced by the
// record's, and every other byte reproduced exactly.
//
// A record with nothing in it removes our fields and adds none, which is how a
// cleared decision leaves a foreign sidecar intact.
func Merge(existing []byte, rec decide.Record) ([]byte, error) {
	// Both fields are owned whether or not the record carries them, which is
	// what makes an empty record a removal rather than a no-op.
	props := []Property{
		{Space: xmpNS, Local: "Rating"},
		{Space: xmpNS, Local: "Label"},
	}
	for _, f := range fields(rec) {
		value := f.value
		for i := range props {
			if props[i].Local == f.name {
				// Values come from a closed set — an integer and two colour
				// names — so there is nothing to escape.
				props[i].Render = func(func(string) string) string { return value }
			}
		}
	}
	return MergeProperties(existing, props)
}

// MergeChanges merges a metadata edit into a sidecar that already exists: the
// edit's properties replace their previous values and every other byte of the
// file — Lightroom's develop settings, keywords, an earlier edit's fields —
// is reproduced exactly. A sidecar this package cannot parse is refused, and
// the caller must not write over it: a file whose contents cannot be read
// cannot be promised to survive.
func MergeChanges(existing []byte, c exif.Changes) ([]byte, error) {
	var props []Property
	for _, p := range c.XMPProperties() {
		prop := Property{Space: p.Space, Local: p.Local}
		if p.Inner != nil {
			inner := p.Inner
			prop.Render = func(prefix func(uri string) string) string {
				return inner(prefix(rdfNS))
			}
		}
		props = append(props, prop)
	}
	return MergeProperties(existing, props)
}

// MergeProperties returns existing with the owned properties replaced and
// every other byte reproduced exactly.
//
// It works on the raw bytes rather than re-serialising a parse tree: an XMP
// packet carries formatting, comments, unusual prefixes and namespaces this
// app has never heard of, and a round trip through an encoder would quietly
// rewrite all of it. The parser is used to find the owned properties — in
// element form and in the attribute form Lightroom writes — and the bytes it
// points at are the only ones that move.
func MergeProperties(existing []byte, props []Property) ([]byte, error) {
	owned := func(space, local string) bool {
		for _, p := range props {
			if p.Space == space && p.Local == local {
				return true
			}
		}
		return false
	}
	var want []Property
	for _, p := range props {
		if p.Render != nil {
			want = append(want, p)
		}
	}

	edits, descs, err := survey(existing, owned)
	if err != nil {
		return nil, err
	}
	if len(want) > 0 {
		target, ok := insertionPoint(descs, want)
		if !ok {
			return nil, ErrNoDescription
		}
		edits = append(edits, splice(existing, target, want)...)
	}
	return apply(existing, edits)
}

// survey walks the packet, collecting the edits that remove the owned
// properties and the description blocks that could hold their replacements.
func survey(data []byte, owned func(space, local string) bool) ([]edit, []description, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var (
		edits []edit
		descs []description
		scope []map[string]string // namespace bindings, one frame per open element
		// open records, for every element currently open, which description it
		// is — an index into descs, -1 for anything else. The decoder reports a
		// self-closing element's EndElement at the same offset as its
		// StartElement, so matching ends to starts by position alone would let
		// a self-closing nested description donate its offset to the block
		// enclosing it; the stack pairs each end with the start it closes.
		open []int
		// The span of the last run of whitespace, so removing an element takes
		// its indentation with it rather than leaving a blank line behind.
		wsAt, wsEnd = -1, -1
		prev        int64
	)

	for {
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %v", ErrNotXMP, err)
		}
		start, end := int(prev), int(d.InputOffset())
		prev = d.InputOffset()

		switch t := tok.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(t)) == "" {
				wsAt, wsEnd = start, end
				continue
			}
		case xml.StartElement:
			scope = append(scope, bindings(t))

			if owned(t.Name.Space, t.Name.Local) {
				if err := d.Skip(); err != nil {
					return nil, nil, fmt.Errorf("%w: %v", ErrNotXMP, err)
				}
				scope = scope[:len(scope)-1]
				from := start
				if wsAt >= 0 && wsEnd == start {
					from = wsAt
				}
				edits = append(edits, edit{at: from, end: int(d.InputOffset())})
				prev = d.InputOffset()
				wsAt, wsEnd = -1, -1
				continue
			}
			idx := -1
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				raw := data[start:end]
				descs = append(descs, description{
					start:       start,
					startEnd:    end,
					endAt:       -1,
					selfClosing: bytes.HasSuffix(raw, []byte("/>")),
					scope:       append([]map[string]string(nil), scope...),
					taken:       prefixes(scope),
				})
				idx = len(descs) - 1
				for _, span := range ownedAttributes(raw, scope, owned) {
					edits = append(edits, edit{at: start + span.at, end: start + span.end})
				}
			}
			open = append(open, idx)
		case xml.EndElement:
			scope = scope[:len(scope)-1]
			if len(open) == 0 {
				// Unreachable in well-formed XML, which is the only kind the
				// decoder delivers; refusing beats indexing past the stack.
				return nil, nil, fmt.Errorf("%w: end tag with no start", ErrNotXMP)
			}
			idx := open[len(open)-1]
			open = open[:len(open)-1]
			// A self-closing description has no end tag of its own: the
			// decoder's synthetic EndElement sits at the start tag's offset
			// and must not be recorded as anyone's endAt.
			if idx >= 0 && !descs[idx].selfClosing {
				descs[idx].endAt = start
			}
		}
		wsAt, wsEnd = -1, -1
	}
	return edits, descs, nil
}

// insertionPoint picks the block the properties go in: the first one that
// already binds one of their namespaces, otherwise the first block there is.
func insertionPoint(descs []description, want []Property) (description, bool) {
	for _, d := range descs {
		for _, p := range want {
			if prefixFor(d.scope, p.Space) != "" {
				return d, true
			}
		}
	}
	if len(descs) > 0 {
		return descs[0], true
	}
	return description{}, false
}

// renderProperties produces the elements for the properties in the prefixes
// the block binds, and the xmlns declarations for the namespaces it does not.
func renderProperties(d description, want []Property) (elements []string, declare string) {
	taken := map[string]bool{}
	for p := range d.taken {
		taken[p] = true
	}
	bound := map[string]string{}
	var declared []string
	prefix := func(uri string) string {
		if p := prefixFor(d.scope, uri); p != "" {
			return p
		}
		if p, ok := bound[uri]; ok {
			return p
		}
		p := freePrefix(taken, suggestedPrefix(uri))
		taken[p] = true
		bound[uri] = p
		declared = append(declared, fmt.Sprintf(` xmlns:%s="%s"`, p, uri))
		return p
	}

	for _, pr := range want {
		p := prefix(pr.Space)
		elements = append(elements, fmt.Sprintf("<%s:%s>%s</%s:%s>", p, pr.Local, pr.Render(prefix), p, pr.Local))
	}
	return elements, strings.Join(declared, "")
}

// splice builds the edits that put the properties into a description block:
// the elements themselves, namespace bindings when the file lacks them, and
// the end tag a self-closing block has to grow before it can hold anything.
func splice(data []byte, d description, want []Property) []edit {
	elements, declare := renderProperties(d, want)

	if d.selfClosing {
		indent := lineIndent(data[:d.start])
		var b strings.Builder
		b.WriteString(declare)
		b.WriteString(">")
		for _, e := range elements {
			b.WriteString("\n" + indent + "  " + e)
		}
		b.WriteString("\n" + indent + "</" + tagName(data[d.start:d.startEnd]) + ">")
		// The "/>" is what turns into the rest of the block.
		return []edit{{at: d.startEnd - 2, end: d.startEnd, text: b.String()}}
	}

	// The whitespace in front of the end tag is the block's own indentation. A
	// block laid out on one line keeps its fields on that line.
	ws := trailingSpace(data[:d.endAt])
	var b strings.Builder
	if strings.Contains(ws, "\n") {
		closing := ws[strings.LastIndexByte(ws, '\n')+1:]
		indent := childIndent(data[d.startEnd:d.endAt], closing+"  ")
		for _, e := range elements {
			b.WriteString("\n" + indent + e)
		}
	} else {
		for _, e := range elements {
			b.WriteString(e)
		}
	}
	edits := []edit{{at: d.endAt - len(ws), end: d.endAt - len(ws), text: b.String()}}
	if declare != "" {
		// Just inside the start tag's closing bracket.
		edits = append(edits, edit{at: d.startEnd - 1, end: d.startEnd - 1, text: declare})
	}
	return edits
}

// apply splices the edits into data, leaving every other byte where it was.
func apply(data []byte, edits []edit) ([]byte, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].at < edits[j].at })
	var out bytes.Buffer
	at := 0
	for _, e := range edits {
		if e.at < at || e.end > len(data) || e.at > e.end {
			// Unreachable unless the survey and the splice disagree about the
			// file, which would mean writing something neither of them meant.
			return nil, fmt.Errorf("xmpexport: overlapping edit at %d in a %d byte sidecar", e.at, len(data))
		}
		out.Write(data[at:e.at])
		out.WriteString(e.text)
		at = e.end
	}
	out.Write(data[at:])
	return out.Bytes(), nil
}

// --- namespaces -------------------------------------------------------------

// bindings is the prefix to namespace map an element declares. The parser
// resolves names for us but keeps the prefixes to itself, and the prefixes are
// what the merged bytes have to be written in.
func bindings(t xml.StartElement) map[string]string {
	out := map[string]string{}
	for _, a := range t.Attr {
		switch {
		case a.Name.Space == "xmlns":
			out[a.Name.Local] = a.Value
		case a.Name.Space == "" && a.Name.Local == "xmlns":
			out[""] = a.Value
		}
	}
	return out
}

// prefixFor is a prefix that means uri at the point scope describes, empty
// when there is none. A prefix only counts if its innermost binding is the
// wanted namespace: an outer binding shadowed by a rebinding further in would
// put the property in whatever namespace the rebinding names.
// The default namespace is not a candidate: our fields are written with a
// prefix so they cannot be caught out by a default binding changing further
// down the file.
func prefixFor(scope []map[string]string, uri string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		for prefix, bound := range scope[i] {
			if prefix != "" && bound == uri && resolve(scope, prefix) == uri {
				return prefix
			}
		}
	}
	return ""
}

// prefixes is every prefix bound anywhere in scope.
func prefixes(scope []map[string]string) map[string]bool {
	out := map[string]bool{}
	for _, frame := range scope {
		for prefix := range frame {
			if prefix != "" {
				out[prefix] = true
			}
		}
	}
	return out
}

// freePrefix is the name to bind a namespace to: the suggestion, unless the
// file has already used it for something else.
func freePrefix(taken map[string]bool, base string) string {
	if !taken[base] {
		return base
	}
	for n := 2; ; n++ {
		candidate := base + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// suggestedPrefix is the conventional prefix for a namespace this package may
// have to declare, so a merged sidecar reads the way every other tool writes.
func suggestedPrefix(uri string) string {
	switch uri {
	case xmpNS:
		return defaultPrefix
	case rdfNS:
		return "rdf"
	case "http://purl.org/dc/elements/1.1/":
		return "dc"
	case "http://ns.adobe.com/exif/1.0/":
		return "exif"
	}
	return "ns"
}

// --- raw tag reading --------------------------------------------------------

// span is a byte range within a start tag.
type span struct{ at, end int }

// ownedAttributes finds owned properties written as attributes on a start
// tag, which is the compact form Lightroom writes. The parser gives attribute
// values but not where they sit, so the tag is read again here; it has already
// parsed, so this reader only has to agree with it, not to validate it.
//
// The span returned takes the whitespace in front of the attribute with it, so
// removing one does not leave a double space behind.
func ownedAttributes(tag []byte, scope []map[string]string, owned func(space, local string) bool) []span {
	var out []span
	for _, a := range attributes(tag) {
		prefix, local, ok := strings.Cut(a.name, ":")
		// An unprefixed attribute is in no namespace at all, whatever default
		// the element declares, so it can never be an owned one.
		if !ok || prefix == "xmlns" {
			continue
		}
		if owned(resolve(scope, prefix), local) {
			out = append(out, a.span)
		}
	}
	return out
}

// resolve is the namespace a prefix stands for in scope.
func resolve(scope []map[string]string, prefix string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		if uri, ok := scope[i][prefix]; ok {
			return uri
		}
	}
	return ""
}

type attribute struct {
	name string
	span span
}

// attributes reads the name and span of every attribute in a start tag.
func attributes(tag []byte) []attribute {
	i := 1 // past the '<'
	for i < len(tag) && !space(tag[i]) && tag[i] != '>' && tag[i] != '/' {
		i++
	}

	var out []attribute
	for i < len(tag) {
		from := i
		for i < len(tag) && space(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' || tag[i] == '/' {
			break
		}
		nameAt := i
		for i < len(tag) && !space(tag[i]) && tag[i] != '=' && tag[i] != '>' && tag[i] != '/' {
			i++
		}
		name := string(tag[nameAt:i])
		for i < len(tag) && space(tag[i]) {
			i++
		}
		if i >= len(tag) || tag[i] != '=' {
			continue
		}
		i++
		for i < len(tag) && space(tag[i]) {
			i++
		}
		if i >= len(tag) || (tag[i] != '"' && tag[i] != '\'') {
			break
		}
		quote := tag[i]
		i++
		for i < len(tag) && tag[i] != quote {
			i++
		}
		if i < len(tag) {
			i++ // the closing quote
		}
		out = append(out, attribute{name: name, span: span{at: from, end: i}})
	}
	return out
}

// tagName is the qualified name a start tag opens, prefix and all, so the end
// tag written for it matches.
func tagName(tag []byte) string {
	i := 1
	for i < len(tag) && !space(tag[i]) && tag[i] != '>' && tag[i] != '/' {
		i++
	}
	return string(tag[1:i])
}

// trailingSpace is the run of whitespace at the end of data.
func trailingSpace(data []byte) string {
	i := len(data)
	for i > 0 && space(data[i-1]) {
		i--
	}
	return string(data[i:])
}

// childIndent is how a block indents the children it already has, so a field
// spliced into it lines up with them instead of with a guess. A block with no
// children of its own has nothing to copy and takes the fallback.
func childIndent(inner []byte, fallback string) string {
	lines := strings.Split(string(inner), "\n")
	// The first line is the tail of the start tag, not a child.
	for _, line := range lines[1:] {
		body := strings.TrimLeft(line, " \t")
		if strings.HasPrefix(body, "<") {
			return line[:len(line)-len(body)]
		}
	}
	return fallback
}

// lineIndent is the indentation of the line data ends on.
func lineIndent(data []byte) string {
	line := data
	if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
		line = data[i+1:]
	}
	indent := line
	for i, c := range line {
		if !space(c) {
			indent = line[:i]
			break
		}
	}
	return string(indent)
}

func space(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
