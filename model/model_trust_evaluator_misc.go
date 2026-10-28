package model

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/internal/sigattr"
	"github.com/OpenMellon/pkcs7"
	"github.com/OpenMellon/timestamp"
	"golang.org/x/crypto/ocsp"
)

func (t *trustEvaluator) trustedSignatureTimestamp(p7 *pkcs7.PKCS7, certs []*x509.Certificate, evidence revocationEvidence) (time.Time, []string, []string) {
	if p7 == nil || len(p7.Signers) != 1 {
		return time.Time{}, nil, nil
	}
	signer := p7.Signers[0]
	var strs []string
	for _, unauthenticatedAttribute := range signer.UnauthenticatedAttributes {
		if !unauthenticatedAttribute.Type.Equal(pkcs7.OIDAttributeTimeStampToken) {
			continue
		}
		verifySignatureTimestamp, strs2, err := t.verifySignatureTimestamp(unauthenticatedAttribute.Value.Bytes, signer.EncryptedDigest, p7.Certificates, certs, evidence)
		if err != nil {
			strs = append(strs, err.Error())
			continue
		}
		return verifySignatureTimestamp, strs2, nil
	}
	return time.Time{}, nil, strs
}

func (t *trustEvaluator) vriCertificates(s string) []*x509.Certificate {
	if t.reader.DSS == nil {
		return nil
	}
	vri := t.reader.DSS.VRI[s]
	if vri == nil {
		return nil
	}
	return parseCertificates(vri.Cert)
}

func (t *trustEvaluator) evaluateRevocation(result *SignatureValidationResult, ok bool, evidence revocationEvidence) {
	result.RevocationStatus = RevocationStatusUnknown
	if len(result.TrustChain) < 2 {
		t.reportUnknownRevocation(result, "revocation status unknown: the signer certificate is a trust root and has no issuer to ask")
		return
	}
	checkChainRevocation := t.checkChainRevocation(result.TrustChain, result.ValidatedAt, ok, evidence)
	result.IsOcspFound = checkChainRevocation.leaf.ocspFound
	result.IsCrlFound = checkChainRevocation.leaf.crlFound
	result.RevocationStatus = checkChainRevocation.status
	switch checkChainRevocation.status {
	case RevocationStatusRevoked:
		result.RevocationTime = checkChainRevocation.revokedAt
		result.IsTrusted = false
		result.TrustErrors = append(result.TrustErrors, fmt.Sprintf("certificate %q revoked at %s", checkChainRevocation.revokedCert.Subject.String(), checkChainRevocation.revokedAt.UTC().Format(time.RFC3339)))
	case RevocationStatusGood:
		result.TrustWarnings = append(result.TrustWarnings, checkChainRevocation.warnings...)
	default:
		t.reportUnknownRevocation(result, append(checkChainRevocation.warnings, revocationUnknownMessage)...)
	}
}

type trustEvaluator struct {
	reader            *PdfReader
	opts              *SignatureValidationOpts
	dssCerts          []*x509.Certificate
	dssCertsLoaded    bool
	dssEvidence       revocationEvidence
	dssEvidenceLoaded bool
	crlByHash         map[[32]byte]*parsedCRL
	crlByStream       map[*core.PdfObjectStream]*parsedCRL
	ocspCache         map[ocspKey]*ocsp.Response
	fetchCache        map[fetchKey]fetchResult
}

