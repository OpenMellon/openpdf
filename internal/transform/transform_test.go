package transform_test

import (
	"math"
	"testing"

	"github.com/OpenMellon/openpdf/internal/transform"
)

func TestMatrixInverseRoundTrip(t *testing.T) {
	m := transform.NewMatrix(2, 1, 3, 4, 5, 6)
	inverse, ok := m.Inverse()
	if !ok {
		t.Fatal("expected invertible matrix")
	}
	x, y := m.Transform(7, -2)
	x, y = inverse.Transform(x, y)
	if math.Abs(x-7) > 1e-9 || math.Abs(y+2) > 1e-9 {
		t.Fatalf("round trip = (%v, %v)", x, y)
	}
	if _, ok := transform.NewMatrix(1, 2, 2, 4, 0, 0).Inverse(); ok {
		t.Fatal("singular matrix was invertible")
	}
}
