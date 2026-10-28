package xmputil

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"

	"github.com/OpenMellon/go-xmp/models/dc"
	"github.com/OpenMellon/go-xmp/models/pdf"
	xmpbase "github.com/OpenMellon/go-xmp/models/xmp_base"
	xmpmm "github.com/OpenMellon/go-xmp/models/xmp_mm"
	"github.com/OpenMellon/go-xmp/xmp"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"github.com/OpenMellon/openpdf/internal/uuid"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaextension"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfaid"
	"github.com/OpenMellon/openpdf/model/xmputil/pdfuaid"
)

// Marshal the document into xml byte stream.
func (d *Document) Marshal() ([]byte, error) {
	if d.doc.IsDirty() {
		if err := d.doc.SyncModels(); err != nil {
			return nil, err
		}
	}
	return xmp.Marshal(d.doc)
}

// GetPdfInfo gets the document pdf info.
func (d *Document) GetPdfInfo() (*PdfInfo, bool) {
	info := PdfInfo{}
	var infoDict *core.PdfObjectDictionary
	setInfo := func(key string, val core.PdfObject) {
		if infoDict == nil {
			infoDict = core.MakeDict()
		}
		infoDict.Set(core.PdfObjectName(key), val)
	}
	pdfModel, hasPDF := d.doc.FindModel(pdf.NsPDF).(*pdf.PDFInfo)
	xmpBase, hasXMPBase := d.doc.FindModel(xmpbase.NsXmp).(*xmpbase.XmpBase)
	dcModel, hasDC := d.doc.FindModel(dc.NsDc).(*dc.DublinCore)
	if !hasPDF && !hasXMPBase && !hasDC {
		return nil, false
	}
	if hasDC {
		if title := altStringText(dcModel.Title); title != "" {
			setInfo("Title", core.MakeString(title))
		}
		if len(dcModel.Creator) > 0 {
			setInfo("Author", core.MakeString(dcModel.Creator[0]))
		}
		if description := altStringText(dcModel.Description); description != "" {
			setInfo("Subject", core.MakeString(description))
		}
	}
	if hasXMPBase {
		if xmpBase.CreatorTool != "" {
			setInfo("Creator", core.MakeString(string(xmpBase.CreatorTool)))
		}
		if !xmpBase.CreateDate.IsZero() {
			setInfo("CreationDate", core.MakeString(timeutils.FormatPdfTime(xmpBase.CreateDate.Value())))
		}
		if !xmpBase.ModifyDate.IsZero() {
			setInfo("ModDate", core.MakeString(timeutils.FormatPdfTime(xmpBase.ModifyDate.Value())))
		}
	}
	if hasPDF {
		info.Copyright = pdfModel.Copyright
		info.PdfVersion = pdfModel.PDFVersion
		info.Marked = bool(pdfModel.Marked)
		if pdfModel.Keywords != "" {
			setInfo("Keywords", core.MakeString(pdfModel.Keywords))
		}
		if pdfModel.Producer != "" {
			setInfo("Producer", core.MakeString(string(pdfModel.Producer)))
		}
		if pdfModel.Trapped {
			setInfo("Trapped", core.MakeName("True"))
		}
	}
	info.InfoDict = infoDict
	return &info, true
}

// Packet returns the XMP packet the document was loaded from, transcoded to UTF-8.
// It is nil for a document that was not loaded from a packet.
func (d *Document) Packet() []byte { return d.packet }

// MarshalIndent the document into xml byte stream with predefined prefix and indent.
func (d *Document) MarshalIndent(prefix, indent string) ([]byte, error) {
	if d.doc.IsDirty() {
		if err := d.doc.SyncModels(); err != nil {
			return nil, err
		}
	}
	return xmp.MarshalIndent(d.doc, prefix, indent)
}

