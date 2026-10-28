// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package sighandler implements digital signature handlers for PDF signature validation and signing.
//
// A handler's Validate checks signature integrity only: the signature value
// is verified with the certificate embedded in the document, so IsVerified
// holds for any well-formed signature, including one made with a self-signed
// certificate. Whether the signer is trusted is decided separately by
// model.PdfReader.ValidateSignaturesWithOpts against caller-supplied roots.
package sighandler

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"math/big"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/sigattr"
	"github.com/OpenMellon/openpdf/model/mdp"
	"github.com/OpenMellon/openpdf/model/sigutil"
	"github.com/OpenMellon/pkcs7"
	"github.com/OpenMellon/timestamp"
)

// GetTimestampServerURL returns the URL of the timestamp server.
func (h *etsiPAdES) GetTimestampServerURL() string { return h.timestampServerURL }

const (
	LevelB EtsiPAdESLevel = iota
	LevelT
	LevelLT
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

// NewDocMDPHandler returns the new DocMDP handler with the specific DocMDP restriction level.
func NewDocMDPHandler(handler model.SignatureHandler, permission mdp.DocMDPPermission) (model.SignatureHandler, error) {
	return &DocMDPHandler{handler: handler, Permission: permission}, nil
}

// Validate validates PdfSignature.
func (h *adobePKCS7Detached) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
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
	return model.SignatureValidationResult{IsSigned: true, IsVerified: true, Certificates: certsFromPKCS7(p7)}, nil
}

// InitSignature initialises the PdfSignature.
func (ts *docTimeStamp) InitSignature(sig *model.PdfSignature) error {
	clone := *ts
	sig.Type = core.MakeName("DocTimeStamp")
	sig.Handler = &clone
	sig.Filter = core.MakeName("Adobe.PPKLite")
	sig.SubFilter = core.MakeName("ETSI.RFC3161")
	sig.Reference = nil
	if ts.signatureSize > 0 {
		sig.Contents = core.MakeHexString(string(make([]byte, ts.signatureSize)))
	} else {
		digest, err := ts.NewDigest(sig)
		if err != nil {
			return err
		}
		digest.Write([]byte("calculate the Contents field size"))
		if err = clone.Sign(sig, digest); err != nil {
			return err
		}
		ts.signatureSize = clone.signatureSize
	}
	return nil
}

func (h *adobeX509RSASHA1) getHashAlgorithm(sig *model.PdfSignature) (crypto.Hash, error) {
	cert, err := h.getCertificate(sig)
	if err != nil {
		if h.hashAlgorithm != 0 {
			return h.hashAlgorithm, nil
		}
		return defaultHashAlgorithm, err
	}
	if pubKey, ok := cert.PublicKey.(*rsa.PublicKey); ok && sig.Contents != nil {
		contents := sig.Contents.Bytes()
		var sigBytes []byte
		if _, unmarshalErr := asn1.Unmarshal(contents, &sigBytes); unmarshalErr == nil {
			detected := detectRSAHash(pubKey, sigBytes)
			if detected > 0 {
				return detected, nil
			}
		}
	}
	if h.hashAlgorithm != 0 {
		return h.hashAlgorithm, nil
	}
	return defaultHashAlgorithm, nil
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

// SetTimestampClient sets the HTTP client for timestamp requests.
func (h *etsiPAdES) SetTimestampClient(timestampClient *sigutil.TimestampClient) {
	h.TimestampClient = timestampClient
}

// SetCertificate sets the signing certificate for the signature handler.
func (h *etsiPAdES) SetCertificate(certificate *x509.Certificate) { h.certificate = certificate }

// SetOCSPClient sets the client for retrieving OCSP responses.
func (h *etsiPAdES) SetOCSPClient(ocspClient *sigutil.OCSPClient) { h.OCSPClient = ocspClient }

// Sign sets the Contents fields for the PdfSignature.
func (h *adobeX509RSASHA1) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	var signature []byte
	var err error
	if h.signFunc != nil {
		signature, err = h.signFunc(sig, digest)
		if err != nil {
			return err
		}
	} else {
		hasher, ok := digest.(hash.Hash)
		if !ok {
			return errors.New("hash type error")
		}
		hashAlg := defaultHashAlgorithm
		if h.hashAlgorithm != 0 {
			hashAlg = h.hashAlgorithm
		}
		signature, err = rsa.SignPKCS1v15(rand.Reader, h.privateKey, hashAlg, hasher.Sum(nil))
		if err != nil {
			return err
		}
	}
	signature, err = asn1.Marshal(signature)
	if err != nil {
		return err
	}
	sig.Contents = core.MakeHexString(string(signature))
	return nil
}

