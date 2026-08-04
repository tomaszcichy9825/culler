package xmpexport

import (
	"strings"
	"testing"

	"github.com/tomaszcichy9825/culler/internal/decide"
)

// A sidecar written by another tool: element form for our two fields, and a
// spread of foreign values around them — a structured dc:subject bag, a
// language alternative, a namespace this app has never heard of, and an
// attribute-form field belonging to Camera Raw.
const foreign = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="Adobe XMP Core 6.0">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description rdf:about=""
        xmlns:dc="http://purl.org/dc/elements/1.1/"
        xmlns:xmp="http://ns.adobe.com/xap/1.0/"
        xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
        crs:Exposure2012="+0.35"
        crs:Version="15.4">
      <dc:subject>
        <rdf:Bag>
          <rdf:li>gulls</rdf:li>
          <rdf:li>harbour</rdf:li>
        </rdf:Bag>
      </dc:subject>
      <dc:rights>
        <rdf:Alt>
          <rdf:li xml:lang="x-default">&#169; Someone Else</rdf:li>
        </rdf:Alt>
      </dc:rights>
      <xmp:Rating>1</xmp:Rating>
      <xmp:Label>Blue</xmp:Label>
      <xmp:CreatorTool>Some Other App</xmp:CreatorTool>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>
`

// The fragments of the foreign sidecar that a merge must reproduce byte for
// byte. Nothing this app writes gives it the right to reformat them.
var foreignFragments = []string{
	`x:xmptk="Adobe XMP Core 6.0"`,
	`crs:Exposure2012="+0.35"`,
	`crs:Version="15.4"`,
	"<rdf:li>gulls</rdf:li>",
	"<rdf:li>harbour</rdf:li>",
	`<rdf:li xml:lang="x-default">&#169; Someone Else</rdf:li>`,
	"<xmp:CreatorTool>Some Other App</xmp:CreatorTool>",
}

func mergeOK(t *testing.T, existing string, rec decide.Record) string {
	t.Helper()
	out, err := Merge([]byte(existing), rec)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	wellFormed(t, string(out))
	return string(out)
}

func assertKeeps(t *testing.T, body string, fragments []string) {
	t.Helper()
	for _, want := range fragments {
		if !strings.Contains(body, want) {
			t.Errorf("merge lost %q:\n%s", want, body)
		}
	}
}

func TestMergePreservesForeignFields(t *testing.T) {
	body := mergeOK(t, foreign, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 5})

	assertKeeps(t, body, foreignFragments)
	if !strings.Contains(body, "<xmp:Rating>5</xmp:Rating>") {
		t.Errorf("our rating did not replace theirs:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:Label>Green</xmp:Label>") {
		t.Errorf("our label did not replace theirs:\n%s", body)
	}
	if strings.Contains(body, "<xmp:Rating>1</xmp:Rating>") || strings.Contains(body, "Blue") {
		t.Errorf("the old values are still there:\n%s", body)
	}
	if n := strings.Count(body, "<xmp:Label>"); n != 1 {
		t.Errorf("want one label, got %d:\n%s", n, body)
	}
}

func TestMergeClearingKeepsForeignFields(t *testing.T) {
	body := mergeOK(t, foreign, decide.Record{})

	assertKeeps(t, body, foreignFragments)
	if strings.Contains(body, "<xmp:Rating>") || strings.Contains(body, "<xmp:Label>") {
		t.Errorf("our fields survived the clear:\n%s", body)
	}
	// The clear must not take the neighbouring xmp: field with it.
	if !strings.Contains(body, "<xmp:CreatorTool>") {
		t.Errorf("a field in our namespace that is not ours was removed:\n%s", body)
	}
}

// Lightroom writes the compact form: our fields as attributes on the
// description rather than as elements. They have to be found there too, or the
// merge leaves a stale rating behind that readers prefer to the one it wrote.
const compact = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/" x:xmptk="Adobe XMP Core 6.0">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmp="http://ns.adobe.com/xap/1.0/"
    xmlns:photoshop="http://ns.adobe.com/photoshop/1.0/"
    xmp:Rating="2"
    xmp:Label="Yellow"
    xmp:ModifyDate="2026-03-01T10:11:12+00:00"
    photoshop:City="Whitby">
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>
`

func TestMergeReplacesTheAttributeForm(t *testing.T) {
	body := mergeOK(t, compact, decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth, Rating: 1})

	assertKeeps(t, body, []string{
		`photoshop:City="Whitby"`,
		`xmp:ModifyDate="2026-03-01T10:11:12+00:00"`,
	})
	if strings.Contains(body, `xmp:Rating="2"`) || strings.Contains(body, `xmp:Label="Yellow"`) {
		t.Errorf("the attribute form was left in place:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:Rating>1</xmp:Rating>") || !strings.Contains(body, "<xmp:Label>Red</xmp:Label>") {
		t.Errorf("our values are missing:\n%s", body)
	}
}

func TestMergeClearsTheAttributeForm(t *testing.T) {
	body := mergeOK(t, compact, decide.Record{})

	if strings.Contains(body, "xmp:Rating") || strings.Contains(body, "xmp:Label") {
		t.Errorf("our attributes survived the clear:\n%s", body)
	}
	assertKeeps(t, body, []string{`photoshop:City="Whitby"`, `xmp:ModifyDate=`})
}

// A description carrying nothing but attributes is written self-closing, and
// there is no end tag to put an element in front of.
const selfClosing = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmp:Rating="3" xmp:CreatorTool="Theirs"/>
 </rdf:RDF>
</x:xmpmeta>
`

