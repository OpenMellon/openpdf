package security

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/internal/stringprep"
)

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

func truncatePassword(pass []byte) []byte {
	if len(pass) > maxPasswordLen {
		return pass[:maxPasswordLen]
	}
	return pass
}

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

type stdHandlerR6 struct{}

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

// Authenticate implements StdHandler interface.
func (h stdHandlerR6) Authenticate(d *StdEncryptDict, pass []byte) ([]byte, Permissions, error) {
	return h.alg2a(d, pass)
}

func (h stdHandlerR6) alg2b(R int, input, pass, udata []byte) ([]byte, error) {
	if R == 5 {
		return sha256Sum(input)
	}
	return alg2bHash(input, pass, udata)
}