// NewDigest creates a new digest.
func (h *adobeX509RSASHA1) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	if hashAlg, err := h.getHashAlgorithm(sig); hashAlg != 0 && err == nil {
		return hashAlg.New(), nil
	}
	return defaultHashAlgorithm.New(), nil
}

const defaultHashAlgorithm = crypto.SHA1

// SetAppender sets the PDF appender.
func (h *etsiPAdES) SetAppender(appender *model.PdfAppender) { h.appender = appender }

// GetTimestampClient returns the HTTP client for timestamp requests.
func (h *etsiPAdES) GetTimestampClient() *sigutil.TimestampClient { return h.TimestampClient }

// ValidateWithOpts validates a PDF signature by checking PdfReader or PdfParser by the DiffPolicy
// params describes parameters for the DocMDP checks.
func (h *DocMDPHandler) ValidateWithOpts(sig *model.PdfSignature, digest model.Hasher, params model.SignatureHandlerDocMDPParams) (model.SignatureValidationResult, error) {
	result, err := h.handler.Validate(sig, digest)
	if err != nil {
		return result, err
	}
	parser := params.Parser
	if parser == nil {
		return model.SignatureValidationResult{}, errors.New("parser can't be null")
	}
	if !result.IsVerified {
		return result, nil
	}
	diffPolicy := params.DiffPolicy
	if diffPolicy == nil {
		diffPolicy = mdp.NewDefaultDiffPolicy()
	}
	for revNum := 0; revNum <= parser.GetRevisionNumber(); revNum++ {
		revision, revErr := parser.GetRevision(revNum)
		if revErr != nil {
			return model.SignatureValidationResult{}, revErr
		}
		trailer := revision.GetTrailer()
		if trailer == nil {
			return model.SignatureValidationResult{}, errors.New("undefined the trailer object")
		}
		root, ok := core.GetDict(trailer.Get("Root"))
		if !ok {
			return model.SignatureValidationResult{}, errors.New("undefined the root object")
		}
		acroForm, ok := core.GetDict(root.Get("AcroForm"))
		if !ok {
			continue
		}
		fields, ok := core.GetArray(acroForm.Get("Fields"))
		if !ok {
			continue
		}
		for _, fieldObj := range fields.Elements() {
			fieldDict, fieldOK := core.GetDict(fieldObj)
			if !fieldOK {
				continue
			}
			sigDict, fieldOK := core.GetDict(fieldDict.Get("V"))
			if !fieldOK {
				continue
			}
			if core.EqualObjects(sigDict.Get("Contents"), sig.Contents) {
				result.DiffResults, revErr = diffPolicy.ReviewFile(revision, parser, &mdp.MDPParameters{DocMDPLevel: h.Permission})
				if revErr != nil {
					return model.SignatureValidationResult{}, revErr
				}
				result.IsVerified = result.DiffResults.IsPermitted()
				return result, nil
			}
		}
	}
	return model.SignatureValidationResult{}, errors.New("don't found this signature in the revisions")
}

// GetOCSPClient returns the client for retrieving OCSP responses.
func (h *etsiPAdES) GetOCSPClient() *sigutil.OCSPClient { return h.OCSPClient }

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

