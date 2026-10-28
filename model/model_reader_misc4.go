package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

func newPdfReaderTracked(r io.ReaderAt, size int64, opts *ReaderOpts, ok, found bool) (*PdfReader, error) {
	readerCore, err := newPdfReaderCore(r, opts, ok, found)
	if err != nil {
		return nil, err
	}
	opts = readerCore.opts
	var parser *core.PdfParser
	if !readerCore.complianceMode {
		opts2 := core.ParserOpts{RelaxedMode: opts.RelaxedMode, AutoRepairXrefTable: opts.ParserAutoRepair, MaxDecodedStreamSize: opts.MaxDecodedStreamSize, MaxDecodedTotalSize: opts.MaxDecodedTotalSize}
		parser, err = core.NewParserWithOptsAt(r, size, opts2)
	} else {
		parser, err = core.NewCompliancePdfParserAt(r, size)
		if err == nil && (opts.MaxDecodedStreamSize != 0 || opts.MaxDecodedTotalSize != 0) {
			parser.Opts = &core.ParserOpts{MaxDecodedStreamSize: opts.MaxDecodedStreamSize, MaxDecodedTotalSize: opts.MaxDecodedTotalSize}
		}
	}
	if err != nil {
		return nil, err
	}
	return readerCore.loadFromParser(parser)
}

func (r *PdfReader) newPdfSignatureFromIndirect(indirect *core.PdfIndirectObject) (*PdfSignature, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Signature container not containing a dictionary")
		return nil, ErrTypeCheck
	}
	if sig, ok := r.modelManager.GetModelFromPrimitive(indirect).(*PdfSignature); ok {
		return sig, nil
	}
	sig := &PdfSignature{}
	sig.primitive = indirect
	sig.Type, _ = core.GetName(dict.Get("Type"))
	sig.Filter, ok = core.GetName(dict.Get("Filter"))
	if !ok {
		common.Log.Error("ERROR: Signature Filter attribute invalid or missing")
		return nil, ErrInvalidAttribute
	}
	sig.SubFilter, _ = core.GetName(dict.Get("SubFilter"))
	sig.Contents, ok = core.GetString(dict.Get("Contents"))
	if !ok {
		common.Log.Error("ERROR: Signature contents missing")
		return nil, ErrInvalidAttribute
	}
	if reference, ok := core.GetArray(dict.Get("Reference")); ok {
		sig.Reference = core.MakeArray()
		for _, obj := range reference.Elements() {
			dict, ok := core.GetDict(obj)
			if !ok {
				common.Log.Error("ERROR: Reference contents invalidated")
				return nil, ErrInvalidAttribute
			}
			signatureReference, err := r.newPdfSignatureReferenceFromDict(dict)
			if err != nil {
				return nil, err
			}
			sig.Reference.Append(signatureReference.ToPdfObject())
		}
	}
	sig.Cert = dict.Get("Cert")
	sig.ByteRange, _ = core.GetArray(dict.Get("ByteRange"))
	sig.Changes, _ = core.GetArray(dict.Get("Changes"))
	sig.Name, _ = core.GetString(dict.Get("Name"))
	sig.M, _ = core.GetString(dict.Get("M"))
	sig.Location, _ = core.GetString(dict.Get("Location"))
	sig.Reason, _ = core.GetString(dict.Get("Reason"))
	sig.ContactInfo, _ = core.GetString(dict.Get("ContactInfo"))
	sig.R, _ = core.GetInt(dict.Get("R"))
	sig.V, _ = core.GetInt(dict.Get("V"))
	sig.PropBuild, _ = core.GetDict(dict.Get("Prop_Build"))
	sig.PropAuthTime, _ = core.GetInt(dict.Get("Prop_AuthTime"))
	sig.PropAuthType, _ = core.GetName(dict.Get("Prop_AuthType"))
	r.modelManager.Register(indirect, sig)
	return sig, nil
}

// PdfVersion returns version of the PDF file.
func (r *PdfReader) PdfVersion() core.Version { return r.parser.PdfVersion() }

func (r *PdfReader) resolveReference(ref *core.PdfObjectReference) (core.PdfObject, bool, error) {
	if cachedObject, ok := r.parser.LoadCachedObject(int(ref.ObjectNumber)); ok {
		return cachedObject, true, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Reader Lookup ref: %s", ref)
	}
	lookupByReference, err := r.parser.LookupByReference(*ref)
	if err != nil {
		return nil, false, err
	}
	r.parser.StoreCachedObject(int(ref.ObjectNumber), lookupByReference)
	return lookupByReference, false, nil
}