func TestMergeOpensASelfClosingDescription(t *testing.T) {
	body := mergeOK(t, selfClosing, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 4})

	assertKeeps(t, body, []string{`xmp:CreatorTool="Theirs"`, `rdf:about=""`})
	if strings.Contains(body, `xmp:Rating="3"`) {
		t.Errorf("the old attribute is still there:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:Rating>4</xmp:Rating>") || !strings.Contains(body, "</rdf:Description>") {
		t.Errorf("the description was not opened up:\n%s", body)
	}
}

// A sidecar that never mentions our namespace still has to end up with a
// binding for it, or the merged file is not the XMP it claims to be.
const noXMPNamespace = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title>Harbour</dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`

func TestMergeDeclaresOurNamespaceWhenItIsMissing(t *testing.T) {
	body := mergeOK(t, noXMPNamespace, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 2})

	assertKeeps(t, body, []string{"<dc:title>Harbour</dc:title>"})
	if !strings.Contains(body, `xmlns:xmp="http://ns.adobe.com/xap/1.0/"`) {
		t.Errorf("the namespace was not declared:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:Rating>2</xmp:Rating>") {
		t.Errorf("our rating is missing:\n%s", body)
	}
}

// The prefix is the sidecar's business, not ours: a file that binds our
// namespace to some other name gets its fields written under that name.
const oddPrefix = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:basic="http://ns.adobe.com/xap/1.0/" basic:Rating="1"/>
 </rdf:RDF>
</x:xmpmeta>
`

func TestMergeFollowsTheSidecarsOwnPrefix(t *testing.T) {
	body := mergeOK(t, oddPrefix, decide.Record{Verdict: decide.Cut, Mask: decide.MaskBoth, Rating: 3})

	if !strings.Contains(body, "<basic:Rating>3</basic:Rating>") {
		t.Errorf("the sidecar's own prefix was not used:\n%s", body)
	}
	if strings.Contains(body, `basic:Rating="1"`) {
		t.Errorf("the old attribute is still there:\n%s", body)
	}
}

func TestMergeRefusesAFileThatIsNotXML(t *testing.T) {
	if _, err := Merge([]byte("<not xml"), decide.Record{Rating: 1}); err == nil {
		t.Error("want an error for a file that does not parse")
	}
}

func TestMergeRefusesAPacketWithNoDescription(t *testing.T) {
	const empty = `<x:xmpmeta xmlns:x="adobe:ns:meta/"><nothing/></x:xmpmeta>`
	if _, err := Merge([]byte(empty), decide.Record{Rating: 1}); err == nil {
		t.Error("want an error when there is nowhere to put our fields")
	}
	// With nothing to write there is nothing to refuse: the file comes back
	// unchanged rather than as an error.
	out, err := Merge([]byte(empty), decide.Record{})
	if err != nil {
		t.Fatalf("clearing a packet with no description: %v", err)
	}
	if string(out) != empty {
		t.Errorf("the packet was rewritten: %q", out)
	}
}

// Our fields can sit in any of the description blocks a sidecar is split into,
// and every one of them has to be swept.
func TestMergeSweepsEveryDescription(t *testing.T) {
	const split = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">
   <dc:title>Harbour</dc:title>
  </rdf:Description>
  <rdf:Description rdf:about="" xmlns:xmp="http://ns.adobe.com/xap/1.0/" xmp:Label="Purple">
   <xmp:Rating>2</xmp:Rating>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`
	body := mergeOK(t, split, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 5})

	assertKeeps(t, body, []string{"<dc:title>Harbour</dc:title>"})
	if strings.Contains(body, "Purple") || strings.Contains(body, "<xmp:Rating>2</xmp:Rating>") {
		t.Errorf("an old value in the second description survived:\n%s", body)
	}
	if n := strings.Count(body, "<xmp:Rating>"); n != 1 {
		t.Errorf("want one rating, got %d:\n%s", n, body)
	}
	if !strings.Contains(body, "<xmp:Label>Green</xmp:Label>") {
		t.Errorf("our label is missing:\n%s", body)
	}
}

// A rating from a hand-edited database cannot become a rating no reader
// understands: the scale is 0 to 5 and the export stays inside it.
func TestMergeClampsARatingOffTheScale(t *testing.T) {
	body := mergeOK(t, noXMPNamespace, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 99})
	if !strings.Contains(body, "<xmp:Rating>5</xmp:Rating>") {
		t.Errorf("the rating was not clamped:\n%s", body)
	}
}

// Sidecars in the wild start with a byte-order mark often enough that losing
// one, or misreading every offset after it, would be a common failure.
func TestMergeKeepsALeadingByteOrderMark(t *testing.T) {
	body := mergeOK(t, "\uFEFF"+foreign, decide.Record{Verdict: decide.Keep, Mask: decide.MaskBoth, Rating: 5})

	if !strings.HasPrefix(body, "\uFEFF") {
		t.Error("the mark was dropped")
	}
	assertKeeps(t, body, foreignFragments)
	if !strings.Contains(body, "<xmp:Rating>5</xmp:Rating>") {
		t.Errorf("our rating is missing:\n%s", body)
	}
}
