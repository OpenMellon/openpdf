package endian_test

import (
	"encoding/binary"
	"testing"

	"github.com/OpenMellon/openpdf/internal/endian"
)

func TestByteOrder(t *testing.T) {
	if endian.IsBig() == endian.IsLittle() {
		t.Fatal("endianness flags should be complementary")
	}
	var b [2]byte
	endian.ByteOrder.PutUint16(b[:], 0x0102)
	if endian.IsBig() && b != [2]byte{1, 2} {
		t.Fatalf("big-endian bytes = %v", b)
	}
	if endian.IsLittle() && b != [2]byte{2, 1} {
		t.Fatalf("little-endian bytes = %v", b)
	}
	if binary.NativeEndian.Uint16(b[:]) != 0x0102 {
		t.Fatalf("byte order differs from native endian: %v", b)
	}
}
