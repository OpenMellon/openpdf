package errors_test

import (
	"errors"
	jerrors "github.com/OpenMellon/openpdf/internal/jbig2/errors"
	"strings"
	"testing"
)

func TestWrapRetainsCause(t *testing.T) {
	cause := errors.New("bad data")
	err := jerrors.Wrapf(cause, "decode", "page %d", 3)
	if !errors.Is(err, cause) {
		t.Fatalf("wrapped error lost cause: %v", err)
	}
	for _, part := range []string{"Process: decode", "Message: page 3", "bad data"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("%q missing from %q", part, err)
		}
	}
}
