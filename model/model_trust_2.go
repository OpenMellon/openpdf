package model

import (
	"bytes"
	"crypto/x509"
	"fmt"
	_ "image/gif"
	"time"

	"github.com/OpenMellon/timestamp"
	"github.com/unidoc/pkcs7"
)

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
