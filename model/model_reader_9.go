package model

import (
	"crypto"
	"crypto/x509"
	_ "image/gif"
	"io"
	"sync"

	"github.com/OpenMellon/openpdf/core"
)

// CompliancePdfReader is a wrapper over PdfReader that is used for verifying if the input Pdf document matches the
// compliance rules of standards like PDF/A.
// NOTE: This implementation is in experimental development state.
//
//	Keep in mind that it might change in the subsequent minor versions.
type CompliancePdfReader struct {
	*PdfReader
	parserMetadata core.ParserMetadata
}

// NewCompliancePdfReader creates a PdfReader that scans the file for the
// extra metadata needed by PDF/A or PDF/X compliance checks. The source's
// byte length is auto-detected; for exotic io.ReaderAt sources use
// NewCompliancePdfReaderAt.
// NOTE: This implementation is in experimental development state and may
// change in subsequent minor versions.
func NewCompliancePdfReader(r io.ReaderAt) (*CompliancePdfReader, error) {
	sizeOfReaderAt, err := core.SizeOfReaderAt(r)
	if err != nil {
		return nil, err
	}
	return NewCompliancePdfReaderAt(r, sizeOfReaderAt)
}

// ParserMetadata gets the parser  metadata.
func (r *CompliancePdfReader) ParserMetadata() core.ParserMetadata {
	if r.parserMetadata == (core.ParserMetadata{}) {
		r.parserMetadata, _ = r.parser.ParserMetadata()
	}
	return r.parserMetadata
}

// NewCompliancePdfReaderAt is the explicit-size form of NewCompliancePdfReader.
func NewCompliancePdfReaderAt(r io.ReaderAt, size int64) (*CompliancePdfReader, error) {
	readerTracked, err := newPdfReaderTracked(r, size, &ReaderOpts{ComplianceMode: true}, false, false)
	if err != nil {
		return nil, err
	}
	return &CompliancePdfReader{PdfReader: readerTracked}, nil
}

type modelManager struct {
	primitiveByModel map[PdfModel]core.PdfObject
	modelByPrimitive map[core.PdfObject]PdfModel
	primitiveLock    sync.RWMutex
	modelLock        sync.RWMutex
}

func newModelManager() *modelManager {
	manager := modelManager{}
	manager.primitiveByModel = map[PdfModel]core.PdfObject{}
	manager.modelByPrimitive = map[core.PdfObject]PdfModel{}
	manager.primitiveLock = sync.RWMutex{}
	manager.modelLock = sync.RWMutex{}
	return &manager
}

// Register registers (caches) a model to primitive object relationship.
func (m *modelManager) Register(primitive core.PdfObject, model PdfModel) {
	m.primitiveLock.Lock()
	defer m.primitiveLock.Unlock()
	m.modelLock.Lock()
	defer m.modelLock.Unlock()
	m.primitiveByModel[model] = primitive
	m.modelByPrimitive[primitive] = model
}

// GetPrimitiveFromModel returns the primitive object corresponding to the input `model`.
func (m *modelManager) GetPrimitiveFromModel(model PdfModel) core.PdfObject {
	m.primitiveLock.RLock()
	defer m.primitiveLock.RUnlock()
	primitiveByModel, ok := m.primitiveByModel[model]
	if !ok {
		return nil
	}
	return primitiveByModel
}

// GetModelFromPrimitive returns the model corresponding to the `primitive` PdfObject.
func (m *modelManager) GetModelFromPrimitive(primitive core.PdfObject) PdfModel {
	m.modelLock.RLock()
	defer m.modelLock.RUnlock()
	model, ok := m.modelByPrimitive[primitive]
	if !ok {
		return nil
	}
	return model
}

// PdfModel is a higher level PDF construct which can be collapsed into a PdfObject.
// Each PdfModel has an underlying PdfObject and vice versa (one-to-one).
// Under normal circumstances there should only be one copy of each.
// Copies can be made, but care must be taken to do it properly.
type PdfModel interface {
	ToPdfObject() core.PdfObject
	GetContainingPdfObject() core.PdfObject
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

// NewReaderOpts generates a default `ReaderOpts` instance.
func NewReaderOpts() *ReaderOpts { return &ReaderOpts{Password: "", LazyLoad: true} }
