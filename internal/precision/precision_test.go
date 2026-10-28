package precision_test

import (
	"testing"

	"github.com/OpenMellon/openpdf/internal/precision"
)

func TestRoundFloat(t *testing.T) {
	for _, tc := range []struct {
		value     float64
		precision int
		want      float64
	}{
		{1.23456, 4, 1.2346}, {-1.25, 1, -1.3}, {149, -2, 100}, {151, -2, 200},
	} {
		if got := precision.RoundFloat(tc.value, tc.precision); got != tc.want {
			t.Errorf("RoundFloat(%v, %d) = %v, want %v", tc.value, tc.precision, got, tc.want)
		}
	}
}
