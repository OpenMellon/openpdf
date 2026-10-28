package security

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"

	"github.com/OpenMellon/pkcs7"
)

const pubSecAlwaysSetBits = Permissions(1) | Permissions(1<<9)

// PubSecPermissionBytes encodes p as the 4 big-endian permission bytes that
// follow the seed in a recipient's envelope: the Table 24 bits of p, bits 1
// and 10 always set, and the owner bit (bit 2) when p grants every
// permission (PermOwner, or all Perm* flags), which makes the recipient
// owner-equivalent. PubSecPermissions reads the result back as PermOwner, or
// as p masked to Table 24 plus PermDisabilityExtract.
func PubSecPermissionBytes(p Permissions) [4]byte {
	bits := (p & pubSecGrantAllBits) | pubSecAlwaysSetBits
	if bits&pubSecGrantAllBits == pubSecGrantAllBits {
		bits |= pubSecOwnerBit
	}
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], uint32(bits))
	return out
}

const pubSecGrantAllBits = pubSecTable24Mask &^ pubSecOwnerBit

// PubSecEncryptDict holds the entries specific to a public-key encryption
// dictionary. For V4 and V5 the /Recipients array and EncryptMetadata live in
// the crypt-filter dictionary named by /StmF; for V1 and V2 the array is at
// the top level of the encryption dictionary.
type PubSecEncryptDict struct {
	// SubFilter names the dictionary layout (one of the PubSecSubFilter*
	// constants).
	SubFilter string

	// Recipients holds the DER-encoded CMS EnvelopedData objects in array
	// order. The order matters: it is part of the key derivation.
	Recipients [][]byte

	// EncryptMetadata indicates whether the document-level metadata stream
	// is encrypted. It changes the derived key.
	EncryptMetadata bool
}

var ErrRecipientNotOpened = errors.New("a recipient entry addressed to the certificate could not be opened with the private key")

var (
	_               StdHandler = stdHandlerR4{}
	ErrNotRecipient            = errors.New("certificate is not among the recipients of this document")
)

// PubSecPermissions decodes the 4 big-endian permission bytes that follow the
// seed in a recipient's envelope. Bits outside those defined by Table 24 are
// ignored. A recipient with the owner bit set gets PermOwner.
func PubSecPermissions(p [4]byte) Permissions {
	perm := Permissions(binary.BigEndian.Uint32(p[:])) & pubSecTable24Mask
	if perm&pubSecOwnerBit != 0 {
		return PermOwner
	}
	return perm
}

const (
	PubSecSubFilterS3    = "adbe.pkcs7.s3"
	PubSecSubFilterS4    = "adbe.pkcs7.s4"
	PubSecSubFilterS5    = "adbe.pkcs7.s5"
	PubSecSeedLength     = 20
	PubSecEnvelopeLength = PubSecSeedLength + 4
)

// PubSecWrap builds the /Recipients entries for recipients, which all get
// the same permission bytes: one CMS EnvelopedData object addressed to every
// certificate (Table 23: one object per set of equal access rights),
// enveloping the 20-byte seed followed by the 4 permission bytes. The content
// is encrypted with AES-128-CBC and its key transported with RSA PKCS#1
// v1.5; every certificate must carry an RSA public key.
func PubSecWrap(seed []byte, perm [4]byte, recipients []*x509.Certificate) ([][]byte, error) {
	if len(seed) != PubSecSeedLength {
		return nil, fmt.Errorf("seed is %d bytes, want %d", len(seed), PubSecSeedLength)
	}
	if len(recipients) == 0 {
		return nil, errors.New("at least one recipient certificate is required")
	}
	envelope := make([]byte, PubSecEnvelopeLength)
	copy(envelope, seed)
	copy(envelope[PubSecSeedLength:], perm[:])
	for i, cert := range recipients {
		if cert == nil {
			return nil, fmt.Errorf("recipient %d: certificate is nil", i)
		}
		if _, ok := cert.PublicKey.(*rsa.PublicKey); !ok {
			return nil, fmt.Errorf("recipient %d: public key is %T, only RSA keys are supported", i, cert.PublicKey)
		}
	}
	content, err := pkcs7.EncryptWithAlgorithm(envelope, recipients, pkcs7.EncryptionAlgorithmAES128CBC)
	if err != nil {
		return nil, err
	}
	return [][]byte{content}, nil
}

const (
	pubSecOwnerBit    = Permissions(1 << 1)
	pubSecTable24Mask = Permissions(0x0F3E)
)