func (h *adobeX509RSASHA1) getCertificate(sig *model.PdfSignature) (*x509.Certificate, error) {
	certs, err := h.getCertificates(sig)
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

// Sign adds a new reference to signature's references array.
func (h *DocMDPHandler) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	return h.handler.Sign(sig, digest)
}

// GetCRLClient returns the client for retrieving CRLs.
func (h *etsiPAdES) GetCRLClient() *sigutil.CRLClient { return h.CRLClient }

// GetAppender returns the PDF appender.
func (h *etsiPAdES) GetAppender() *model.PdfAppender { return h.appender }

// Sign sets the Contents fields.
func (h *adobePKCS7Detached) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	if h.empty {
		size := h.signatureLen
		if size <= 0 {
			size = 8192
		}
		sig.Contents = core.MakeHexString(string(make([]byte, size)))
		return nil
	}
	buf, ok := digest.(*bytes.Buffer)
	if !ok {
		return fmt.Errorf("cast to buffer fails")
	}
	signedData, err := pkcs7.NewSignedData(buf.Bytes())
	if err != nil {
		return err
	}
	if signerErr := signedData.AddSigner(h.certificate, h.privateKey, pkcs7.SignerInfoConfig{}); signerErr != nil {
		return signerErr
	}
	signedData.Detach()
	signed, err := signedData.Finish()
	if err != nil {
		return err
	}
	padded := make([]byte, 8192)
	copy(padded, signed)
	sig.Contents = core.MakeHexString(string(padded))
	return nil
}

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

type adobePKCS7Detached struct {
	privateKey   crypto.PrivateKey
	certificate  *x509.Certificate
	empty        bool
	signatureLen int
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

// NewEtsiPAdESLevelT creates a new Adobe.PPKLite ETSI.CAdES.detached Level T signature handler.
func NewEtsiPAdESLevelT(privateKey *rsa.PrivateKey, certificate *x509.Certificate, caCert *x509.Certificate, certificateTimestampServerURL string) (model.SignatureHandler, error) {
	return &etsiPAdES{level: LevelT, certificate: certificate, privateKey: privateKey, caCert: caCert, timestampServerURL: certificateTimestampServerURL}, nil
}

func (h *adobeX509RSASHA1) sign(sig *model.PdfSignature, digest model.Hasher, estimateSize bool) error {
	if !estimateSize {
		return h.Sign(sig, digest)
	}
	pubKey, ok := h.certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return fmt.Errorf("invalid public key type: %T", pubKey)
	}
	placeholder, err := asn1.Marshal(make([]byte, pubKey.Size()))
	if err != nil {
		return err
	}
	sig.Contents = core.MakeHexString(string(placeholder))
	return nil
}

// DocTimeStampOpts defines options for configuring the timestamp handler.
type DocTimeStampOpts struct {
	// SignatureSize is the estimated size of the signature contents in bytes.
	// If not provided, a default signature size of 4192 is used.
	// The signing process will report the model.ErrSignNotEnoughSpace error
	// if the estimated signature size is smaller than the actual size of the
	// signature.
	SignatureSize int

	// Client is the timestamp client used to make the signature request.
	// If no client is provided, a default one is used.
	Client *sigutil.TimestampClient
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

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *etsiPAdES) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "ETSI.CAdES.detached"
}

// SkipRevocationInfoAttribute skips adding revocation attribute during signing process.
// Generally, it is not recommended, call it only in case your signature validator requires it.
func (h *etsiPAdES) SkipRevocationInfoAttribute() { h.skipRevocationInfo = true }

// SkipSigningCertificateAttribute skips adding signing certificate attribute during signing process.
// Generally, it is not recommended, call it only in case your signature validator requires it.
func (h *etsiPAdES) SkipSigningCertificateAttribute() { h.skipSigningCert = true }

// NewDigest creates a new digest.
func (h *DocMDPHandler) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return h.handler.NewDigest(sig)
}

