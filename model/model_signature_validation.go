package model

import (
	"bytes"
	"crypto/x509"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/openpdf/model/mdp"
	"github.com/OpenMellon/openpdf/model/sigutil"
)

// SignatureValidationResult defines the response from the signature validation handler.
type SignatureValidationResult struct {
	// List of errors when validating the signature.
	Errors   []string
	IsSigned bool

	// IsVerified is true when the signature value matches the signed
	// document bytes under the signer's public key. It is an integrity
	// verdict only: the key comes from the certificate embedded in the
	// document, so a self-signed certificate under any name verifies. It
	// says nothing about who the signer is; see IsTrusted for that.
	IsVerified bool

	// IsTrusted is true when the signer certificate chains to one of the
	// trust roots given in SignatureValidationOpts at the validation time and
	// may sign documents:
	//   - its key usage permits digital signatures;
	//   - its extended key usage, if any, includes anyExtendedKeyUsage,
	//     emailProtection, codeSigning or a document signing usage (RFC 9336,
	//     Microsoft or Adobe), so a TLS-only certificate is not trusted;
	//   - every intermediate CA that has an extended key usage allows the same.
	// For a document timestamp the signer and every CA of its chain, the root
	// included, must allow time stamping instead. It is only ever set by
	// PdfReader.ValidateSignaturesWithOpts; without roots it stays false and
	// TrustErrors says why.
	IsTrusted bool

	// TrustErrors lists the reasons IsTrusted is false. It is empty when
	// IsTrusted is true.
	TrustErrors []string

	// TrustWarnings lists problems met while establishing trust that did
	// not change the verdict, such as an unreachable revocation endpoint or
	// an unknown revocation status that was not required.
	TrustWarnings []string

	// TrustChain is the verified path from the signer certificate to the
	// trust root, nil when no such path exists. It stays set when a
	// revocation check later cleared IsTrusted, so the revoked chain can be
	// inspected.
	TrustChain []*x509.Certificate

	// ValidatedAt is the instant the certificate chain was evaluated at.
	// Zero when the evaluation stopped before the chain was checked, with
	// the reason in TrustErrors: no roots, failed integrity, no signer
	// certificate, or a key usage that rules the signer out.
	ValidatedAt time.Time

	// RevocationStatus is the outcome of the revocation check selected by
	// SignatureValidationOpts.Revocation.
	RevocationStatus RevocationStatus

	// RevocationTime is when the chain certificate was revoked. Set only
	// when RevocationStatus is RevocationStatusRevoked.
	RevocationTime time.Time
	Fields         []*PdfField
	Name           string
	Date           PdfDate
	Reason         string
	Location       string
	ContactInfo    string
	DiffResults    *mdp.DiffResults

	// IsCrlFound and IsOcspFound report whether a CRL, respectively an OCSP
	// response, covering the signer certificate at the validation time was
	// found and checked. When no revocation check ran, because none was
	// requested or the chain was not trusted, they keep the handler's
	// value: the PAdES handler sets them to whether the signature carries a
	// revocation attribute at all.
	IsCrlFound  bool
	IsOcspFound bool

	// IsCoveringEntireFile is true if the signature's ByteRange, plus the
	// /Contents gap it excludes, spans the whole underlying file — no bytes
	// were appended after this signature was applied.
	IsCoveringEntireFile bool

	// IsCoveringEntireRevision is true if the signature covers everything up
	// to the end of the document revision it was applied in, even when later
	// incremental-update revisions were appended afterward (the common, valid
	// incremental-signing case). IsCoveringEntireFile implies it.
	IsCoveringEntireRevision bool

	// RevisionsAfterSignature is the number of incremental-update revisions
	// appended to the file after the revision covered by this signature.
	// Meaningful only when IsCoveringEntireRevision is true: zero then means
	// the signature covers the latest revision, and the field stays zero when
	// no revision is covered at all.
	RevisionsAfterSignature int

	// ContentsGapOnly is true if the only bytes excluded from the signed
	// range are exactly the signature's own /Contents hex string, including
	// its <> delimiters. False indicates the ByteRange excludes something
	// else too — a red flag that unsigned content may be hiding inside the
	// nominally signed span rather than only appended at the end of the
	// file. It is also false for unusual but benign layouts (a ByteRange not
	// starting at offset 0, or a non-hexadecimal /Contents).
	ContentsGapOnly  bool
	byteRangeChecked bool

	// Certificates is the certificate chain embedded in the signature, with
	// the signer's certificate first. Handlers set it to the chain they
	// verified the signature with; otherwise it is filled best-effort from
	// PdfSignature.GetCerts and stays nil when no chain could be extracted.
	// Only a handler-reported chain is used for trust (see TrustChain).
	Certificates []*x509.Certificate

	// GeneralizedTime is the time at which the time-stamp token has been created by the TSA (RFC 3161).
	GeneralizedTime time.Time
}

