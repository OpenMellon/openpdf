package timeutils_test

import (
	"github.com/OpenMellon/openpdf/internal/timeutils"
	"testing"
	"time"
)

func TestPDFTimeRoundTrip(t *testing.T) {
	original := time.Date(2024, 2, 29, 12, 34, 56, 0, time.FixedZone("test", -(5*60+30)*60))
	encoded := timeutils.FormatPdfTime(original)
	if encoded != "D:20240229123456-05'30'" {
		t.Fatalf("unexpected PDF date: %s", encoded)
	}
	parsed, err := timeutils.ParsePdfTime(encoded)
	if err != nil || !parsed.Equal(original) {
		t.Fatalf("ParsePdfTime(%q) = %v, %v", encoded, parsed, err)
	}
}

func TestParsePDFTimeDefaultsAndInvalid(t *testing.T) {
	got, err := timeutils.ParsePdfTime("2024")
	if err != nil || got.Year() != 2024 || got.Month() != time.January || got.Day() != 1 {
		t.Fatalf("unexpected date: %v, %v", got, err)
	}
	if _, err := timeutils.ParsePdfTime(""); err == nil {
		t.Fatal("empty date should fail")
	}
}