// GetTrailer returns the PDF's trailer dictionary.
func (r *PdfReader) GetTrailer() (*core.PdfObjectDictionary, error) {
	trailer := r.parser.GetTrailer()
	if trailer == nil {
		return nil, errors.New("trailer missing")
	}
	return trailer, nil
}

// GetRevisionNumber returns the version of the current Pdf document
func (r *PdfReader) GetRevisionNumber() int { return r.parser.GetRevisionNumber() }

func (r *PdfReader) traverseObjectData(obj core.PdfObject) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return core.ResolveReferencesDeep(obj, r.traversed)
}

// PrintPdfObjects is a helper function printing all objects in a given PDF file
// It can be useful while debugging. Use os.Stdout as w io.Writer to print to console.
func (r *PdfReader) PrintPdfObjects(w io.Writer) error {
	objectNums := r.GetObjectNums()
	fmt.Fprintf(w, "%d PDF objects:\n", len(objectNums))
	for _, objectNum := range objectNums {
		fmt.Fprintln(w, "=========================================================")
		if err := r.PrintPdfObject(w, objectNum); err != nil {
			return err
		}
	}
	return nil
}

func (r *PdfReader) newPdfSignatureReferenceFromDict(dict *core.PdfObjectDictionary) (*PdfSignatureReference, error) {
	if signatureReference, ok := r.modelManager.GetModelFromPrimitive(dict).(*PdfSignatureReference); ok {
		return signatureReference, nil
	}
	signatureReference := &PdfSignatureReference{primitive: dict, Data: dict.Get("Data")}
	var ok bool
	signatureReference.Type, _ = core.GetName(dict.Get("Type"))
	signatureReference.TransformMethod, ok = core.GetName(dict.Get("TransformMethod"))
	if !ok {
		common.Log.Error("ERROR: Signature Reference TransformMethod attribute invalid or missing")
		return nil, ErrInvalidAttribute
	}
	signatureReference.TransformParams, _ = core.GetDict(dict.Get("TransformParams"))
	signatureReference.DigestMethod, _ = core.GetName(dict.Get("DigestMethod"))
	return signatureReference, nil
}

// PrintPdfObject is a helper function printing object with a given number in a PDF file.
// It can be useful while debugging. Use os.Stdout as w io.Writer to print to console.
// If objNum is -1, then it prints trailer of the PDF file.
func (r *PdfReader) PrintPdfObject(w io.Writer, objNum int) error {
	if objNum == -1 {
		trailer, err := r.GetTrailer()
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Trailer: %s\n", trailer.String())
		return nil
	}
	indirectObjectByNumber, err := r.GetIndirectObjectByNumber(objNum)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Object %d: %s\n", objNum, indirectObjectByNumber.String())
	if stream, ok := indirectObjectByNumber.(*core.PdfObjectStream); ok {
		data, err := core.DecodeStream(stream)
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "Decoded:\n%s", data)
	} else if indirect, ok := indirectObjectByNumber.(*core.PdfIndirectObject); ok {
		fmt.Fprintf(w, "%T\n", indirect.PdfObject)
		fmt.Fprintf(w, "%s\n", indirect.PdfObject.String())
	}
	return nil
}

