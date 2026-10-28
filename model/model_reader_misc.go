package model

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"io"
	"os"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/core/security"
)

// GetCatalogMetadata gets the catalog defined XMP Metadata.
func (r *PdfReader) GetCatalogMetadata() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	metadata := r.catalog.Get("Metadata")
	return metadata, metadata != nil
}

// GetCatalogStructTreeRoot gets the catalog StructTreeRoot object.
func (r *PdfReader) GetCatalogStructTreeRoot() (core.PdfObject, bool) {
	structTreeRoot := core.ResolveReference(r.catalog.Get("StructTreeRoot"))
	if structTreeRoot == nil {
		return nil, false
	}
	if !r.lazy {
		err := r.traverseObjectData(structTreeRoot)
		if err != nil {
			common.Log.Debug("ERROR: Failed to traverse StructTreeRoot object (%s)", err)
			return nil, false
		}
	}
	return structTreeRoot, true
}

func (r *PdfReader) loadAnnotations(annots core.PdfObject) ([]*PdfAnnotation, error) {
	annotsArr, ok := core.GetArray(annots)
	if !ok {
		return nil, fmt.Errorf("Annots not an array")
	}
	var annots2 []*PdfAnnotation
	for _, obj := range annotsArr.Elements() {
		obj = core.ResolveReference(obj)
		if _, ok := obj.(*core.PdfObjectNull); ok {
			continue
		}
		dict, ok := obj.(*core.PdfObjectDictionary)
		indirect, isIndirect := obj.(*core.PdfIndirectObject)
		if ok {
			indirect = &core.PdfIndirectObject{}
			indirect.PdfObject = dict
		} else {
			if !isIndirect {
				return nil, fmt.Errorf("annotation not in an indirect object")
			}
		}
		annot, err := r.newPdfAnnotationFromIndirectObject(indirect)
		if err != nil {
			return nil, err
		}
		switch v := annot.GetContext().(type) {
		case *PdfAnnotationWidget:
			for _, allField := range r.AcroForm.AllFields() {
				if allField.primitive == v.Parent {
					v.field = allField
					break
				}
			}
		}
		if annot != nil {
			annots2 = append(annots2, annot)
		}
	}
	return annots2, nil
}

// NewPdfReaderAt is the explicit-size form of NewPdfReader. Use this when
// the source's byte length cannot be auto-detected — e.g. a custom io.ReaderAt
// that doesn't implement Size/Seek/Stat.
func NewPdfReaderAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	return newPdfReaderTracked(r, size, &ReaderOpts{}, false, false)
}

// NewPdfReader returns a new PdfReader for the given io.ReaderAt source.
// The source's byte length is auto-detected via Size() / Seek() / Stat() —
// *os.File, *bytes.Reader, and *io.SectionReader all work without a manual
// size argument. For exotic io.ReaderAt sources that can't be auto-sized,
// use NewPdfReaderAt.
//
// Immediately loads and traverses the PDF structure including pages and page
// contents (if not encrypted). Loads entire document structure into memory.
// Alternatively, a lazy-loading reader can be created with NewPdfReaderLazy
// which loads references from disk on demand.
func NewPdfReader(r io.ReaderAt) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderAt(r, sizeOfReaderAt)
}

// GetCatalogMarkInfo gets catalog MarkInfo object.
func (r *PdfReader) GetCatalogMarkInfo() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	markInfo := r.catalog.Get("MarkInfo")
	return markInfo, markInfo != nil
}

// PdfReader represents a PDF file reader. It is a frontend to the lower level parsing mechanism and provides
// a higher level access to work with PDF structure and information, such as the page structure etc.
type PdfReader struct {
	parser         *core.PdfParser
	root           core.PdfObject
	pagesObj       *core.PdfIndirectObject
	pagesDict      *core.PdfObjectDictionary
	pageObjs       []*core.PdfIndirectObject
	PageList       []*PdfPage
	pageCount      int
	catalog        *core.PdfObjectDictionary
	outlineTree    *PdfOutlineTreeNode
	AcroForm       *PdfAcroForm
	DSS            *DSS
	Rotate         *int64
	perms          *Permissions
	revisionCache  map[*PdfReader]*PdfReader
	revisions      []*PdfReader
	modelManager   *modelManager
	lazy           bool
	skipOutlines   bool
	skipPerms      bool
	traversed      map[core.PdfObject]struct{}
	mu             sync.Mutex
	readSeeker     io.ReadSeeker
	complianceMode bool
	fileName       string
	opts           *ReaderOpts
	autoDecrypt    bool
}

