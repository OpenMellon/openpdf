package sighandler

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"math/big"
	"time"

	"github.com/OpenMellon/openpdf/model"
	"github.com/OpenMellon/openpdf/model/internal/sigattr"
	"github.com/OpenMellon/pkcs7"
)

const (
	LevelB EtsiPAdESLevel = iota
	LevelT
	LevelLT
)

const defaultHashAlgorithm = crypto.SHA1

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

// NewEmptyAdobePKCS7Detached creates a new Adobe.PPKMS/Adobe.PPKLite adbe.pkcs7.detached
// signature handler. The generated signature is empty and of size signatureLen.
// The signatureLen parameter can be 0 for the signature validation.
func NewEmptyAdobePKCS7Detached(signatureLen int) (model.SignatureHandler, error) {
	return &adobePKCS7Detached{empty: true, signatureLen: signatureLen}, nil
}

// RevocationInfoArchival is OIDAttributeAdobeRevocation attribute.
type RevocationInfoArchival = sigattr.RevocationInfoArchival

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

// SignFunc represents a custom signing function. The function should return
// the computed signature.
type SignFunc func(sig *model.PdfSignature, digest model.Hasher) ([]byte, error)
