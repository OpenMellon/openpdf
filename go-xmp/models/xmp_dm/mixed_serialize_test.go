package xmpdm

import (
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/go-xmp/xmp"
)

// Track mixes ,attr fields (trackName, frameRate, trackType) with an element field
// (markers). When markers is non-empty the node carries rdf:parseType="Resource",
// so the ,attr fields must serialize as child elements (no property attributes) and
// still round-trip.
func TestTrackMixedSerialization(t *testing.T) {
	d := xmp.NewDocument()
	m, err := MakeModel(d)
	if err != nil {
		t.Fatal(err)
	}
	m.Tracks = TrackArray{Track{Name: "audio", Markers: MarkerList{{Name: "m1"}}}}

	b, err := xmp.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	if strings.Contains(out, `xmpDM:trackName="`) {
		t.Errorf("trackName serialized as property attribute alongside parseType=Resource:\n%s", out)
	}
	if !strings.Contains(out, "<xmpDM:trackName>audio</xmpDM:trackName>") {
		t.Errorf("trackName not serialized as element:\n%s", out)
	}

	d2 := xmp.NewDocument()
	if err := xmp.Unmarshal(b, d2); err != nil {
		t.Fatal(err)
	}
	tr := FindModel(d2).Tracks
	if len(tr) != 1 || tr[0].Name != "audio" || len(tr[0].Markers) != 1 {
		t.Errorf("Track round-trip failed: %+v", tr)
	}
}
