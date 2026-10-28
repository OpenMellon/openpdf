package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// PdfAppender appends new PDF content to an existing PDF document via incremental updates.
type PdfAppender struct {
	readSeeker     io.ReadSeeker
	parser         *core.PdfParser
	roReader       *PdfReader
	Reader         *PdfReader
	pages          []*PdfPage
	acroForm       *PdfAcroForm
	dss            *DSS
	perms          *Permissions
	xrefs          core.XrefTable
	xrefOffset     int64
	greatestObjNum int
	newObjects     []core.PdfObject
	hasNewObject   map[core.PdfObject]struct{}
	objNumMap      map[core.PdfObject]int64
	skipObjects    map[core.PdfObject]struct{}
	traversed      map[core.PdfObject]struct{}
	originalSize   int64
	written        bool
	password       string
	encryptOptions *EncryptOptions
	docInfo        *PdfInfo
}

func (a *PdfAppender) hasPriorSignature() bool {
	if a.roReader.AcroForm == nil {
		return false
	}
	for _, signatureField := range a.roReader.AcroForm.signatureFields() {
		if signatureField.V != nil {
			return true
		}
	}
	return false
}

// SetDSS sets the DSS dictionary (ETSI TS 102 778-4 V1.1.1) of the current
// document revision.
func (a *PdfAppender) SetDSS(dss *DSS) {
	if dss != nil {
		a.updateObjectsDeep(dss.ToPdfObject(), nil)
	}
	a.dss = dss
}

// RemovePage removes a page by number.
func (a *PdfAppender) RemovePage(pageNum int) {
	n := pageNum - 1
	a.pages = append(a.pages[0:n], a.pages[pageNum:]...)
}

func (a *PdfAppender) addNewObject(obj core.PdfObject) {
	if _, ok := a.hasNewObject[obj]; !ok {
		a.newObjects = append(a.newObjects, obj)
		a.hasNewObject[obj] = struct{}{}
	}
}

// SetDocInfo sets the document /Info metadata.
// This will overwrite any globally declared document info.
func (a *PdfAppender) SetDocInfo(info *PdfInfo) { a.docInfo = info }

func (a *PdfAppender) replaceObject(obj, obj2 core.PdfObject) {
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		a.objNumMap[obj2] = v.ObjectNumber
	case *core.PdfObjectStream:
		a.objNumMap[obj2] = v.ObjectNumber
	}
}

// Sign signs a specific page with a digital signature.
// The signature field parameter must have a valid signature dictionary
// specified by its V field.
func (a *PdfAppender) Sign(pageNum int, field *PdfFieldSignature) error {
	if field == nil {
		return errors.New("signature field cannot be nil")
	}
	sig := field.V
	if sig == nil {
		return errors.New("signature dictionary cannot be nil")
	}
	n := pageNum - 1
	if n < 0 || n > len(a.pages)-1 {
		return fmt.Errorf("page %d not found", pageNum)
	}
	page := a.Reader.PageList[n]
	field.P = page.ToPdfObject()
	if field.T == nil || field.T.String() == "" {
		field.T = core.MakeString(fmt.Sprintf("Signature %d", pageNum))
	}
	page.AddAnnotation(field.PdfAnnotation)
	if a.acroForm == a.roReader.AcroForm {
		a.acroForm = a.Reader.AcroForm
	}
	acroForm := a.acroForm
	if acroForm == nil {
		acroForm = NewPdfAcroForm()
	}
	acroForm.SigFlags = core.MakeInteger(3)
	if acroForm.NeedAppearances != nil {
		acroForm.NeedAppearances = nil
	}
	fields := append(acroForm.AllFields(), field.PdfField)
	acroForm.Fields = &fields
	a.ReplaceAcroForm(acroForm)
	a.UpdatePage(page)
	a.pages[n] = page
	if _, ok := field.V.GetDocMDPPermission(); ok {
		a.perms = NewPermissions(field.V)
	}
	return nil
}

// NewPdfAppender creates a new Pdf appender from a Pdf reader. The appender
// performs incremental writes over the original bytes and therefore requires
// a seekable source — the reader must have been constructed from a source
// that implements both io.ReadSeeker and io.ReaderAt (e.g. *os.File,
// *bytes.Reader).
func NewPdfAppender(reader *PdfReader) (*PdfAppender, error) {
	if reader.readSeeker == nil {
		return nil, errors.New("appender requires the reader's source to also implement io.ReadSeeker (e.g. *os.File, *bytes.Reader)")
	}
	r, ok := reader.readSeeker.(io.ReaderAt)
	if !ok {
		return nil, errors.New("appender requires a source that implements both io.ReadSeeker and io.ReaderAt")
	}
	appender := &PdfAppender{readSeeker: reader.readSeeker, Reader: reader, parser: reader.parser, traversed: reader.traversed}
	seek, err := appender.readSeeker.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	appender.originalSize = seek
	if _, err = appender.readSeeker.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	appender.roReader, err = newAppenderSnapshotReader(r, seek, inheritReaderOpts(reader, nil), false)
	if err != nil {
		return nil, err
	}
	for _, objectNum := range appender.Reader.GetObjectNums() {
		if appender.greatestObjNum < objectNum {
			appender.greatestObjNum = objectNum
		}
	}
	appender.xrefs = appender.parser.GetXrefTable()
	appender.xrefOffset = appender.parser.GetXrefOffset()
	appender.pages = append(appender.pages, appender.roReader.PageList...)
	appender.hasNewObject = make(map[core.PdfObject]struct{})
	appender.objNumMap = make(map[core.PdfObject]int64)
	appender.skipObjects = make(map[core.PdfObject]struct{})
	appender.acroForm = appender.roReader.AcroForm
	appender.dss = appender.roReader.DSS
	return appender, nil
}

