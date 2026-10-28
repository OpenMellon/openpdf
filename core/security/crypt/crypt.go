// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package crypt

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"fmt"
	"io"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
)

func init() { registerFilter("AESV2", newFilterAESV2) }
func newFilterAESV2(dict FilterDict) (Filter, error) {
	if dict.Length == 128 {
		common.Log.Debug("AESV2 crypt filter length appears to be in bits rather than bytes - assuming bits (%d)", dict.Length)
		dict.Length /= 8
	}
	if dict.Length != 0 && dict.Length != 16 {
		return nil, fmt.Errorf("invalid AESV2 crypt filter length (%d)", dict.Length)
	}
	return filterAESV2{}, nil
}

// NewFilterV2 creates a RC4-based filter with a specified key length (in bytes).
func NewFilterV2(length int) Filter {
	filter, err := newFilterV2(FilterDict{Length: length})
	if err != nil {
		common.Log.Error("ERROR: could not create RC4 V2 crypt filter: %v", err)
		return filterV2{keyLength: length}
	}
	return filter
}

// EncryptBytes implements Filter interface.
func (filterV2) EncryptBytes(buf []byte, okey []byte) ([]byte, error) {
	c, err := rc4.NewCipher(okey)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("RC4 Encrypt: % x", buf)
	}
	c.XORKeyStream(buf, buf)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("to: % x", buf)
	}
	return buf, nil
}

// KeyLength implements Filter interface.
func (filterAESV3) KeyLength() int { return 256 / 8 }

type filterV2 struct{ keyLength int }

var (
	_ Filter = filterAESV3{}
	_ Filter = filterAESV2{}
)

// NewFilterAESV3 creates an AES-based filter with a 256 bit key (AESV3).
func NewFilterAESV3() Filter {
	filter, err := newFilterAESV3(FilterDict{})
	if err != nil {
		common.Log.Error("ERROR: could not create AES V3 crypt filter: %v", err)
		return filterAESV3{}
	}
	return filter
}

type filterIdentity struct{}

func init() { registerFilter("AESV3", newFilterAESV3) }

// Filter is a common interface for crypt filter methods.
type Filter interface {
	// Name returns a name of the filter that should be used in CFM field of Encrypt dictionary.
	Name() string

	// KeyLength returns a length of the encryption key in bytes.
	KeyLength() int

	// PDFVersion reports the minimal version of PDF document that introduced this filter.
	PDFVersion() [2]int

	// HandlerVersion reports V and R parameters that should be used for this filter.
	HandlerVersion() (V, R int)

	// MakeKey generates a object encryption key based on file encryption key and object numbers.
	// Used only for legacy filters - AESV3 doesn't change the key for each object.
	MakeKey(objNum, genNum uint32, ekey []byte) ([]byte, error)

	// EncryptBytes encrypts a buffer using object encryption key, as returned by MakeKey.
	// Implementation may reuse a buffer and encrypt data in-place.
	EncryptBytes(buf []byte, okey []byte) ([]byte, error)

	// DecryptBytes decrypts a buffer using object encryption key, as returned by MakeKey.
	// Implementation may reuse a buffer and decrypt data in-place.
	DecryptBytes(buf []byte, okey []byte) ([]byte, error)
}

// PDFVersion implements Filter interface.
func (filterAESV2) PDFVersion() [2]int            { return [2]int{1, 5} }
func (filterIdentity) HandlerVersion() (V, R int) { return }

// KeyLength implements Filter interface.
func (f filterV2) KeyLength() int   { return f.keyLength }
func (filterIdentity) Name() string { return "Identity" }

// KeyLength implements Filter interface.
func (filterAESV2) KeyLength() int { return 128 / 8 }

