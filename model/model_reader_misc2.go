package model

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"os"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// GetEncryptionMethod returns a descriptive information string about the encryption method used.
func (r *PdfReader) GetEncryptionMethod() string {
	crypter := r.parser.GetCrypter()
	return crypter.String()
}

// FlattenFieldsWithOpts flattens the AcroForm fields of the reader using the
// provided field appearance generator and the specified options. If no options
// are specified, all form fields are flattened.
// If a filter function is provided using the opts parameter, only the filtered
// fields are flattened. Otherwise, all form fields are flattened.
// At the end of the process, the AcroForm contains all the fields which were
// not flattened. If all fields are flattened, the reader's AcroForm field
// is set to nil.
func (r *PdfReader) FlattenFieldsWithOpts(appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	return r.flattenFieldsWithOpts(false, appgen, opts)
}

// IsEncrypted returns true if the PDF file is encrypted.
func (r *PdfReader) IsEncrypted() (bool, error) { return r.parser.IsEncrypted() }

func (r *PdfReader) loadDSS() (*DSS, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	dss := r.catalog.Get("DSS")
	if dss == nil {
		return nil, nil
	}
	dssIndirect, _ := core.GetIndirect(dss)
	dss = core.TraceToDirectObject(dss)
	switch v := dss.(type) {
	case *core.PdfObjectNull:
		return nil, nil
	case *core.PdfObjectDictionary:
		return newDSSFromDict(dssIndirect, v)
	}
	return nil, fmt.Errorf("invalid DSS entry %T", dss)
}

func (r *PdfReader) flattenFieldsWithOpts(allannots bool, appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	if opts == nil {
		opts = &FieldFlattenOpts{}
	}
	var ok bool
	set := map[*PdfAnnotation]bool{}
	{
		var fields []*PdfField
		acroForm := r.AcroForm
		if acroForm != nil {
			if opts.FilterFunc != nil {
				fields = acroForm.filteredFields(opts.FilterFunc, true)
				ok = acroForm.Fields != nil && len(*acroForm.Fields) > 0
			} else {
				fields = acroForm.AllFields()
			}
		}
		for _, field := range fields {
			if len(field.Annotations) < 1 {
				common.Log.Debug("No annotation found for: %v, look into Kids Object", field.PartialName())
				for i, kid := range field.Kids {
					for _, annotation := range kid.Annotations {
						set[annotation.PdfAnnotation] = field.V != nil
						if kid.V == nil {
							kid.V = field.V
						}
						if kid.T == nil {
							kid.T = core.MakeString(fmt.Sprintf("%s#%d", field.PartialName(), i))
						}
						if appgen != nil {
							generateAppearanceDict, err := appgen.GenerateAppearanceDict(acroForm, kid, annotation)
							if err != nil {
								return err
							}
							annotation.AP = generateAppearanceDict
						}
					}
				}
			}
			for _, annotation := range field.Annotations {
				set[annotation.PdfAnnotation] = field.V != nil
				if appgen != nil {
					generateAppearanceDict, err := appgen.GenerateAppearanceDict(acroForm, field, annotation)
					if err != nil {
						return err
					}
					annotation.AP = generateAppearanceDict
				}
			}
		}
	}
	if allannots {
		for _, page := range r.PageList {
			annotations, err := page.GetAnnotations()
			if err != nil {
				return err
			}
			for _, annotation := range annotations {
				set[annotation] = true
			}
		}
	}
	for _, page := range r.PageList {
		err := page.flattenFieldsWithOpts(appgen, opts, set)
		if err != nil {
			return err
		}
	}
	if !ok {
		r.AcroForm = nil
	}
	return nil
}

// DecryptWithCertificate decrypts a document encrypted with a public-key
// security handler (/Filter /Adobe.PubSec) using a recipient's certificate and
// private key (an *rsa.PrivateKey), then loads the document structure. It
// returns security.ErrNotRecipient when the certificate is not among the
// recipients, security.ErrRecipientNotOpened when the recipient's entry does
// not open with the key, and core.ErrPasswordRequired when the document uses
// the standard security handler.
func (r *PdfReader) DecryptWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	ok, err := r.parser.DecryptWithCertificate(cert, key)
	if err != nil {
		return ok, err
	}
	if !ok {
		return false, nil
	}
	err = r.loadStructure()
	if err != nil {
		common.Log.Debug("ERROR: Fail to load structure (%s)", err)
		return false, err
	}
	return true, nil
}