// PubSecFileKey derives the file encryption key from a recipient's seed and
// the complete /Recipients array (ISO 32000-1 §7.6.4.3). The hash is SHA-1 for
// V up to 4 and SHA-256 for V 5; its input is the seed, the raw bytes of every
// recipient entry in array order, and four 0xFF bytes when the metadata is
// not encrypted. The first lengthBits/8 bytes of the digest form the key.
//
// The digest is selected by V and not by SubFilter: PDFBox labels V4 AESV2
// files adbe.pkcs7.s5 and still hashes with SHA-1.
func PubSecFileKey(seed []byte, recipients [][]byte, v, lengthBits int, encryptMetadata bool) []byte {
	var h hash.Hash
	if v >= 5 {
		h = sha256.New()
	} else {
		h = sha1.New()
	}
	h.Write(seed)
	for _, r := range recipients {
		h.Write(r)
	}
	if !encryptMetadata {
		h.Write([]byte{0xff, 0xff, 0xff, 0xff})
	}
	digest := h.Sum(nil)
	n := lengthBits / 8
	if n <= 0 || n > len(digest) {
		n = len(digest)
	}
	return digest[:n]
}

var (
	_                       StdHandler = stdHandlerR6{}
	ErrUnsupportedRecipient            = errors.New("a recipient entry of this document uses an unsupported format")
)

// PubSecUnwrap finds the /Recipients entry that cert can open, decrypts it
// with key and returns the 20-byte seed, the 4 permission bytes and the index
// of the entry. Entries are tried in array order and the first match wins
// (ISO 32000-2 Table 23: a recipient listed twice gets the permissions of the
// first list). An entry that cannot be read, or whose recipient matches cert
// by issuer and serial but does not decrypt with key (two certificates can
// share an issuer and serial), does not stop the search. When nothing opens,
// the error is, in order: ErrRecipientNotOpened if an entry addressed to cert
// did not open, ErrUnsupportedRecipient if some entry could not be read,
// otherwise ErrNotRecipient.
//
// key must be a complete *rsa.PrivateKey; RSA key transport is the only
// recipient type the CMS library decrypts. Problems with the key itself are
// reported as such, since they reveal nothing about the document.
func PubSecUnwrap(recipients [][]byte, cert *x509.Certificate, key crypto.PrivateKey) (seed []byte, perm [4]byte, index int, err error) {
	if cert == nil || key == nil {
		return nil, perm, -1, errors.New("certificate and private key are required")
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, perm, -1, fmt.Errorf("private key is %T, only *rsa.PrivateKey is supported", key)
	}
	if rsaKey.N == nil || rsaKey.D == nil {
		return nil, perm, -1, errors.New("private key is incomplete")
	}
	var unsupportedErr, notOpenedErr error
	for i, raw := range recipients {
		p7, err := pkcs7.Parse(raw)
		if err != nil {
			if unsupportedErr == nil {
				unsupportedErr = fmt.Errorf("recipient %d: %w", i, err)
			}
			continue
		}
		plain, err := p7.Decrypt(cert, key)
		if err != nil {
			if errors.Is(err, pkcs7.ErrNotEncryptedContent) {
				if unsupportedErr == nil {
					unsupportedErr = fmt.Errorf("recipient %d: %w", i, err)
				}
				continue
			}
			if errors.Is(err, pkcs7.ErrNoRecipient) {
				continue
			}
			if errors.Is(err, pkcs7.ErrUnsupportedAlgorithm) {
				if unsupportedErr == nil {
					unsupportedErr = fmt.Errorf("recipient %d: %w", i, err)
				}
				continue
			}
			if notOpenedErr == nil {
				notOpenedErr = fmt.Errorf("recipient %d: %w", i, ErrRecipientNotOpened)
			}
			continue
		}
		if len(plain) != PubSecEnvelopeLength {
			if notOpenedErr == nil {
				notOpenedErr = fmt.Errorf("recipient %d: %w", i, ErrRecipientNotOpened)
			}
			continue
		}
		copy(perm[:], plain[PubSecSeedLength:])
		return plain[:PubSecSeedLength], perm, i, nil
	}
	switch {
	case notOpenedErr != nil:
		return nil, perm, -1, notOpenedErr
	case unsupportedErr != nil:
		return nil, perm, -1, fmt.Errorf("%w (%v)", ErrUnsupportedRecipient, unsupportedErr)
	}
	return nil, perm, -1, ErrNotRecipient
}