type timestampInfo struct {
	Version        int
	Policy         asn1.RawValue
	MessageImprint struct {
		HashAlgorithm pkix.AlgorithmIdentifier
		HashedMessage []byte
	}
	SerialNumber    asn1.RawValue
	GeneralizedTime time.Time
}

func leftPad(data []byte, size int) (padded []byte) {
	n := min(len(data), size)
	padded = make([]byte, size)
	copy(padded[len(padded)-n:], data)
	return
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

// NewAdobeX509RSASHA1CustomWithOpts creates a new Adobe.PPKMS/Adobe.PPKLite
// adbe.x509.rsa_sha1 signature handler with a custom signing function. The
// handler is configured based on the provided options. If no options are
// provided, default options will be used. Both the certificate and the sign
// function can be nil for the signature validation.
func NewAdobeX509RSASHA1CustomWithOpts(certificate *x509.Certificate, signFunc SignFunc, opts *AdobeX509RSASHA1Opts) (model.SignatureHandler, error) {
	if opts == nil {
		opts = &AdobeX509RSASHA1Opts{}
	}
	return &adobeX509RSASHA1{certificate: certificate, signFunc: signFunc, estimateSize: opts.EstimateSize, hashAlgorithm: opts.Algorithm}, nil
}

// InitSignature initialises the PdfSignature.
func (h *adobePKCS7Detached) InitSignature(sig *model.PdfSignature) error {
	if !h.empty {
		if h.certificate == nil {
			return errors.New("certificate must not be nil")
		}
		if h.privateKey == nil {
			return errors.New("privateKey must not be nil")
		}
	}
	clone := *h
	sig.Handler = &clone
	sig.Filter = core.MakeName("Adobe.PPKLite")
	sig.SubFilter = core.MakeName("adbe.pkcs7.detached")
	sig.Reference = nil
	digest, err := clone.NewDigest(sig)
	if err != nil {
		return err
	}
	digest.Write([]byte("calculate the Contents field size"))
	return clone.Sign(sig, digest)
}

// SetDSS sets the Document Security Store (DSS) for the signature handler.
// The DSS contains validation-related information such as certificates, CRLs, and OCSP responses.
func (h *etsiPAdES) SetDSS(dss *model.DSS) { h.dss = dss }

// NewAdobeX509RSASHA1Custom creates a new Adobe.PPKMS/Adobe.PPKLite
// adbe.x509.rsa_sha1 signature handler with a custom signing function. Both the
// certificate and the sign function can be nil for the signature validation.
// A certificate given here is used as the verification key in place of the
// one embedded in the document; it does not make the signature trusted (see
// model.PdfReader.ValidateSignaturesWithOpts).
// NOTE: the handler will do a mock Sign when initializing the signature in
// order to estimate the signature size. Use NewAdobeX509RSASHA1CustomWithOpts
// for configuring the handler to estimate the signature size.
func NewAdobeX509RSASHA1Custom(certificate *x509.Certificate, signFunc SignFunc) (model.SignatureHandler, error) {
	return &adobeX509RSASHA1{certificate: certificate, signFunc: signFunc}, nil
}

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

// InitSignature initialization of the DocMDP signature.
func (h *DocMDPHandler) InitSignature(sig *model.PdfSignature) error {
	err := h.handler.InitSignature(sig)
	if err != nil {
		return err
	}
	sig.Handler = h
	if sig.Reference == nil {
		sig.Reference = core.MakeArray()
	}
	sig.Reference.Append(model.NewPdfSignatureReferenceDocMDP(model.NewPdfTransformParamsDocMDP(h.Permission)).ToPdfObject())
	return nil
}

type docTimeStamp struct {
	timestampServerURL string
	hashAlgorithm      crypto.Hash
	signatureSize      int
	timestampClient    *sigutil.TimestampClient
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

func certsFromPKCS7(p7 *pkcs7.PKCS7) []*x509.Certificate {
	signer := p7.GetOnlySigner()
	if signer == nil {
		return nil
	}
	certs := make([]*x509.Certificate, 0, len(p7.Certificates))
	certs = append(certs, signer)
	for _, cert := range p7.Certificates {
		if cert != signer {
			certs = append(certs, cert)
		}
	}
	return certs
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *DocMDPHandler) IsApplicable(sig *model.PdfSignature) bool {
	found := false
	for _, ref := range sig.Reference.Elements() {
		if refDict, ok := core.GetDict(ref); ok {
			if method, hasMethod := core.GetNameVal(refDict.Get("TransformMethod")); hasMethod {
				if method != "DocMDP" {
					return false
				}
				if params, hasParams := core.GetDict(refDict.Get("TransformParams")); hasParams {
					_, err := core.GetNumberAsInt64(params.Get("P"))
					if err != nil {
						return false
					}
					found = true
					break
				}
			}
		}
	}
	return found && h.handler.IsApplicable(sig)
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

// DocMDPHandler describes handler for the DocMDP realization.
type DocMDPHandler struct {
	handler    model.SignatureHandler
	Permission mdp.DocMDPPermission
}

// NewAdobeX509RSASHA1 creates a new Adobe.PPKMS/Adobe.PPKLite
// adbe.x509.rsa_sha1 signature handler. Both the private key and the
// certificate can be nil for the signature validation, which checks integrity
// only; see model.PdfReader.ValidateSignaturesWithOpts for trust.
func NewAdobeX509RSASHA1(privateKey *rsa.PrivateKey, certificate *x509.Certificate) (model.SignatureHandler, error) {
	return &adobeX509RSASHA1{certificate: certificate, privateKey: privateKey}, nil
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

// AdobeX509RSASHA1Opts defines options for configuring the adbe.x509.rsa_sha1
// signature handler.
type AdobeX509RSASHA1Opts struct {
	// EstimateSize specifies whether the size of the signature contents
	// should be estimated based on the modulus size of the public key
	// extracted from the signing certificate. If set to false, a mock Sign
	// call is made in order to estimate the size of the signature contents.
	EstimateSize bool

	// Algorithm specifies the algorithm used for performing signing.
	// If not specified, defaults to SHA1.
	Algorithm crypto.Hash
}

// NewDigest creates a new digest.
func (ts *docTimeStamp) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
}

// Validate validates PdfSignature.
func (ts *docTimeStamp) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	contents := sig.Contents.Bytes()
	p7, err := pkcs7.Parse(contents)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	if err = p7.Verify(); err != nil {
		return model.SignatureValidationResult{}, err
	}
	certs := certsFromPKCS7(p7)
	var info timestampInfo
	_, err = asn1.Unmarshal(p7.Content, &info)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	hashAlg, err := hashFromOID(info.MessageImprint.HashAlgorithm.Algorithm)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	hasher := hashAlg.New()
	buf, ok := digest.(*bytes.Buffer)
	if !ok {
		return model.SignatureValidationResult{}, fmt.Errorf("cast to buffer fails")
	}
	hasher.Write(buf.Bytes())
	sum := hasher.Sum(nil)
	result := model.SignatureValidationResult{IsSigned: true, IsVerified: bytes.Equal(sum, info.MessageImprint.HashedMessage), GeneralizedTime: info.GeneralizedTime, Certificates: certs}
	return result, nil
}

// NewDigest creates a new digest.
func (h *adobePKCS7Detached) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
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

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (ts *docTimeStamp) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "ETSI.RFC3161"
}

func detectRSAHash(pub *rsa.PublicKey, sig []byte) crypto.Hash {
	keySize := pub.Size()
	if keySize != len(sig) {
		return 0
	}
	modExp := func(result *big.Int, key *rsa.PublicKey, base *big.Int) *big.Int {
		exp := big.NewInt(int64(key.E))
		result.Exp(base, exp, key.N)
		return result
	}
	sigInt := new(big.Int).SetBytes(sig)
	decrypted := modExp(new(big.Int), pub, sigInt)
	em := leftPad(decrypted.Bytes(), keySize)
	if em[0] != 0 || em[1] != 1 {
		return 0
	}
	digestInfos := []struct {
		Hash   crypto.Hash
		Prefix []byte
	}{{Hash: crypto.SHA1, Prefix: []byte{0x30, 0x21, 0x30, 0x09, 0x06, 0x05, 0x2b, 0x0e, 0x03, 0x02, 0x1a, 0x05, 0x00, 0x04, 0x14}}, {Hash: crypto.SHA256, Prefix: []byte{0x30, 0x31, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x01, 0x05, 0x00, 0x04, 0x20}}, {Hash: crypto.SHA384, Prefix: []byte{0x30, 0x41, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x02, 0x05, 0x00, 0x04, 0x30}}, {Hash: crypto.SHA512, Prefix: []byte{0x30, 0x51, 0x30, 0x0d, 0x06, 0x09, 0x60, 0x86, 0x48, 0x01, 0x65, 0x03, 0x04, 0x02, 0x03, 0x05, 0x00, 0x04, 0x40}}, {Hash: crypto.RIPEMD160, Prefix: []byte{0x30, 0x20, 0x30, 0x08, 0x06, 0x06, 0x28, 0xcf, 0x06, 0x03, 0x00, 0x31, 0x04, 0x14}}}
	for _, info := range digestInfos {
		hashSize := info.Hash.Size()
		tailLen := len(info.Prefix) + hashSize
		if bytes.Equal(em[keySize-tailLen:keySize-hashSize], info.Prefix) {
			return info.Hash
		}
	}
	return 0
}

// SetTimestampServerURL sets the URL of the timestamp server.
func (h *etsiPAdES) SetTimestampServerURL(timestampServerURL string) {
	h.timestampServerURL = timestampServerURL
}

// NewAdobePKCS7Detached creates a new Adobe.PPKMS/Adobe.PPKLite adbe.pkcs7.detached signature handler.
// Both parameters may be nil for the signature validation, which checks
// integrity only; see model.PdfReader.ValidateSignaturesWithOpts for trust.
func NewAdobePKCS7Detached(privateKey *rsa.PrivateKey, certificate *x509.Certificate) (model.SignatureHandler, error) {
	return &adobePKCS7Detached{certificate: certificate, privateKey: privateKey}, nil
}

// InitSignature initialises the PdfSignature.
func (h *adobeX509RSASHA1) InitSignature(sig *model.PdfSignature) error {
	if h.certificate == nil {
		return errors.New("certificate must not be nil")
	}
	if h.privateKey == nil && h.signFunc == nil {
		return errors.New("must provide either a private key or a signing function")
	}
	clone := *h
	sig.Handler = &clone
	sig.Filter = core.MakeName("Adobe.PPKLite")
	sig.SubFilter = core.MakeName("adbe.x509.rsa_sha1")
	sig.Cert = core.MakeString(string(clone.certificate.Raw))
	sig.Reference = nil
	digest, err := clone.NewDigest(sig)
	if err != nil {
		return err
	}
	digest.Write([]byte("calculate the Contents field size"))
	return clone.sign(sig, digest, h.estimateSize)
}

type adobeX509RSASHA1 struct {
	privateKey    *rsa.PrivateKey
	certificate   *x509.Certificate
	signFunc      SignFunc
	estimateSize  bool
	hashAlgorithm crypto.Hash
}

func (h *adobeX509RSASHA1) getCertificates(sig *model.PdfSignature) ([]*x509.Certificate, error) {
	if h.certificate != nil {
		return []*x509.Certificate{h.certificate}, nil
	}
	return sig.GetCerts()
}

// NewDocTimeStampWithOpts returns a new DocTimeStamp configured using the
// specified options. If no options are provided, default options will be used.
// Both the timestamp server URL and the hash algorithm can be empty for the
// signature validation.
// The following hash algorithms are supported:
// crypto.SHA1, crypto.SHA256, crypto.SHA384, crypto.SHA512.
func NewDocTimeStampWithOpts(timestampServerURL string, hashAlgorithm crypto.Hash, opts *DocTimeStampOpts) (model.SignatureHandler, error) {
	if opts == nil {
		opts = &DocTimeStampOpts{}
	}
	if opts.SignatureSize <= 0 {
		opts.SignatureSize = 4192
	}
	return &docTimeStamp{timestampServerURL: timestampServerURL, hashAlgorithm: hashAlgorithm, signatureSize: opts.SignatureSize, timestampClient: opts.Client}, nil
}

// EtsiPAdESLevel represents the PAdES signature level.
type EtsiPAdESLevel int

// NewDocTimeStamp creates a new DocTimeStamp signature handler.
// Both the timestamp server URL and the hash algorithm can be empty for the
// signature validation.
// The following hash algorithms are supported:
// crypto.SHA1, crypto.SHA256, crypto.SHA384, crypto.SHA512.
// NOTE: the handler will do a mock Sign when initializing the signature
// in order to estimate the signature size. Use NewDocTimeStampWithOpts
// for providing the signature size.
func NewDocTimeStamp(timestampServerURL string, hashAlgorithm crypto.Hash) (model.SignatureHandler, error) {
	return &docTimeStamp{timestampServerURL: timestampServerURL, hashAlgorithm: hashAlgorithm}, nil
}

// NewEmptyAdobePKCS7Detached creates a new Adobe.PPKMS/Adobe.PPKLite adbe.pkcs7.detached
// signature handler. The generated signature is empty and of size signatureLen.
// The signatureLen parameter can be 0 for the signature validation.
func NewEmptyAdobePKCS7Detached(signatureLen int) (model.SignatureHandler, error) {
	return &adobePKCS7Detached{empty: true, signatureLen: signatureLen}, nil
}

// SetCRLClient sets the client for retrieving CRLs.
func (h *etsiPAdES) SetCRLClient(crlClient *sigutil.CRLClient) { h.CRLClient = crlClient }

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *adobeX509RSASHA1) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "adbe.x509.rsa_sha1"
}

