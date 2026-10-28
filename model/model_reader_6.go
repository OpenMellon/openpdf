package model

import (
	"errors"
	"fmt"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// IsLinearized reports whether the underlying PDF is linearized
// ("fast web view") as defined in ISO 32000-2 Annex F.
func (r *PdfReader) IsLinearized() bool { return r.parser.IsLinearized() }

func (r *PdfReader) newPdfOutlineItemFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfOutlineItem, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("outline object not a dictionary")
	}
	item := NewPdfOutlineItem()
	title := dict.Get("Title")
	if title == nil {
		return nil, fmt.Errorf("missing Title from Outline Item (required)")
	}
	titleStr, isTitleStr := core.GetString(title)
	if !isTitleStr {
		return nil, fmt.Errorf("title not a string (%T)", title)
	}
	item.Title = titleStr
	if count := dict.Get("Count"); count != nil {
		num, ok := count.(*core.PdfObjectInteger)
		if !ok {
			return nil, fmt.Errorf("count not an integer (%T)", count)
		}
		n := int64(*num)
		item.Count = &n
	}
	if dest := dict.Get("Dest"); dest != nil {
		item.Dest = core.ResolveReference(dest)
		if !r.lazy {
			err := r.traverseObjectData(item.Dest)
			if err != nil {
				return nil, err
			}
		}
	}
	if a := dict.Get("A"); a != nil {
		item.A = core.ResolveReference(a)
		if !r.lazy {
			err := r.traverseObjectData(item.A)
			if err != nil {
				return nil, err
			}
		}
	}
	if se := dict.Get("SE"); se != nil {
		item.SE = nil
	}
	if c := dict.Get("C"); c != nil {
		item.C = core.ResolveReference(c)
	}
	if f := dict.Get("F"); f != nil {
		item.F = core.ResolveReference(f)
	}
	return item, nil
}

// GetNameDictionary returns the Names entry in the PDF catalog.
// See section 7.7.4 "Name Dictionary" (p. 80 PDF32000_2008).
func (r *PdfReader) GetNameDictionary() (core.PdfObject, error) {
	names := core.ResolveReference(r.catalog.Get("Names"))
	if names == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(names)
		if err != nil {
			return nil, err
		}
	}
	return names, nil
}

// NewPdfReaderWithOptsAt is the explicit-size form of NewPdfReaderWithOpts.
func NewPdfReaderWithOptsAt(r io.ReaderAt, size int64, opts *ReaderOpts) (*PdfReader, error) {
	return newPdfReaderTracked(r, size, opts, true, false)
}

// GetPdfInfo returns the PDF info dictionary.
func (r *PdfReader) GetPdfInfo() (*PdfInfo, error) {
	trailer, err := r.GetTrailer()
	if err != nil {
		return nil, err
	}
	var dict *core.PdfObjectDictionary
	info := trailer.Get("Info")
	switch v := info.(type) {
	case *core.PdfObjectReference:
		ref := v
		info, err = r.GetIndirectObjectByNumber(int(ref.ObjectNumber))
		info = core.TraceToDirectObject(info)
		if err != nil {
			return nil, err
		}
		dict, _ = info.(*core.PdfObjectDictionary)
	case *core.PdfObjectDictionary:
		dict = v
	}
	if dict == nil {
		return nil, errors.New("info dictionary not present")
	}
	info2, err := NewPdfInfoFromObject(dict)
	if err != nil {
		return nil, err
	}
	return info2, nil
}

// NewPdfReaderFromParser builds a PdfReader on top of a pre-built parser.
// Useful for sharing a single parser across goroutines — the parser's
// internal sync.Map cache and mutexes make concurrent reader.GetPage / page
// content access safe.
//
// The resulting reader has no io.ReadSeeker source, so it cannot be passed to
// NewPdfAppender (incremental signing/updates require a seekable source — use
// NewPdfReader for that). Reading existing signatures is supported, since the
// parser reads raw bytes via ReadBytesAt. Encrypted PDFs are not supported.
func NewPdfReaderFromParser(parser *core.PdfParser) (*PdfReader, error) {
	reader := &PdfReader{parser: parser, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: false, opts: NewReaderOpts()}
	ok, err := reader.IsEncrypted()
	if err != nil {
		return nil, err
	}
	if ok {
		return nil, errors.New("encrypted PDFs are not supported via NewPdfReaderFromParser")
	}
	if err := reader.loadStructure(); err != nil {
		return nil, err
	}
	reader.revisionCache = make(map[*PdfReader]*PdfReader)
	reader.revisions = make([]*PdfReader, parser.GetRevisionNumber())
	return reader, nil
}