// ReplaceAcroForm replaces the acrobat form. It appends a new form to the Pdf which
// replaces the original AcroForm.
func (a *PdfAppender) ReplaceAcroForm(acroForm *PdfAcroForm) {
	if acroForm != nil {
		a.updateObjectsDeep(acroForm.ToPdfObject(), nil)
	}
	a.acroForm = acroForm
}

// GetDSS gets the DSS dictionary (ETSI TS 102 778-4 V1.1.1) of the current
// document revision.
func (a *PdfAppender) GetDSS() (dss *DSS) { return a.dss }

// SignWithAppearance signs a specific page with a digital signature, like
// Sign, and generates the widget appearance of `field` using `appGen`.
// Set the signature dictionary entries displayed by the generated appearance
// (e.g. Name, M, Reason, Location) before calling. An appearance the caller
// already attached to the field widget is honored according to the `appGen`
// implementation (the default annotator.FieldAppearance leaves it untouched).
// If `appGen` is nil, it behaves exactly like Sign.
func (a *PdfAppender) SignWithAppearance(pageNum int, field *PdfFieldSignature, appGen FieldAppearanceGenerator) error {
	if field == nil {
		return errors.New("signature field cannot be nil")
	}
	if field.V == nil {
		return errors.New("signature dictionary cannot be nil")
	}
	if pageNum < 1 || pageNum > len(a.pages) {
		return fmt.Errorf("page %d not found", pageNum)
	}
	if appGen != nil {
		if field.PdfAnnotationWidget == nil {
			return errors.New("signature field has no widget annotation")
		}
		if a.acroForm == a.roReader.AcroForm {
			a.acroForm = a.Reader.AcroForm
		}
		acroForm := a.acroForm
		if acroForm == nil {
			acroForm = NewPdfAcroForm()
			a.acroForm = acroForm
		}
		generateAppearanceDict, err := appGen.GenerateAppearanceDict(acroForm, field.PdfField, field.PdfAnnotationWidget)
		if err != nil {
			return err
		}
		if generateAppearanceDict != nil {
			field.PdfAnnotationWidget.AP = generateAppearanceDict
		}
	}
	return a.Sign(pageNum, field)
}

// AddPages adds pages to be appended to the end of the source PDF.
func (a *PdfAppender) AddPages(pages ...*PdfPage) {
	for _, page := range pages {
		page = page.Duplicate()
		page.ToPdfObject()
		a.pages = append(a.pages, page)
	}
}

func (a *PdfAppender) mergeResources(obj, obj2 core.PdfObject, nameByName map[core.PdfObjectName]core.PdfObjectName) core.PdfObject {
	if obj2 == nil && obj == nil {
		return nil
	}
	if obj2 == nil {
		return obj
	}
	dict, ok := core.GetDict(obj2)
	if !ok {
		return obj
	}
	if obj == nil {
		dict2 := core.MakeDict()
		dict2.Merge(dict)
		return obj2
	}
	dict2, ok := core.GetDict(obj)
	if !ok {
		common.Log.Error("Error resource is not a dictionary")
		dict2 = core.MakeDict()
	}
	for _, name := range dict.Keys() {
		if name2, ok := nameByName[name]; ok {
			dict2.Set(name2, dict.Get(name))
		} else {
			dict2.Set(name, dict.Get(name))
		}
	}
	return dict2
}

