package model

import (
	"crypto/x509"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// ValidateLinearization validates the PDF's linearization structure against
// ISO 32000-2 Annex F, returning a slice of violations (empty if it passes).
// Returns [core.LinErrNotLinearized] if the PDF is not linearized.
func (r *PdfReader) ValidateLinearization() ([]core.LinearizationViolation, error) {
	return r.parser.ValidateLinearization()
}

// ValidateLinearizationTrailerID runs only the linearization /ID consistency
// check (first-xref trailer vs main trailer), without the full Annex F battery.
// Returns nil if the PDF is not linearized or the /ID values match.
func (r *PdfReader) ValidateLinearizationTrailerID() *core.LinearizationViolation {
	return r.parser.ValidateLinearizationTrailerID()
}

// ValidateSignatures validates the integrity of the digital signatures in
// the document. No certificate trust is established: every result carries
// IsTrusted false. Use ValidateSignaturesWithOpts with trust roots to decide
// whether the signers are trusted.
func (r *PdfReader) ValidateSignatures(handlers []SignatureHandler) ([]SignatureValidationResult, error) {
	return r.ValidateSignaturesWithOpts(handlers, nil)
}

// ValidateSignaturesWithOpts validates the digital signatures in the
// document: integrity through the matching handler, then certificate trust
// according to opts. A nil opts performs the integrity check only.
func (r *PdfReader) ValidateSignaturesWithOpts(handlers []SignatureHandler, opts *SignatureValidationOpts) ([]SignatureValidationResult, error) {
	if r.AcroForm == nil {
		return nil, nil
	}
	if r.AcroForm.Fields == nil {
		return nil, nil
	}
	type sigFieldPair struct {
		signature *PdfSignature
		field     *PdfField
		handler   SignatureHandler
	}
	var sigFieldPairs []*sigFieldPair
	for _, allField := range r.AcroForm.AllFields() {
		if allField.V == nil {
			continue
		}
		if dict, ok := core.GetDict(allField.V); ok {
			if s, ok := core.GetNameVal(dict.Get("Type")); ok && (s == "Sig" || s == "DocTimeStamp") {
				indirect, ok := core.GetIndirect(allField.V)
				if !ok {
					common.Log.Debug("ERROR: Signature container is nil")
					return nil, ErrTypeCheck
				}
				sig, err := r.newPdfSignatureFromIndirect(indirect)
				if err != nil {
					return nil, err
				}
				var signatureHandler SignatureHandler
				for _, handler := range handlers {
					if handler.IsApplicable(sig) {
						signatureHandler = handler
						break
					}
				}
				sigFieldPairs = append(sigFieldPairs, &sigFieldPair{signature: sig, field: allField, handler: signatureHandler})
			}
		}
	}
	evaluator := &trustEvaluator{reader: r, opts: opts}
	var results []SignatureValidationResult
	fn := func(sig *PdfSignature, result SignatureValidationResult, certs []*x509.Certificate) {
		evaluator.evaluate(sig, &result, certs)
		results = append(results, result)
	}
	for _, sigFieldPair := range sigFieldPairs {
		result := SignatureValidationResult{IsSigned: true, Fields: []*PdfField{sigFieldPair.field}}
		byteRange := sigFieldPair.signature.ByteRange
		var nums []int64
		var err error
		if byteRange != nil {
			nums, err = parseByteRange(byteRange, r.parser.Size())
			if err == nil {
				fillSignatureCoverage(r.parser, nums, sigFieldPair.signature.Contents, &result)
			}
		}
		if certs, err := sigFieldPair.signature.GetCerts(); err != nil {
			if common.Log.IsLogLevel(common.LogLevelDebug) {
				common.Log.Debug("ERROR: unable to extract signature certificates: %v", err)
			}
		} else {
			result.Certificates = certs
		}
		if sigFieldPair.handler == nil {
			result.Errors = append(result.Errors, "handler not set")
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		digest, digestErr := sigFieldPair.handler.NewDigest(sigFieldPair.signature)
		if digestErr != nil {
			result.Errors = append(result.Errors, "digest error", digestErr.Error())
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		if byteRange == nil {
			result.Errors = append(result.Errors, "ByteRange not set")
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		if err != nil {
			result.Errors = append(result.Errors, err.Error())
			fn(sigFieldPair.signature, result, nil)
			continue
		}
		for i := 0; i < len(nums); i += 2 {
			bytesAt, err := r.parser.ReadBytesAt(nums[i], nums[i+1])
			if err != nil {
				return nil, err
			}
			digest.Write(bytesAt)
		}
		var result2 SignatureValidationResult
		if signatureHandlerDocMDP, ok := sigFieldPair.handler.(SignatureHandlerDocMDP); ok {
			result2, digestErr = signatureHandlerDocMDP.ValidateWithOpts(sigFieldPair.signature, digest, SignatureHandlerDocMDPParams{Parser: r.parser})
		} else {
			result2, digestErr = sigFieldPair.handler.Validate(sigFieldPair.signature, digest)
		}
		if digestErr != nil {
			common.Log.Debug("ERROR: %v (%T) - skip", digestErr, sigFieldPair.handler)
			result2.Errors = append(result2.Errors, digestErr.Error())
		}
		result2.copyCoverageFrom(&result)
		certificates := result2.Certificates
		if result2.Certificates == nil {
			result2.Certificates = result.Certificates
		}
		result2.Name = sigFieldPair.signature.Name.Decoded()
		result2.Reason = sigFieldPair.signature.Reason.Decoded()
		if sigFieldPair.signature.M != nil {
			if date, err := NewPdfDate(sigFieldPair.signature.M.String()); err != nil {
				common.Log.Debug("ERROR: %v", err)
				result2.Errors = append(result2.Errors, err.Error())
			} else {
				result2.Date = date
			}
		}
		result2.ContactInfo = sigFieldPair.signature.ContactInfo.Decoded()
		result2.Location = sigFieldPair.signature.Location.Decoded()
		result2.Fields = result.Fields
		fn(sigFieldPair.signature, result2, certificates)
	}
	return results, nil
}