func (filterAES) DecryptBytes(buf []byte, okey []byte) ([]byte, error) {
	block, err := aes.NewCipher(okey)
	if err != nil {
		return nil, err
	}
	if len(buf) < 16 {
		common.Log.Debug("ERROR AES invalid buf %s", buf)
		return buf, fmt.Errorf("AES: Buf len < 16 (%d)", len(buf))
	}
	iv := buf[:16]
	buf = buf[16:]
	if len(buf)%16 != 0 {
		common.Log.Debug(" iv (%d): % x", len(iv), iv)
		common.Log.Debug("buf (%d): % x", len(buf), buf)
		return buf, fmt.Errorf("AES buf length not multiple of 16 (%d)", len(buf))
	}
	mode := cipher.NewCBCDecrypter(block, iv)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("AES Decrypt (%d): % x", len(buf), buf)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("chop AES Decrypt (%d): % x", len(buf), buf)
	}
	mode.CryptBlocks(buf, buf)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("to (%d): % x", len(buf), buf)
	}
	if len(buf) == 0 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Empty buf, returning empty string")
		}
		return buf, nil
	}
	padLen := int(buf[len(buf)-1])
	if padLen > len(buf) {
		common.Log.Debug("Illegal pad length (%d > %d)", padLen, len(buf))
		return buf, fmt.Errorf("invalid pad length")
	}
	buf = buf[:len(buf)-padLen]
	return buf, nil
}

// Name implements Filter interface.
func (filterAESV2) Name() string { return "AESV2" }

// HandlerVersion implements Filter interface.
func (filterAESV3) HandlerVersion() (V, R int) { V, R = 5, 6; return }

// NewIdentity creates an identity filter that bypasses all data without changes.
func NewIdentity() Filter { return filterIdentity{} }

// NewFilter creates CryptFilter from a corresponding dictionary.
func NewFilter(d FilterDict) (Filter, error) {
	ctor, err := lookupFilter(d.CFM)
	if err != nil {
		return nil, err
	}
	filter, err := ctor(d)
	if err != nil {
		return nil, err
	}
	return filter, nil
}

func registerFilter(name string, ctor filterFunc) {
	if _, exists := filterRegistry[name]; exists {
		panic("already registered")
	}
	filterRegistry[name] = ctor
}
func init() { registerFilter("V2", newFilterV2) }

// MakeKey implements Filter interface.
func (filterAESV2) MakeKey(objNum, genNum uint32, ekey []byte) ([]byte, error) {
	return makeObjectKey(objNum, genNum, ekey, true)
}

func lookupFilter(name string) (filterFunc, error) {
	ctor := filterRegistry[name]
	if ctor == nil {
		return nil, fmt.Errorf("unsupported crypt filter: %q", name)
	}
	return ctor, nil
}
func (filterIdentity) DecryptBytes(p []byte, okey []byte) ([]byte, error) { return p, nil }
func newFilterV2(dict FilterDict) (Filter, error) {
	if dict.Length%8 != 0 {
		return nil, fmt.Errorf("crypt filter length not multiple of 8 (%d)", dict.Length)
	}
	if dict.Length < 5 || dict.Length > 16 {
		if dict.Length == 40 || dict.Length == 64 || dict.Length == 128 {
			common.Log.Debug("STANDARD VIOLATION: Crypt Length appears to be in bits rather than bytes - assuming bits (%d)", dict.Length)
			dict.Length /= 8
		} else {
			return nil, fmt.Errorf("crypt filter length not in range 40 - 128 bit (%d)", dict.Length)
		}
	}
	return filterV2{keyLength: dict.Length}, nil
}

type filterAESV3 struct{ filterAES }

var filterRegistry = make(map[string]filterFunc)

func (filterIdentity) MakeKey(objNum, genNum uint32, fkey []byte) ([]byte, error) { return fkey, nil }

type filterFunc func(dict FilterDict) (Filter, error)

func (filterIdentity) PDFVersion() [2]int { return [2]int{} }

// HandlerVersion implements Filter interface.
func (f filterV2) HandlerVersion() (V, R int)                             { V, R = 2, 3; return }
func (filterIdentity) EncryptBytes(p []byte, okey []byte) ([]byte, error) { return p, nil }

type filterAESV2 struct{ filterAES }

