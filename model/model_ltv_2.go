package model

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	_ "image/gif"
	"math/big"
	"slices"

	"golang.org/x/crypto/ocsp"
)

type parsedCRL struct {
	*x509.RevocationList
	number *big.Int
	idp    *issuingDistributionPoint
	idpRaw []byte
	memo   map[[32]byte]bool
}

func (c *parsedCRL) signedBy(cert *x509.Certificate, data [32]byte) bool {
	ok, found := c.memo[data]
	if !found {
		ok = c.CheckSignatureFrom(cert) == nil
		if c.memo == nil {
			c.memo = map[[32]byte]bool{}
		}
		c.memo[data] = ok
	}
	return ok
}

func (c *parsedCRL) covers(cert *x509.Certificate) bool {
	idp := c.idp
	if idp == nil {
		return true
	}
	if idp.OnlyContainsAttributeCerts || idp.OnlySomeReasons.BitLength > 0 {
		return false
	}
	ok := cert.BasicConstraintsValid && cert.IsCA
	if idp.OnlyContainsUserCerts && ok || idp.OnlyContainsCACerts && !ok {
		return false
	}
	if len(idp.DistributionPoint.RelativeName.FullBytes) > 0 {
		return false
	}
	if len(idp.DistributionPoint.FullName) == 0 {
		return true
	}
	for _, fullName := range idp.DistributionPoint.FullName {
		if fullName.Class != asn1.ClassContextSpecific || fullName.Tag != 6 {
			continue
		}
		if slices.Contains(cert.CRLDistributionPoints, string(fullName.Bytes)) {
			return true
		}
	}
	return false
}

func (c *parsedCRL) newerThan(crl *parsedCRL) bool {
	if c.Number != nil && crl.Number != nil && bytes.Equal(c.idpRaw, crl.idpRaw) {
		return c.Number.Cmp(crl.Number) > 0
	}
	return c.ThisUpdate.After(crl.ThisUpdate)
}

func (c *parsedCRL) isDelta() bool { return c.number != nil }

func (c *parsedCRL) entryFor(cert *x509.Certificate) *x509.RevocationListEntry {
	if c.idp != nil && c.idp.OnlyContainsAttributeCerts {
		return nil
	}
	ok := true
	for i := range c.RevokedCertificateEntries {
		revokedCertificateEntry := &c.RevokedCertificateEntries[i]
		for _, extension := range revokedCertificateEntry.Extensions {
			if extension.Id.Equal(oidCertificateIssuer) {
				ok = matchesAuthorityCertIssuer(extension.Value, cert.RawIssuer)
			}
		}
		if ok && revokedCertificateEntry.SerialNumber.Cmp(cert.SerialNumber) == 0 {
			return revokedCertificateEntry
		}
	}
	return nil
}

type ocspAnswer struct {
	*ocsp.Response
	online     bool
	authorised bool
}

func ocspResponderAuthorizedByIssuer(resp *ocsp.Response, cert, cert2 *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert2) {
		return true
	}
	if certificate.Equal(cert) || !hasExtKeyUsage(certificate, x509.ExtKeyUsageOCSPSigning) {
		return false
	}
	return !resp.ProducedAt.Before(certificate.NotBefore) && !resp.ProducedAt.After(certificate.NotAfter)
}

func ocspResponderAuthorized(resp *ocsp.Response, cert *x509.Certificate) bool {
	certificate := resp.Certificate
	if certificate == nil || certificate.Equal(cert) {
		return true
	}
	if resp.ThisUpdate.After(resp.ProducedAt) {
		return false
	}
	return resp.NextUpdate.IsZero() || !resp.NextUpdate.After(certificate.NotAfter)
}

type (
	ocspEvidence struct {
		raw    []byte
		digest [32]byte
		online bool
	}
	crlEvidence struct {
		*parsedCRL
		online bool
	}
)

func ocspEvidenceFromDER(dataList [][]byte) []ocspEvidence {
	ocsps := make([]ocspEvidence, 0, len(dataList))
	for _, data := range dataList {
		ocsps = append(ocsps, newOCSPEvidence(data, false))
	}
	return ocsps
}

func newOCSPEvidence(body []byte, ok bool) ocspEvidence {
	return ocspEvidence{raw: body, digest: sha256.Sum256(body), online: ok}
}