// SetCertClient sets the client for retrieving certificates.
func (h *etsiPAdES) SetCertClient(certClient *sigutil.CertClient) { h.CertClient = certClient }

// NewDigest creates a new digest.
func (h *etsiPAdES) NewDigest(_ *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
}

func (ts *docTimeStamp) getCertificate(sig *model.PdfSignature) (*x509.Certificate, error) {
	certs, err := sig.GetCerts()
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

// RevocationInfoArchival is OIDAttributeAdobeRevocation attribute.
type RevocationInfoArchival = sigattr.RevocationInfoArchival

// Validate validates PdfSignature.
func (h *adobeX509RSASHA1) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	certs, err := h.getCertificates(sig)
	if err != nil {
		return model.SignatureValidationResult{}, err
	}
	cert := certs[0]
	pubKey, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		return model.SignatureValidationResult{}, fmt.Errorf("certificate public key is %T, RSA required", cert.PublicKey)
	}
	if sig.Contents == nil {
		return model.SignatureValidationResult{}, errors.New("signature Contents not set")
	}
	contents := sig.Contents.Bytes()
	var sigBytes []byte
	if _, unmarshalErr := asn1.Unmarshal(contents, &sigBytes); unmarshalErr != nil {
		return model.SignatureValidationResult{}, unmarshalErr
	}
	hasher, ok := digest.(hash.Hash)
	if !ok {
		return model.SignatureValidationResult{}, errors.New("hash type error")
	}
	hashAlg, _ := h.getHashAlgorithm(sig)
	if hashAlg == 0 {
		hashAlg = defaultHashAlgorithm
	}
	if verifyErr := rsa.VerifyPKCS1v15(pubKey, hashAlg, hasher.Sum(nil), sigBytes); verifyErr != nil {
		return model.SignatureValidationResult{}, verifyErr
	}
	return model.SignatureValidationResult{IsSigned: true, IsVerified: true, Certificates: certs}, nil
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

