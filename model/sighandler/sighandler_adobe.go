package sighandler

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"errors"
	"fmt"
	"hash"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/pkcs7"
)

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

func (h *adobeX509RSASHA1) getCertificate(sig *model.PdfSignature) (*x509.Certificate, error) {
	certs, err := h.getCertificates(sig)
	if err != nil {
		return nil, err
	}
	return certs[0], nil
}

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

type adobePKCS7Detached struct {
	privateKey   crypto.PrivateKey
	certificate  *x509.Certificate
	empty        bool
	signatureLen int
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

// NewAdobeX509RSASHA1 creates a new Adobe.PPKMS/Adobe.PPKLite
// adbe.x509.rsa_sha1 signature handler. Both the private key and the
// certificate can be nil for the signature validation, which checks integrity
// only; see model.PdfReader.ValidateSignaturesWithOpts for trust.
func NewAdobeX509RSASHA1(privateKey *rsa.PrivateKey, certificate *x509.Certificate) (model.SignatureHandler, error) {
	return &adobeX509RSASHA1{certificate: certificate, privateKey: privateKey}, nil
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
func (h *adobePKCS7Detached) NewDigest(sig *model.PdfSignature) (model.Hasher, error) {
	return bytes.NewBuffer(nil), nil
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

// IsApplicable returns true if the signature handler is applicable for the PdfSignature.
func (h *adobeX509RSASHA1) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "adbe.x509.rsa_sha1"
}

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

// NewAdobePKCS7DetachedEcdsa creates a new Adobe.PPKMS/Adobe.PPKLite adbe.pkcs7.detached signature handler.
// Both parameters may be nil for the signature validation.
// ECDSA keys are supported by PDF version >= 2.0, for earlier versions use NewAdobePKCS7Detached.
func NewAdobePKCS7DetachedEcdsa(privateKey *ecdsa.PrivateKey, certificate *x509.Certificate) (model.SignatureHandler, error) {
	return &adobePKCS7Detached{certificate: certificate, privateKey: privateKey}, nil
}

// IsApplicable returns true if the signature handler is applicable for the PdfSignature
func (h *adobePKCS7Detached) IsApplicable(sig *model.PdfSignature) bool {
	if sig == nil || sig.Filter == nil || sig.SubFilter == nil {
		return false
	}
	return (*sig.Filter == "Adobe.PPKMS" || *sig.Filter == "Adobe.PPKLite") && *sig.SubFilter == "adbe.pkcs7.detached"
}
