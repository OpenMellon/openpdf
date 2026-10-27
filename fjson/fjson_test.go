package fjson_test

import (
	"strings"
	"testing"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/fjson"
)

func TestJSONFieldData(t *testing.T) {
	data, err := fjson.LoadFromJSON(strings.NewReader(`[{"name":"choice","value":"yes","options":["yes","no"]},{"name":"empty","value":""}]`))
	if err != nil {
		t.Fatal(err)
	}
	values, err := data.FieldValues()
	if err != nil {
		t.Fatal(err)
	}
	value, ok := values["choice"].(*core.PdfObjectString)
	if !ok || value.Decoded() != "yes" || len(values) != 1 {
		t.Fatalf("unexpected field values: %#v", values)
	}
	got, err := data.JSON()
	if err != nil {
		t.Fatal(err)
	}
	want := "[\n    {\n        \"name\": \"choice\",\n        \"value\": \"yes\",\n        \"options\": [\n            \"yes\",\n            \"no\"\n        ]\n    },\n    {\n        \"name\": \"empty\",\n        \"value\": \"\"\n    }\n]"
	if got != want {
		t.Fatalf("JSON() = %q, want %q", got, want)
	}
}