func hashFromOID(oid asn1.ObjectIdentifier) (crypto.Hash, error) {
	switch {
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA1), oid.Equal(pkcs7.OIDDigestAlgorithmECDSASHA1), oid.Equal(pkcs7.OIDDigestAlgorithmDSA), oid.Equal(pkcs7.OIDDigestAlgorithmDSASHA1), oid.Equal(pkcs7.OIDEncryptionAlgorithmRSA):
		return crypto.SHA1, nil
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA256), oid.Equal(pkcs7.OIDDigestAlgorithmECDSASHA256):
		return crypto.SHA256, nil
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA384), oid.Equal(pkcs7.OIDDigestAlgorithmECDSASHA384):
		return crypto.SHA384, nil
	case oid.Equal(pkcs7.OIDDigestAlgorithmSHA512), oid.Equal(pkcs7.OIDDigestAlgorithmECDSASHA512):
		return crypto.SHA512, nil
	}
	return crypto.Hash(0), pkcs7.ErrUnsupportedAlgorithm
}

// NewAdobePKCS7DetachedEcdsa creates a new Adobe.PPKMS/Adobe.PPKLite adbe.pkcs7.detached signature handler.
// Both parameters may be nil for the signature validation.
// ECDSA keys are supported by PDF version >= 2.0, for earlier versions use NewAdobePKCS7Detached.
func NewAdobePKCS7DetachedEcdsa(privateKey *ecdsa.PrivateKey, certificate *x509.Certificate) (model.SignatureHandler, error) {
	return &adobePKCS7Detached{certificate: certificate, privateKey: privateKey}, nil
}

