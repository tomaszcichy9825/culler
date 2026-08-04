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

// ours reports whether a local name in the XMP basic namespace is one of the
// two fields this package writes. Everything else in that namespace —
// xmp:CreatorTool, xmp:ModifyDate — belongs to whoever put it there.
func ours(local string) bool {
	return local == "Rating" || local == "Label"
}

// element renders the field under a prefix. Both values come from a closed set
// — an integer and one of two colour names — so there is nothing to escape.
func (f field) element(prefix string) string {
	return fmt.Sprintf("<%s:%s>%s</%s:%s>", prefix, f.name, f.value, prefix, f.name)
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
	// prefix is what our namespace is already bound to in this block's scope,
	// empty when it is not bound at all.
	prefix string
	// taken is every prefix bound in that scope, so a new binding does not
	// steal a name the file is already using for something else.
	taken map[string]bool
}

// Merge returns existing with our fields replaced by the record's, and every
// other byte reproduced exactly.
//
// It works on the raw bytes rather than re-serialising a parse tree: an XMP
// packet carries formatting, comments, unusual prefixes and namespaces this
// app has never heard of, and a round trip through an encoder would quietly
// rewrite all of it. The parser is used to find our fields — in element form
// and in the attribute form Lightroom writes — and the bytes it points at are
// the only ones that move.
//
// A record with nothing in it removes our fields and adds none, which is how a
// cleared decision leaves a foreign sidecar intact.
func Merge(existing []byte, rec decide.Record) ([]byte, error) {
	want := fields(rec)
	edits, descs, err := survey(existing)
	if err != nil {
		return nil, err
	}
	if len(want) > 0 {
		target, ok := insertionPoint(descs)
		if !ok {
			return nil, ErrNoDescription
		}
		edits = append(edits, splice(existing, target, want)...)
	}
	return apply(existing, edits)
}

// survey walks the packet, collecting the edits that remove our fields and the
// description blocks that could hold them.
func survey(data []byte) ([]edit, []description, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	var (
		edits []edit
		descs []description
		scope []map[string]string // namespace bindings, one frame per open element
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

			if t.Name.Space == xmpNS && ours(t.Name.Local) {
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
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				raw := data[start:end]
				descs = append(descs, description{
					start:       start,
					startEnd:    end,
					endAt:       -1,
					selfClosing: bytes.HasSuffix(raw, []byte("/>")),
					prefix:      prefixFor(scope, xmpNS),
					taken:       prefixes(scope),
				})
				for _, span := range ourAttributes(raw, scope) {
					edits = append(edits, edit{at: start + span.at, end: start + span.end})
				}
			}
		case xml.EndElement:
			scope = scope[:len(scope)-1]
			if t.Name.Space == rdfNS && t.Name.Local == "Description" {
				// The innermost block still waiting for its end tag is this
				// one; a description nested in a structured value cannot
				// close before the block holding it.
				for i := len(descs) - 1; i >= 0; i-- {
					if !descs[i].selfClosing && descs[i].endAt < 0 {
						descs[i].endAt = start
						break
					}
				}
			}
		}
		wsAt, wsEnd = -1, -1
	}
	return edits, descs, nil
}

// insertionPoint picks the block our fields go in: the first one that already
// binds our namespace, otherwise the first block there is.
func insertionPoint(descs []description) (description, bool) {
	for _, d := range descs {
		if d.prefix != "" {
			return d, true
		}
	}
	if len(descs) > 0 {
		return descs[0], true
	}
	return description{}, false
}

// splice builds the edits that put our fields into a description block: the
// elements themselves, a namespace binding when the file has none, and the end
// tag a self-closing block has to grow before it can hold anything.
func splice(data []byte, d description, want []field) []edit {
	prefix, declare := d.prefix, ""
	if prefix == "" {
		prefix = freePrefix(d.taken)
		declare = fmt.Sprintf(` xmlns:%s="%s"`, prefix, xmpNS)
	}

	if d.selfClosing {
		indent := lineIndent(data[:d.start])
		var b strings.Builder
		b.WriteString(declare)
		b.WriteString(">")
		for _, f := range want {
			b.WriteString("\n" + indent + "  " + f.element(prefix))
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
		for _, f := range want {
			b.WriteString("\n" + indent + f.element(prefix))
		}
	} else {
		for _, f := range want {
			b.WriteString(f.element(prefix))
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

// prefixFor is the innermost prefix bound to uri, empty when there is none.
// The default namespace is not a candidate: our fields are written with a
// prefix so they cannot be caught out by a default binding changing further
// down the file.
func prefixFor(scope []map[string]string, uri string) string {
	for i := len(scope) - 1; i >= 0; i-- {
		for prefix, bound := range scope[i] {
			if prefix != "" && bound == uri {
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

// freePrefix is the name to bind our namespace to: "xmp" unless the file has
// already used it for something else.
func freePrefix(taken map[string]bool) string {
	if !taken[defaultPrefix] {
		return defaultPrefix
	}
	for n := 2; ; n++ {
		candidate := defaultPrefix + strconv.Itoa(n)
		if !taken[candidate] {
			return candidate
		}
	}
}

// --- raw tag reading --------------------------------------------------------

// span is a byte range within a start tag.
type span struct{ at, end int }

// ourAttributes finds our fields written as attributes on a start tag, which
// is the compact form Lightroom writes. The parser gives attribute values but
// not where they sit, so the tag is read again here; it has already parsed, so
// this reader only has to agree with it, not to validate it.
//
// The span returned takes the whitespace in front of the attribute with it, so
// removing one does not leave a double space behind.
func ourAttributes(tag []byte, scope []map[string]string) []span {
	var out []span
	for _, a := range attributes(tag) {
		prefix, local, ok := strings.Cut(a.name, ":")
		// An unprefixed attribute is in no namespace at all, whatever default
		// the element declares, so it can never be one of ours.
		if !ok || prefix == "xmlns" || !ours(local) {
			continue
		}
		if resolve(scope, prefix) == xmpNS {
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
