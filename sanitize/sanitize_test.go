package sanitize

import (
	"testing"

	"github.com/OpenMellon/openpdf/core"
)

func TestActionClassificationAndJavaScriptClearing(t *testing.T) {
	action := core.MakeDict()
	action.Set("S", core.MakeName("JavaScript"))
	action.Set("JS", core.MakeString("alert(1)"))
	if kind, ok := actionType(action); !ok || kind != "JavaScript" {
		t.Fatalf("action type = %q, %v", kind, ok)
	}
	clearJavaScript(action)
	value, ok := core.GetString(action.Get("JS"))
	if !ok || value.Str() != "" {
		t.Fatalf("JavaScript after clearing = %v", action.Get("JS"))
	}
}

func TestNullIsNotPresent(t *testing.T) {
	if isPresentNonNull(nil) || isPresentNonNull(core.MakeNull()) {
		t.Fatal("nil or null should not count as present")
	}
	if !isPresentNonNull(core.MakeString("")) {
		t.Fatal("empty string is present")
	}
}

func TestRenditionActionRequiresJavaScriptPayload(t *testing.T) {
	action := core.MakeDict()
	action.Set("S", core.MakeName("Rendition"))
	if _, ok := suspiciousActionType(action); ok {
		t.Fatal("rendition without JavaScript should not be classified as suspicious")
	}
	action.Set("JS", core.MakeString("script"))
	if kind, ok := suspiciousActionType(action); !ok || kind != "Rendition" {
		t.Fatalf("action = %q, %v", kind, ok)
	}
}

func TestRemoveCollectedObjectsPreservesNameArrayPositions(t *testing.T) {
	removed := core.MakeString("payload")
	kept := core.MakeString("safe")
	nameArray := core.MakeArray(core.MakeString("key"), removed)
	ordinary := core.MakeArray(removed, kept)
	root := core.MakeDict()
	root.Set("Names", nameArray)
	root.Set("Other", ordinary)
	removeCollectedObjects(root, map[core.PdfObject]struct{}{removed: {}}, map[*core.PdfObjectArray]struct{}{nameArray: {}}, map[core.PdfObject]struct{}{})
	if nameArray.Len() != 2 {
		t.Fatalf("name array length = %d", nameArray.Len())
	}
	if _, ok := nameArray.Elements()[1].(*core.PdfObjectNull); !ok {
		t.Fatalf("name array value = %T", nameArray.Elements()[1])
	}
	if ordinary.Len() != 1 || ordinary.Elements()[0] != kept {
		t.Fatalf("ordinary array = %v", ordinary.Elements())
	}
}

func TestProcessObjectsRemovesCatalogOpenAction(t *testing.T) {
	catalog := core.MakeDict()
	catalog.Set("Type", core.MakeName("Catalog"))
	action := core.MakeDict()
	action.Set("S", core.MakeName("JavaScript"))
	action.Set("JS", core.MakeString("alert(1)"))
	catalog.Set("OpenAction", action)

	objects := []core.PdfObject{core.MakeIndirectObject(catalog)}
	kept, err := New(SanitizationOpts{OpenAction: true}).Optimize(objects)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 1 {
		t.Fatalf("kept object count = %d, want 1", len(kept))
	}
	if catalog.Get("OpenAction") != nil {
		t.Fatal("catalog still contains OpenAction")
	}
}