func (r *SignatureValidationResult) copyCoverageFrom(result *SignatureValidationResult) {
	r.IsCoveringEntireFile = result.IsCoveringEntireFile
	r.IsCoveringEntireRevision = result.IsCoveringEntireRevision
	r.RevisionsAfterSignature = result.RevisionsAfterSignature
	r.ContentsGapOnly = result.ContentsGapOnly
	r.byteRangeChecked = result.byteRangeChecked
}

// SignerCertificate returns the signer's (end-entity) certificate: the first
// entry of Certificates, or nil when the chain is empty.
func (r SignatureValidationResult) SignerCertificate() *x509.Certificate {
	if len(r.Certificates) == 0 {
		return nil
	}
	return r.Certificates[0]
}

func (r SignatureValidationResult) String() string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Name: %s\n", r.Name)
	if r.Date.year > 0 {
		fmt.Fprintf(&buf, "Date: %s\n", r.Date.ToGoTime().String())
	} else {
		buf.WriteString("Date not specified\n")
	}
	if len(r.Reason) > 0 {
		fmt.Fprintf(&buf, "Reason: %s\n", r.Reason)
	} else {
		buf.WriteString("No reason specified\n")
	}
	if len(r.Location) > 0 {
		fmt.Fprintf(&buf, "Location: %s\n", r.Location)
	} else {
		buf.WriteString("Location not specified\n")
	}
	if len(r.ContactInfo) > 0 {
		fmt.Fprintf(&buf, "Contact Info: %s\n", r.ContactInfo)
	} else {
		buf.WriteString("Contact info not specified\n")
	}
	fmt.Fprintf(&buf, "Fields: %d\n", len(r.Fields))
	if r.IsSigned {
		buf.WriteString("Signed: Document is signed\n")
	} else {
		buf.WriteString("Signed: Not signed\n")
	}
	if r.IsVerified {
		buf.WriteString("Signature validation: Is valid\n")
	} else {
		buf.WriteString("Signature validation: Is invalid\n")
	}
	if r.IsTrusted {
		fmt.Fprintf(&buf, "Trusted: Certificate is trusted (chain of %d certificate(s))\n", len(r.TrustChain))
	} else {
		buf.WriteString("Trusted: Untrusted certificate\n")
	}
	for _, trustError := range r.TrustErrors {
		fmt.Fprintf(&buf, "\t%s\n", trustError)
	}
	for _, trustWarning := range r.TrustWarnings {
		fmt.Fprintf(&buf, "Trust warning: %s\n", trustWarning)
	}
	if !r.ValidatedAt.IsZero() {
		fmt.Fprintf(&buf, "Validated at: %s\n", r.ValidatedAt.UTC().Format(time.RFC3339))
	}
	if r.RevocationStatus != RevocationStatusNotChecked {
		fmt.Fprintf(&buf, "Revocation status: %s\n", r.RevocationStatus)
	}
	if !r.RevocationTime.IsZero() {
		fmt.Fprintf(&buf, "Revocation time: %s\n", r.RevocationTime.UTC().Format(time.RFC3339))
	}
	if signerCertificate := r.SignerCertificate(); signerCertificate != nil {
		fmt.Fprintf(&buf, "Certificate: %s\n", signerCertificate.Subject.String())
		fmt.Fprintf(&buf, "Certificate issuer: %s\n", signerCertificate.Issuer.String())
		fmt.Fprintf(&buf, "Certificate validity: %s - %s\n", signerCertificate.NotBefore.UTC().Format(time.RFC3339), signerCertificate.NotAfter.UTC().Format(time.RFC3339))
		fmt.Fprintf(&buf, "Certificate chain: %d certificate(s)\n", len(r.Certificates))
	} else {
		buf.WriteString("Certificate: not available\n")
	}
	if !r.GeneralizedTime.IsZero() {
		fmt.Fprintf(&buf, "GeneralizedTime: %s\n", r.GeneralizedTime.String())
	}
	if r.byteRangeChecked {
		if r.IsCoveringEntireFile {
			buf.WriteString("Coverage: Entire file is covered by the signature\n")
		} else if r.IsCoveringEntireRevision {
			fmt.Fprintf(&buf, "Coverage: Entire revision is covered by the signature (%d revision(s) appended after signing)\n", r.RevisionsAfterSignature)
		} else {
			buf.WriteString("Coverage: Signed range stops before the end of its revision\n")
		}
		if r.ContentsGapOnly {
			buf.WriteString("ByteRange gap: matches /Contents\n")
		} else {
			buf.WriteString("ByteRange gap: does not exactly match /Contents\n")
		}
	}
	if r.DiffResults != nil {
		fmt.Fprintf(&buf, "diff is permitted: %v\n", r.DiffResults.IsPermitted())
		if len(r.DiffResults.Warnings) > 0 {
			buf.WriteString("MDP warnings:\n")
			for _, warning := range r.DiffResults.Warnings {
				fmt.Fprintf(&buf, "\t%s\n", warning)
			}
		}
		if len(r.DiffResults.Errors) > 0 {
			buf.WriteString("MDP errors:\n")
			for _, diffResult := range r.DiffResults.Errors {
				fmt.Fprintf(&buf, "\t%s\n", diffResult)
			}
		}
	}
	if r.IsCrlFound {
		buf.WriteString("Revocation data: CRL found\n")
	} else {
		buf.WriteString("Revocation data: CRL not found\n")
	}
	if r.IsOcspFound {
		buf.WriteString("Revocation data: OCSP found\n")
	} else {
		buf.WriteString("Revocation data: OCSP not found\n")
	}
	return buf.String()
}