// GetCatalogViewerPreferences gets catalog ViewerPreferences object.
func (r *PdfReader) GetCatalogViewerPreferences() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	viewerPreferences := r.catalog.Get("ViewerPreferences")
	return viewerPreferences, viewerPreferences != nil
}

// GetCatalogVersion gets the catalog /Version entry. Per ISO 32000-2 §7.7.2
// (Table 29) this entry supersedes the file header version only when it is
// later than the header; callers must apply that max rule themselves (this
// accessor just returns the raw entry).
func (r *PdfReader) GetCatalogVersion() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	version := r.catalog.Get("Version")
	return version, version != nil
}

func newPdfReaderCore(r io.ReaderAt, opts *ReaderOpts, autoDecrypt, ok bool) (*PdfReader, error) {
	if opts == nil {
		opts = NewReaderOpts()
	}
	s2 := ""
	if file, ok := r.(*os.File); ok {
		s2 = file.Name()
	}
	var rs io.ReadSeeker
	if rs2, ok := r.(io.ReadSeeker); ok {
		rs = rs2
	}
	opts2 := *opts
	reader := &PdfReader{readSeeker: rs, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: opts.LazyLoad, complianceMode: opts.ComplianceMode, skipOutlines: ok, skipPerms: ok, autoDecrypt: autoDecrypt, opts: &opts2, fileName: s2}
	return reader, nil
}

// AcroFormNeedsRepair returns true if the document contains widget annotations
// linked to fields which are not referenced in the AcroForm. The AcroForm can
// be repaired using the RepairAcroForm method of the reader.
func (r *PdfReader) AcroFormNeedsRepair() (bool, error) {
	var fields []*PdfField
	if r.AcroForm != nil {
		fields = r.AcroForm.AllFields()
	}
	set := make(map[*PdfField]struct{}, len(fields))
	for _, field := range fields {
		set[field] = struct{}{}
	}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return false, err
		}
		for _, annotation := range annotations {
			widget, ok := annotation.GetContext().(*PdfAnnotationWidget)
			if !ok {
				continue
			}
			field := widget.Field()
			if field == nil {
				return true, nil
			}
			if _, ok := set[field]; !ok {
				return true, nil
			}
		}
	}
	return false, nil
}

// GetCatalogLanguage gets catalog Language object.
func (r *PdfReader) GetCatalogLanguage() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	lang := r.catalog.Get("Lang")
	return lang, lang != nil
}

func (r *PdfReader) newPdfAcroFormFromDict(indirect *core.PdfIndirectObject, dict *core.PdfObjectDictionary) (*PdfAcroForm, error) {
	acroForm := NewPdfAcroForm()
	if indirect != nil {
		acroForm.primitive = indirect
		indirect.PdfObject = core.MakeDict()
	}
	if fields := dict.Get("Fields"); fields != nil && !core.IsNullObject(fields) {
		fieldsArr, ok := core.GetArray(fields)
		if !ok {
			return nil, fmt.Errorf("fields not an array (%T)", fields)
		}
		var fields []*PdfField
		for _, obj := range fieldsArr.Elements() {
			indirect, ok := core.GetIndirect(obj)
			if !ok {
				if _, ok := obj.(*core.PdfObjectNull); ok {
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("Skipping over null field")
					}
					continue
				}
				common.Log.Debug("Field not contained in indirect object %T", obj)
				return nil, fmt.Errorf("field not in an indirect object")
			}
			field, err := r.newPdfFieldFromIndirectObject(indirect, nil)
			if err != nil {
				return nil, err
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("AcroForm Field: %+v", *field)
			}
			fields = append(fields, field)
		}
		acroForm.Fields = &fields
	}
	if needAppearances := dict.Get("NeedAppearances"); needAppearances != nil {
		needAppearancesBool, ok := core.GetBool(needAppearances)
		if ok {
			acroForm.NeedAppearances = needAppearancesBool
		} else {
			common.Log.Debug("ERROR: NeedAppearances invalid (got %T)", needAppearances)
		}
	}
	if sigFlags := dict.Get("SigFlags"); sigFlags != nil {
		sigFlagsInt, ok := core.GetInt(sigFlags)
		if ok {
			acroForm.SigFlags = sigFlagsInt
		} else {
			common.Log.Debug("ERROR: SigFlags invalid (got %T)", sigFlags)
		}
	}
	if co := dict.Get("CO"); co != nil {
		coArr, ok := core.GetArray(co)
		if ok {
			acroForm.CO = coArr
		} else {
			common.Log.Debug("ERROR: CO invalid (got %T)", co)
		}
	}
	if dr := dict.Get("DR"); dr != nil {
		if drDict, ok := core.GetDict(dr); ok {
			resources, err := NewPdfPageResourcesFromDict(drDict)
			if err != nil {
				common.Log.Error("Invalid DR: %v", err)
				return nil, err
			}
			acroForm.DR = resources
		} else {
			common.Log.Debug("ERROR: DR invalid (got %T)", dr)
		}
	}
	if da := dict.Get("DA"); da != nil {
		daStr, ok := core.GetString(da)
		if ok {
			acroForm.DA = daStr
		} else {
			common.Log.Debug("ERROR: DA invalid (got %T)", da)
		}
	}
	if q := dict.Get("Q"); q != nil {
		qInt, ok := core.GetInt(q)
		if ok {
			acroForm.Q = qInt
		} else {
			common.Log.Debug("ERROR: Q invalid (got %T)", q)
		}
	}
	if xfa := dict.Get("XFA"); xfa != nil {
		acroForm.XFA = xfa
	}
	if adbeEchoSign := dict.Get("ADBE_EchoSign"); adbeEchoSign != nil {
		acroForm.ADBEEchoSign = adbeEchoSign
	}
	acroForm.ToPdfObject()
	return acroForm, nil
}

