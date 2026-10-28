package model

import (
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model/internal/sigattr"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"github.com/unidoc/pkcs7"
	"golang.org/x/crypto/ocsp"
)

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

func (t *trustEvaluator) checkChainRevocation(trustChain []*x509.Certificate, validatedAt time.Time, ok bool, evidence revocationEvidence) chainRevocation {
	revocation := chainRevocation{status: RevocationStatusGood}
	for i := 0; i+1 < len(trustChain); i++ {
		cert, cert2 := trustChain[i], trustChain[i+1]
		checkCertRevocation := t.checkCertRevocation(cert, cert2, validatedAt, ok, evidence)
		if !checkCertRevocation.settled() && t.opts.Revocation == RevocationCheckOnline {
			var strs []string
			checkCertRevocation, strs = t.checkOnline(checkCertRevocation, cert, cert2, validatedAt, ok, evidence)
			revocation.warnings = append(revocation.warnings, strs...)
		}
		if i == 0 {
			revocation.leaf = checkCertRevocation
		}
		switch checkCertRevocation.status {
		case RevocationStatusRevoked:
			revocation.status = RevocationStatusRevoked
			revocation.revokedCert = cert
			revocation.revokedAt = checkCertRevocation.revokedAt
			return revocation
		case RevocationStatusUnknown:
			revocation.status = RevocationStatusUnknown
		}
	}
	return revocation
}

func (t *trustEvaluator) checkOnline(revocation certRevocation, cert, cert2 *x509.Certificate, t2 time.Time, ok bool, evidence revocationEvidence) (certRevocation, []string) {
	var strs []string
	evidence = revocationEvidence{ocsps: append([]ocspEvidence(nil), evidence.ocsps...), crls: append([]crlEvidence(nil), evidence.crls...)}
	ocspClient := t.opts.OCSPClient
	if ocspClient == nil {
		ocspClient = sigutil.NewOCSPClient()
	}
	sum256, data := sha256.Sum256(cert.Raw), sha256.Sum256(cert2.Raw)
	for _, ocspServer := range cert.OCSPServer {
		if revocation.settled() {
			return revocation, strs
		}
		if !isHTTPURL(ocspServer) {
			continue
		}
		fetch := t.fetch(fetchKey{url: ocspServer, certHash: sum256, issuerHash: data}, func() fetchResult {
			_, data, err := ocspClient.MakeRequest(ocspServer, cert, cert2)
			return fetchResult{body: data, err: err}
		})
		if fetch.err != nil {
			strs = append(strs, fmt.Sprintf("OCSP request to %s failed: %v", ocspServer, fetch.err))
			continue
		}
		evidence.ocsps = append(evidence.ocsps, newOCSPEvidence(fetch.body, true))
		revocation = t.checkCertRevocation(cert, cert2, t2, ok, evidence)
	}
	crlClient := t.opts.CRLClient
	if crlClient == nil {
		crlClient = sigutil.NewCRLClient()
	}
	for _, crlDistributionPoint := range cert.CRLDistributionPoints {
		if revocation.settled() {
			return revocation, strs
		}
		if !isHTTPURL(crlDistributionPoint) {
			continue
		}
		fetch := t.fetch(fetchKey{url: crlDistributionPoint}, func() fetchResult {
			request, err := crlClient.MakeRequest(crlDistributionPoint, cert)
			if err != nil {
				return fetchResult{err: err}
			}
			return fetchResult{crl: t.parseCRL(request)}
		})
		if fetch.err != nil {
			strs = append(strs, fmt.Sprintf("CRL request to %s failed: %v", crlDistributionPoint, fetch.err))
			continue
		}
		if fetch.crl == nil {
			strs = append(strs, fmt.Sprintf("CRL from %s is not a valid CRL", crlDistributionPoint))
			continue
		}
		evidence.crls = append(evidence.crls, crlEvidence{parsedCRL: fetch.crl, online: true})
		revocation = t.checkCertRevocation(cert, cert2, t2, ok, evidence)
	}
	return revocation, strs
}

func (t *trustEvaluator) checkCertRevocation(cert, cert2 *x509.Certificate, t2 time.Time, ok bool, evidence revocationEvidence) certRevocation {
	sum256, data := sha256.Sum256(cert.Raw), sha256.Sum256(cert2.Raw)
	var answers []ocspAnswer
	numByResp := map[*ocsp.Response]int{}
	for _, ocspResp := range evidence.ocsps {
		resp := t.parseOCSP(ocspKey{responseHash: ocspResp.digest, certHash: sum256, issuerHash: data}, ocspResp.raw, cert, cert2)
		if resp == nil {
			continue
		}
		if n, ok := numByResp[resp]; ok {
			answers[n].online = answers[n].online || ocspResp.online
			continue
		}
		if !ocspResponderAuthorizedByIssuer(resp, cert, cert2) {
			continue
		}
		numByResp[resp] = len(answers)
		answers = append(answers, ocspAnswer{Response: resp, online: ocspResp.online, authorised: ocspResponderAuthorized(resp, cert2)})
	}
	var crls []crlEvidence
	numByCrl := map[*parsedCRL]int{}
	for _, crl := range evidence.crls {
		if n, ok := numByCrl[crl.parsedCRL]; ok {
			crls[n].online = crls[n].online || crl.online
			continue
		}
		if !equalDistinguishedNames(crl.RawIssuer, cert.RawIssuer) || !crl.signedBy(cert2, data) {
			continue
		}
		numByCrl[crl.parsedCRL] = len(crls)
		crls = append(crls, crl)
	}
	for _, answer := range answers {
		if answer.Status == ocsp.Revoked && answer.RevocationReason != ocsp.CertificateHold && !answer.RevokedAt.After(t2) {
			return certRevocation{status: RevocationStatusRevoked, revokedAt: answer.RevokedAt, ocspFound: true}
		}
	}
	if revocationEntry := findRevocationEntry(crls, cert, t2); revocationEntry != nil {
		return certRevocation{status: RevocationStatusRevoked, revokedAt: revocationEntry.RevocationTime, crlFound: true}
	}
	if report, isReport := findHoldReport(answers, crls, cert, t2); isReport {
		t3 := t2
		if ok {
			t3 = time.Time{}
		}
		switch {
		case report.lifted(answers, crls, cert, t3):
		case !ok && report.thisUpdate.Before(t2) && report.lifted(answers, crls, cert, time.Time{}):
			return certRevocation{status: RevocationStatusUnknown, ocspFound: report.crl == nil, crlFound: report.crl != nil}
		default:
			return certRevocation{status: RevocationStatusRevoked, revokedAt: report.revokedAt, onHold: true, ocspFound: report.crl == nil, crlFound: report.crl != nil}
		}
	}
	revocation := certRevocation{status: RevocationStatusUnknown}
	for _, answer := range answers {
		if answer.Status == ocsp.Unknown || answer.Status == ocsp.Revoked && !answer.RevokedAt.After(t2) {
			continue
		}
		if answer.authorised && crlIsCurrent(answer.ThisUpdate, answer.NextUpdate, cert, t2, answer.online) {
			revocation.status, revocation.ocspFound = RevocationStatusGood, true
		}
	}
	if anyCRLCovers(crls, cert, t2) {
		revocation.status, revocation.crlFound = RevocationStatusGood, true
	}
	return revocation
}

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
