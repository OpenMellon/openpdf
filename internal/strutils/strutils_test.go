package strutils_test

import (
	"bytes"
	"testing"

	"github.com/OpenMellon/openpdf/internal/strutils"
)

func TestUTF16RoundTrip(t *testing.T) {
	text := "PDF € 🦊"
	if got := strutils.UTF16ToString([]byte(strutils.StringToUTF16(text))); got != text {
		t.Fatalf("round trip = %q, want %q", got, text)
	}
}

func TestPDFDocEncoding(t *testing.T) {
	encoded := []byte{'A', 0x80, 0xa0}
	const decoded = "A•€"
	if got := strutils.PDFDocEncodingToString(encoded); got != decoded {
		t.Fatalf("decoded = %q", got)
	}
	if got := strutils.StringToPDFDocEncoding(decoded); !bytes.Equal(got, encoded) {
		t.Fatalf("encoded = %x", got)
	}
}