// FlattenFields flattens the form fields and annotations for the PDF loaded in `pdf` and makes
// non-editable.
// Looks up all widget annotations corresponding to form fields and flattens them by drawing the content
// through the content stream rather than annotations.
// References to flattened annotations will be removed from Page Annots array. For fields the AcroForm entry
// will be emptied.
// When `allannots` is true, all annotations will be flattened. Keep false if want to keep non-form related
// annotations intact.
// When `appgen` is not nil, it will be used to generate appearance streams for the field annotations.
func (r *PdfReader) FlattenFields(allannots bool, appgen FieldAppearanceGenerator) error {
	return r.flattenFieldsWithOpts(allannots, appgen, nil)
}

// GetIndirectObjectByNumber retrieves and returns a specific PdfObject by object number.
func (r *PdfReader) GetIndirectObjectByNumber(number int) (core.PdfObject, error) {
	lookupByNumber, err := r.parser.LookupByNumber(number)
	return lookupByNumber, err
}

func (r *PdfReader) newPdfFieldSignatureFromDict(dict *core.PdfObjectDictionary) (*PdfFieldSignature, error) {
	signatureField := &PdfFieldSignature{}
	v, ok := core.GetIndirect(dict.Get("V"))
	if ok {
		var err error
		signatureField.V, err = r.newPdfSignatureFromIndirect(v)
		if err != nil {
			return nil, err
		}
	}
	signatureField.Lock, _ = core.GetIndirect(dict.Get("Lock"))
	signatureField.SV, _ = core.GetIndirect(dict.Get("SV"))
	return signatureField, nil
}

// FirstXrefTrailer returns the trailer dictionary of the first xref table
// (the one immediately after the linearization parameter dictionary) for a
// linearized PDF. Returns nil if the PDF is not linearized, or an error if
// the first xref cannot be located.
func (r *PdfReader) FirstXrefTrailer() (*core.PdfObjectDictionary, error) {
	return r.parser.FirstXrefTrailer()
}

// NewPdfReaderFromFile creates a new PdfReader from the specified PDF file.
// If ReaderOpts is nil it will be set to default value from NewReaderOpts.
func NewPdfReaderFromFile(pdfFile string, opts *ReaderOpts) (*PdfReader, *os.File, error) {
	open, err := os.Open(pdfFile)
	if err != nil {
		return nil, nil, err
	}
	stat, err := open.Stat()
	if err != nil {
		open.Close()
		return nil, nil, err
	}
	readerTracked, err := newPdfReaderTracked(open, stat.Size(), opts, true, false)
	if err != nil {
		open.Close()
		return nil, nil, err
	}
	readerTracked.fileName = pdfFile
	return readerTracked, open, nil
}

func (r *PdfReader) loadForms() (*PdfAcroForm, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return nil, fmt.Errorf("file need to be decrypted first")
	}
	catalog := r.catalog
	acroForm := catalog.Get("AcroForm")
	if acroForm == nil {
		return nil, nil
	}
	acroFormIndirect, ok := core.GetIndirect(acroForm)
	acroForm = core.TraceToDirectObject(acroForm)
	if core.IsNullObject(acroForm) {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Acroform is a null object (empty)\n")
		}
		return nil, nil
	}
	acroFormDict, isAcroFormDict := core.GetDict(acroForm)
	if !isAcroFormDict {
		common.Log.Debug("Invalid AcroForm entry %T", acroForm)
		common.Log.Debug("Does not have forms")
		if r.opts.RelaxedMode {
			common.Log.Debug("RelaxedMode enabled. Ignoring AcroForm loading errors")
			return nil, nil
		}
		return nil, fmt.Errorf("invalid acroform entry %T", acroForm)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Has Acro forms")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Traverse the Acroforms structure")
	}
	if !r.lazy {
		err := r.traverseObjectData(acroFormDict)
		if err != nil {
			common.Log.Debug("ERROR: Unable to traverse AcroForms (%s)", err)
			return nil, err
		}
	}
	form, err := r.newPdfAcroFormFromDict(acroFormIndirect, acroFormDict)
	if err != nil {
		return nil, err
	}
	form.direct = !ok
	return form, nil
}