// MergePageWith appends page content to source Pdf file page content.
func (a *PdfAppender) MergePageWith(pageNum int, page *PdfPage) error {
	n := pageNum - 1
	var page2 *PdfPage
	for i, page3 := range a.pages {
		if i == n {
			page2 = page3
		}
	}
	if page2 == nil {
		return fmt.Errorf("ERROR: Page dictionary %d not found in the source document", pageNum)
	}
	if page2.primitive != nil && page2.primitive.GetParser() == a.roReader.parser {
		page2 = page2.Duplicate()
		a.pages[n] = page2
	}
	page = page.Duplicate()
	page.ToPdfObject()
	pageResourceObjects := collectPageResourceObjects(page2)
	objByName := collectPageResourceObjects(page)
	nameByName := make(map[core.PdfObjectName]core.PdfObjectName)
	for name := range objByName {
		if _, ok := pageResourceObjects[name]; ok {
			for i := 1; true; i++ {
				name2 := core.PdfObjectName(string(name) + strconv.Itoa(i))
				if _, ok := pageResourceObjects[name2]; !ok {
					nameByName[name] = name2
					break
				}
			}
		}
	}
	contentStreams, err := page.GetContentStreams()
	if err != nil {
		return err
	}
	strs, err := page2.GetContentStreams()
	if err != nil {
		return err
	}
	for i, contentStream := range contentStreams {
		for name, name2 := range nameByName {
			contentStream = strings.ReplaceAll(contentStream, "/"+string(name), "/"+string(name2))
		}
		contentStreams[i] = contentStream
	}
	strs = append(strs, contentStreams...)
	if err := page2.SetContentStreams(strs, core.NewFlateEncoder()); err != nil {
		return err
	}
	page2.annotations = append(page2.annotations, page.annotations...)
	if page2.Resources == nil {
		page2.Resources = NewPdfPageResources()
	}
	if page.Resources != nil {
		page2.Resources.Font = a.mergeResources(page2.Resources.Font, page.Resources.Font, nameByName)
		page2.Resources.XObject = a.mergeResources(page2.Resources.XObject, page.Resources.XObject, nameByName)
		page2.Resources.Properties = a.mergeResources(page2.Resources.Properties, page.Resources.Properties, nameByName)
		if page2.Resources.ProcSet == nil {
			page2.Resources.ProcSet = page.Resources.ProcSet
		}
		page2.Resources.Shading = a.mergeResources(page2.Resources.Shading, page.Resources.Shading, nameByName)
		page2.Resources.ExtGState = a.mergeResources(page2.Resources.ExtGState, page.Resources.ExtGState, nameByName)
	}
	mediaBox, err := page2.GetMediaBox()
	if err != nil {
		return err
	}
	rect, err := page.GetMediaBox()
	if err != nil {
		return err
	}
	var ok bool
	if mediaBox.Llx > rect.Llx {
		mediaBox.Llx = rect.Llx
		ok = true
	}
	if mediaBox.Lly > rect.Lly {
		mediaBox.Lly = rect.Lly
		ok = true
	}
	if mediaBox.Urx < rect.Urx {
		mediaBox.Urx = rect.Urx
		ok = true
	}
	if mediaBox.Ury < rect.Ury {
		mediaBox.Ury = rect.Ury
		ok = true
	}
	if ok {
		page2.MediaBox = mediaBox
	}
	return nil
}

// ReplacePage replaces the original page to a new page.
func (a *PdfAppender) ReplacePage(pageNum int, page *PdfPage) {
	n := pageNum - 1
	for i := range a.pages {
		if i == n {
			duplicate := page.Duplicate()
			duplicate.ToPdfObject()
			a.pages[i] = duplicate
		}
	}
}

// NewPdfAppenderWithOpts creates a new Pdf appender from a Pdf reader with
// options. See NewPdfAppender for the seekable-source requirement.
func NewPdfAppenderWithOpts(reader *PdfReader, opts *ReaderOpts, encryptOptions *EncryptOptions) (*PdfAppender, error) {
	if reader.readSeeker == nil {
		return nil, errors.New("appender requires the reader's source to also implement io.ReadSeeker (e.g. *os.File, *bytes.Reader)")
	}
	r, ok := reader.readSeeker.(io.ReaderAt)
	if !ok {
		return nil, errors.New("appender requires a source that implements both io.ReadSeeker and io.ReaderAt")
	}
	appender := &PdfAppender{readSeeker: reader.readSeeker, Reader: reader, parser: reader.parser, traversed: reader.traversed}
	seek, err := appender.readSeeker.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	appender.originalSize = seek
	if _, err = appender.readSeeker.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	appender.roReader, err = newAppenderSnapshotReader(r, seek, inheritReaderOpts(reader, opts), true)
	if err != nil {
		return nil, err
	}
	for _, objectNum := range appender.Reader.GetObjectNums() {
		if appender.greatestObjNum < objectNum {
			appender.greatestObjNum = objectNum
		}
	}
	appender.xrefs = appender.parser.GetXrefTable()
	appender.xrefOffset = appender.parser.GetXrefOffset()
	appender.pages = append(appender.pages, appender.roReader.PageList...)
	appender.hasNewObject = make(map[core.PdfObject]struct{})
	appender.objNumMap = make(map[core.PdfObject]int64)
	appender.skipObjects = make(map[core.PdfObject]struct{})
	appender.acroForm = appender.roReader.AcroForm
	appender.dss = appender.roReader.DSS
	if opts != nil {
		appender.password = opts.Password
	}
	if encryptOptions != nil {
		appender.encryptOptions = encryptOptions
	}
	return appender, nil
}

// WriteToFile writes the Appender output to file specified by path.
func (a *PdfAppender) WriteToFile(outputPath string) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()
	return a.Write(file)
}