// Sign sets the Contents fields for the PdfSignature.
func (ts *docTimeStamp) Sign(sig *model.PdfSignature, digest model.Hasher) error {
	req, err := sigutil.NewTimestampRequest(digest.(*bytes.Buffer), &timestamp.RequestOptions{Hash: ts.hashAlgorithm, Certificates: true})
	if err != nil {
		return err
	}
	client := ts.timestampClient
	if client == nil {
		client = sigutil.NewTimestampClient()
	}
	token, err := client.GetEncodedToken(ts.timestampServerURL, req)
	if err != nil {
		return err
	}
	tokenLen := len(token)
	if ts.signatureSize > 0 && tokenLen > ts.signatureSize {
		return model.ErrSignNotEnoughSpace
	}
	if tokenLen > 0 {
		ts.signatureSize = tokenLen + 128
	}
	if sig.Contents != nil {
		existing := sig.Contents.Bytes()
		copy(existing, token)
		token = existing
	}
	sig.Contents = core.MakeHexString(string(token))
	return nil
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature
func (h *adobePKCS7Detached) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "adbe.pkcs7.detached"
}

// SignFunc represents a custom signing function. The function should return
// the computed signature.
type SignFunc func(sig *model.PdfSignature, digest model.Hasher) ([]byte, error)

// Validate implementation of the SignatureHandler interface
// This check is impossible without checking the document's content.
// Please, use ValidateWithOpts with the PdfParser.
func (h *DocMDPHandler) Validate(sig *model.PdfSignature, digest model.Hasher) (model.SignatureValidationResult, error) {
	return model.SignatureValidationResult{}, errors.New("impossible validation without parse")
}

// GetCertClient returns the client for retrieving certificates.
func (h *etsiPAdES) GetCertClient() *sigutil.CertClient { return h.CertClient }

// SetCA sets the CA certificate for the signature handler.
func (h *etsiPAdES) SetCA(certificate *x509.Certificate) { h.caCert = certificate }
