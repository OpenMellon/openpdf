// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textshaping

import (
	"slices"
	"strings"

	"github.com/OpenMellon/garabic"
	"golang.org/x/text/unicode/bidi"
)

// ArabicShape returns shaped arabic glyphs string.
func ArabicShape(text string) (string, error) {
	para := bidi.Paragraph{}
	para.SetString(text)
	ordering, err := para.Order()
	if err != nil {
		return "", err
	}
	for runIdx := 0; runIdx < ordering.NumRuns(); runIdx++ {
		run := ordering.Run(runIdx)
		runText := run.String()
		if run.Direction() == bidi.RightToLeft {
			var (
				shaped      = garabic.Shape(runText)
				shapedRunes = []rune(shaped)
				reversed    = make([]rune, len(shapedRunes))
			)
			dst := 0
			for _, shapedRune := range slices.Backward(shapedRunes) {
				reversed[dst] = shapedRune
				dst++
			}
			runText = string(reversed)
			text = strings.Replace(text, strings.TrimSpace(run.String()), runText, 1)
		}
	}
	return text, nil
}
