package model

import (
	"bytes"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	_ "image/gif"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/mdp"
	"github.com/OpenMellon/pkcs7"
)

func signatureContentsDigestHex(sig *PdfSignature) (string, error) {
	if sig.Contents == nil {
		return "", errors.New("signature Contents not set")
	}
	data, err := sha1Digest(sig.Contents.Bytes())
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(data)), nil
}

// NewPdfSignatureReferenceDocMDP returns PdfSignatureReference for the transformParams.
func NewPdfSignatureReferenceDocMDP(transformParams *PdfTransformParamsDocMDP) *PdfSignatureReference {
	return &PdfSignatureReference{Type: core.MakeName("SigRef"), TransformMethod: core.MakeName("DocMDP"), TransformParams: transformParams.ToPdfObject()}
}

// SetName sets the `Name` field of the signature.
func (s *PdfSignature) SetName(name string) { s.Name = core.MakeEncodedString(name, true) }

// ToPdfObject implements interface PdfModel.
func (r *PdfSignatureReference) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Type", r.Type)
	dict.SetIfNotNil("TransformMethod", r.TransformMethod)
	dict.SetIfNotNil("TransformParams", r.TransformParams)
	dict.SetIfNotNil("Data", r.Data)
	dict.SetIfNotNil("DigestMethod", r.DigestMethod)
	return dict
}

// SignatureHandler interface defines the common functionality for PDF signature handlers, which
// need to be capable of validating digital signatures and signing PDF documents.
type SignatureHandler interface {
	// IsApplicable checks if a given signature dictionary `sig` is applicable for the signature handler.
	// For example a signature of type `adbe.pkcs7.detached` might not fit for a rsa.sha1 handler.
	IsApplicable(sig *PdfSignature) bool

	// Validate validates a PDF signature against a given digest (hash) such as that determined
	// for an input file. Returns validation results.
	Validate(sig *PdfSignature, hasher Hasher) (SignatureValidationResult, error)

	// InitSignature prepares the signature dictionary for signing. This involves setting all
	// necessary fields, and also allocating sufficient space to the Contents so that the
	// finalized signature can be inserted once the hash is calculated.
	InitSignature(sig *PdfSignature) error

	// NewDigest creates a new digest/hasher based on the signature dictionary and handler.
	NewDigest(sig *PdfSignature) (Hasher, error)

	// Sign receives the hash `digest` (for example hash of an input file), and signs based
	// on the signature dictionary `sig` and applies the signature data to the signature
	// dictionary Contents field.
	Sign(sig *PdfSignature, hasher Hasher) error
}

// SignatureHandlerDocMDP extends SignatureHandler with the ValidateWithOpts method for checking the DocMDP policy.
type SignatureHandlerDocMDP interface {
	SignatureHandler

	// ValidateWithOpts validates a PDF signature by checking PdfReader or PdfParser
	// ValidateWithOpts shall contain Validate call
	ValidateWithOpts(sig *PdfSignature, hasher Hasher, signatureHandlerDocMDPParams SignatureHandlerDocMDPParams) (SignatureValidationResult, error)
}

// GetContainingPdfObject implements interface PdfModel.
func (r *PdfSignatureReference) GetContainingPdfObject() core.PdfObject { return r.primitive }

// SetDate sets the `M` field of the signature.
func (s *PdfSignature) SetDate(date time.Time, format string) {
	if format == "" {
		format = "D:20060102150405-07'00'"
	}
	s.M = core.MakeString(date.Format(format))
}

// GetContainingPdfObject implements interface PdfModel.
func (s *PdfSignature) GetContainingPdfObject() core.PdfObject { return s.primitive }

// ToPdfObject implements interface PdfModel.
func (s *PdfSignature) ToPdfObject() core.PdfObject {
	indirect := s.primitive
	var dict *core.PdfObjectDictionary
	if signDict, ok := indirect.PdfObject.(*pdfSignDictionary); ok {
		dict = signDict.PdfObjectDictionary
	} else {
		dict = indirect.PdfObject.(*core.PdfObjectDictionary)
	}
	dict.SetIfNotNil("Type", s.Type)
	dict.SetIfNotNil("Filter", s.Filter)
	dict.SetIfNotNil("SubFilter", s.SubFilter)
	dict.SetIfNotNil("ByteRange", s.ByteRange)
	dict.SetIfNotNil("Contents", s.Contents)
	dict.SetIfNotNil("Cert", s.Cert)
	dict.SetIfNotNil("Name", s.Name)
	dict.SetIfNotNil("Reason", s.Reason)
	dict.SetIfNotNil("M", s.M)
	dict.SetIfNotNil("Reference", s.Reference)
	dict.SetIfNotNil("Changes", s.Changes)
	dict.SetIfNotNil("ContactInfo", s.ContactInfo)
	dict.SetIfNotNil("Location", s.Location)
	return indirect
}

// NewPdfSignature creates a new PdfSignature object.
func NewPdfSignature(handler SignatureHandler) *PdfSignature {
	sig := &PdfSignature{Type: core.MakeName("Sig"), Handler: handler}
	signDict := &pdfSignDictionary{PdfObjectDictionary: core.MakeDict(), handler: &handler, signature: sig}
	sig.primitive = core.MakeIndirectObject(signDict)
	return sig
}

