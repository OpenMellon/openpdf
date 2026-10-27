// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package streamlimits

import (
	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

type Walker struct {
	MinRepeatCost int
	active        map[*core.PdfObjectStream]struct{}
	seen          map[*core.PdfObjectStream]struct{}
	depth         int
	walks         int
	ops           int
	bytes         int
	exhausted     bool
}

const RenderRepeatCost = MaxStreamOpsPerPage / 10000

func (w *Walker) Leave(stream *core.PdfObjectStream) { delete(w.active, stream); w.depth-- }

const MaxStreamOpsPerPage = 1000000

func (w *Walker) Exhausted() bool { return w.exhausted }

const GroupCost = MaxStreamOpsPerPage / 10000

func (w *Walker) Enter(stream *core.PdfObjectStream, name string) bool {
	if w.exhausted {
		common.Log.Debug("page budget for nested streams is spent; skipping %s", name)
		return false
	}
	if _, onStack := w.active[stream]; onStack {
		common.Log.Debug("%s is already on the invocation stack; skipping", name)
		return false
	}
	if w.depth >= MaxDepth {
		common.Log.Debug("%s nested deeper than %d levels; skipping", name, MaxDepth)
		return false
	}
	if w.active == nil {
		w.active = map[*core.PdfObjectStream]struct{}{}
	}
	w.active[stream] = struct{}{}
	w.depth++
	return true
}
func (w *Walker) Ops() int { return w.ops }
func (w *Walker) Charge(stream *core.PdfObjectStream, ops, parsed, buffers int) {
	w.walks++
	cost := buffers * GroupCost
	if _, repeated := w.seen[stream]; repeated {
		cost += max(1, w.MinRepeatCost, ops)
		w.bytes += parsed
	} else {
		if w.seen == nil {
			w.seen = map[*core.PdfObjectStream]struct{}{}
		}
		w.seen[stream] = struct{}{}
	}
	w.ops += cost
	if !w.exhausted && (w.ops > MaxStreamOpsPerPage || w.bytes > MaxStreamBytesPerPage) {
		common.Log.Debug("page passed its budget of %d repeated nested stream operators or %d bytes; refusing later streams", MaxStreamOpsPerPage, MaxStreamBytesPerPage)
		w.exhausted = true
	}
}
func (w *Walker) Walks() int { return w.walks }

const MaxDepth = 64

func (w *Walker) Depth() int  { return w.depth }
func (w *Walker) Bytes() int  { return w.bytes }
func (w *Walker) Active() int { return len(w.active) }

const MaxStreamBytesPerPage = 256 << 20