// SetPdfInfo sets the pdf info into selected document.
func (d *Document) SetPdfInfo(options *PdfInfoOptions) error {
	if options == nil {
		return errors.New("nil pdf options provided")
	}
	model, err := pdf.MakeModel(d.doc)
	if err != nil {
		return err
	}
	if options.Overwrite {
		*model = pdf.PDFInfo{}
	}
	if options.InfoDict != nil {
		infoDict, ok := core.GetDict(options.InfoDict)
		if !ok {
			return fmt.Errorf("invalid pdf object type: %T", options.InfoDict)
		}
		var str *core.PdfObjectString
		for _, key := range infoDict.Keys() {
			switch key {
			case "Title":
				str, ok = core.GetString(infoDict.Get("Title"))
				if ok {
					model.Title = xmp.NewAltString(str)
				}
			case "Author":
				str, ok = core.GetString(infoDict.Get("Author"))
				if ok {
					model.Author = xmp.NewStringList(str.String())
				}
			case "Keywords":
				str, ok = core.GetString(infoDict.Get("Keywords"))
				if ok {
					model.Keywords = str.String()
				}
			case "Creator":
				str, ok = core.GetString(infoDict.Get("Creator"))
				if ok {
					model.Creator = xmp.AgentName(str.String())
				}
			case "Subject":
				str, ok = core.GetString(infoDict.Get("Subject"))
				if ok {
					model.Subject = xmp.NewAltString(str.String())
				}
			case "Producer":
				str, ok = core.GetString(infoDict.Get("Producer"))
				if ok {
					model.Producer = xmp.AgentName(str.String())
				}
			case "Trapped":
				name, nameOK := core.GetName(infoDict.Get("Trapped"))
				if nameOK {
					switch name.String() {
					case "True":
						model.Trapped = true
					case "False":
						model.Trapped = false
					default:
						model.Trapped = true
					}
				}
			case "CreationDate":
				if dateStr, dateOK := core.GetString(infoDict.Get("CreationDate")); dateOK && dateStr.String() != "" {
					date, parseErr := timeutils.ParsePdfTime(dateStr.String())
					if parseErr != nil {
						return fmt.Errorf("invalid CreationDate field: %w", parseErr)
					}
					model.CreationDate = xmp.NewDate(date)
				}
			case "ModDate":
				if modStr, modOK := core.GetString(infoDict.Get("ModDate")); modOK && modStr.String() != "" {
					modDate, modErr := timeutils.ParsePdfTime(modStr.String())
					if modErr != nil {
						return fmt.Errorf("invalid ModDate field: %w", modErr)
					}
					model.ModifyDate = xmp.NewDate(modDate)
				}
			}
		}
	}
	if options.PdfVersion != "" {
		model.PDFVersion = options.PdfVersion
	}
	if options.Marked {
		model.Marked = xmp.Bool(options.Marked)
	}
	if options.Copyright != "" {
		model.Copyright = options.Copyright
	}
	if err = model.SyncToXMP(d.doc); err != nil {
		return err
	}
	return nil
}

// GetPdfAID gets the pdfaid xmp metadata model.
func (d *Document) GetPdfAID() (*PdfAID, bool) {
	model, ok := d.doc.FindModel(pdfaid.Namespace).(*pdfaid.Model)
	if !ok {
		return nil, false
	}
	return &PdfAID{Part: model.Part, Conformance: model.Conformance, Rev: model.Rev}, true
}

// GetMediaManagement gets the media management metadata from provided xmp document.
func (d *Document) GetMediaManagement() (*MediaManagement, bool) {
	mm := xmpmm.FindModel(d.doc)
	if mm == nil {
		return nil, false
	}
	versions := make([]MediaManagementVersion, len(mm.Versions))
	for i, v := range mm.Versions {
		versions[i] = MediaManagementVersion{VersionID: v.Version, ModifyDate: v.ModifyDate.Value(), Comments: v.Comments, Modifier: v.Modifier}
	}
	result := &MediaManagement{OriginalDocumentID: GUID(mm.OriginalDocumentID.Value()), DocumentID: GUID(mm.DocumentID.Value()), InstanceID: GUID(mm.InstanceID.Value()), VersionID: mm.VersionID, Versions: versions}
	if mm.DerivedFrom != nil {
		result.DerivedFrom = &MediaManagementDerivedFrom{OriginalDocumentID: GUID(mm.DerivedFrom.OriginalDocumentID), DocumentID: GUID(mm.DerivedFrom.DocumentID), InstanceID: GUID(mm.DerivedFrom.InstanceID), VersionID: mm.DerivedFrom.VersionID}
	}
	return result, true
}

