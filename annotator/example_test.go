package annotator_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/OpenMellon/openpdf/annotator"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type values map[string]core.PdfObject

func (v values) FieldValues() (map[string]core.PdfObject, error) { return v, nil }

// Create a form with a text field, fill it in, then flatten it so the value
// becomes plain page content.
func Example_forms() {
	dir, _ := os.MkdirTemp("", "openpdf")
	defer os.RemoveAll(dir)
	blank, filled := filepath.Join(dir, "form.pdf"), filepath.Join(dir, "filled.pdf")

	// Build a one-page form with a single text field.
	page := model.NewPdfPage()
	field, err := annotator.NewTextField(page, "name", []float64{50, 700, 300, 730}, annotator.TextFieldOptions{})
	if err != nil {
		log.Fatal(err)
	}
	form := model.NewPdfAcroForm()
	form.Fields = &[]*model.PdfField{field.PdfField}
	w := model.NewPdfWriter()
	if err := w.AddPage(page); err != nil {
		log.Fatal(err)
	}
	if err := w.SetForms(form); err != nil {
		log.Fatal(err)
	}
	if err := w.WriteToFile(blank); err != nil {
		log.Fatal(err)
	}

	// Fill it in.
	reader, f, err := model.NewPdfReaderFromFile(blank, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	appearance := annotator.FieldAppearance{OnlyIfMissing: true}
	err = reader.AcroForm.FillWithAppearance(values{"name": core.MakeString("Ada Lovelace")}, appearance)
	if err != nil {
		log.Fatal(err)
	}
	for _, fv := range reader.AcroForm.AllFieldValues() {
		fmt.Printf("%s = %s\n", fv.Name, fv.Value.(*core.PdfObjectString).Decoded())
	}

	// Flatten: the field disappears and its appearance is drawn onto the page.
	if err := reader.FlattenFieldsWithOpts(appearance, nil); err != nil {
		log.Fatal(err)
	}
	w2 := model.NewPdfWriter()
	page1, err := reader.GetPage(1)
	if err != nil {
		log.Fatal(err)
	}
	if err := w2.AddPage(page1); err != nil {
		log.Fatal(err)
	}
	if err := w2.WriteToFile(filled); err != nil {
		log.Fatal(err)
	}
	fmt.Println("flattened")
	// Output:
	// name = Ada Lovelace
	// flattened
}
