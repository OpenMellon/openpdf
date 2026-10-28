package model

import (
	"crypto/x509"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model/sigutil"
)

// NewLTV returns a new LTV client.
func NewLTV(appender *PdfAppender) (*LTV, error) {
	dss := appender.Reader.DSS
	if dss == nil {
		dss = NewDSS()
	}
	if err := dss.GenerateHashMaps(); err != nil {
		return nil, err
	}
	return &LTV{CertClient: sigutil.NewCertClient(), OCSPClient: sigutil.NewOCSPClient(), CRLClient: sigutil.NewCRLClient(), SkipExisting: true, appender: appender, dss: dss}, nil
}

// LTV represents an LTV (Long-Term Validation) client. It is used to LTV
// enable signatures by adding validation and revocation data (certificate,
// OCSP and CRL information) to the DSS dictionary of a PDF document.
//
// LTV is added through the DSS by:
//   - Adding certificates, OCSP and CRL information in the global scope of the
//     DSS. The global data is used for validating any of the signatures present
//     in the document.
//   - Adding certificates, OCSP and CRL information for a single signature,
//     through an entry in the VRI dictionary of the DSS. The added data is used
//     for validating that particular signature only. This is the recommended
//     method for adding validation data for a signature. However, this is not
//     possible in the same revision the signature is applied. Validation data
//     for a signature is added based on the Contents entry of the signature,
//     which is known only after the revision is written. Even if the Contents
//     are known (e.g. when signing externally), updating the DSS at that point
//     would invalidate the calculated signature. As a result, if adding LTV
//     in the same revision is a requirement, use the first method.
//     See LTV.EnableChain.
//
// The client applies both methods, when possible.
//
// If `LTV.SkipExisting` is set to true (the default), validations are
// not added for signatures which are already present in the VRI entry of the
// document's DSS dictionary.
type LTV struct {
	// CertClient is the client used to retrieve certificates.
	CertClient *sigutil.CertClient

	// OCSPClient is the client used to retrieve OCSP validation information.
	OCSPClient *sigutil.OCSPClient

	// CRLClient is the client used to retrieve CRL validation information.
	CRLClient *sigutil.CRLClient

	// SkipExisting specifies whether existing signature validations
	// should be skipped.
	SkipExisting bool
	appender     *PdfAppender
	dss          *DSS
}

func (l *LTV) getOCSPs(certs []*x509.Certificate, certByKey map[string]*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, ocspServer := range cert.OCSPServer {
			if l.CertClient.IsCA(cert) {
				continue
			}
			cert2, ok := certByKey[cert.Issuer.CommonName]
			if !ok {
				common.Log.Debug("WARN: Skipping OCSP request: issuer certificate not found")
				continue
			}
			_, data, err := l.OCSPClient.MakeRequest(ocspServer, cert, cert2)
			if err != nil {
				common.Log.Debug("WARN: OCSP request error: %v", err)
				continue
			}
			dataList = append(dataList, data)
		}
	}
	return dataList, nil
}

// EnableChain adds the specified certificate chain and validation data (OCSP
// and CRL information) for it to the global scope of the document DSS. The
// added data is used for validating any of the signatures present in the
// document. The LTV client attempts to build the certificate chain up to a
// trusted root by downloading any missing certificates.
func (l *LTV) EnableChain(chain []*x509.Certificate) error { return l.enable(nil, chain, "") }

// Enable LTV enables the specified signature. The signing certificate
// chain is extracted from the signature dictionary. Optionally, additional
// certificates can be specified through the `extraCerts` parameter.
// The LTV client attempts to build the certificate chain up to a trusted root
// by downloading any missing certificates.
func (l *LTV) Enable(sig *PdfSignature, extraCerts []*x509.Certificate) error {
	if err := l.validateSig(sig); err != nil {
		return err
	}
	s, err := signatureContentsDigestHex(sig)
	if err != nil {
		return err
	}
	if _, ok := l.dss.VRI[s]; ok && l.SkipExisting {
		return nil
	}
	certs, err := sig.GetCerts()
	if err != nil {
		return err
	}
	return l.enable(certs, extraCerts, s)
}

