package sighandler

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/mdp"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"github.com/OpenMellon/pkcs7"
	"github.com/OpenMellon/timestamp"
)

// NewDocMDPHandler returns the new DocMDP handler with the specific DocMDP restriction level.
func NewDocMDPHandler(handler model.SignatureHandler, permission mdp.DocMDPPermission) (model.SignatureHandler, error) {
	return &DocMDPHandler{handler: handler, Permission: permission}, nil
}

// InitSignature initialises the PdfSignature.
func (ts *docTimeStamp) InitSignature(sig *model.PdfSignature) error {
	clone := *ts
	sig.Type = core.MakeName("DocTimeStamp")
	sig.Handler = &clone
	sig.Filter = core.MakeName("Adobe.PPKLite")
	sig.SubFilter = core.MakeName("ETSI.RFC3161")
	sig.Reference = nil
	if ts.signatureSize > 0 {
		sig.Contents = core.MakeHexString(string(make([]byte, ts.signatureSize)))
	} else {
		digest, err := ts.NewDigest(sig)
		if err != nil {
			return err
		}
		digest.Write([]byte("calculate the Contents field size"))
		if err = clone.Sign(sig, digest); err != nil {
			return err
		}
		ts.signatureSize = clone.signatureSize
	}
	return nil
}

// ValidateWithOpts validates a PDF signature by checking PdfReader or PdfParser by the DiffPolicy
// params describes parameters for the DocMDP checks.
func (h *DocMDPHandler) ValidateWithOpts(sig *model.PdfSignature, digest model.Hasher, params model.SignatureHandlerDocMDPParams) (model.SignatureValidationResult, error) {
	result, err := h.handler.Validate(sig, digest)
	if err != nil {
		return result, err
	}
	parser := params.Parser
	if parser == nil {
		return model.SignatureValidationResult{}, errors.New("parser can't be null")
	}
	if !result.IsVerified {
		return result, nil
	}
	diffPolicy := params.DiffPolicy
	if diffPolicy == nil {
		diffPolicy = mdp.NewDefaultDiffPolicy()
	}
	for revNum := 0; revNum <= parser.GetRevisionNumber(); revNum++ {
		revision, revErr := parser.GetRevision(revNum)
		if revErr != nil {
			return model.SignatureValidationResult{}, revErr
		}
		trailer := revision.GetTrailer()
		if trailer == nil {
			return model.SignatureValidationResult{}, errors.New("undefined the trailer object")
		}
		root, ok := core.GetDict(trailer.Get("Root"))
		if !ok {
			return model.SignatureValidationResult{}, errors.New("undefined the root object")
		}
		acroForm, ok := core.GetDict(root.Get("AcroForm"))
		if !ok {
			continue
		}
		fields, ok := core.GetArray(acroForm.Get("Fields"))
		if !ok {
			continue
		}
		for _, fieldObj := range fields.Elements() {
			fieldDict, fieldOK := core.GetDict(fieldObj)
			if !fieldOK {
				continue
			}
			sigDict, fieldOK := core.GetDict(fieldDict.Get("V"))
			if !fieldOK {
				continue
			}
			if core.EqualObjects(sigDict.Get("Contents"), sig.Contents) {
				result.DiffResults, revErr = diffPolicy.ReviewFile(revision, parser, &mdp.MDPParameters{DocMDPLevel: h.Permission})
				if revErr != nil {
					return model.SignatureValidationResult{}, revErr
				}
				result.IsVerified = result.DiffResults.IsPermitted()
				return result, nil
			}
		}
	}
	return model.SignatureValidationResult{}, errors.New("don't found this signature in the revisions")
}

// Sign adds a new reference to signature's references array.
func (h *DocMDPHandler) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	return h.handler.Sign(sig, digest)
}

// DocTimeStampOpts defines options for configuring the timestamp handler.
type DocTimeStampOpts struct {
	// SignatureSize is the estimated size of the signature contents in bytes.
	// If not provided, a default signature size of 4192 is used.
	// The signing process will report the model.ErrSignNotEnoughSpace error
	// if the estimated signature size is smaller than the actual size of the
	// signature.
	SignatureSize int

	// Client is the timestamp client used to make the signature request.
	// If no client is provided, a default one is used.
	Client *sigutil.TimestampClient
}

// NewDigest creates a new digest.
func (h *DocMDPHandler) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return h.handler.NewDigest(sig)
}

// InitSignature initialization of the DocMDP signature.
func (h *DocMDPHandler) InitSignature(sig *model.PdfSignature) error {
	err := h.handler.InitSignature(sig)
	if err != nil {
		return err
	}
	sig.Handler = h
	if sig.Reference == nil {
		sig.Reference = core.MakeArray()
	}
	sig.Reference.Append(model.NewPdfSignatureReferenceDocMDP(model.NewPdfTransformParamsDocMDP(h.Permission)).ToPdfObject())
	return nil
}

