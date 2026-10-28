package sighandler

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/sigutil"
)

// NewEtsiPAdESLevelTEcdsa creates a new Adobe.PPKLite ETSI.CAdES.detached Level T signature handler.
// ECDSA keys are supported by PDF version >= 2.0, for earlier versions use NewEtsiPAdESLevelT.
func NewEtsiPAdESLevelTEcdsa(privateKey *ecdsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate, certificateTimestampServerURL string) (model.SignatureHandler, error) {
	return &etsiPAdES{level: LevelT, certificate: certificate, privateKey: privateKey, caCert: caCert, timestampServerURL: certificateTimestampServerURL}, nil
}

// NewEtsiPAdESLevelBEcdsa creates a new Adobe.PPKLite ETSI.CAdES.detached Level B signature handler.
// ECDSA keys are supported by PDF version >= 2.0, for earlier versions use NewEtsiPAdESLevelB.
func NewEtsiPAdESLevelBEcdsa(privateKey *ecdsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate) (model.SignatureHandler, error) {
	return &etsiPAdES{level: LevelB, certificate: certificate, privateKey: privateKey, caCert: caCert}, nil
}

// NewEtsiPAdESLevelB creates a new Adobe.PPKLite ETSI.CAdES.detached Level B signature handler.
func NewEtsiPAdESLevelB(privateKey *rsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate) (model.SignatureHandler, error) {
	return &etsiPAdES{level: LevelB, certificate: certificate, privateKey: privateKey, caCert: caCert}, nil
}

// NewEtsiPAdES creates a new PAdES signature handler with the specified algorithm and level.
// The handler should be further configured using the available setter methods.
func NewEtsiPAdES(level EtsiPAdESLevel) *etsiPAdES { return &etsiPAdES{level: level} }

func (h *etsiPAdES) GetCerts(chain []*x509.Certificate) ([][]byte, error) {
	certs := make([][]byte, 0, len(chain))
	for _, cert := range chain {
		certs = append(certs, cert.Raw)
	}
	return certs, nil
}

// SetPrivateKey sets the private key for the signature handler.
func (h *etsiPAdES) SetPrivateKey(privateKey crypto.PrivateKey) { h.privateKey = privateKey }

// GetCA returns the CA certificate for the signature handler.
func (h *etsiPAdES) GetCA() *x509.Certificate { return h.caCert }

// SetCertificate sets the signing certificate for the signature handler.
func (h *etsiPAdES) SetCertificate(certificate *x509.Certificate) { h.certificate = certificate }

// SetOCSPClient sets the client for retrieving OCSP responses.
func (h *etsiPAdES) SetOCSPClient(ocspClient *sigutil.OCSPClient) { h.OCSPClient = ocspClient }

// SetAppender sets the PDF appender.
func (h *etsiPAdES) SetAppender(appender *model.PdfAppender) { h.appender = appender }

// GetOCSPClient returns the client for retrieving OCSP responses.
func (h *etsiPAdES) GetOCSPClient() *sigutil.OCSPClient { return h.OCSPClient }

// GetCRLClient returns the client for retrieving CRLs.
func (h *etsiPAdES) GetCRLClient() *sigutil.CRLClient { return h.CRLClient }

// GetAppender returns the PDF appender.
func (h *etsiPAdES) GetAppender() *model.PdfAppender { return h.appender }

// GetDSS returns the Document Security Store (DSS) for the signature handler.
func (h *etsiPAdES) GetDSS() *model.DSS { return h.dss }

func (h *etsiPAdES) GetOCSPs(chain []*x509.Certificate, chainMap map[string]*x509.Certificate) ([][]byte, error) {
	ocsps := make([][]byte, 0, len(chain))
	if h.OCSPClient == nil {
		common.Log.Error("ERROR: no OCSPClient set")
		return ocsps, model.ErrNoOcspClient
	}
	for _, cert := range chain {
		for _, ocspURL := range cert.OCSPServer {
			if h.CertClient.IsCA(cert) {
				continue
			}
			issuer, ok := chainMap[cert.Issuer.CommonName]
			if !ok {
				common.Log.Debug("WARN: Skipping OCSP request: issuer certificate not found")
				continue
			}
			_, resp, err := h.OCSPClient.MakeRequest(ocspURL, cert, issuer)
			if err != nil {
				common.Log.Debug("WARN: OCSP request error: %v", err)
				continue
			}
			ocsps = append(ocsps, resp)
		}
	}
	return ocsps, nil
}

// GetPrivateKey returns the private key for the signature handler.
func (h *etsiPAdES) GetPrivateKey() crypto.PrivateKey { return h.privateKey }

