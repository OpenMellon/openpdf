// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textshaping

import (
	"github.com/OpenMellon/garabic"
	"golang.org/x/text/unicode/bidi"
	"strings"
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
			for src := len(shapedRunes) - 1; src >= 0; src-- {
				reversed[dst] = shapedRunes[src]
				dst++
			}
			runText = string(reversed)
			text = strings.Replace(text, strings.TrimSpace(run.String()), runText, 1)
		}
	}
	return text, nil
}