// PdfSignature represents a PDF signature dictionary and is used for signing via form signature fields.
// (Section 12.8, Table 252 - Entries in a signature dictionary p. 475 in PDF32000_2008).
type PdfSignature struct {
	Handler   SignatureHandler
	primitive *core.PdfIndirectObject

	// Type: Sig/DocTimeStamp
	Type         *core.PdfObjectName
	Filter       *core.PdfObjectName
	SubFilter    *core.PdfObjectName
	Contents     *core.PdfObjectString
	Cert         core.PdfObject
	ByteRange    *core.PdfObjectArray
	Reference    *core.PdfObjectArray
	Changes      *core.PdfObjectArray
	Name         *core.PdfObjectString
	M            *core.PdfObjectString
	Location     *core.PdfObjectString
	Reason       *core.PdfObjectString
	ContactInfo  *core.PdfObjectString
	R            *core.PdfObjectInteger
	V            *core.PdfObjectInteger
	PropBuild    *core.PdfObjectDictionary
	PropAuthTime *core.PdfObjectInteger
	PropAuthType *core.PdfObjectName
}

// GetDocMDPPermission returns the DocMDP level of the restrictions
func (s *PdfSignature) GetDocMDPPermission() (mdp.DocMDPPermission, bool) {
	for _, obj := range s.Reference.Elements() {
		if dict, ok := core.GetDict(obj); ok {
			if transformMethod, ok := core.GetNameVal(dict.Get("TransformMethod")); ok && transformMethod == "DocMDP" {
				if transformParams, ok := core.GetDict(dict.Get("TransformParams")); ok {
					if P, ok := core.GetIntVal(transformParams.Get("P")); ok {
						return mdp.DocMDPPermission(P), true
					}
				}
			}
		}
	}
	return 0, false
}

// SignatureHandlerDocMDPParams describe the specific parameters for the SignatureHandlerEx
// These parameters describe how to check the difference between revisions.
// Revisions of the document get from the PdfParser.
type SignatureHandlerDocMDPParams struct {
	Parser     *core.PdfParser
	DiffPolicy mdp.DiffPolicy
}

// GetCerts returns the certificate chain embedded in the signature, with the
// signer's certificate first. For PKCS#7 based SubFilters the signer is the
// certificate named by the SignerInfo; the /Cert entry lists it first already.
// When a SubFilter offers several sources, the next one is tried if the
// previous fails, and an error is returned only when none yields a chain.
func (s *PdfSignature) GetCerts() ([]*x509.Certificate, error) {
	var fns []func() ([]*x509.Certificate, error)
	switch s2, _ := core.GetNameVal(s.SubFilter); s2 {
	case "adbe.pkcs7.detached", "ETSI.CAdES.detached":
		fns = append(fns, s.extractChainFromPKCS7, s.extractChainFromCert)
	case "adbe.x509.rsa_sha1":
		fns = append(fns, s.extractChainFromCert)
	case "ETSI.RFC3161":
		fns = append(fns, s.extractChainFromPKCS7)
	default:
		return nil, fmt.Errorf("unsupported SubFilter: %s", s2)
	}
	var err error
	for _, fn := range fns {
		certs, certsErr := fn()
		if certsErr != nil {
			if err == nil {
				err = certsErr
			}
			continue
		}
		if len(certs) > 0 {
			return certs, nil
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, ErrSignNoCertificates
}

// SetReason sets the `Reason` field of the signature.
func (s *PdfSignature) SetReason(reason string) {
	s.Reason = core.MakeEncodedString(reason, true)
}

func (s *PdfSignature) extractChainFromPKCS7() ([]*x509.Certificate, error) {
	if s.Contents == nil {
		return nil, errors.New("signature Contents not set")
	}
	p7, err := pkcs7.Parse(s.Contents.Bytes())
	if err != nil {
		return nil, err
	}
	if len(p7.Certificates) == 0 {
		return nil, nil
	}
	onlySigner := p7.GetOnlySigner()
	if onlySigner == nil {
		return nil, errors.New("signer certificate not identified in PKCS#7 package")
	}
	certs := make([]*x509.Certificate, 0, len(p7.Certificates))
	certs = append(certs, onlySigner)
	for _, certificate := range p7.Certificates {
		if certificate != onlySigner {
			certs = append(certs, certificate)
		}
	}
	return certs, nil
}

// PdfSignatureReference represents a PDF signature reference dictionary and is used for signing via form signature fields.
// (Section 12.8.1, Table 253 - Entries in a signature reference dictionary p. 469 in PDF32000_2008).
type PdfSignatureReference struct {
	primitive       *core.PdfObjectDictionary
	Type            *core.PdfObjectName
	TransformMethod *core.PdfObjectName
	TransformParams core.PdfObject
	Data            core.PdfObject
	DigestMethod    *core.PdfObjectName
}

// SetLocation sets the `Location` field of the signature.
func (s *PdfSignature) SetLocation(location string) {
	s.Location = core.MakeEncodedString(location, true)
}

// Initialize initializes the PdfSignature.
func (s *PdfSignature) Initialize() error {
	if s.Handler == nil {
		return errors.New("signature handler cannot be nil")
	}
	return s.Handler.InitSignature(s)
}

func (s *PdfSignature) extractChainFromCert() ([]*x509.Certificate, error) {
	var arr *core.PdfObjectArray
	switch v := core.TraceToDirectObject(s.Cert).(type) {
	case nil:
		return nil, nil
	case *core.PdfObjectString:
		arr = core.MakeArray(v)
	case *core.PdfObjectArray:
		arr = v
	default:
		return nil, fmt.Errorf("invalid signature certificate object type: %T", v)
	}
	var buf bytes.Buffer
	for _, obj := range arr.Elements() {
		str, ok := core.GetString(obj)
		if !ok {
			return nil, fmt.Errorf("invalid certificate object type in signature certificate chain: %T", obj)
		}
		if _, err := buf.Write(str.Bytes()); err != nil {
			return nil, err
		}
	}
	return x509.ParseCertificates(buf.Bytes())
}
