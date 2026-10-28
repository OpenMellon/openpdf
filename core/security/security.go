// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package security

import (
	"bytes"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/stringprep"
	"github.com/OpenMellon/pkcs7"
)

func (h stdHandlerR4) alg4(key []byte, _ []byte) ([]byte, error) {
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, errors.New("failed rc4 ciph")
	}
	pad := []byte(passwordPadding)
	out := make([]byte, len(pad))
	c.XORKeyStream(out, pad)
	return out, nil
}

func (x *ecbDecrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		common.Log.Error("ERROR: ECB decrypt: input not full blocks")
		return
	}
	if len(dst) < len(src) {
		common.Log.Error("ERROR: ECB decrypt: output smaller than input")
		return
	}
	for len(src) > 0 {
		x.block.Decrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}
func (x *ecbDecrypter) BlockSize() int { return x.blockSize }

const (
	PermOwner             = Permissions(math.MaxUint32)
	PermPrinting          = Permissions(1 << 2)
	PermModify            = Permissions(1 << 3)
	PermExtractGraphics   = Permissions(1 << 4)
	PermAnnotate          = Permissions(1 << 5)
	PermFillForms         = Permissions(1 << 8)
	PermDisabilityExtract = Permissions(1 << 9)
	PermRotateInsert      = Permissions(1 << 10)
	PermFullPrintQuality  = Permissions(1 << 11)
)

func newECBDecrypter(block cipher.Block) cipher.BlockMode { return (*ecbDecrypter)(newECB(block)) }

// StdEncryptDict is a set of additional fields used in standard encryption dictionary.
type StdEncryptDict struct {
	R               int
	P               Permissions
	EncryptMetadata bool
	O, U            []byte
	OE, UE          []byte
	Perms           []byte
}

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

// GenerateParams generates and sets O and U parameters for the encryption dictionary.
// It expects R, P and EncryptMetadata fields to be set.
func (h stdHandlerR4) GenerateParams(d *StdEncryptDict, opass, upass []byte) ([]byte, error) {
	O, err := h.alg3(d.R, upass, opass)
	if err != nil {
		common.Log.Debug("ERROR: Error generating O for encryption (%s)", err)
		return nil, err
	}
	d.O = O
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("gen O: % x", O)
	}
	key := h.alg2(d, upass)
	U, err := h.alg5(key, upass)
	if err != nil {
		common.Log.Debug("ERROR: Error generating O for encryption (%s)", err)
		return nil, err
	}
	d.U = U
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("gen U: % x", U)
	}
	return key, nil
}

func (h stdHandlerR6) alg11(d *StdEncryptDict, pass []byte) ([]byte, error) {
	if err := checkFieldLen("alg11", "U", 48, d.U); err != nil {
		return nil, err
	}
	buf := make([]byte, len(pass)+8)
	n := copy(buf, pass)
	n += copy(buf[n:], d.U[32:40])
	hashed, err := h.alg2b(d.R, buf, pass, nil)
	if err != nil {
		return nil, err
	}
	hashed = hashed[:32]
	if !bytes.Equal(hashed, d.U[:32]) {
		return nil, nil
	}
	return hashed, nil
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

type errInvalidField struct {
	Func  string
	Field string
	Exp   int
	Got   int
}

func (h stdHandlerR4) alg5(key []byte, _ []byte) ([]byte, error) {
	md := md5.New()
	md.Write([]byte(passwordPadding))
	md.Write([]byte(h.ID0))
	sum := md.Sum(nil)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("alg5")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ekey: % x", key)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("ID: % x", h.ID0)
	}
	if len(sum) != 16 {
		return nil, errors.New("hash length not 16 bytes")
	}
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, errors.New("failed rc4 ciph")
	}
	buf := make([]byte, 16)
	c.XORKeyStream(buf, sum)
	roundKey := make([]byte, len(key))
	for i := 0; i < 19; i++ {
		for j := 0; j < len(key); j++ {
			roundKey[j] = key[j] ^ byte(i+1)
		}
		c, err = rc4.NewCipher(roundKey)
		if err != nil {
			return nil, errors.New("failed rc4 ciph")
		}
		c.XORKeyStream(buf, buf)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("i = %d, ekey: % x", i, roundKey)
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("i = %d -> % x", i, buf)
		}
	}
	result := make([]byte, 32)
	for i := 0; i < 16; i++ {
		result[i] = buf[i]
	}
	_, err = rand.Read(result[16:32])
	if err != nil {
		return nil, errors.New("failed to gen rand number")
	}
	return result, nil
}