// EnableByName LTV enables the signature dictionary of the PDF AcroForm
// field identified the specified name. The signing certificate chain is
// extracted from the signature dictionary. Optionally, additional certificates
// can be specified through the `extraCerts` parameter. The LTV client attempts
// to build the certificate chain up to a trusted root by downloading any
// missing certificates.
func (l *LTV) EnableByName(name string, extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		if partialName := signatureField.PartialName(); partialName != name {
			continue
		}
		return l.Enable(signatureField.V, extraCerts)
	}
	return nil
}

func (l *LTV) buildCertChain(certs, certs2 []*x509.Certificate) ([]*x509.Certificate, map[string]*x509.Certificate, error) {
	certByKey := map[string]*x509.Certificate{}
	for _, cert := range certs {
		certByKey[cert.Subject.CommonName] = cert
	}
	certs3 := certs
	for _, cert := range certs2 {
		commonName := cert.Subject.CommonName
		if _, ok := certByKey[commonName]; ok {
			continue
		}
		certByKey[commonName] = cert
		certs3 = append(certs3, cert)
	}
	if len(certs3) == 0 {
		return nil, nil, ErrSignNoCertificates
	}
	var err error
	for cert := certs3[0]; cert != nil && !l.CertClient.IsCA(cert); {
		cert2, ok := certByKey[cert.Issuer.CommonName]
		if !ok {
			if cert2, err = l.CertClient.GetIssuer(cert); err != nil {
				common.Log.Debug("WARN: Could not retrieve certificate issuer: %v", err)
				break
			}
			certByKey[cert.Issuer.CommonName] = cert2
			certs3 = append(certs3, cert2)
		}
		cert = cert2
	}
	return certs3, certByKey, nil
}

func (l *LTV) enable(certs, chain []*x509.Certificate, s string) error {
	certChain, certByKey, err := l.buildCertChain(certs, chain)
	if err != nil {
		return err
	}
	dataList, err := l.getCerts(certChain)
	if err != nil {
		return err
	}
	var list, list2 [][]byte
	if l.OCSPClient != nil {
		list, err = l.getOCSPs(certChain, certByKey)
		if err != nil {
			return err
		}
	}
	if l.CRLClient != nil {
		list2, err = l.getCRLs(certChain)
		if err != nil {
			return err
		}
	}
	dss := l.dss
	addCerts, err := dss.AddCerts(dataList)
	if err != nil {
		return err
	}
	addOCSPs, err := dss.AddOCSPs(list)
	if err != nil {
		return err
	}
	addCRLs, err := dss.AddCRLs(list2)
	if err != nil {
		return err
	}
	if s != "" {
		dss.VRI[s] = &VRI{Cert: addCerts, OCSP: addOCSPs, CRL: addCRLs}
	}
	l.appender.SetDSS(dss)
	return nil
}

func (l *LTV) getCerts(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		dataList = append(dataList, cert.Raw)
	}
	return dataList, nil
}

func (l *LTV) validateSig(sig *PdfSignature) error {
	if sig == nil || sig.Contents == nil || len(sig.Contents.Bytes()) == 0 {
		return fmt.Errorf("invalid signature field: %v", sig)
	}
	return nil
}

// EnableAll LTV enables all signatures in the PDF document.
// The signing certificate chain is extracted from each signature dictionary.
// Optionally, additional certificates can be specified through the
// `extraCerts` parameter. The LTV client attempts to build the certificate
// chain up to a trusted root by downloading any missing certificates.
func (l *LTV) EnableAll(extraCerts []*x509.Certificate) error {
	acroForm := l.appender.roReader.AcroForm
	for _, allField := range acroForm.AllFields() {
		signatureField, _ := allField.GetContext().(*PdfFieldSignature)
		if signatureField == nil {
			continue
		}
		sig := signatureField.V
		if err := l.validateSig(sig); err != nil {
			common.Log.Debug("WARN: Skipping invalid signature field: %v", err)
		}
		if err := l.Enable(sig, extraCerts); err != nil {
			return err
		}
	}
	return nil
}

func (l *LTV) getCRLs(certs []*x509.Certificate) ([][]byte, error) {
	dataList := make([][]byte, 0, len(certs))
	for _, cert := range certs {
		for _, crlDistributionPoint := range cert.CRLDistributionPoints {
			if l.CertClient.IsCA(cert) {
				continue
			}
			request, err := l.CRLClient.MakeRequest(crlDistributionPoint, cert)
			if err != nil {
				common.Log.Debug("WARN: CRL request error: %v", err)
				continue
			}
			dataList = append(dataList, request)
		}
	}
	return dataList, nil
}
