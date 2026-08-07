package xmpexport

import (
	"strings"
	"testing"
	"time"

	"github.com/tomaszcichy9825/culler/internal/exif"
)

func strp(s string) *string { return &s }

func mergeChangesOK(t *testing.T, existing string, c exif.Changes) string {
	t.Helper()
	out, err := MergeChanges([]byte(existing), c)
	if err != nil {
		t.Fatalf("MergeChanges: %v", err)
	}
	wellFormed(t, string(out))
	return string(out)
}

// A metadata edit merged into a foreign sidecar keeps every byte that is not
// the edit's own: the rating, the keywords, Camera Raw's settings. The exif
// namespace the foreign file never binds is declared rather than assumed.
func TestMergeChangesPreservesForeignFields(t *testing.T) {
	body := mergeChangesOK(t, foreign, exif.Changes{
		SetGPS: &exif.GPSCoord{Latitude: 51.5066667, Longitude: -0.1275},
	})

	assertKeeps(t, body, foreignFragments)
	if !strings.Contains(body, "<xmp:Rating>1</xmp:Rating>") {
		t.Errorf("the foreign rating was lost:\n%s", body)
	}
	if !strings.Contains(body, `xmlns:exif="http://ns.adobe.com/exif/1.0/"`) {
		t.Errorf("the exif namespace was not declared:\n%s", body)
	}
	if !strings.Contains(body, "<exif:GPSLatitude>") || !strings.Contains(body, "<exif:GPSLongitude>") {
		t.Errorf("the location is missing:\n%s", body)
	}
}

// Two successive edits accumulate: the second merges into the sidecar the
// first produced, replacing only its own field.
func TestMergeChangesReplacesOnlyItsOwnFields(t *testing.T) {
	first := string(exif.RenderXMP(exif.Changes{Artist: strp("Tomasz Cichy")}))
	body := mergeChangesOK(t, first, exif.Changes{Copyright: strp("© 2026")})

	if !strings.Contains(body, "Tomasz Cichy") {
		t.Errorf("the earlier edit's artist was destroyed:\n%s", body)
	}
	if !strings.Contains(body, "© 2026") {
		t.Errorf("the copyright did not land:\n%s", body)
	}

	// And editing the same field replaces it rather than doubling it.
	again := mergeChangesOK(t, body, exif.Changes{Artist: strp("Someone Else")})
	if strings.Contains(again, "Tomasz Cichy") {
		t.Errorf("the old artist survived its replacement:\n%s", again)
	}
	if n := strings.Count(again, "<dc:creator>"); n != 1 {
		t.Errorf("want one dc:creator, got %d:\n%s", n, again)
	}
}

// A new position without an altitude takes a stale altitude off, the same way
// the JPEG write does — a leftover would silently qualify the new coordinates.
func TestMergeChangesRemovesAStaleAltitude(t *testing.T) {
	withAltitude := string(exif.RenderXMP(exif.Changes{
		SetGPS: &exif.GPSCoord{Latitude: 10, Longitude: 20, Altitude: 100, HasAltitude: true},
	}))
	body := mergeChangesOK(t, withAltitude, exif.Changes{
		SetGPS: &exif.GPSCoord{Latitude: 48.8584, Longitude: 2.2945},
	})

	if strings.Contains(body, "GPSAltitude") {
		t.Errorf("a stale altitude survived the new location:\n%s", body)
	}
	if !strings.Contains(body, "48,") {
		t.Errorf("the new latitude is missing:\n%s", body)
	}
}

// Clearing a field removes it from the sidecar and touches nothing else.
func TestMergeChangesClearsAFieldWithAnEmptyValue(t *testing.T) {
	first := string(exif.RenderXMP(exif.Changes{Artist: strp("Tomasz Cichy"), Copyright: strp("© 2026")}))
	body := mergeChangesOK(t, first, exif.Changes{Artist: strp("")})

	if strings.Contains(body, "dc:creator") || strings.Contains(body, "Tomasz Cichy") {
		t.Errorf("the cleared artist is still there:\n%s", body)
	}
	if !strings.Contains(body, "© 2026") {
		t.Errorf("clearing one field cleared another:\n%s", body)
	}
}

// The attribute form Lightroom writes is swept too: a stale exif:GPSLatitude
// sitting on the description must not outlive the element that replaces it.
func TestMergeChangesReplacesTheAttributeForm(t *testing.T) {
	const compact = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:exif="http://ns.adobe.com/exif/1.0/"
    exif:GPSLatitude="10,30.000000N"
    exif:GPSLongitude="20,15.000000E">
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`
	body := mergeChangesOK(t, compact, exif.Changes{
		SetGPS: &exif.GPSCoord{Latitude: 48.8584, Longitude: 2.2945},
	})
	if strings.Contains(body, `exif:GPSLatitude="10`) {
		t.Errorf("the stale attribute-form position is still there:\n%s", body)
	}
	if !strings.Contains(body, "<exif:GPSLatitude>48,") {
		t.Errorf("the new position is missing:\n%s", body)
	}
}

// A capture time edit updates both the exif and the xmp renderings of it.
func TestMergeChangesWritesTheCaptureTimeTwice(t *testing.T) {
	when := time.Date(2026, 8, 5, 10, 11, 12, 0, time.FixedZone("", 2*3600))
	body := mergeChangesOK(t, foreign, exif.Changes{
		DateTimeOriginal: &exif.CaptureTime{Value: when, HasOffset: true},
	})
	if !strings.Contains(body, "<exif:DateTimeOriginal>2026-08-05T10:11:12+02:00</exif:DateTimeOriginal>") {
		t.Errorf("exif:DateTimeOriginal is missing:\n%s", body)
	}
	if !strings.Contains(body, "<xmp:CreateDate>2026-08-05T10:11:12+02:00</xmp:CreateDate>") {
		t.Errorf("xmp:CreateDate is missing:\n%s", body)
	}
}