var (
	_               StdHandler = stdHandlerR4{}
	ErrNotRecipient            = errors.New("certificate is not among the recipients of this document")
)

func (h stdHandlerR6) alg2aUser(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	uHash, err := h.alg11(d, pass)
	if err != nil {
		return nil, 0, err
	}
	if len(uHash) == 0 {
		return nil, 0, nil
	}
	buf := make([]byte, len(pass)+8)
	n := copy(buf, pass)
	copy(buf[n:], d.U[40:48])
	return h.alg2aFileKey(d, buf, pass, nil, d.UE, d.P)
}

func (h stdHandlerR6) alg10(d *StdEncryptDict, key []byte) error {
	if err := checkFieldLen("alg10", "Key", 32, key); err != nil {
		return err
	}
	p64 := uint64(uint32(d.P)) | (math.MaxUint32 << 32)
	Perms := make([]byte, 16)
	binary.LittleEndian.PutUint64(Perms[:8], p64)
	if d.EncryptMetadata {
		Perms[8] = 'T'
	} else {
		Perms[8] = 'F'
	}
	copy(Perms[9:12], "adb")
	if _, err := io.ReadFull(rand.Reader, Perms[12:16]); err != nil {
		return err
	}
	block, err := newAESCipher(key[:32])
	if err != nil {
		return err
	}
	enc := newECBEncrypter(block)
	enc.CryptBlocks(Perms, Perms)
	d.Perms = Perms[:16]
	return nil
}

// Normalized returns p with the reserved bits of the P entry set to the
// values required by the PDF specification.
func (p Permissions) Normalized() Permissions { return (p | permReservedOnes) &^ permReservedZeros }

func checkFieldLen(fn, field string, want int, data []byte) error {
	if len(data) < want {
		return errInvalidField{Func: fn, Field: field, Exp: want, Got: len(data)}
	}
	return nil
}

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

type ecbEncrypter ecb

const (
	EventDocOpen = AuthEvent("DocOpen")
	EventEFOpen  = AuthEvent("EFOpen")
)

const (
	permReservedOnes  = Permissions(0xFFFFF0C0)
	permReservedZeros = Permissions(0x3)
)

func truncatePassword(pass []byte) []byte {
	if len(pass) > maxPasswordLen {
		return pass[:maxPasswordLen]
	}
	return pass
}

type stdHandlerR4 struct {
	Length int
	ID0    string
}

const (
	PubSecSubFilterS3    = "adbe.pkcs7.s3"
	PubSecSubFilterS4    = "adbe.pkcs7.s4"
	PubSecSubFilterS5    = "adbe.pkcs7.s5"
	PubSecSeedLength     = 20
	PubSecEnvelopeLength = PubSecSeedLength + 4
)

