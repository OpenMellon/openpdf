// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package endian

import (
	"encoding/binary"
	"unsafe"
)

func IsBig() bool { return bigEndian }
func init() {
	const intSize = int(unsafe.Sizeof(0))
	value := 1
	bytes := (*[intSize]byte)(unsafe.Pointer(&value))
	if bytes[0] == 0 {
		bigEndian = true
		ByteOrder = binary.BigEndian
	} else {
		ByteOrder = binary.LittleEndian
	}
}

var (
	ByteOrder binary.ByteOrder
	bigEndian bool
)

func IsLittle() bool { return !bigEndian }
