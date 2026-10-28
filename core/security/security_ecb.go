package security

import (
	"crypto/aes"
	"crypto/cipher"

	"github.com/OpenMellon/openpdf/common"
)

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

func newECBDecrypter(block cipher.Block) cipher.BlockMode { return (*ecbDecrypter)(newECB(block)) }

type ecbEncrypter ecb

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

func newECBEncrypter(block cipher.Block) cipher.BlockMode { return (*ecbEncrypter)(newECB(block)) }

func (x *ecbEncrypter) BlockSize() int { return x.blockSize }

type ecb struct {
	block     cipher.Block
	blockSize int
}
type ecbDecrypter ecb

func newECB(block cipher.Block) *ecb { return &ecb{block: block, blockSize: block.BlockSize()} }
