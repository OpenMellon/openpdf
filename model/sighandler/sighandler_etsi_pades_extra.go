package sighandler

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"github.com/OpenMellon/pkcs7"
	"github.com/OpenMellon/timestamp"
)

// GetTimestampServerURL returns the URL of the timestamp server.
func (h *etsiPAdES) GetTimestampServerURL() string { return h.timestampServerURL }

// SetTimestampClient sets the HTTP client for timestamp requests.
func (h *etsiPAdES) SetTimestampClient(timestampClient *sigutil.TimestampClient) {
	h.TimestampClient = timestampClient
}

// GetTimestampClient returns the HTTP client for timestamp requests.
func (h *etsiPAdES) GetTimestampClient() *sigutil.TimestampClient { return h.TimestampClient }

func (h *etsiPAdES) makeTimestampRequest(data []byte) (asn1.RawValue, error) {
	hasher := crypto.SHA512.New()
	hasher.Write(data)
	hashed := hasher.Sum(nil)
	req := timestamp.Request{HashAlgorithm: crypto.SHA512, HashedMessage: hashed, Certificates: true, Extensions: nil, ExtraExtensions: nil}
	var client *sigutil.TimestampClient
	if h.TimestampClient != nil {
		client = h.TimestampClient
	} else {
		client = sigutil.NewTimestampClient()
	}
	token, err := client.GetEncodedToken(h.timestampServerURL, &req)
	if err != nil {
		return asn1.NullRawValue, err
	}
	return asn1.RawValue{FullBytes: token}, nil
}

// Sign sets the Contents fields for the PdfSignature.
func (h *etsiPAdES) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	buf, ok := digest.(*bytes.Buffer)
	if !ok {
		return fmt.Errorf("cast to buffer fails")
	}
	signedData, err := pkcs7.NewSignedData(buf.Bytes())
	if err != nil {
		return err
	}
	signedData.SetDigestAlgorithm(pkcs7.OIDDigestAlgorithmSHA256)
	signerConfig := pkcs7.SignerInfoConfig{}
	hasher := crypto.SHA256.New()
	hasher.Write(h.certificate.Raw)
	var signingCertAttr struct {
		Seq struct{ Seq struct{ Value []byte } }
	}
	signingCertAttr.Seq.Seq.Value = hasher.Sum(nil)
	var timestampCerts []*x509.Certificate
	var caCerts []*x509.Certificate
	if h.caCert != nil {
		caCerts = []*x509.Certificate{h.caCert}
	}
	revInfo := RevocationInfoArchival{Crl: []asn1.RawValue{}, Ocsp: []asn1.RawValue{}, OtherRevInfo: []asn1.RawValue{}}
	dssSize := 0
	if h.appender != nil && (len(h.timestampServerURL) > 0 || h.TimestampClient != nil) {
		tsToken, tsErr := h.makeTimestampRequest(([]byte)(""))
		if tsErr != nil {
			return tsErr
		}
		tsResp, tsErr := timestamp.Parse(tsToken.FullBytes)
		if tsErr != nil {
			return tsErr
		}
		timestampCerts = append(timestampCerts, tsResp.Certificates...)
	}
	if h.appender != nil {
		added, dssErr := h.addDss([]*x509.Certificate{h.certificate}, caCerts, &revInfo)
		if dssErr != nil {
			return dssErr
		}
		dssSize += added
		if len(timestampCerts) > 0 {
			added, dssErr = h.addDss(timestampCerts, nil, &revInfo)
			if dssErr != nil {
				return dssErr
			}
			dssSize += added
		}
		if !h.estimatingSize {
			h.appender.SetDSS(h.dss)
		}
	}
	if !h.skipSigningCert {
		signerConfig.ExtraSignedAttributes = append(signerConfig.ExtraSignedAttributes, pkcs7.Attribute{Type: pkcs7.OIDAttributeSigningCertificateV2, Value: signingCertAttr})
	}
	if !h.skipRevocationInfo {
		signerConfig.ExtraSignedAttributes = append(signerConfig.ExtraSignedAttributes, pkcs7.Attribute{Type: pkcs7.OIDAttributeAdobeRevocation, Value: revInfo})
	}
	if signerErr := signedData.AddSignerChainPAdES(h.certificate, h.privateKey, caCerts, signerConfig); signerErr != nil {
		return signerErr
	}
	signedData.Detach()
	if len(h.timestampServerURL) > 0 {
		encDigest := signedData.GetSignedData().SignerInfos[0].EncryptedDigest
		signerToken, tokenErr := h.makeTimestampRequest(encDigest)
		if tokenErr != nil {
			return tokenErr
		}
		tokenErr = signedData.AddTimestampTokenToSigner(0, signerToken.FullBytes)
		if tokenErr != nil {
			return tokenErr
		}
	}
	signed, err := signedData.Finish()
	if err != nil {
		return err
	}
	const padUnit = 1024
	units := (len(signed) / padUnit) + 2
	contents := make([]byte, padUnit*units+dssSize)
	copy(contents, signed)
	sig.Contents = core.MakeHexString(string(contents))
	if !h.estimatingSize && h.dss != nil {
		hasher = crypto.SHA1.New()
		hasher.Write(contents)
		vriKey := strings.ToUpper(hex.EncodeToString(hasher.Sum(nil)))
		if vriKey != "" {
			h.dss.VRI[vriKey] = &model.VRI{Cert: h.dss.Certs, OCSP: h.dss.OCSPs, CRL: h.dss.CRLs}
		}
		h.appender.SetDSS(h.dss)
	}
	return nil
}