// NewEtsiPAdESLevelT creates a new Adobe.PPKLite ETSI.CAdES.detached Level T signature handler.
func NewEtsiPAdESLevelT(privateKey *rsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate, certificateTimestampServerURL string) (model.SignatureHandler, error) {
	return &etsiPAdES{level: LevelT, certificate: certificate, privateKey: privateKey, caCert: caCert, timestampServerURL: certificateTimestampServerURL}, nil
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *etsiPAdES) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "ETSI.CAdES.detached"
}

func (h *etsiPAdES) addDss(sigCerts, extraCerts []*x509.Certificate, revInfo *RevocationInfoArchival) (int, error) {
	chain, chainMap, err := h.BuildCertChain(sigCerts, extraCerts)
	if err != nil {
		return 0, err
	}
	certs, err := h.GetCerts(chain)
	if err != nil {
		return 0, err
	}
	var ocsps, crls [][]byte
	ocsps, err = h.GetOCSPs(chain, chainMap)
	if err != nil {
		return 0, err
	}
	crls, err = h.GetCRLs(chain)
	if err != nil {
		return 0, err
	}
	if !h.estimatingSize {
		_, err = h.dss.AddCerts(certs)
		if err != nil {
			return 0, err
		}
		_, err = h.dss.AddOCSPs(ocsps)
		if err != nil {
			return 0, err
		}
		_, err = h.dss.AddCRLs(crls)
		if err != nil {
			return 0, err
		}
	}
	size := 0
	for _, crl := range crls {
		size += len(crl)
		revInfo.Crl = append(revInfo.Crl, asn1.RawValue{FullBytes: crl})
	}
	for _, ocsp := range ocsps {
		size += len(ocsp)
		revInfo.Ocsp = append(revInfo.Ocsp, asn1.RawValue{FullBytes: ocsp})
	}
	return size, nil
}

// GetCertificate returns the signing certificate for the signature handler.
func (h *etsiPAdES) GetCertificate() *x509.Certificate { return h.certificate }

// SetDSS sets the Document Security Store (DSS) for the signature handler.
// The DSS contains validation-related information such as certificates, CRLs, and OCSP responses.
func (h *etsiPAdES) SetDSS(dss *model.DSS) { h.dss = dss }

func (h *etsiPAdES) BuildCertChain(sigCerts, extraCerts []*x509.Certificate) ([]*x509.Certificate, map[string]*x509.Certificate, error) {
	if h.CertClient == nil {
		common.Log.Error("ERROR: no CertClient set")
		return nil, nil, model.ErrNoCertClient
	}
	certMap := map[string]*x509.Certificate{}
	for _, cert := range sigCerts {
		certMap[cert.Subject.CommonName] = cert
	}
	chain := sigCerts
	for _, extra := range extraCerts {
		name := extra.Subject.CommonName
		if _, seen := certMap[name]; seen {
			continue
		}
		certMap[name] = extra
		chain = append(chain, extra)
	}
	if len(chain) == 0 {
		return nil, nil, model.ErrSignNoCertificates
	}
	var err error
	for cur := chain[0]; cur != nil && !h.CertClient.IsCA(cur); {
		var issuer *x509.Certificate
		_, found := certMap[cur.Issuer.CommonName]
		if !found {
			if issuer, err = h.CertClient.GetIssuer(cur); err != nil {
				common.Log.Debug("WARN: Could not retrieve certificate issuer: %v", err)
				break
			}
			certMap[cur.Issuer.CommonName] = issuer
			chain = append(chain, issuer)
		} else {
			break
		}
		cur = issuer
	}
	return chain, certMap, nil
}

// NewEtsiPAdESLevelLTEcdsa creates a new Adobe.PPKLite ETSI.CAdES.detached Level LT signature handler.
// ECDSA keys are supported by PDF version >= 2.0, for earlier versions use NewEtsiPAdESLevelLT.
func NewEtsiPAdESLevelLTEcdsa(privateKey *ecdsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate, certificateTimestampServerURL string, appender *model.PdfAppender) (model.SignatureHandler, error) {
	dss := appender.Reader.DSS
	if dss == nil {
		dss = model.NewDSS()
	}
	if err := dss.GenerateHashMaps(); err != nil {
		return nil, err
	}
	return &etsiPAdES{level: LevelLT, certificate: certificate, privateKey: privateKey, caCert: caCert, timestampServerURL: certificateTimestampServerURL, CertClient: sigutil.NewCertClient(), OCSPClient: sigutil.NewOCSPClient(), CRLClient: sigutil.NewCRLClient(), appender: appender, dss: dss}, nil
}

