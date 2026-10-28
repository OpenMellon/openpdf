package model

import (
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
)

// Permissions specify a permissions dictionary (PDF 1.5).
// (Section 12.8.4, Table 258 - Entries in a permissions dictionary p. 477 in PDF32000_2008).
type Permissions struct {
	DocMDP    *PdfSignature
	primitive *core.PdfObjectDictionary
}

// GetContainingPdfObject implements interface PdfModel.
func (p *Permissions) GetContainingPdfObject() core.PdfObject { return p.primitive }

// NewPermissions returns a new permissions object.
func NewPermissions(docMdp *PdfSignature) *Permissions {
	perms := Permissions{}
	perms.DocMDP = docMdp
	dict := core.MakeDict()
	dict.Set("DocMDP", docMdp.ToPdfObject())
	perms.primitive = dict
	return &perms
}

// ToPdfObject implements interface PdfModel.
func (p *Permissions) ToPdfObject() core.PdfObject { return p.primitive }

type preserializedObject struct {
	objNum int64
	genNum int64
	obj    core.PdfObject
	data   []byte
	offset int64
}

type xrefStreamPlan struct {
	objNum  int64
	entries []xrefEntry
	size    int64
	prev    int64
	hasPrev bool
	root    *core.PdfIndirectObject
	info    *core.PdfIndirectObject
	encrypt *core.PdfIndirectObject
	id      *core.PdfObjectArray
}

type xrefEntry struct {
	objNum     int64
	entryType  byte
	offset     uint32
	genOrIndex uint16
}

type crossReference struct {
	Type int

	// Type 1
	Offset     int64
	Generation int64

	// Type 2
	ObjectNumber int
	Index        int
}

// EncryptionAlgorithm is used in EncryptOptions to change the default algorithm used to encrypt the document.
type EncryptionAlgorithm int

// EncryptOptions represents encryption options for an output PDF.
type EncryptOptions struct {
	// Permissions granted when the document is opened with the user password
	// (Encrypt) or to every recipient (EncryptForRecipients). The zero value
	// grants none; security.PermOwner grants all, as does a nil
	// EncryptOptions. Encrypt sets the reserved bits of /P itself.
	// EncryptForRecipients writes only bits 3-6 and 9-12, always sets bit 10
	// as PDF 2.0 requires, and makes recipients granted every permission
	// owner-equivalent (able to change the security settings).
	Permissions security.Permissions

	// Algorithm selects the cipher. The zero value is RC4_128bit, which
	// Encrypt uses as its default; EncryptForRecipients does not produce RC4
	// output and treats the zero value as AES_256bit.
	Algorithm EncryptionAlgorithm
}

type lazyStreamWriter struct{ writer *PdfWriter }

func (w lazyStreamWriter) Write(p []byte) (int, error) {
	w.writer.writeBytes(p)
	if w.writer.writeErr != nil {
		return 0, w.writer.writeErr
	}
	return len(p), nil
}

type linearizationLayout struct {
	firstSection    []int64
	mainSection     []int64
	sharedObjs      []int64
	orderedObjs     []core.PdfObject
	firstShared     []int64
	pageObjs        [][]int64
	pageShared      [][]int64
	mainShared      []int64
	pageCount       int
	firstPageObjNum int64
	maxObjNum       int64
}

// LinearizationOptions configures linearized ("Fast Web View") output.
type LinearizationOptions struct {
	// UseObjectStreams toggles object-stream packaging for compressible
	// main-section objects, with xref STREAMS replacing the textual
	// xref tables. Eligible objects are indirect dictionaries that are
	// not pages, page-tree nodes, the catalog, an encryption dict, an
	// existing ObjectStreams / XRef container, or a signature dict.
	// The first-page section is kept uncompressed so page 1 can be
	// displayed without decoding any container; this matches what other
	// linearization-aware producers do.
	UseObjectStreams bool
}

type linearizedPlan struct {
	header             []byte
	linDict            []byte
	firstXref          []byte
	firstSection       []byte
	hintStream         []byte
	mainSection        []byte
	mainXref           []byte
	linDictOffset      int64
	firstXrefOffset    int64
	hintOffset         int64
	mainXrefOffset     int64
	mainXrefFirstEntry int64
	firstPageEnd       int64
	fileLength         int64
}

func (p *linearizedPlan) emit(w io.Writer) error {
	for _, data := range [][]byte{p.header, p.linDict, p.firstXref, p.hintStream, p.firstSection, p.mainSection, p.mainXref} {
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}

const linearizationDictObjNum2 = 1

func objectNumbers(obj core.PdfObject) (int64, int64) {
	switch v := obj.(type) {
	case *core.PdfIndirectObject:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStream:
		return v.ObjectNumber, v.GenerationNumber
	case *core.PdfObjectStreams:
		return v.ObjectNumber, v.GenerationNumber
	}
	return 0, 0
}

// Optimizer is the interface that performs optimization of PDF object structure for output writing.
//
// Optimize receives a slice of input `objects`, performs optimization, including removing, replacing objects and
// output the optimized slice of objects.
type Optimizer interface {
	Optimize(objs []core.PdfObject) ([]core.PdfObject, error)
}

type objectStreamsState struct {
	members         []*core.PdfIndirectObject
	packedObjNums   []int64
	streams         *core.PdfObjectStreams
	streamObjNum    int64
	mainXrefObjNum  int64
	firstXrefObjNum int64
}