// Validate validates PdfSignature.
func (h *etsiPAdES) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	contents := sig.Contents.Bytes()
	p7, err := pkcs7.Parse(contents)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	buf, ok := digest.(*bytes.Buffer)
	if !ok {
		return model.SignatureValidationResult{}, fmt.Errorf("cast to buffer fails")
	}
	p7.Content = buf.Bytes()
	if err = p7.Verify(); err != nil {
		return model.SignatureValidationResult{}, err
	}
	certs := certsFromPKCS7(p7)
	ocspFound := false
	crlFound := false
	var tsTime time.Time
	for _, signer := range p7.Signers {
		encDigest := signer.EncryptedDigest
		var revInfo RevocationInfoArchival
		err = p7.UnmarshalSignedAttribute(pkcs7.OIDAttributeAdobeRevocation, &revInfo)
		if err == nil {
			if len(revInfo.Crl) > 0 {
				crlFound = true
			}
			if len(revInfo.Ocsp) > 0 {
				ocspFound = true
			}
		}
		for _, attr := range signer.UnauthenticatedAttributes {
			if attr.Type.Equal(pkcs7.OIDAttributeTimeStampToken) {
				tsInfo, tsErr := timestamp.Parse(attr.Value.Bytes)
				if tsErr != nil {
					return model.SignatureValidationResult{}, tsErr
				}
				tsTime = tsInfo.Time
				hasher := tsInfo.HashAlgorithm.New()
				hasher.Write(encDigest)
				if !bytes.Equal(hasher.Sum(nil), tsInfo.HashedMessage) {
					return model.SignatureValidationResult{}, errors.New("hash in timestamp is different from pkcs7")
				}
				break
			}
		}
	}
	result := model.SignatureValidationResult{IsSigned: true, IsVerified: true, IsCrlFound: crlFound, IsOcspFound: ocspFound, GeneralizedTime: tsTime, Certificates: certs}
	return result, nil
}

// SkipRevocationInfoAttribute skips adding revocation attribute during signing process.
// Generally, it is not recommended, call it only in case your signature validator requires it.
func (h *etsiPAdES) SkipRevocationInfoAttribute() { h.skipRevocationInfo = true }

// SkipSigningCertificateAttribute skips adding signing certificate attribute during signing process.
// Generally, it is not recommended, call it only in case your signature validator requires it.
func (h *etsiPAdES) SkipSigningCertificateAttribute() { h.skipSigningCert = true }

// SetTimestampServerURL sets the URL of the timestamp server.
func (h *etsiPAdES) SetTimestampServerURL(timestampServerURL string) {
	h.timestampServerURL = timestampServerURL
}