func (r *PdfReader) loadPerms() (*Permissions, error) {
	if perms := r.catalog.Get("Perms"); perms != nil {
		if permsDict, ok := core.GetDict(perms); ok {
			docMDP := permsDict.Get("DocMDP")
			if docMDP == nil {
				return nil, nil
			}
			if docMDPIndirect, ok := core.GetIndirect(docMDP); ok {
				sig, err := r.newPdfSignatureFromIndirect(docMDPIndirect)
				if err != nil {
					return nil, err
				}
				return NewPermissions(sig), nil
			}
			return nil, fmt.Errorf("invalid DocMDP entry")
		}
		return nil, fmt.Errorf("invalid Perms entry")
	}
	return nil, nil
}

func (r *PdfReader) lookupPageByObject(obj core.PdfObject) (*PdfPage, error) {
	return nil, errors.New("page not found")
}

// GetObjectNums returns the object numbers of the PDF objects in the file
// Numbered objects are either indirect objects or stream objects.
// e.g. objNums := pdfReader.GetObjectNums()
// The underlying objects can then be accessed with
// pdfReader.GetIndirectObjectByNumber(objNums[0]) for the first available object.
func (r *PdfReader) GetObjectNums() []int { return r.parser.GetObjectNums() }

// LinearizationInfo returns linearization parameters parsed from the PDF, or
// nil if the PDF is not linearized.
func (r *PdfReader) LinearizationInfo() *core.LinearizationInfo {
	return r.parser.LinearizationInfo()
}

// GetOutlineTree returns the outline tree.
func (r *PdfReader) GetOutlineTree() *PdfOutlineTreeNode { return r.outlineTree }

// NewPdfReaderLazy creates a new PdfReader in lazy-loading mode. Objects are
// loaded into memory only when needed. The source's byte length is
// auto-detected; for exotic io.ReaderAt sources use NewPdfReaderLazyAt.
func NewPdfReaderLazy(r io.ReaderAt) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderLazyAt(r, sizeOfReaderAt)
}

// GetNumPages returns the number of pages in the document.
func (r *PdfReader) GetNumPages() (int, error) {
	if r.parser.GetCrypter() != nil && !r.parser.IsAuthenticated() {
		return 0, fmt.Errorf("file need to be decrypted first")
	}
	return len(r.pageObjs), nil
}

// NewPdfReaderWithOpts creates a new PdfReader for the given io.ReaderAt
// source with the supplied options. If opts is nil, the defaults from
// NewReaderOpts are used. The source's byte length is auto-detected; for
// exotic io.ReaderAt sources use NewPdfReaderWithOptsAt.
func NewPdfReaderWithOpts(r io.ReaderAt, opts *ReaderOpts) (*PdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewPdfReaderWithOptsAt(r, sizeOfReaderAt, opts)
}

// GetPreviousRevision returns the previous revision of PdfReader for the Pdf document
func (r *PdfReader) GetPreviousRevision() (*PdfReader, error) {
	if r.parser.GetRevisionNumber() == 0 {
		return nil, errors.New("previous version is not exist")
	}
	if revisionCache, ok := r.revisionCache[r]; ok {
		return revisionCache, nil
	}
	previousRevisionParser, err := r.parser.GetPreviousRevisionParser()
	if err != nil {
		return nil, err
	}
	readerCore, err := newPdfReaderCore(previousRevisionParser.GetReaderAt(), r.opts, r.autoDecrypt, r.skipOutlines && r.skipPerms)
	if err != nil {
		return nil, err
	}
	if readerCore, err = readerCore.loadFromParser(previousRevisionParser); err != nil {
		return nil, err
	}
	r.revisions[r.parser.GetRevisionNumber()-1] = readerCore
	r.revisionCache[r] = readerCore
	readerCore.revisionCache = r.revisionCache
	return readerCore, nil
}

// GetPerms returns the Permissions dictionary
func (r *PdfReader) GetPerms() *Permissions { return r.perms }

// NewPdfReaderLazyAt is the explicit-size form of NewPdfReaderLazy.
func NewPdfReaderLazyAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	return newPdfReaderTracked(r, size, &ReaderOpts{LazyLoad: true}, false, false)
}

// Inspect inspects the object types, subtypes and content in the PDF file returning a map of
// object type to number of instances of each.
func (r *PdfReader) Inspect() (map[string]int, error) { return r.parser.Inspect() }

// GetPieceInfo gets catalog piece info object.
func (r *PdfReader) GetPieceInfo() (core.PdfObject, bool) {
	if r.catalog == nil {
		return nil, false
	}
	pieceInfo := r.catalog.Get("PieceInfo")
	return pieceInfo, pieceInfo != nil
}

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
