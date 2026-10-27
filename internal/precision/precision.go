// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package precision

import "math"

func RoundDefault(value float64) float64 { return RoundFloat(value, DefaultPrecision) }
func RoundFloat(value float64, precision int) float64 {
	var scale float64
	if precision >= 0 && precision < len(powersOfTen) {
		scale = powersOfTen[precision]
	} else {
		scale = math.Pow(10, float64(precision))
	}
	return math.Round(value*scale) / scale
}

const (
	DefaultPrecision = 4
)

var powersOfTen = [...]float64{1e0, 1e1, 1e2, 1e3, 1e4, 1e5, 1e6, 1e7, 1e8, 1e9, 1e10, 1e11, 1e12, 1e13, 1e14, 1e15, 1e16, 1e17, 1e18, 1e19, 1e20, 1e21, 1e22}