// Adobe writes structured values — xmpMM:DerivedFrom, Camera Raw's masks — as
// a nested rdf:Description, usually self-closing. A merged property must land
// in the outer description, after the structured value, never inside it.
const derivedFrom = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/"
    xmlns:stRef="http://ns.adobe.com/xap/1.0/sType/ResourceRef#"
    xmlns:dc="http://purl.org/dc/elements/1.1/">
   <xmpMM:DerivedFrom>
    <rdf:Description stRef:instanceID="xmp.iid:0001" stRef:documentID="xmp.did:0002"/>
   </xmpMM:DerivedFrom>
   <dc:title>Harbour</dc:title>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`

func assertLandsOutsideTheStructuredValue(t *testing.T, body, closeTag string) {
	t.Helper()
	creator := strings.Index(body, "<dc:creator>")
	structEnd := strings.Index(body, closeTag)
	if creator < 0 {
		t.Fatalf("the artist did not land:\n%s", body)
	}
	if structEnd < 0 {
		t.Fatalf("the structured value did not survive:\n%s", body)
	}
	if creator < structEnd {
		t.Errorf("the artist landed inside the structured value:\n%s", body)
	}
}

func TestMergeChangesLandsAfterASelfClosingStructuredValue(t *testing.T) {
	body := mergeChangesOK(t, derivedFrom, exif.Changes{Artist: strp("Tomasz Cichy")})

	assertLandsOutsideTheStructuredValue(t, body, "</xmpMM:DerivedFrom>")
	assertKeeps(t, body, []string{
		`stRef:instanceID="xmp.iid:0001"`,
		`stRef:documentID="xmp.did:0002"`,
		"<dc:title>Harbour</dc:title>",
	})
}

// The longhand form of the same structure — the nested description written
// with its own end tag — is the control: it must land in the same place.
func TestMergeChangesLandsAfterALonghandStructuredValue(t *testing.T) {
	longhand := strings.Replace(derivedFrom,
		`<rdf:Description stRef:instanceID="xmp.iid:0001" stRef:documentID="xmp.did:0002"/>`,
		`<rdf:Description stRef:instanceID="xmp.iid:0001" stRef:documentID="xmp.did:0002"></rdf:Description>`, 1)
	body := mergeChangesOK(t, longhand, exif.Changes{Artist: strp("Tomasz Cichy")})

	assertLandsOutsideTheStructuredValue(t, body, "</xmpMM:DerivedFrom>")
	assertKeeps(t, body, []string{`stRef:instanceID="xmp.iid:0001"`, "<dc:title>Harbour</dc:title>"})
}

// Camera Raw nests descriptions two deep for its mask corrections. Every end
// tag has to be matched to the start it actually closes, or the merge picks
// the wrong block to splice into.
func TestMergeChangesLandsOutsideNestedDescriptions(t *testing.T) {
	const nested = `<x:xmpmeta xmlns:x="adobe:ns:meta/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about=""
    xmlns:crs="http://ns.adobe.com/camera-raw-settings/1.0/"
    xmlns:dc="http://purl.org/dc/elements/1.1/">
   <crs:MaskGroupBasedCorrections>
    <rdf:Description crs:What="Correction">
     <crs:CorrectionMasks>
      <rdf:Description crs:What="Mask"/>
     </crs:CorrectionMasks>
    </rdf:Description>
   </crs:MaskGroupBasedCorrections>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`
	body := mergeChangesOK(t, nested, exif.Changes{Artist: strp("Tomasz Cichy")})

	assertLandsOutsideTheStructuredValue(t, body, "</crs:MaskGroupBasedCorrections>")
	assertKeeps(t, body, []string{`crs:What="Correction"`, `crs:What="Mask"`})
}

// A prefix is only usable where its innermost binding is the wanted
// namespace. A description that rebinds exif: to something foreign has to get
// a fresh prefix, not a property that reads back in the wrong namespace.
func TestMergeChangesDoesNotUseAShadowedPrefix(t *testing.T) {
	const shadowed = `<x:xmpmeta xmlns:x="adobe:ns:meta/" xmlns:exif="http://ns.adobe.com/exif/1.0/">
 <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
  <rdf:Description rdf:about="" xmlns:exif="http://example.com/not-exif/">
   <exif:Weird>kept</exif:Weird>
  </rdf:Description>
 </rdf:RDF>
</x:xmpmeta>
`
	body := mergeChangesOK(t, shadowed, exif.Changes{
		SetGPS: &exif.GPSCoord{Latitude: 48.8584, Longitude: 2.2945},
	})

	if strings.Contains(body, "<exif:GPSLatitude>") {
		t.Errorf("the property was written under a prefix the description rebinds to a foreign namespace:\n%s", body)
	}
	if !strings.Contains(body, `="http://ns.adobe.com/exif/1.0/"`) {
		t.Errorf("no binding for the exif namespace was declared:\n%s", body)
	}
	if !strings.Contains(body, "GPSLatitude>48,") {
		t.Errorf("the latitude is missing:\n%s", body)
	}
	assertKeeps(t, body, []string{"<exif:Weird>kept</exif:Weird>"})
}

func TestMergeChangesRefusesAFileThatIsNotXML(t *testing.T) {
	if _, err := MergeChanges([]byte("JFIF not xml at all \x00\x01"), exif.Changes{Artist: strp("x")}); err == nil {
		t.Error("want an error for a file that does not parse; overwriting it would destroy whatever it was")
	}
}