func alg2bHash(input, pass, udata []byte) ([]byte, error) {
	var sha256h, sha384h, sha512h hash.Hash
	sha256h = sha256.New()
	kBuf := make([]byte, 64)
	h0 := sha256h
	h0.Write(input)
	K := h0.Sum(kBuf[:0])
	buf := make([]byte, 64*(127+64+48))
	round := func(_ int) ([]byte, error) {
		size := len(pass) + len(K) + len(udata)
		seq := buf[:size]
		n := copy(seq, pass)
		n += copy(seq[n:], K[:])
		n += copy(seq[n:], udata)
		if n != size {
			common.Log.Error("ERROR: unexpected round input size.")
			return nil, errors.New("wrong size")
		}
		K1 := buf[:size*64]
		repeatFill(K1, size)
		block, err := newAESCipher(K[0:16])
		if err != nil {
			return nil, err
		}
		enc := cipher.NewCBCEncrypter(block, K[16:32])
		enc.CryptBlocks(K1, K1)
		E := K1
		sum := 0
		for i := 0; i < 16; i++ {
			sum += int(E[i] % 3)
		}
		var next hash.Hash
		switch sum % 3 {
		case 0:
			next = sha256h
		case 1:
			if sha384h == nil {
				sha384h = sha512.New384()
			}
			next = sha384h
		case 2:
			if sha512h == nil {
				sha512h = sha512.New()
			}
			next = sha512h
		}
		next.Reset()
		next.Write(E)
		K = next.Sum(kBuf[:0])
		return E, nil
	}
	for i := 0; ; {
		E, err := round(i)
		if err != nil {
			return nil, err
		}
		last := E[len(E)-1]
		i++
		if i >= 64 && last <= uint8(i-32) {
			break
		}
	}
	return K[:32], nil
}

func (h stdHandlerR6) alg8(d *StdEncryptDict, key []byte, pass []byte) error {
	if err := checkFieldLen("alg8", "Key", 32, key); err != nil {
		return err
	}
	var salts [16]byte
	if _, err := io.ReadFull(rand.Reader, salts[:]); err != nil {
		return err
	}
	valSalt := salts[0:8]
	keySalt := salts[8:16]
	buf := make([]byte, len(pass)+len(valSalt))
	n := copy(buf, pass)
	copy(buf[n:], valSalt)
	hashed, err := h.alg2b(d.R, buf, pass, nil)
	if err != nil {
		return err
	}
	U := make([]byte, len(hashed)+len(valSalt)+len(keySalt))
	n = copy(U, hashed[:32])
	n += copy(U[n:], valSalt)
	copy(U[n:], keySalt)
	d.U = U
	n = len(pass)
	copy(buf[n:], keySalt)
	hashed, err = h.alg2b(d.R, buf, pass, nil)
	if err != nil {
		return err
	}
	block, err := newAESCipher(hashed[:32])
	if err != nil {
		return err
	}
	iv := make([]byte, aes.BlockSize)
	enc := cipher.NewCBCEncrypter(block, iv)
	UE := make([]byte, 32)
	enc.CryptBlocks(UE, key[:32])
	d.UE = UE
	return nil
}

func (h stdHandlerR6) alg2aFileKey(d *StdEncryptDict, input, pass, udata, encKey []byte, perm Permissions) ([]byte, Permissions, error) {
	if err := checkFieldLen("alg2a", "Key", 32, encKey); err != nil {
		return nil, 0, err
	}
	encKey = encKey[:32]
	hashed, err := h.alg2b(d.R, input, pass, udata)
	if err != nil {
		return nil, 0, err
	}
	block, err := aes.NewCipher(hashed[:32])
	if err != nil {
		return nil, 0, err
	}
	iv := make([]byte, aes.BlockSize)
	dec := cipher.NewCBCDecrypter(block, iv)
	fileKey := make([]byte, 32)
	dec.CryptBlocks(fileKey, encKey)
	if d.R == 5 {
		return fileKey, perm, nil
	}
	err = h.alg13(d, fileKey)
	if err != nil {
		return nil, 0, err
	}
	return fileKey, perm, nil
}

func (h stdHandlerR6) alg2a(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	if err := checkFieldLen("alg2a", "O", 48, d.O); err != nil {
		return nil, 0, err
	}
	if err := checkFieldLen("alg2a", "U", 48, d.U); err != nil {
		return nil, 0, err
	}
	raw := truncatePassword(pass)
	cands := [][]byte{raw}
	prepared, err := saslPrep(pass)
	if err != nil {
		common.Log.Debug("AESV3 password preparation failed; trying raw bytes")
	} else if prepared = truncatePassword(prepared); !bytes.Equal(prepared, raw) {
		cands = [][]byte{prepared, raw}
	}
	for _, cand := range cands {
		key, perm, err := h.alg2aOwner(d, cand)
		if err != nil || len(key) != 0 {
			return key, perm, err
		}
	}
	for _, cand := range cands {
		key, perm, err := h.alg2aUser(d, cand)
		if err != nil || len(key) != 0 {
			return key, perm, err
		}
	}
	if len(cands[0]) != 0 && len(raw) != 0 {
		return h.alg2aUser(d, nil)
	}
	return nil, 0, nil
}