// SignatureValidationOpts configures the certificate trust checks that
// PdfReader.ValidateSignaturesWithOpts performs after a handler has verified
// the integrity of a signature. Trust anchors always come from the caller;
// certificates embedded in the document are used as intermediates only.
type SignatureValidationOpts struct {
	// Roots is the set of trusted anchors. When nil no trust check is
	// performed and SignatureValidationResult.IsTrusted stays false. Use
	// the authorities you accept for document signing; the system pool
	// would make the holder of any TLS server certificate a trusted signer.
	Roots *x509.CertPool

	// Intermediates supplements the certificates embedded in the document
	// when building the chain from the signer to a root.
	Intermediates []*x509.Certificate

	// ValidationTime is the instant the chain and revocation data are
	// evaluated at. Zero selects the time asserted by a signature timestamp
	// token whose TSA chains to Roots, else the current time. A document
	// timestamp is judged at the current time, never at the time its own
	// token asserts. The signature dictionary's /M date is never used
	// because the signer chooses it; a caller who accepts it can pass it
	// here.
	ValidationTime time.Time

	// Revocation selects how the revocation status of the chain is
	// established. The default performs no revocation check.
	Revocation RevocationCheck

	// RequireRevocationStatus makes an unknown revocation status fail
	// trust, with the reason in TrustErrors. By default an unknown status
	// leaves IsTrusted unchanged and is reported in TrustWarnings.
	RequireRevocationStatus bool

	// CRLClient and OCSPClient perform the online lookups of
	// RevocationCheckOnline. nil selects the sigutil defaults, which use a
	// 5 second timeout per request.
	CRLClient  *sigutil.CRLClient
	OCSPClient *sigutil.OCSPClient
}