// CheckAccessRights checks access rights and permissions for a specified password.  If either user/owner
// password is specified,  full rights are granted, otherwise the access rights are specified by the
// Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
//
// For AES-256 the password is prepared as described on Decrypt.
func (r *PdfReader) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	return r.parser.CheckAccessRights(password)
}

// GetAttachedFiles retrieves all the attached files info and content.
func (r *PdfReader) GetAttachedFiles() ([]*EmbeddedFile, error) {
	files := []*EmbeddedFile{}
	nameDictionary, err := r.GetNameDictionary()
	if err != nil {
		return nil, err
	}
	if nameDictionary == nil {
		return files, nil
	}
	names := newNamesFromPdfObject(nameDictionary)
	if names.EmbeddedFiles == nil {
		return nil, nil
	}
	obj := names.EmbeddedFiles.Get("Names")
	arr, ok := obj.(*core.PdfObjectArray)
	if !ok {
		return nil, errors.New("Invalid Names array")
	}
	for i := 1; i < len(arr.Elements()); i += 2 {
		if i%2 != 0 {
			obj := arr.Get(i)
			filespec, err := NewPdfFilespecFromObj(obj)
			if err != nil {
				return nil, err
			}
			embeddedFile, err := NewEmbeddedFileFromObject(filespec.EF)
			if err != nil {
				return nil, err
			}
			str, ok := filespec.F.(*core.PdfObjectString)
			if ok {
				embeddedFile.Name = str.Str()
			}
			embeddedFile.Description = string(filespec.Desc.Write())
			embeddedFile.Relationship = RelationshipUnspecified
			if filespec.AFRelationship != nil {
				switch string(filespec.AFRelationship.Write()) {
				case "Source":
					embeddedFile.Relationship = RelationshipSource
				case "Data":
					embeddedFile.Relationship = RelationshipData
				case "Alternative":
					embeddedFile.Relationship = RelationshipAlternative
				case "Supplement":
					embeddedFile.Relationship = RelationshipSupplement
				default:
					embeddedFile.Relationship = RelationshipUnspecified
				}
			}
			files = append(files, embeddedFile)
		}
	}
	return files, nil
}

func (r *PdfReader) newPdfAnnotation3DFromDict(dict *core.PdfObjectDictionary) (*PdfAnnotation3D, error) {
	annot3D := PdfAnnotation3D{}
	annot3D.T3DD = dict.Get("3DD")
	annot3D.T3DV = dict.Get("3DV")
	annot3D.T3DA = dict.Get("3DA")
	annot3D.T3DI = dict.Get("3DI")
	annot3D.T3DB = dict.Get("3DB")
	return &annot3D, nil
}

// CheckAccessRightsWithCertificate reports whether the certificate can open a
// public-key encrypted document and the permissions its recipient entry
// grants, without decrypting the document.
func (r *PdfReader) CheckAccessRightsWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	return r.parser.CheckAccessRightsWithCertificate(cert, key)
}