var errPasswordProhibited = errors.New("password contains characters prohibited by SASLprep")

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

func (h stdHandlerR4) alg6(d *StdEncryptDict, pass []byte) ([]byte, error) {
	var (
		computed []byte
		err      error
	)
	key := h.alg2(d, pass)
	if d.R == 2 {
		computed, err = h.alg4(key, pass)
	} else if d.R >= 3 {
		computed, err = h.alg5(key, pass)
	} else {
		return nil, errors.New("invalid R")
	}
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("check: % x == % x ?", string(computed), string(d.U))
	}
	got := computed
	want := d.U
	if d.R >= 3 {
		if len(got) > 16 {
			got = got[0:16]
		}
		if len(want) > 16 {
			want = want[0:16]
		}
	}
	if !bytes.Equal(got, want) {
		return nil, nil
	}
	return key, nil
}

const maxPasswordLen = 127

func (h stdHandlerR6) alg2aOwner(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	oHash, err := h.alg12(d, pass)
	if err != nil {
		return nil, 0, err
	}
	if len(oHash) == 0 {
		return nil, 0, nil
	}
	buf := make([]byte, len(pass)+8+48)
	n := copy(buf, pass)
	n += copy(buf[n:], d.O[40:48])
	copy(buf[n:], d.U[0:48])
	return h.alg2aFileKey(d, buf, pass, d.U[0:48], d.OE, PermOwner)
}

func isPrintableASCII(b []byte) bool {
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			return false
		}
	}
	return true
}

func (stdHandlerR4) paddedPass(pass []byte) []byte {
	out := make([]byte, 32)
	n := copy(out, pass)
	for ; n < 32; n++ {
		out[n] = passwordPadding[n-len(pass)]
	}
	return out
}

func (x *ecbEncrypter) CryptBlocks(dst, src []byte) {
	if len(src)%x.blockSize != 0 {
		common.Log.Error("ERROR: ECB encrypt: input not full blocks")
		return
	}
	if len(dst) < len(src) {
		common.Log.Error("ERROR: ECB encrypt: output smaller than input")
		return
	}
	for len(src) > 0 {
		x.block.Encrypt(dst, src[:x.blockSize])
		src = src[x.blockSize:]
		dst = dst[x.blockSize:]
	}
}

func newAESCipher(key []byte) (cipher.Block, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		common.Log.Error("ERROR: could not create AES cipher: %v", err)
		return nil, err
	}
	return block, nil
}

func repeatFill(buf []byte, size int) {
	filled := size
	for filled < len(buf) {
		copy(buf[filled:], buf[:filled])
		filled *= 2
	}
}

func (h stdHandlerR4) alg3Key(R int, pass []byte) []byte {
	md := md5.New()
	padded := h.paddedPass(pass)
	md.Write(padded)
	if R >= 3 {
		for i := 0; i < 50; i++ {
			sum := md.Sum(nil)
			md = md5.New()
			md.Write(sum)
		}
	}
	key := md.Sum(nil)
	if R == 2 {
		key = key[0:5]
	} else {
		key = key[0 : h.Length/8]
	}
	return key
}

const (
	pubSecOwnerBit    = Permissions(1 << 1)
	pubSecTable24Mask = Permissions(0x0F3E)
)

