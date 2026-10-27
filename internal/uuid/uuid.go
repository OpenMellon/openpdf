// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package uuid

import (
	"crypto/rand"
	"encoding/hex"
	"io"
)

func encode(dst []byte, id UUID) {
	hex.Encode(dst, id[:4])
	dst[8] = '-'
	hex.Encode(dst[9:13], id[4:6])
	dst[13] = '-'
	hex.Encode(dst[14:18], id[6:8])
	dst[18] = '-'
	hex.Encode(dst[19:23], id[8:10])
	dst[23] = '-'
	hex.Encode(dst[24:], id[10:])
}

var Nil = zeroUUID
var randomReader = rand.Reader

func MustUUID() UUID {
	uuid, err := NewUUID()
	if err != nil {
		panic(err)
	}
	return uuid
}
func (id UUID) String() string { var buf [36]byte; encode(buf[:], id); return string(buf[:]) }

var zeroUUID UUID

func NewUUID() (UUID, error) {
	var uuid UUID
	_, err := io.ReadFull(randomReader, uuid[:])
	if err != nil {
		return zeroUUID, err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40
	uuid[8] = (uuid[8] & 0x3f) | 0x80
	return uuid, nil
}

type UUID [16]byte