func (r *PdfReader) newPdfFieldFromIndirectObject(indirect *core.PdfIndirectObject, field *PdfField) (*PdfField, error) {
	if field, ok := r.modelManager.GetModelFromPrimitive(indirect).(*PdfField); ok {
		return field, nil
	}
	indirectDict, ok := core.GetDict(indirect)
	if !ok {
		return nil, fmt.Errorf("PdfField indirect object not containing a dictionary")
	}
	field2 := NewPdfField()
	field2.primitive = indirect
	field2.primitive.PdfObject = indirectDict
	if ft, ok := core.GetName(indirectDict.Get("FT")); ok {
		field2.FT = ft
	}
	if field != nil {
		field2.Parent = field
	}
	field2.T, _ = indirectDict.Get("T").(*core.PdfObjectString)
	field2.TU, _ = indirectDict.Get("TU").(*core.PdfObjectString)
	field2.TM, _ = indirectDict.Get("TM").(*core.PdfObjectString)
	field2.Ff, _ = indirectDict.Get("Ff").(*core.PdfObjectInteger)
	field2.V = indirectDict.Get("V")
	field2.DV = indirectDict.Get("DV")
	field2.AA = indirectDict.Get("AA")
	if DA := indirectDict.Get("DA"); DA != nil {
		DA, _ := core.GetString(DA)
		field2.VariableText = &VariableText{DA: DA}
		Q, _ := indirectDict.Get("Q").(*core.PdfObjectInteger)
		DS, _ := indirectDict.Get("DS").(*core.PdfObjectString)
		RV := indirectDict.Get("RV")
		field2.VariableText.Q = Q
		field2.VariableText.DS = DS
		field2.VariableText.RV = RV
	}
	name := field2.FT
	if name == nil && field != nil {
		name = field.FT
	}
	if name != nil {
		switch *name {
		case "Tx":
			fieldText, err := newPdfFieldTextFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldText.PdfField = field2
			field2.context = fieldText
		case "Ch":
			fieldChoice, err := newPdfFieldChoiceFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldChoice.PdfField = field2
			field2.context = fieldChoice
		case "Btn":
			fieldButton, err := newPdfFieldButtonFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			fieldButton.PdfField = field2
			field2.context = fieldButton
		case "Sig":
			signatureField, err := r.newPdfFieldSignatureFromDict(indirectDict)
			if err != nil {
				return nil, err
			}
			signatureField.PdfField = field2
			field2.context = signatureField
		default:
			common.Log.Debug("ERROR: Unsupported field type %s", *field2.FT)
			return nil, errors.New("unsupported field type")
		}
	}
	if subtype, ok := core.GetName(indirectDict.Get("Subtype")); ok {
		if *subtype == "Widget" {
			annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
			if err != nil {
				return nil, err
			}
			widget, ok := annot.GetContext().(*PdfAnnotationWidget)
			if !ok {
				return nil, errors.New("invalid widget annotation")
			}
			widget.field = field2
			widget.Parent = field2.primitive
			field2.Annotations = append(field2.Annotations, widget)
			return field2, nil
		}
	}
	found := true
	if kids, isKids := core.GetArray(indirectDict.Get("Kids")); isKids {
		indirects := make([]*core.PdfIndirectObject, 0, kids.Len())
		for _, obj := range kids.Elements() {
			indirect, isIndirect := core.GetIndirect(obj)
			if !isIndirect {
				stream, ok := core.GetStream(obj)
				if ok && stream.PdfObjectDictionary != nil {
					s, ok := core.GetNameVal(stream.Get("Type"))
					if ok && s == "Metadata" {
						common.Log.Debug("ERROR: form field Kids array contains invalid Metadata stream. Skipping.")
						continue
					}
				}
				return nil, errors.New("not an indirect object (form field)")
			}
			indirectDict, isIndirectDict := core.GetDict(indirect)
			if !isIndirectDict {
				return nil, ErrTypeCheck
			}
			if found {
				found = !hasFieldKeys(indirectDict)
			}
			indirects = append(indirects, indirect)
		}
		for _, indirect := range indirects {
			if found {
				annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
				if err != nil {
					common.Log.Debug("Error loading widget annotation for field: %v", err)
					return nil, err
				}
				widget, ok := annot.context.(*PdfAnnotationWidget)
				if !ok {
					return nil, ErrTypeCheck
				}
				widget.field = field2
				field2.Annotations = append(field2.Annotations, widget)
			} else {
				field3, err := r.newPdfFieldFromIndirectObject(indirect, field2)
				if err != nil {
					common.Log.Debug("Error loading child field: %v", err)
					return nil, err
				}
				field2.Kids = append(field2.Kids, field3)
			}
		}
	}
	return field2, nil
}

// NewReaderForText makes a new PdfReader for an input PDF content string. For use in testing.
func NewReaderForText(txt string) *PdfReader {
	return &PdfReader{traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), parser: core.NewParserFromString(txt)}
}

// Decrypt decrypts the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful,
// false otherwise.
//
// For AES-256 (R=5/6) encryption the password bytes are interpreted as UTF-8
// and prepared with the SASLprep profile (RFC 4013), as required by
// ISO 32000-2. If the prepared password does not authenticate, the raw bytes
// are tried as well. Input that is not valid UTF-8 is used as given without
// preparation. For RC4 and AES-128 the password bytes are used as given.
func (r *PdfReader) Decrypt(password []byte) (bool, error) {
	ok, err := r.parser.Decrypt(password)
	if err != nil {
		return ok, err
	}
	if !ok {
		return false, nil
	}
	err = r.loadStructure()
	if err != nil {
		common.Log.Debug("ERROR: Fail to load structure (%s)", err)
		return false, err
	}
	return true, nil
}