// Authenticate implements StdHandler interface.
func (h stdHandlerR4) Authenticate(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Debugging authentication - owner pass")
	}
	key, err := h.alg7(d, pass)
	if err != nil {
		return nil, 0, err
	}
	if key != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("this.authenticated = True")
		}
		return key, PermOwner, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Debugging authentication - user pass")
	}
	key, err = h.alg6(d, pass)
	if err != nil {
		return nil, 0, err
	}
	if key != nil {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("this.authenticated = True")
		}
		return key, d.P, nil
	}
	return nil, 0, nil
}

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

func (h stdHandlerR6) alg9(d *StdEncryptDict, key []byte, pass []byte) error {
	if err := checkFieldLen("alg9", "Key", 32, key); err != nil {
		return err
	}
	if err := checkFieldLen("alg9", "U", 48, d.U); err != nil {
		return err
	}
	var salts [16]byte
	if _, err := io.ReadFull(rand.Reader, salts[:]); err != nil {
		return err
	}
	valSalt := salts[0:8]
	keySalt := salts[8:16]
	uKey := d.U[:48]
	buf := make([]byte, len(pass)+len(valSalt)+len(uKey))
	n := copy(buf, pass)
	n += copy(buf[n:], valSalt)
	n += copy(buf[n:], uKey)
	hashed, err := h.alg2b(d.R, buf, pass, uKey)
	if err != nil {
		return err
	}
	O := make([]byte, len(hashed)+len(valSalt)+len(keySalt))
	n = copy(O, hashed[:32])
	n += copy(O[n:], valSalt)
	n += copy(O[n:], keySalt)
	d.O = O
	n = len(pass)
	n += copy(buf[n:], keySalt)
	hashed, err = h.alg2b(d.R, buf, pass, uKey)
	if err != nil {
		return err
	}
	block, err := newAESCipher(hashed[:32])
	if err != nil {
		return err
	}
	iv := make([]byte, aes.BlockSize)
	enc := cipher.NewCBCEncrypter(block, iv)
	OE := make([]byte, 32)
	enc.CryptBlocks(OE, key[:32])
	d.OE = OE
	return nil
}

// Permissions is a bitmask of access permissions for a PDF file.
type Permissions uint32

func (h stdHandlerR6) alg13(d *StdEncryptDict, key []byte) error {
	if err := checkFieldLen("alg13", "Key", 32, key); err != nil {
		return err
	}
	if err := checkFieldLen("alg13", "Perms", 16, d.Perms); err != nil {
		return err
	}
	perms := make([]byte, 16)
	copy(perms, d.Perms[:16])
	block, err := aes.NewCipher(key[:32])
	if err != nil {
		return err
	}
	dec := newECBDecrypter(block)
	dec.CryptBlocks(perms, perms)
	if !bytes.Equal(perms[9:12], []byte("adb")) {
		return errors.New("decoded permissions are invalid")
	}
	p := Permissions(binary.LittleEndian.Uint32(perms[0:4]))
	if p != d.P {
		return errors.New("permissions validation failed")
	}
	var encryptMetadata bool
	switch perms[8] {
	case 'T':
		encryptMetadata = true
	case 'F':
		encryptMetadata = false
	default:
		return errors.New("decoded metadata encryption flag is invalid")
	}
	if encryptMetadata != d.EncryptMetadata {
		return errors.New("metadata encryption validation failed")
	}
	return nil
}

func (h stdHandlerR4) alg7(d *StdEncryptDict, pass []byte) ([]byte, error) {
	key := h.alg3Key(d.R, pass)
	buf := make([]byte, len(d.O))
	if d.R == 2 {
		c, err := rc4.NewCipher(key)
		if err != nil {
			return nil, errors.New("failed cipher")
		}
		c.XORKeyStream(buf, d.O)
	} else if d.R >= 3 {
		tmp := append([]byte{}, d.O...)
		for i := 0; i < 20; i++ {
			k := append([]byte{}, key...)
			for j := 0; j < len(key); j++ {
				k[j] ^= byte(19 - i)
			}
			c, err := rc4.NewCipher(k)
			if err != nil {
				return nil, errors.New("failed cipher")
			}
			c.XORKeyStream(buf, tmp)
			tmp = append([]byte{}, buf...)
		}
	} else {
		return nil, errors.New("invalid R")
	}
	userKey, err := h.alg6(d, buf)
	if err != nil {
		return nil, nil
	}
	return userKey, nil
}