func (t *trustEvaluator) embeddedRevocationEvidence(p7 *pkcs7.PKCS7, s string) revocationEvidence {
	var evidence revocationEvidence
	if p7 != nil {
		var revocationInfoArchival sigattr.RevocationInfoArchival
		if err := p7.UnmarshalSignedAttribute(pkcs7.OIDAttributeAdobeRevocation, &revocationInfoArchival); err == nil {
			dataList := make([][]byte, 0, len(revocationInfoArchival.Ocsp))
			for _, rawValue := range revocationInfoArchival.Ocsp {
				dataList = append(dataList, rawValue.FullBytes)
			}
			evidence.ocsps = ocspEvidenceFromDER(dataList)
			var crls []*parsedCRL
			for _, crl := range revocationInfoArchival.Crl {
				if crl2 := t.parseCRL(crl.FullBytes); crl2 != nil {
					crls = append(crls, crl2)
				}
			}
			evidence.crls = crlEvidenceFromParsed(crls)
		}
	}
	dss := t.reader.DSS
	if dss == nil {
		return evidence
	}
	if vri := dss.VRI[s]; vri != nil {
		evidence.ocsps = append(evidence.ocsps, ocspEvidenceFromDER(decodeStreams(vri.OCSP))...)
		evidence.crls = append(evidence.crls, crlEvidenceFromParsed(t.streamCRLs(vri.CRL))...)
	}
	if !t.dssEvidenceLoaded {
		t.dssEvidenceLoaded = true
		t.dssEvidence = revocationEvidence{ocsps: ocspEvidenceFromDER(decodeStreams(dss.OCSPs)), crls: crlEvidenceFromParsed(t.streamCRLs(dss.CRLs))}
	}
	evidence.ocsps = append(evidence.ocsps, t.dssEvidence.ocsps...)
	evidence.crls = append(evidence.crls, t.dssEvidence.crls...)
	return evidence
}

func (t *trustEvaluator) sharedCertificates(certs []*x509.Certificate) []*x509.Certificate {
	certs2 := append([]*x509.Certificate(nil), certs...)
	certs2 = append(certs2, t.dssCertificates()...)
	return append(certs2, t.opts.Intermediates...)
}

func (t *trustEvaluator) streamCRLs(crLs []*core.PdfObjectStream) []*parsedCRL {
	var crls []*parsedCRL
	for _, crL := range crLs {
		crlByStream, ok := t.crlByStream[crL]
		if !ok {
			if dssStream, ok := decodeDSSStream(crL); ok {
				crlByStream = t.parseCRL(dssStream)
			}
			if t.crlByStream == nil {
				t.crlByStream = map[*core.PdfObjectStream]*parsedCRL{}
			}
			t.crlByStream[crL] = crlByStream
		}
		if crlByStream != nil {
			crls = append(crls, crlByStream)
		}
	}
	return crls
}

func (t *trustEvaluator) parseCRL(data []byte) *parsedCRL {
	sum256 := sha256.Sum256(data)
	if crlByHash, ok := t.crlByHash[sum256]; ok {
		return crlByHash
	}
	crl := parseCRL(data)
	if t.crlByHash == nil {
		t.crlByHash = map[[32]byte]*parsedCRL{}
	}
	t.crlByHash[sum256] = crl
	return crl
}

func (t *trustEvaluator) reportUnknownRevocation(result *SignatureValidationResult, strs ...string) {
	if t.opts.RequireRevocationStatus {
		result.TrustErrors = append(result.TrustErrors, strs...)
		result.IsTrusted = false
		return
	}
	result.TrustWarnings = append(result.TrustWarnings, strs...)
}

func (t *trustEvaluator) dssCertificates() []*x509.Certificate {
	if !t.dssCertsLoaded && t.reader.DSS != nil {
		t.dssCertsLoaded = true
		t.dssCerts = parseCertificates(t.reader.DSS.Certs)
	}
	return t.dssCerts
}

func (t *trustEvaluator) fetch(key fetchKey, fn func() fetchResult) fetchResult {
	if fetchCache, ok := t.fetchCache[key]; ok {
		return fetchCache
	}
	result := fn()
	if t.fetchCache == nil {
		t.fetchCache = map[fetchKey]fetchResult{}
	}
	t.fetchCache[key] = result
	return result
}

