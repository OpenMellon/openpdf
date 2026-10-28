package model

import (
	"crypto/sha256"
	"crypto/x509"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/openpdf/model/sigutil"
	"golang.org/x/crypto/ocsp"
)

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