func (h stdHandlerR4) alg3(R int, upass, opass []byte) ([]byte, error) {
	var key []byte
	if len(opass) > 0 {
		key = h.alg3Key(R, opass)
	} else {
		key = h.alg3Key(R, upass)
	}
	c, err := rc4.NewCipher(key)
	if err != nil {
		return nil, errors.New("failed rc4 ciph")
	}
	padded := h.paddedPass(upass)
	out := make([]byte, len(padded))
	c.XORKeyStream(out, padded)
	if R >= 3 {
		k := make([]byte, len(key))
		for i := 0; i < 19; i++ {
			for j := 0; j < len(key); j++ {
				k[j] = key[j] ^ byte(i+1)
			}
			c, err := rc4.NewCipher(k)
			if err != nil {
				return nil, errors.New("failed rc4 ciph")
			}
			c.XORKeyStream(out, out)
		}
	}
	return out, nil
}

const passwordPadding = "\x28\277\116\136\x4e\x75\x8a\x41\x64\000\x4e\x56\377" + "\xfa\001\010\056\x2e\x00\xb6\xd0\x68\076\x80\x2f\014" + "\251\xfe\x64\x53\x69\172"

func newECBEncrypter(block cipher.Block) cipher.BlockMode { return (*ecbEncrypter)(newECB(block)) }
func (h stdHandlerR6) alg12(d *StdEncryptDict, pass []byte) ([]byte, error) {
	if err := checkFieldLen("alg12", "U", 48, d.U); err != nil {
		return nil, err
	}
	if err := checkFieldLen("alg12", "O", 48, d.O); err != nil {
		return nil, err
	}
	buf := make([]byte, len(pass)+8+48)
	n := copy(buf, pass)
	n += copy(buf[n:], d.O[32:40])
	n += copy(buf[n:], d.U[0:48])
	hashed, err := h.alg2b(d.R, buf, pass, d.U[0:48])
	if err != nil {
		return nil, err
	}
	hashed = hashed[:32]
	if !bytes.Equal(hashed, d.O[:32]) {
		return nil, nil
	}
	return hashed, nil
}

// StdHandler is an interface for standard security handlers.
type StdHandler interface {
	// GenerateParams uses owner and user passwords to set encryption parameters and generate an encryption key.
	// It assumes that R, P and EncryptMetadata are already set.
	GenerateParams(d *StdEncryptDict, opass, upass []byte) ([]byte, error)

	// Authenticate uses encryption dictionary parameters and the password to calculate
	// the document encryption key. It also returns permissions that should be granted to a user.
	// In case of failed authentication, it returns an empty key with no error.
	// Permissions may be zero even when authentication succeeds.
	Authenticate(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error)
}

func (x *ecbEncrypter) BlockSize() int { return x.blockSize }

var (
	_                       StdHandler = stdHandlerR6{}
	ErrUnsupportedRecipient            = errors.New("a recipient entry of this document uses an unsupported format")
)

// NewHandlerR6 creates a new standard security handler for R=5 and R=6.
func NewHandlerR6() StdHandler { return stdHandlerR6{} }

func prepareAESV3Password(pass []byte) ([]byte, error) {
	prepared, err := saslPrep(pass)
	if err != nil {
		return nil, err
	}
	if len(prepared) == 0 && len(pass) != 0 {
		return nil, errors.New("password is empty after SASLprep preparation")
	}
	if !utf8.Valid(pass) {
		common.Log.Warning("AESV3 password is not valid UTF-8; it is used unprepared and conforming readers will not accept it as typed")
	}
	return truncatePassword(prepared), nil
}

func sha256Sum(data []byte) ([]byte, error) {
	h := sha256.New()
	h.Write(data)
	return h.Sum(nil), nil
}

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