func (t *trustEvaluator) evaluate(sig *PdfSignature, result *SignatureValidationResult, certs []*x509.Certificate) {
	result.IsTrusted = false
	result.TrustChain = nil
	result.TrustErrors = nil
	result.TrustWarnings = nil
	result.ValidatedAt = time.Time{}
	result.RevocationStatus = RevocationStatusNotChecked
	result.RevocationTime = time.Time{}
	if t.opts == nil || t.opts.Roots == nil {
		result.TrustErrors = append(result.TrustErrors, noTrustRootsMessage)
		return
	}
	if !result.IsVerified {
		result.TrustErrors = append(result.TrustErrors, "signature integrity not verified")
		return
	}
	if len(certs) == 0 {
		result.TrustErrors = append(result.TrustErrors, "signer certificate not identified by the signature handler")
		return
	}
	cert := certs[0]
	if cert.KeyUsage != 0 && cert.KeyUsage&(x509.KeyUsageDigitalSignature|x509.KeyUsageContentCommitment) == 0 {
		result.TrustErrors = append(result.TrustErrors, "signer certificate key usage does not permit digital signatures")
		return
	}
	s, _ := core.GetNameVal(sig.SubFilter)
	ok := s == "ETSI.RFC3161"
	if ok {
		if !hasExtKeyUsage(cert, x509.ExtKeyUsageTimeStamping) {
			result.TrustErrors = append(result.TrustErrors, "timestamp certificate lacks the time stamping extended key usage")
			return
		}
	} else if !hasAcceptableKeyUsage(cert) {
		result.TrustErrors = append(result.TrustErrors, "signer certificate extended key usage does not permit document signing")
		return
	}
	var p7 *pkcs7.PKCS7
	if sig.Contents != nil {
		p7, _ = pkcs7.Parse(sig.Contents.Bytes())
	}
	s2, _ := signatureContentsDigestHex(sig)
	vriCertificates := t.vriCertificates(s2)
	var evidence revocationEvidence
	if t.opts.Revocation != RevocationCheckNone {
		evidence = t.embeddedRevocationEvidence(p7, s2)
	}
	var found bool
	result.ValidatedAt, found = t.validationTime(p7, result, vriCertificates, evidence)
	usage := x509.ExtKeyUsageAny
	if ok {
		usage = x509.ExtKeyUsageTimeStamping
	}
	verify, err := cert.Verify(x509.VerifyOptions{Roots: t.opts.Roots, Intermediates: t.intermediatePool(vriCertificates, certs[1:]), CurrentTime: result.ValidatedAt, KeyUsages: []x509.ExtKeyUsage{usage}})
	if err != nil {
		result.TrustErrors = append(result.TrustErrors, err.Error())
		return
	}
	certs2 := verify[0]
	if !ok {
		if certs2 = findChainWithValidIntermediates(verify, hasAcceptableKeyUsage); certs2 == nil {
			result.TrustErrors = append(result.TrustErrors, "intermediate CA certificate extended key usage does not permit document signing")
			return
		}
	}
	result.TrustChain = certs2
	result.IsTrusted = true
	if t.opts.Revocation != RevocationCheckNone {
		t.evaluateRevocation(result, found, evidence)
	}
}

func (t *trustEvaluator) validationTime(p7 *pkcs7.PKCS7, result *SignatureValidationResult, certs []*x509.Certificate, evidence revocationEvidence) (t2 time.Time, ok bool) {
	if !t.opts.ValidationTime.IsZero() {
		return t.opts.ValidationTime, false
	}
	t2, strs, strs2 := t.trustedSignatureTimestamp(p7, certs, evidence)
	if !t2.IsZero() {
		result.TrustWarnings = append(result.TrustWarnings, strs...)
		return t2, false
	}
	for _, str := range strs2 {
		result.TrustWarnings = append(result.TrustWarnings, "signature timestamp ignored: "+str)
	}
	return time.Now(), true
}

func (t *trustEvaluator) intermediatePool(certs, certs2 []*x509.Certificate) *x509.CertPool {
	certPool := x509.NewCertPool()
	for _, cert := range certs2 {
		certPool.AddCert(cert)
	}
	for _, sharedCertificate := range t.sharedCertificates(certs) {
		certPool.AddCert(sharedCertificate)
	}
	return certPool
}