type etsiPAdES struct {
	level              EtsiPAdESLevel
	privateKey         crypto.PrivateKey
	certificate        *x509.Certificate
	estimatingSize     bool
	caCert             *x509.Certificate
	timestampServerURL string

	// TimestampClient an optional client used to connect to a timestamp server.
	//
	// If not defined then a default client would be used.
	TimestampClient *sigutil.TimestampClient

	// CertClient is the client used to retrieve certificates.
	CertClient *sigutil.CertClient

	// OCSPClient is the client used to retrieve OCSP validation information.
	OCSPClient *sigutil.OCSPClient

	// CRLClient is the client used to retrieve CRL validation information.
	CRLClient          *sigutil.CRLClient
	appender           *model.PdfAppender
	dss                *model.DSS
	skipSigningCert    bool
	skipRevocationInfo bool
}

// NewEtsiPAdESLevelLT creates a new Adobe.PPKLite ETSI.CAdES.detached Level LT signature handler.
func NewEtsiPAdESLevelLT(privateKey *rsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate, certificateTimestampServerURL string, appender *model.PdfAppender) (model.SignatureHandler, error) {
	dss := appender.Reader.DSS
	if dss == nil {
		dss = model.NewDSS()
	}
	if err := dss.GenerateHashMaps(); err != nil {
		return nil, err
	}
	return &etsiPAdES{level: LevelLT, certificate: certificate, privateKey: privateKey, caCert: caCert, timestampServerURL: certificateTimestampServerURL, CertClient: sigutil.NewCertClient(), OCSPClient: sigutil.NewOCSPClient(), CRLClient: sigutil.NewCRLClient(), appender: appender, dss: dss}, nil
}

func (h *etsiPAdES) GetCRLs(chain []*x509.Certificate) ([][]byte, error) {
	crls := make([][]byte, 0, len(chain))
	if h.CRLClient == nil {
		common.Log.Error("ERROR: no CRLClient set")
		return crls, model.ErrNoCrlClient
	}
	for _, cert := range chain {
		for _, crlURL := range cert.CRLDistributionPoints {
			if h.CertClient.IsCA(cert) {
				continue
			}
			crl, err := h.CRLClient.MakeRequest(crlURL, cert)
			if err != nil {
				common.Log.Debug("WARN: CRL request error: %v", err)
				continue
			}
			crls = append(crls, crl)
		}
	}
	return crls, nil
}

// EtsiPAdESLevel represents the PAdES signature level.
type EtsiPAdESLevel int

// SetCRLClient sets the client for retrieving CRLs.
func (h *etsiPAdES) SetCRLClient(crlClient *sigutil.CRLClient) { h.CRLClient = crlClient }

// SetCertClient sets the client for retrieving certificates.
func (h *etsiPAdES) SetCertClient(certClient *sigutil.CertClient) { h.CertClient = certClient }

// NewDigest creates a new digest.
func (h *etsiPAdES) NewDigest(_ *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
}

// InitSignature initialises the PdfSignature.
func (h *etsiPAdES) InitSignature(sig *model.PdfSignature) error {
	if h.certificate == nil {
		return errors.New("certificate must not be nil")
	}
	if h.privateKey == nil {
		return errors.New("private key must not be nil")
	}
	if h.level >= LevelT {
		if h.timestampServerURL == "" {
			return errors.New("certificate timestamp server URL must not be empty for Level T & LT signature handler")
		}
		if h.TimestampClient == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Timestamp client not specified, using a default client")
			}
			h.TimestampClient = sigutil.NewTimestampClient()
		}
	}
	if h.level == LevelLT {
		if h.CertClient == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("cert client not specified, using a default client")
			}
			h.CertClient = sigutil.NewCertClient()
		}
		if h.OCSPClient == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("OCSP client not specified, using a default client")
			}
			h.OCSPClient = sigutil.NewOCSPClient()
		}
		if h.CRLClient == nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("CRL client not specified, using a default client")
			}
			h.CRLClient = sigutil.NewCRLClient()
		}
		if h.appender == nil {
			return errors.New("appender must not be nil for Level LT signature handler")
		}
		if h.dss == nil {
			dss := h.appender.Reader.DSS
			if dss == nil {
				dss = model.NewDSS()
			}
			if err := dss.GenerateHashMaps(); err != nil {
				return err
			}
			h.dss = dss
		}
	}
	clone := *h
	sig.Handler = &clone
	sig.Filter = core.MakeName("Adobe.PPKLite")
	sig.SubFilter = core.MakeName("ETSI.CAdES.detached")
	sig.Reference = nil
	digest, err := clone.NewDigest(sig)
	if err != nil {
		return err
	}
	_, err = digest.Write([]byte("calculate the Contents field size"))
	if err != nil {
		return err
	}
	clone.estimatingSize = true
	err = clone.Sign(sig, digest)
	clone.estimatingSize = false
	return err
}

// GetCertClient returns the client for retrieving certificates.
func (h *etsiPAdES) GetCertClient() *sigutil.CertClient { return h.CertClient }

// SetCA sets the CA certificate for the signature handler.
func (h *etsiPAdES) SetCA(certificate *x509.Certificate) { h.caCert = certificate }
