package uuid_test

import (
	"testing"

	"github.com/OpenMellon/openpdf/internal/uuid"
)

func TestUUIDString(t *testing.T) {
	id := uuid.UUID{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	if got, want := id.String(), "00010203-0405-0607-0809-0a0b0c0d0e0f"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestNewUUIDVersionAndVariant(t *testing.T) {
	id, err := uuid.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if id[6]>>4 != 4 || id[8]>>6 != 2 {
		t.Fatalf("invalid UUID version or variant: %x", id)
	}
}
