// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package redactor

import (
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

type pageFormEdits struct {
	page    int
	res     *model.PdfPageResources
	targets []*editTarget
}

func (c *pageFormClones) rewireFormReferences(res *model.PdfPageResources, orig, clone *core.PdfObjectStream) (string, bool) {
	if res == nil || res.XObject == nil {
		return "the page has no XObject resources", false
	}
	xobjects, ok := core.TraceToDirectObject(res.XObject).(*core.PdfObjectDictionary)
	if !ok {
		return "the page XObject resources are not a dictionary", false
	}
	clear(c.noRewire)
	clear(c.rewired)
	c.walks = 0
	c.tooDeep, c.cyclic, c.walkLimit = false, false, false
	newXObjects, changed := c.rewireXObjects(xobjects, orig, clone, 0, map[*core.PdfObjectStream]bool{})
	if c.walkLimit {
		return "the page paints more forms than are walked for one edit, so not every reference to it could be rewired", false
	}
	if !changed {
		return c.unreferencedReason(), false
	}
	res.XObject = newXObjects
	return "", true
}

type formSearch struct {
	form  *core.PdfObjectStream
	depth int
}
