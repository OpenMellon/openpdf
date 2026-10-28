package security

import (
	"bytes"
	"crypto/md5"
	"crypto/rand"
	"crypto/rc4"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/OpenMellon/openpdf/common"
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

func checkFieldLen(fn, field string, want int, data []byte) error {
	if len(data) < want {
		return errInvalidField{Func: fn, Field: field, Exp: want, Got: len(data)}
	}
	return nil
}

type stdHandlerR4 struct {
	Length int
	ID0    string
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

func (stdHandlerR4) paddedPass(pass []byte) []byte {
	out := make([]byte, 32)
	n := copy(out, pass)
	for ; n < 32; n++ {
		out[n] = passwordPadding[n-len(pass)]
	}
	return out
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

// NewHandlerR4 creates a new standard security handler for R<=4.
func NewHandlerR4(id0 string, length int) StdHandler { return stdHandlerR4{ID0: id0, Length: length} }

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

func (e errInvalidField) Error() string {
	return fmt.Sprintf("%s: expected %s field to be %d bytes, got %d", e.Func, e.Field, e.Exp, e.Got)
}
