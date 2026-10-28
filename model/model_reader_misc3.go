package model

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"io"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/license"
)

func (r *PdfReader) loadFromParser(parser *core.PdfParser) (*PdfReader, error) {
	r.parser = parser
	ok, err := r.IsEncrypted()
	if err != nil {
		return nil, err
	}
	if !ok {
		err = r.loadStructure()
		if err != nil {
			return nil, err
		}
	} else if r.autoDecrypt {
		var ok bool
		if crypter := parser.GetCrypter(); crypter != nil && crypter.IsPublicKey() {
			if r.opts.Certificate == nil || r.opts.PrivateKey == nil {
				return nil, core.ErrCertificateRequired
			}
			ok, err = r.DecryptWithCertificate(r.opts.Certificate, r.opts.PrivateKey)
		} else {
			ok, err = r.Decrypt([]byte(r.opts.Password))
		}
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("unable to decrypt password protected file - need to specify pass to Decrypt")
		}
	}
	r.revisionCache = make(map[*PdfReader]*PdfReader)
	r.revisions = make([]*PdfReader, parser.GetRevisionNumber())
	return r, nil
}

// GetOCProperties returns the optional content properties PdfObject.
func (r *PdfReader) GetOCProperties() (core.PdfObject, error) {
	catalog := r.catalog
	ocProperties := catalog.Get("OCProperties")
	ocProperties = core.ResolveReference(ocProperties)
	if !r.lazy {
		err := r.traverseObjectData(ocProperties)
		if err != nil {
			return nil, err
		}
	}
	return ocProperties, nil
}

// GetNamedDestinations returns the Dests entry in the PDF catalog.
// See section 12.3.2.3 "Named Destinations" (p. 367 PDF32000_2008).
func (r *PdfReader) GetNamedDestinations() (core.PdfObject, error) {
	dests := core.ResolveReference(r.catalog.Get("Dests"))
	if dests == nil {
		return nil, nil
	}
	if !r.lazy {
		err := r.traverseObjectData(dests)
		if err != nil {
			return nil, err
		}
	}
	return dests, nil
}

// LinearizationDictObjNum returns the object number of the linearization
// parameter dictionary (the first indirect object after the header), or 0 if
// the PDF does not begin with one. Populated even when IsLinearized() is false
// (e.g. the dictionary was superseded by a later incremental revision).
func (r *PdfReader) LinearizationDictObjNum() int {
	return r.parser.LinearizationDictObjNum()
}

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
	const trackNameNewPdfReaderWithOpts = "model:NewPdfReaderWithOpts"
	return newPdfReaderTracked(r, size, opts, true, false, trackNameNewPdfReaderWithOpts)
}

// ReaderOpts defines options for creating PdfReader instances.
type ReaderOpts struct {
	// Password password of the PDF file encryption.
	// Default: empty ("").
	Password string

	// Certificate and PrivateKey identify the recipient when the document
	// is encrypted with a public-key security handler (/Filter
	// /Adobe.PubSec). Both must be set, and PrivateKey must be an
	// *rsa.PrivateKey (RSA key transport is the only recipient type
	// supported). They are ignored for documents using the standard
	// (password) security handler, so one ReaderOpts can open either kind.
	Certificate *x509.Certificate
	PrivateKey  crypto.PrivateKey

	// LazyLoad set if the PDF file would be loaded using lazy-loading mode.
	// Default: true.
	LazyLoad bool

	// ComplianceMode set if parsed PDF file should contain meta information for the verifiers of the compliance standards like PDF/A.
	ComplianceMode bool

	// RelaxedMode indicates whether the parser should operate in a relaxed mode, ignoring certain insignificant errors and continuing to parse the PDF file.
	// For example, it skips AcroForm loading errors and proceeds with the rest of the file if no form-related processing is required.
	RelaxedMode bool

	// ParserAutoRepair indicates whether the parser should attempt to automatically repair the file if it is found to be corrupted or malformed.
	ParserAutoRepair bool

	// MaxDecodedStreamSize is the maximum number of bytes a single stream, or a single filter stage
	// of a filter chain, may decode to. 0 selects core.DefaultMaxDecodedStreamSize (256 MiB unless
	// changed with core.SetDefaultMaxDecodedStreamSize).
	// Decoding past the limit fails with core.ErrDecodedSizeLimitExceeded.
	// With ComplianceMode the limits apply once the cross-reference data is loaded; the streams
	// read while loading it use core.DefaultMaxDecodedStreamSize. See core.ParserOpts for the
	// decoders the limits do not cover.
	MaxDecodedStreamSize int64

	// MaxDecodedTotalSize is the maximum number of bytes all stream decodes of the document may
	// produce together, repeated decodes of the same stream included. See
	// core.ParserOpts.MaxDecodedTotalSize. 0 disables the limit.
	MaxDecodedTotalSize int64
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
	const trackNameNewPdfReaderFromParser = "model:NewPdfReaderFromParser"
	randomID, err := randomIDWithPrefix("r")
	if err != nil {
		return nil, err
	}
	if err := license.Track(randomID, trackNameNewPdfReaderFromParser, ""); err != nil {
		return nil, err
	}
	reader := &PdfReader{parser: parser, traversed: map[core.PdfObject]struct{}{}, modelManager: newModelManager(), lazy: false, opts: NewReaderOpts(), trackID: randomID}
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
	readerCore, err := newPdfReaderCore(previousRevisionParser.GetReaderAt(), r.opts, r.autoDecrypt, r.skipOutlines && r.skipPerms, "model:GetPreviousRevision")
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

// NewReaderOpts generates a default `ReaderOpts` instance.
func NewReaderOpts() *ReaderOpts { return &ReaderOpts{Password: "", LazyLoad: true} }

// NewPdfReaderLazyAt is the explicit-size form of NewPdfReaderLazy.
func NewPdfReaderLazyAt(r io.ReaderAt, size int64) (*PdfReader, error) {
	const trackNameNewPdfReaderLazy = "model:NewPdfReaderLazy"
	return newPdfReaderTracked(r, size, &ReaderOpts{LazyLoad: true}, false, false, trackNameNewPdfReaderLazy)
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