func (t *trustEvaluator) parseOCSP(key ocspKey, raw []byte, cert, cert2 *x509.Certificate) *ocsp.Response {
	if ocspCache, ok := t.ocspCache[key]; ok {
		return ocspCache
	}
	responseForCert, err := ocsp.ParseResponseForCert(raw, cert, cert2)
	if err != nil {
		responseForCert = nil
	}
	if t.ocspCache == nil {
		t.ocspCache = map[ocspKey]*ocsp.Response{}
	}
	t.ocspCache[key] = responseForCert
	return responseForCert
}

func (t *trustEvaluator) verifySignatureTimestamp(data, encryptedDigest []byte, certificates, certs []*x509.Certificate, evidence revocationEvidence) (time.Time, []string, error) {
	ts, err := timestamp.Parse(data)
	if err != nil {
		return time.Time{}, nil, err
	}
	if !ts.HashAlgorithm.Available() {
		return time.Time{}, nil, fmt.Errorf("unsupported hash algorithm in token")
	}
	digest := ts.HashAlgorithm.New()
	digest.Write(encryptedDigest)
	if !bytes.Equal(digest.Sum(nil), ts.HashedMessage) {
		return time.Time{}, nil, fmt.Errorf("token does not cover this signature")
	}
	p7, err := pkcs7.Parse(data)
	if err != nil {
		return time.Time{}, nil, err
	}
	if len(p7.Certificates) == 0 {
		if len(p7.Signers) != 1 {
			return time.Time{}, nil, fmt.Errorf("token must have exactly one signer")
		}
		issuerAndSerialNumber := p7.Signers[0].IssuerAndSerialNumber
		err := fmt.Errorf("TSA certificate neither in the token nor in the document")
	findTSACertificate:
		for _, certs2 := range [][]*x509.Certificate{certificates, t.sharedCertificates(certs)} {
			for _, cert := range certs2 {
				if bytes.Equal(cert.RawIssuer, issuerAndSerialNumber.IssuerName.FullBytes) && cert.SerialNumber.Cmp(issuerAndSerialNumber.SerialNumber) == 0 {
					p7.Certificates = []*x509.Certificate{cert}
					if err = p7.Verify(); err == nil {
						break findTSACertificate
					}
				}
			}
		}
		if err != nil {
			return time.Time{}, nil, err
		}
	}
	onlySigner := p7.GetOnlySigner()
	if onlySigner == nil {
		return time.Time{}, nil, fmt.Errorf("token must have exactly one signer")
	}
	if !hasExtKeyUsage(onlySigner, x509.ExtKeyUsageTimeStamping) {
		return time.Time{}, nil, fmt.Errorf("TSA certificate lacks the time stamping extended key usage")
	}
	t2 := time.Now()
	verify, err := onlySigner.Verify(x509.VerifyOptions{Roots: t.opts.Roots, Intermediates: t.intermediatePool(certs, append(append([]*x509.Certificate(nil), p7.Certificates...), certificates...)), CurrentTime: t2, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageTimeStamping}})
	if err != nil {
		return time.Time{}, nil, err
	}
	certs2 := verify[0]
	if t.opts.Revocation == RevocationCheckNone {
		return ts.Time, nil, nil
	}
	checkChainRevocation := t.checkChainRevocation(certs2, t2, true, evidence)
	switch checkChainRevocation.status {
	case RevocationStatusRevoked:
		return time.Time{}, nil, fmt.Errorf("TSA certificate %q revoked at %s", checkChainRevocation.revokedCert.Subject.String(), checkChainRevocation.revokedAt.UTC().Format(time.RFC3339))
	case RevocationStatusUnknown:
		if t.opts.RequireRevocationStatus {
			return time.Time{}, nil, fmt.Errorf("TSA revocation status unknown")
		}
		return ts.Time, append(checkChainRevocation.warnings, "TSA revocation status unknown"), nil
	}
	return ts.Time, checkChainRevocation.warnings, nil
}
