package xmpmm

import (
	"strings"
	"testing"
	"time"

	xmpdm "github.com/OpenMellon/openpdf/go-xmp/models/xmp_dm"
	"github.com/OpenMellon/openpdf/go-xmp/xmp"
)

// A ResourceRef with AlternatePaths set is serialized with rdf:parseType="Resource",
// so its ,attr fields become child elements. Fields like FromPart (*xmpdm.Part) have
// an attribute (text) form that differs from their element (struct) form; the element
// must carry the attribute's text value so it still round-trips via UnmarshalText.
func TestResourceRefMixedRoundTrip(t *testing.T) {
	part := &xmpdm.Part{Path: "/page1"}
	roundtrip := func(rr *ResourceRef) *ResourceRef {
		d := xmp.NewDocument()
		m, err := MakeModel(d)
		if err != nil {
			t.Fatal(err)
		}
		m.DerivedFrom = rr
		b, err := xmp.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		d2 := xmp.NewDocument()
		if err := xmp.Unmarshal(b, d2); err != nil {
			t.Fatal(err)
		}
		return FindModel(d2).DerivedFrom
	}

	attrForm := roundtrip(&ResourceRef{DocumentID: "D", FromPart: part})
	mixedForm := roundtrip(&ResourceRef{DocumentID: "D", FromPart: part, AlternatePaths: xmp.UriArray{"/alt"}})

	if attrForm.FromPart == nil || attrForm.FromPart.IsZero() {
		t.Fatalf("attribute-form FromPart lost: %+v", attrForm.FromPart)
	}
	if mixedForm.FromPart == nil || mixedForm.FromPart.Path != attrForm.FromPart.Path {
		t.Errorf("element-form FromPart differs from attribute form: attr=%v element=%v", attrForm.FromPart, mixedForm.FromPart)
	}
	if len(mixedForm.AlternatePaths) != 1 {
		t.Errorf("AlternatePaths not preserved: %v", mixedForm.AlternatePaths)
	}
}

func newVersionsDoc(t *testing.T) *xmp.Document {
	t.Helper()
	d := xmp.NewDocument()
	m, err := MakeModel(d)
	if err != nil {
		t.Fatal(err)
	}
	when, _ := time.Parse(time.RFC3339, "2018-06-05T18:24:03+01:00")
	m.VersionID = "2"
	m.AddVersion(&StVersion{
		Version:    "2",
		ModifyDate: xmp.NewDate(when),
		Modifier:   "tester",
		Comments:   "second version",
		Event: ResourceEvent{
			Action:     ActionSaved,
			InstanceID: xmp.GUID("xmp.iid:INST"),
			When:       xmp.NewDate(when),
		},
	})
	return d
}

// StVersion mixes simple fields with a nested stVer:event element. Serializing the
// simple fields as RDF property attributes alongside rdf:parseType="Resource" produces
// invalid RDF/XML (rejected by PDF/A tooling). They must serialize as child elements.
func TestStVersionSerializesAsElements(t *testing.T) {
	b, err := xmp.Marshal(newVersionsDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	for _, attr := range []string{`stVer:comments="`, `stVer:modifier="`, `stVer:modifyDate="`, `stVer:version="`} {
		if strings.Contains(out, attr) {
			t.Errorf("stVer field serialized as RDF property attribute (invalid with parseType=Resource): %s", attr)
		}
	}
	if !strings.Contains(out, "<stVer:comments>second version</stVer:comments>") {
		t.Errorf("stVer:comments not serialized as element\n%s", out)
	}
}

func TestStVersionRoundTrip(t *testing.T) {
	// Compare decoded field values, not raw bytes: xmlns declaration order is
	// derived from map iteration in Node.Namespaces and is not byte-stable.
	b1, err := xmp.Marshal(newVersionsDoc(t))
	if err != nil {
		t.Fatal(err)
	}
	d2 := xmp.NewDocument()
	if err := xmp.Unmarshal(b1, d2); err != nil {
		t.Fatal(err)
	}
	got := FindModel(d2)
	if got == nil || len(got.Versions) != 1 {
		t.Fatalf("expected 1 version after round-trip, got %+v", got)
	}
	v := got.Versions[0]
	if v.Version != "2" || v.Comments != "second version" || v.Modifier != "tester" {
		t.Errorf("version fields not preserved: %+v", v)
	}
	if v.Event.Action != ActionSaved || v.Event.InstanceID != "xmp.iid:INST" {
		t.Errorf("nested event not preserved: %+v", v.Event)
	}
}

// Files written by older versions used the attribute form; the unmarshaler must still read them.
func TestStVersionReadsLegacyAttributeForm(t *testing.T) {
	const legacy = `<?xpacket begin="" id="W5M0MpCehiHzreSzNTczkc9d"?>
<x:xmpmeta xmlns:x="adobe:ns:meta/">
  <rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">
    <rdf:Description xmlns:xmpMM="http://ns.adobe.com/xap/1.0/mm/" xmlns:stEvt="http://ns.adobe.com/xap/1.0/sType/ResourceEvent#" xmlns:stVer="http://ns.adobe.com/xap/1.0/sType/Version#" rdf:about="">
      <xmpMM:Versions>
        <rdf:Bag>
          <rdf:li stVer:comments="legacy comment" stVer:modifier="legacy mod" stVer:version="7" rdf:parseType="Resource">
            <stVer:event stEvt:action="saved" stEvt:instanceID="xmp.iid:LEG"></stVer:event>
          </rdf:li>
        </rdf:Bag>
      </xmpMM:Versions>
    </rdf:Description>
  </rdf:RDF>
</x:xmpmeta>
<?xpacket end="w"?>`
	d := xmp.NewDocument()
	if err := xmp.Unmarshal([]byte(legacy), d); err != nil {
		t.Fatal(err)
	}
	m := FindModel(d)
	if m == nil || len(m.Versions) != 1 {
		t.Fatalf("legacy version not read: %+v", m)
	}
	v := m.Versions[0]
	if v.Version != "7" || v.Comments != "legacy comment" || v.Modifier != "legacy mod" || v.Event.Action != ActionSaved {
		t.Errorf("legacy attribute-form fields not read back: %+v", v)
	}
}