type docTimeStamp struct {
	timestampServerURL string
	hashAlgorithm      crypto.Hash
	signatureSize      int
	timestampClient    *sigutil.TimestampClient
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *DocMDPHandler) IsApplicable(sig *model.PdfSignature) bool {
	found := false
	for _, ref := range sig.Reference.Elements() {
		if refDict, ok := core.GetDict(ref); ok {
			if method, hasMethod := core.GetNameVal(refDict.Get("TransformMethod")); hasMethod {
				if method != "DocMDP" {
					return false
				}
				if params, hasParams := core.GetDict(refDict.Get("TransformParams")); hasParams {
					_, err := core.GetNumberAsInt64(params.Get("P"))
					if err != nil {
						return false
					}
					found = true
					break
				}
			}
		}
	}
	return found && h.handler.IsApplicable(sig)
}

// DocMDPHandler describes handler for the DocMDP realization.
type DocMDPHandler struct {
	handler    model.SignatureHandler
	Permission mdp.DocMDPPermission
}

// NewDigest creates a new digest.
func (ts *docTimeStamp) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
}

// Validate validates PdfSignature.
func (ts *docTimeStamp) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	contents := sig.Contents.Bytes()
	p7, err := pkcs7.Parse(contents)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	if err = p7.Verify(); err != nil {
		return model.SignatureValidationResult{}, err
	}
	certs := certsFromPKCS7(p7)
	var info timestampInfo
	_, err = asn1.Unmarshal(p7.Content, &info)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	hashAlg, err := hashFromOID(info.MessageImprint.HashAlgorithm.Algorithm)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	hasher := hashAlg.New()
	buf, ok := digest.(*bytes.Buffer)
	if !ok {
		return model.SignatureValidationResult{}, fmt.Errorf("cast to buffer fails")
	}
	hasher.Write(buf.Bytes())
	sum := hasher.Sum(nil)
	result := model.SignatureValidationResult{IsSigned: true, IsVerified: bytes.Equal(sum, info.MessageImprint.HashedMessage), GeneralizedTime: info.GeneralizedTime, Certificates: certs}
	return result, nil
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (ts *docTimeStamp) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "ETSI.RFC3161"
}

// NewDocTimeStampWithOpts returns a new DocTimeStamp configured using the
// specified options. If no options are provided, default options will be used.
// Both the timestamp server URL and the hash algorithm can be empty for the
// signature validation.
// The following hash algorithms are supported:
// crypto.SHA1, crypto.SHA256, crypto.SHA384, crypto.SHA512.
func NewDocTimeStampWithOpts(timestampServerURL string, hashAlgorithm crypto.Hash, opts *DocTimeStampOpts) (model.SignatureHandler, error) {
	if opts == nil {
		opts = &DocTimeStampOpts{}
	}
	if opts.SignatureSize <= 0 {
		opts.SignatureSize = 4192
	}
	return &docTimeStamp{timestampServerURL: timestampServerURL, hashAlgorithm: hashAlgorithm, signatureSize: opts.SignatureSize, timestampClient: opts.Client}, nil
}

// NewDocTimeStamp creates a new DocTimeStamp signature handler.
// Both the timestamp server URL and the hash algorithm can be empty for the
// signature validation.
// The following hash algorithms are supported:
// crypto.SHA1, crypto.SHA256, crypto.SHA384, crypto.SHA512.
// NOTE: the handler will do a mock Sign when initializing the signature
// in order to estimate the signature size. Use NewDocTimeStampWithOpts
// for providing the signature size.
func NewDocTimeStamp(timestampServerURL string, hashAlgorithm crypto.Hash) (model.SignatureHandler, error) {
	return &docTimeStamp{timestampServerURL: timestampServerURL, hashAlgorithm: hashAlgorithm}, nil
}

func (ts *docTimeStamp) getCertificate(sig *model.PdfSignature) (*x509.Certificate, error) {
	certs, err := sig.GetCerts()
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

// Sign sets the Contents fields for the PdfSignature.
func (ts *docTimeStamp) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	req, err := sigutil.NewTimestampRequest(digest.(*bytes.Buffer), &timestamp.RequestOptions{Hash: ts.hashAlgorithm, Certificates: true})
	if err != nil {
		return err
	}
	client := ts.timestampClient
	if client == nil {
		client = sigutil.NewTimestampClient()
	}
	token, err := client.GetEncodedToken(ts.timestampServerURL, req)
	if err != nil {
		return err
	}
	tokenLen := len(token)
	if ts.signatureSize > 0 && tokenLen > ts.signatureSize {
		return model.ErrSignNotEnoughSpace
	}
	if tokenLen > 0 {
		ts.signatureSize = tokenLen + 128
	}
	if sig.Contents != nil {
		existing := sig.Contents.Bytes()
		copy(existing, token)
		token = existing
	}
	sig.Contents = core.MakeHexString(string(token))
	return nil
}

// Validate implementation of the SignatureHandler interface
// This check is impossible without checking the document's content.
// Please, use ValidateWithOpts with the PdfParser.
func (h *DocMDPHandler) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	return model.SignatureValidationResult{}, errors.New("impossible validation without parse")
}