// SetPdfUAIDWithRev sets up pdfuaid xmp metadata with a revision year.
// In example: Part: '2' Rev: '2024' states for PDF/UA-2 revised in 2024.
func (d *Document) SetPdfUAIDWithRev(part, rev int) error {
	model, err := pdfuaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Part = part
	model.Rev = rev
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// Encoding returns the character encoding the XMP packet was stored in.
// A document that was not loaded from a packet reports EncodingUTF8.
func (d *Document) Encoding() string {
	if d.encoding == "" {
		return EncodingUTF8
	}
	return d.encoding
}

// GetPdfaExtensionSchemas gets a pdfa extension schemas.
func (d *Document) GetPdfaExtensionSchemas() ([]pdfaextension.Schema, error) {
	m := d.doc.FindModel(pdfaextension.Namespace)
	if m == nil {
		return nil, nil
	}
	model, ok := m.(*pdfaextension.Model)
	if !ok {
		return nil, fmt.Errorf("invalid model for pdfaExtensions: %T", m)
	}
	return model.Schemas, nil
}

// SetPdfAExtension sets the pdfaExtension XMP metadata.
func (d *Document) SetPdfAExtension() error {
	model, err := pdfaextension.MakeModel(d.doc)
	if err != nil {
		return err
	}
	if err = pdfaextension.FillModel(d.doc, model); err != nil {
		return err
	}
	if err = model.SyncToXMP(d.doc); err != nil {
		return err
	}
	return nil
}

// SetPdfUAIDCorrigendum sets the optional pdfuaid:corr corrigendum identifier
// (ISO 14289-1). An empty value is left unset on marshal. Combine with
// SetPdfUAID to set the part identifier.
func (d *Document) SetPdfUAIDCorrigendum(corr string) error {
	model, err := pdfuaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Corr = corr
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// GetPdfUAID gets the pdfuaid xmp metadata model.
func (d *Document) GetPdfUAID() (*PdfUAID, bool) {
	model, ok := d.doc.FindModel(pdfuaid.Namespace).(*pdfuaid.Model)
	if !ok {
		return nil, false
	}
	return &PdfUAID{Part: model.Part, Amd: model.Amd, Corr: model.Corr, Rev: model.Rev}, true
}

// LoadDocument loads up the xmp document from provided input stream.
//
// The packet may be encoded as UTF-8, UTF-16 or UTF-32, as the XMP
// specification allows; it is transcoded to UTF-8 before decoding.
//
// The decoder runs in lenient mode: a single malformed or unknown property
// (e.g. a loosely formatted date or an extension field with no matching model)
// is skipped rather than failing the whole packet, so well-formed properties
// such as pdfuaid:part and dc:title remain readable. Genuinely malformed XML
// and I/O errors still fail.
func LoadDocument(stream []byte) (*Document, error) {
	stream, enc := decodePacket(stream)
	doc := xmp.NewDocument()
	dec := xmp.NewDecoder(bytes.NewReader(stream))
	dec.SetStrict(false)
	if err := dec.Decode(doc); err != nil {
		return nil, err
	}
	return &Document{doc: doc, encoding: enc, packet: stream}, nil
}

// SetPdfUAID sets up pdfuaid xmp metadata.
// In example: Part: '1' states for PDF/UA-1.
func (d *Document) SetPdfUAID(part int) error {
	model, err := pdfuaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Part = part
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// SetPdfUAIDAmendment sets the optional pdfuaid:amd amendment identifier
// (ISO 14289-1). An empty value is left unset on marshal. Combine with
// SetPdfUAID to set the part identifier.
func (d *Document) SetPdfUAIDAmendment(amd string) error {
	model, err := pdfuaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Amd = amd
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// SetMediaManagement sets up XMP media management metadata: namespace xmpMM.
func (d *Document) SetMediaManagement(options *MediaManagementOptions) error {
	model, err := xmpmm.MakeModel(d.doc)
	if err != nil {
		return err
	}
	if options == nil {
		options = new(MediaManagementOptions)
	}
	derived := xmpmm.ResourceRef{}
	switch {
	case options.DocumentID != "":
		model.DocumentID = xmp.GUID(options.DocumentID)
	case options.NewDocumentID || model.DocumentID.IsZero():
		if !model.DocumentID.IsZero() {
			derived.DocumentID = model.DocumentID
		}
		id, idErr := uuid.NewUUID()
		if idErr != nil {
			return idErr
		}
		model.DocumentID = xmp.GUID(id.String())
	}
	if !model.InstanceID.IsZero() {
		derived.InstanceID = model.InstanceID
	}
	model.InstanceID = xmp.GUID(options.InstanceID)
	if model.InstanceID == "" {
		instanceID, instErr := uuid.NewUUID()
		if instErr != nil {
			return instErr
		}
		model.InstanceID = xmp.GUID(instanceID.String())
	}
	if !derived.IsZero() {
		model.DerivedFrom = &derived
	}
	versionID := options.VersionID
	if model.VersionID != "" {
		prev, convErr := strconv.Atoi(model.VersionID)
		if convErr != nil {
			versionID = strconv.Itoa(len(model.Versions) + 1)
		} else {
			versionID = strconv.Itoa(prev + 1)
		}
	}
	if versionID == "" {
		versionID = "1"
	}
	model.VersionID = versionID
	if err = model.SyncToXMP(d.doc); err != nil {
		return err
	}
	return nil
}

// Document is an implementation of the xmp document.
// It is a wrapper over go-xmp/xmp.Document that provides some Pdf predefined functionality.
type Document struct {
	doc      *xmp.Document
	encoding string
	packet   []byte
}

// GetGoXmpDocument gets direct access to the go-xmp.Document.
// All changes done to specified document would result in change of this document 'd'.
func (d *Document) GetGoXmpDocument() *xmp.Document { return d.doc }

// SetPdfAID sets up pdfaid xmp metadata.
// In example: Part: '1' Conformance: 'B' states for PDF/A 1B.
func (d *Document) SetPdfAID(part int, conformance string) error {
	model, err := pdfaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Part = part
	model.Conformance = conformance
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// SetPdfAIDWithRev sets up pdfaid xmp metadata.
// In example: Part: '4' Conformance: 'F' Rev '2025' states for PDF/A 4F revised in 2025.
func (d *Document) SetPdfAIDWithRev(part int, conformance string, rev int) error {
	model, err := pdfaid.MakeModel(d.doc)
	if err != nil {
		return err
	}
	model.Part = part
	model.Conformance = conformance
	model.Rev = rev
	if syncErr := model.SyncToXMP(d.doc); syncErr != nil {
		return syncErr
	}
	return nil
}

// NewDocument creates a new document without any previous xmp information.
func NewDocument() *Document { doc := xmp.NewDocument(); return &Document{doc: doc} }
