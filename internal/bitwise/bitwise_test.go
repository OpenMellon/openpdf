package bitwise_test

import (
	"testing"

	"github.com/OpenMellon/openpdf/internal/bitwise"
)

func TestBufferedMSBBitRoundTrip(t *testing.T) {
	w := bitwise.BufferedMSB()
	for _, bit := range []int{1, 0, 1, 1, 0, 0, 1, 0} {
		if err := w.WriteBit(bit); err != nil {
			t.Fatal(err)
		}
	}
	data := w.Data()
	if len(data) == 0 || data[0] != 0xb2 {
		t.Fatalf("written bytes = %x, want first byte b2", data)
	}
	r := bitwise.NewReader(data)
	for i, want := range []int{1, 0, 1, 1, 0, 0, 1, 0} {
		got, err := r.ReadBit()
		if err != nil || got != want {
			t.Fatalf("bit %d = %d, %v; want %d", i, got, err, want)
		}
	}
}

func TestFixedWriterBitOrder(t *testing.T) {
	for _, tc := range []struct {
		name      string
		newWriter func([]byte) *bitwise.Writer
		want      byte
	}{
		{"MSB", bitwise.NewWriterMSB, 0x80},
		{"LSB", bitwise.NewWriter, 0x01},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.newWriter(make([]byte, 1))
			if err := w.WriteBit(1); err != nil {
				t.Fatal(err)
			}
			if got := w.Data()[0]; got != tc.want {
				t.Fatalf("first byte = %02x, want %02x", got, tc.want)
			}
		})
	}
}

func TestReaderMarkAndResetWithinByte(t *testing.T) {
	r := bitwise.NewReader([]byte{0xb2})
	if _, err := r.ReadBit(); err != nil {
		t.Fatal(err)
	}
	r.Mark()
	first, err := r.ReadBits(4)
	if err != nil {
		t.Fatal(err)
	}
	r.Reset()
	again, err := r.ReadBits(4)
	if err != nil || again != first {
		t.Fatalf("after reset = %d, %v; want %d", again, err, first)
	}
}

func TestPartialReaderOffsets(t *testing.T) {
	r := bitwise.NewReader([]byte{0x11, 0x22, 0x33})
	partial, err := r.NewPartialReader(1, 2, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := partial.ReadByte()
	if err != nil || got != 0x22 {
		t.Fatalf("first partial byte = %x, %v", got, err)
	}
	if partial.AbsolutePosition() != 2 || partial.Length() != 2 {
		t.Fatalf("position = %d, length = %d", partial.AbsolutePosition(), partial.Length())
	}
	if _, err := r.NewPartialReader(2, 2, false); err == nil {
		t.Fatal("expected out-of-range partial reader to fail")
	}
}

func TestReadUnalignedByte(t *testing.T) {
	r := bitwise.NewReader([]byte{0xb2, 0x68})
	if _, err := r.ReadBits(3); err != nil {
		t.Fatal(err)
	}
	got, err := r.ReadByte()
	if err != nil || got != 0x93 {
		t.Fatalf("unaligned byte = %02x, %v; want 93", got, err)
	}
}

func TestReaderSeekResetsBitAlignment(t *testing.T) {
	r := bitwise.NewReader([]byte{0x80, 0x35})
	if _, err := r.ReadBit(); err != nil {
		t.Fatal(err)
	}
	if pos, err := r.Seek(1, 0); err != nil || pos != 1 {
		t.Fatalf("Seek = %d, %v", pos, err)
	}
	if got, err := r.ReadByte(); err != nil || got != 0x35 {
		t.Fatalf("ReadByte = %x, %v", got, err)
	}
}

func TestBufferedMSBWriteBitsAcrossByteBoundary(t *testing.T) {
	w := bitwise.BufferedMSB()
	if _, err := w.WriteBits(0x1234, 13); err != nil {
		t.Fatal(err)
	}
	r := bitwise.NewReader(w.Data())
	got, err := r.ReadBits(13)
	if err != nil || got != 0x1234 {
		t.Fatalf("read bits = %x, %v; want 1234", got, err)
	}
	if _, err := w.WriteBits(0, 65); err == nil {
		t.Fatal("expected invalid bit count to fail")
	}
}

func TestFixedWriterWriteBitsAndSkip(t *testing.T) {
	w := bitwise.NewWriterMSB(make([]byte, 2))
	if err := w.SkipBits(3); err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteBits(0x1ab, 9); err != nil {
		t.Fatal(err)
	}
	r := bitwise.NewReader(w.Data())
	if prefix, err := r.ReadBits(3); err != nil || prefix != 0 {
		t.Fatalf("prefix = %d, %v", prefix, err)
	}
	if got, err := r.ReadBits(9); err != nil || got != 0x1ab {
		t.Fatalf("value = %x, %v", got, err)
	}
	if err := w.SkipBits(-100); err == nil {
		t.Fatal("expected negative position to fail")
	}
}

func TestReadBitsAcrossBufferedAndWholeBytes(t *testing.T) {
	r := bitwise.NewReader([]byte{0xab, 0xcd, 0xef})
	if got, err := r.ReadBits(3); err != nil || got != 5 {
		t.Fatalf("first bits = %x, %v", got, err)
	}
	if got, err := r.ReadBits(13); err != nil || got != 0xbcd {
		t.Fatalf("next bits = %x, %v", got, err)
	}
	if got, err := r.ReadBits(8); err != nil || got != 0xef {
		t.Fatalf("last byte = %x, %v", got, err)
	}
	if _, err := r.ReadBits(1); err == nil {
		t.Fatal("expected end of input")
	}
}