func (r *PdfReader) loadStructure() error {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return fmt.Errorf("file need to be decrypted first")
	}
	trailer := r.parser.GetTrailer()
	if trailer == nil {
		return fmt.Errorf("missing trailer")
	}
	ref, ok := trailer.Get("Root").(*core.PdfObjectReference)
	if !ok {
		return fmt.Errorf("invalid Root (trailer: %s)", trailer)
	}
	lookupByReference, err := r.parser.LookupByReference(*ref)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read root element catalog: %s", err)
		return err
	}
	indirect, ok := lookupByReference.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Missing catalog: (root %q) (trailer %s)", lookupByReference, trailer)
		return errors.New("missing catalog")
	}
	dict, ok := (*indirect).PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Invalid catalog (%s)", indirect.PdfObject)
		return errors.New("invalid catalog")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Catalog: %s", dict)
	}
	ref2, ok := dict.Get("Pages").(*core.PdfObjectReference)
	if !ok {
		return errors.New("pages in catalog should be a reference")
	}
	obj, err := r.parser.LookupByReference(*ref2)
	if err != nil {
		common.Log.Debug("ERROR: Failed to read pages")
		return err
	}
	indirect2, ok := obj.(*core.PdfIndirectObject)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid")
		common.Log.Debug("op: %p", indirect2)
		return errors.New("pages object invalid")
	}
	dict2, ok := indirect2.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("ERROR: Pages object invalid (%s)", indirect2)
		return errors.New("pages object invalid")
	}
	count, ok := core.GetInt(dict2.Get("Count"))
	if !ok {
		common.Log.Debug("ERROR: Pages count object invalid")
		return errors.New("pages count invalid")
	}
	if _, ok = core.GetName(dict2.Get("Type")); !ok {
		common.Log.Debug("Pages dict Type field not set. Setting Type to Pages.")
		dict2.Set("Type", core.MakeName("Pages"))
	}
	if rotate, ok := core.GetInt(dict2.Get("Rotate")); ok {
		n := int64(*rotate)
		r.Rotate = &n
	}
	r.root = ref
	r.catalog = dict
	r.pagesDict = dict2
	r.pagesObj = indirect2
	r.pageCount = int(*count)
	r.pageObjs = []*core.PdfIndirectObject{}
	set := map[core.PdfObject]struct{}{}
	err = r.buildPageList(indirect2, nil, set)
	if err != nil {
		return err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("---")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("TOC")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Pages")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("%d: %s", len(r.pageObjs), r.pageObjs)
	}
	if !r.skipOutlines {
		r.outlineTree, err = r.loadOutlines()
		if err != nil {
			common.Log.Debug("ERROR: Failed to build outline tree (%s)", err)
			return err
		}
	}
	r.AcroForm, err = r.loadForms()
	if err != nil {
		return err
	}
	r.DSS, err = r.loadDSS()
	if err != nil {
		return err
	}
	if !r.skipPerms {
		r.perms, err = r.loadPerms()
		if err != nil {
			return err
		}
	}
	return nil
}

// GetRevision returns the specific version of the PdfReader for the current Pdf document
func (r *PdfReader) GetRevision(revisionNumber int) (*PdfReader, error) {
	n := r.parser.GetRevisionNumber()
	if revisionNumber < 0 || revisionNumber > n {
		return nil, errors.New("wrong revision number")
	}
	if revisionNumber == n {
		return r, nil
	}
	if r.revisions[revisionNumber] != nil {
		return r.revisions[revisionNumber], nil
	}
	reader := r
	for i := n - 1; i >= revisionNumber; i-- {
		previousRevision, err := reader.GetPreviousRevision()
		if err != nil {
			return nil, err
		}
		r.revisions[i] = previousRevision
		reader = previousRevision
	}
	return reader, nil
}

// RepairAcroForm attempts to rebuild the AcroForm fields using the widget
// annotations present in the document pages. Pass nil for the opts parameter
// in order to use the default options.
// NOTE: Currently, the opts parameter is declared in order to enable adding
// future options, but passing nil will always result in the default options
// being used.
func (r *PdfReader) RepairAcroForm(opts *AcroFormRepairOptions) error {
	var fields []*PdfField
	set := map[*core.PdfIndirectObject]struct{}{}
	for _, page := range r.PageList {
		annotations, err := page.GetAnnotations()
		if err != nil {
			return err
		}
		for _, annotation := range annotations {
			var field *PdfField
			switch v := annotation.GetContext().(type) {
			case *PdfAnnotationWidget:
				if v.field != nil {
					field = v.field
					break
				}
				if parentIndirect, ok := core.GetIndirect(v.Parent); ok {
					field, err = r.newPdfFieldFromIndirectObject(parentIndirect, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", parentIndirect, err)
				}
				if v.primitive != nil {
					field, err = r.newPdfFieldFromIndirectObject(v.primitive, nil)
					if err == nil {
						break
					}
					common.Log.Debug("WARN: could not parse form field %+v: %v", v.primitive, err)
				}
			}
			if field == nil {
				continue
			}
			if _, ok := set[field.primitive]; ok {
				continue
			}
			set[field.primitive] = struct{}{}
			fields = append(fields, field)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	if r.AcroForm == nil {
		r.AcroForm = NewPdfAcroForm()
	}
	r.AcroForm.Fields = &fields
	return nil
}
