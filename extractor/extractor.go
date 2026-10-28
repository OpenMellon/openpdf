// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

// Package extractor is used for quickly extracting PDF content through a simple interface.
// Currently offers functionality for extracting textual content.
package extractor

import (
	"github.com/OpenMellon/openpdf/core"
)

func groupMarksByObject(marks *TextMarkArray) []*TextMarkArray {
	elems := marks.Elements()
	numMarks := len(elems)
	var prevObj core.PdfObject
	groups := []*TextMarkArray{}
	cur := &TextMarkArray{}
	idx := -1
	for i, m := range elems {
		obj := m.DirectObject
		idx = m.Index
		if obj == nil {
			adj := adjacentObjStringIndex(marks, i)
			if prevObj != nil {
				if adj == -1 || adj > i {
					groups = append(groups, cur)
					cur = &TextMarkArray{}
				}
			}
		} else if prevObj == nil {
			if idx == 0 && i > 0 {
				groups = append(groups, cur)
				cur = &TextMarkArray{}
			}
		} else {
			if obj != prevObj {
				groups = append(groups, cur)
				cur = &TextMarkArray{}
			}
		}
		prevObj = obj
		cur.Append(m)
		if i == (numMarks - 1) {
			groups = append(groups, cur)
		}
	}
	return groups
}