type ecb struct {
	block     cipher.Block
	blockSize int
}
type ecbDecrypter ecb

func saslPrep(pass []byte) ([]byte, error) {
	if isPrintableASCII(pass) || !utf8.Valid(pass) {
		return pass, nil
	}
	s, err := stringprep.SASLprep.Prepare(string(pass))
	if err != nil {
		return nil, errPasswordProhibited
	}
	return []byte(s), nil
}

// NewHandlerR4 creates a new standard security handler for R<=4.
func NewHandlerR4(id0 string, length int) StdHandler { return stdHandlerR4{ID0: id0, Length: length} }

type stdHandlerR6 struct{}

// AuthEvent is an event type that triggers authentication.
type AuthEvent string

// GenerateParams is the algorithm opposite to alg2a (R>=5).
// It generates U,O,UE,OE,Perms fields using AESv3 encryption.
// There is no algorithm number assigned to this function in the spec.
// It expects R, P and EncryptMetadata fields to be set.
// Both passwords are prepared with SASLprep and truncated to 127 bytes
// (Algorithm 2.A steps a and b); an error is returned if a password contains
// characters the profile prohibits or consists only of characters it removes.
func (h stdHandlerR6) GenerateParams(d *StdEncryptDict, opass, upass []byte) ([]byte, error) {
	upass, err := prepareAESV3Password(upass)
	if err != nil {
		return nil, fmt.Errorf("user password cannot be used for AESV3 encryption: %w", err)
	}
	opass, err = prepareAESV3Password(opass)
	if err != nil {
		return nil, fmt.Errorf("owner password cannot be used for AESV3 encryption: %w", err)
	}
	if len(opass) == 0 {
		opass = upass
	}
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, err
	}
	d.U = nil
	d.O = nil
	d.UE = nil
	d.OE = nil
	d.Perms = nil
	if err := h.alg8(d, key, upass); err != nil {
		return nil, err
	}
	if err := h.alg9(d, key, opass); err != nil {
		return nil, err
	}
	if d.R == 5 {
		return key, nil
	}
	if err := h.alg10(d, key); err != nil {
		return nil, err
	}
	return key, nil
}
func newECB(block cipher.Block) *ecb { return &ecb{block: block, blockSize: block.BlockSize()} }
func (h stdHandlerR4) alg2(d *StdEncryptDict, pass []byte) []byte {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("alg2")
	}
	padded := h.paddedPass(pass)
	md := md5.New()
	md.Write(padded)
	md.Write(d.O)
	var pBytes [4]byte
	binary.LittleEndian.PutUint32(pBytes[:], uint32(d.P))
	md.Write(pBytes[:])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("go P: % x", pBytes)
	}
	md.Write([]byte(h.ID0))
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("this.R = %d encryptMetadata %v", d.R, d.EncryptMetadata)
	}
	if (d.R >= 4) && !d.EncryptMetadata {
		md.Write([]byte{0xff, 0xff, 0xff, 0xff})
	}
	key := md.Sum(nil)
	if d.R >= 3 {
		md = md5.New()
		for i := 0; i < 50; i++ {
			md.Reset()
			md.Write(key[0 : h.Length/8])
			key = md.Sum(nil)
		}
	}
	if d.R >= 3 {
		return key[0 : h.Length/8]
	}
	return key[0:5]
}

// Authenticate implements StdHandler interface.
func (h stdHandlerR6) Authenticate(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	return h.alg2a(d, pass)
}

func (e errInvalidField) Error() string {
	return fmt.Sprintf("%s: expected %s field to be %d bytes, got %d", e.Func, e.Field, e.Exp, e.Got)
}

// Allowed checks if a set of permissions can be granted.
func (p Permissions) Allowed(p2 Permissions) bool { return p&p2 == p2 }

func (h stdHandlerR6) alg2b(R int, input, pass, udata []byte) ([]byte, error) {
	if R == 5 {
		return sha256Sum(input)
	}
	return alg2bHash(input, pass, udata)
}