// Name implements Filter interface.
func (filterAESV3) Name() string { return "AESV3" }

// Name implements Filter interface.
func (filterV2) Name() string { return "V2" }

func newFilterAESV3(dict FilterDict) (Filter, error) {
	if dict.Length == 256 {
		common.Log.Debug("AESV3 crypt filter length appears to be in bits rather than bytes - assuming bits (%d)", dict.Length)
		dict.Length /= 8
	}
	if dict.Length != 0 && dict.Length != 32 {
		return nil, fmt.Errorf("invalid AESV3 crypt filter length (%d)", dict.Length)
	}
	return filterAESV3{}, nil
}
func (filterIdentity) KeyLength() int { return 0 }

// PDFVersion implements Filter interface.
func (filterAESV3) PDFVersion() [2]int { return [2]int{2, 0} }

// MakeKey implements Filter interface.
func (filterAESV3) MakeKey(_, _ uint32, ekey []byte) ([]byte, error) { return ekey, nil }

// HandlerVersion implements Filter interface.
func (filterAESV2) HandlerVersion() (V, R int) { V, R = 4, 4; return }

// PDFVersion implements Filter interface.
func (f filterV2) PDFVersion() [2]int { return [2]int{} }

type filterAES struct{}

func makeObjectKey(objNum, genNum uint32, ekey []byte, aesSalt bool) ([]byte, error) {
	data := make([]byte, len(ekey)+5)
	copy(data, ekey)
	for i := 0; i < 3; i++ {
		b := byte((objNum >> uint32(8*i)) & 0xff)
		data[i+len(ekey)] = b
	}
	for i := 0; i < 2; i++ {
		b := byte((genNum >> uint32(8*i)) & 0xff)
		data[i+len(ekey)+3] = b
	}
	if aesSalt {
		data = append(data, 0x73)
		data = append(data, 0x41)
		data = append(data, 0x6C)
		data = append(data, 0x54)
	}
	h := md5.New()
	h.Write(data)
	sum := h.Sum(nil)
	if len(ekey)+5 < 16 {
		return sum[0 : len(ekey)+5], nil
	}
	return sum, nil
}

// MakeKey implements Filter interface.
func (f filterV2) MakeKey(objNum, genNum uint32, ekey []byte) ([]byte, error) {
	return makeObjectKey(objNum, genNum, ekey, false)
}

// DecryptBytes implements Filter interface.
func (filterV2) DecryptBytes(buf []byte, okey []byte) ([]byte, error) {
	c, err := rc4.NewCipher(okey)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("RC4 Decrypt: % x", buf)
	}
	c.XORKeyStream(buf, buf)
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("to: % x", buf)
	}
	return buf, nil
}

func (filterAES) EncryptBytes(buf []byte, okey []byte) ([]byte, error) {
	block, err := aes.NewCipher(okey)
	if err != nil {
		return nil, err
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("AES Encrypt (%d): % x", len(buf), buf)
	}
	const blockSize = aes.BlockSize
	padLen := blockSize - len(buf)%blockSize
	for i := 0; i < padLen; i++ {
		buf = append(buf, byte(padLen))
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Padded to %d bytes", len(buf))
	}
	out := make([]byte, blockSize+len(buf))
	iv := out[:blockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return nil, err
	}
	mode := cipher.NewCBCEncrypter(block, iv)
	mode.CryptBlocks(out[blockSize:], buf)
	buf = out
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("to (%d): % x", len(buf), buf)
	}
	return buf, nil
}

// FilterDict represents information from a CryptFilter dictionary.
type FilterDict struct {
	CFM       string
	AuthEvent security.AuthEvent
	Length    int
}

var _ Filter = filterV2{}

// NewFilterAESV2 creates an AES-based filter with a 128 bit key (AESV2).
func NewFilterAESV2() Filter {
	filter, err := newFilterAESV2(FilterDict{})
	if err != nil {
		common.Log.Error("ERROR: could not create AES V2 crypt filter: %v", err)
		return filterAESV2{}
	}
	return filter
}
