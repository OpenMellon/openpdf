package extractor

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/model"
)

func (wb *wordBag) maxDepth() float64 { return wb.pageHeight - wb.Lly }

func (wb *wordBag) removeDuplicates() {
	if debugRemoveDuplicates {
		common.Log.Info("removeDuplicates: %q", wb.text())
	}
	for _, idx := range wb.depthIndexes() {
		if len(wb.bins[idx]) == 0 {
			continue
		}
		first := wb.bins[idx][0]
		tol := duplicateToleranceFactor * first.fontSize
		depth := first.depth
		for _, bandIdx := range wb.depthBand(depth, depth+tol) {
			dups := map[*textWord]struct{}{}
			band := wb.bins[bandIdx]
			for _, w := range band {
				if _, ok := dups[w]; ok {
					continue
				}
				for _, other := range band {
					if _, ok := dups[other]; ok {
						continue
					}
					if other != w && other.text == w.text && math.Abs(other.Llx-w.Llx) < tol && math.Abs(other.Urx-w.Urx) < tol && math.Abs(other.Lly-w.Lly) < tol && math.Abs(other.Ury-w.Ury) < tol {
						dups[other] = struct{}{}
					}
				}
			}
			if len(dups) > 0 {
				n := 0
				for _, kept := range band {
					if _, ok := dups[kept]; !ok {
						band[n] = kept
						n++
					}
				}
				wb.bins[bandIdx] = band[:len(band)-len(dups)]
				if len(wb.bins[bandIdx]) == 0 {
					delete(wb.bins, bandIdx)
				}
			}
		}
	}
}

func removeWordFromSlice(words []*textWord, word *textWord) []*textWord {
	for i, w := range words {
		if w == word {
			return removeWordAt(words, i)
		}
	}
	common.Log.Error("removeWord: words doesn't contain word=%s", word)
	return nil
}

func (wb *wordBag) applyRemovals(removals map[int]map[*textWord]struct{}) {
	for idx, rm := range removals {
		if len(rm) == 0 {
			continue
		}
		bin := wb.bins[idx]
		numKept := len(bin) - len(rm)
		if numKept == 0 {
			delete(wb.bins, idx)
			continue
		}
		kept := make([]*textWord, numKept)
		n := 0
		for _, w := range bin {
			if _, ok := rm[w]; !ok {
				kept[n] = w
				n++
			}
		}
		wb.bins[idx] = kept
	}
}

func makeWordBag(words []*textWord, pageHeight float64, vertRulings, horzRulings rulingList) *wordBag {
	bag := newWordBag(words[0], pageHeight, vertRulings, horzRulings)
	for _, word := range words[1:] {
		idx := depthBinIndex(word.depth)
		bag.bins[idx] = append(bag.bins[idx], word)
		bag.PdfRectangle = unionRects(bag.PdfRectangle, word.PdfRectangle)
	}
	bag.sort()
	return bag
}

func mergeWordBags(bags []*wordBag) []*wordBag {
	if len(bags) <= 1 {
		return bags
	}
	if debugWordBags {
		common.Log.Info("mergeWordBags:")
	}
	sort.Slice(bags, func(i, j int) bool {
		a, b := bags[i], bags[j]
		areaA := a.Width() * a.Height()
		areaB := b.Width() * b.Height()
		if areaA != areaB {
			return areaA > areaB
		}
		if a.Height() != b.Height() {
			return a.Height() > b.Height()
		}
		return i < j
	})
	var merged []*wordBag
	absorbed := make(intSet)
	for i := 0; i < len(bags); i++ {
		if absorbed.has(i) {
			continue
		}
		big := bags[i]
		for j := i + 1; j < len(bags); j++ {
			if absorbed.has(i) {
				continue
			}
			small := bags[j]
			expanded := big.PdfRectangle
			expanded.Llx -= big.maxFontSize
			if rectContains(expanded, small.PdfRectangle) {
				big.absorb(small)
				absorbed.add(j)
			}
		}
		merged = append(merged, big)
	}
	if len(bags) != len(merged)+len(absorbed) {
		common.Log.Error("mergeWordBags: %d→%d absorbed=%d", len(bags), len(merged), len(absorbed))
	}
	return merged
}

func (wb *wordBag) blocked(word *textWord) bool {
	if word.Urx < wb.Llx {
		wordRight := rightEdgeRuling(word.PdfRectangle)
		bagLeft := leftEdgeRuling(wb.PdfRectangle)
		if wb.vertRulings.blocks(wordRight, bagLeft) {
			if debugBlocked {
				common.Log.Info("blocked ←x: %s %s", word, wb)
			}
			return true
		}
	} else if wb.Urx < word.Llx {
		bagRight := rightEdgeRuling(wb.PdfRectangle)
		wordLeft := leftEdgeRuling(word.PdfRectangle)
		if wb.vertRulings.blocks(bagRight, wordLeft) {
			if debugBlocked {
				common.Log.Info("blocked x→ : %s %s", word, wb)
			}
			return true
		}
	}
	if word.Ury < wb.Lly {
		wordTop := topEdgeRuling(word.PdfRectangle)
		bagBottom := bottomEdgeRuling(wb.PdfRectangle)
		if wb.horzRulings.blocks(wordTop, bagBottom) {
			if debugBlocked {
				common.Log.Info("blocked ←y: %s %s", word, wb)
			}
			return true
		}
	} else if wb.Ury < word.Lly {
		bagTop := topEdgeRuling(wb.PdfRectangle)
		wordBottom := bottomEdgeRuling(word.PdfRectangle)
		if wb.horzRulings.blocks(bagTop, wordBottom) {
			if debugBlocked {
				common.Log.Info("blocked y→ : %s %s", word, wb)
			}
			return true
		}
	}
	return false
}

func (wb *wordBag) depthIndexes() []int {
	if len(wb.bins) == 0 {
		return nil
	}
	idxs := make([]int, len(wb.bins))
	n := 0
	for i := range wb.bins {
		idxs[n] = i
		n++
	}
	sort.Ints(idxs)
	return idxs
}

func (wb *wordBag) allWords() []*textWord {
	var words []*textWord
	for _, bin := range wb.bins {
		words = append(words, bin...)
	}
	return words
}

func (wb *wordBag) pullWord(word *textWord, idx int, removals map[int]map[*textWord]struct{}) {
	wb.PdfRectangle = unionRects(wb.PdfRectangle, word.PdfRectangle)
	if word.fontSize > wb.maxFontSize {
		wb.maxFontSize = word.fontSize
	}
	wb.bins[idx] = append(wb.bins[idx], word)
	removals[idx][word] = struct{}{}
}

func (wb *wordBag) advanceBinStart(idx int) int {
	binStart := wb.binStart[idx]
	bin := wb.bins[idx]
	for binStart < len(bin) {
		if _, ok := wb.removed[bin[binStart]]; !ok {
			break
		}
		binStart++
	}
	wb.binStart[idx] = binStart
	return binStart
}

func (wb *wordBag) text() string {
	words := wb.allWords()
	strs := make([]string, len(words))
	for i, word := range words {
		strs[i] = word.text
	}
	return strings.Join(strs, " ")
}

// String returns a description of `b`.
func (wb *wordBag) String() string {
	var strs []string
	for _, n := range wb.depthIndexes() {
		bin := wb.bins[n]
		for _, bin2 := range bin {
			strs = append(strs, bin2.text)
		}
	}
	return fmt.Sprintf("%.2f fontsize=%.2f %d %q", wb.PdfRectangle, wb.maxFontSize, len(strs), strs)
}

func (wb *wordBag) arrangeText(noBreaks bool) *textPara {
	wb.sort()
	if removeDuplicateWords {
		wb.removeDuplicates()
	}
	wb.beginDeferredRemoval()
	defer wb.endDeferredRemoval()
	var lines []*textLine
	for _, binIdx := range wb.depthIndexes() {
		for !wb.empty(binIdx) {
			firstReadingIndex := wb.firstReadingIndex(binIdx)
			seed := wb.firstWord(firstReadingIndex)
			line := newLineFromBag(wb, firstReadingIndex)
			fontSize := seed.fontSize
			if fontSize < minFontSize {
				fontSize = minFontSize
			}
			depthLo := seed.depth - lineMergeFactor*fontSize
			depthHi := seed.depth + lineMergeFactor*fontSize
			maxGap := lineHeightMax * fontSize
			minOverlap := lineHeightMin * fontSize
		scanBands:
			for {
				var next *textWord
				nextIdx := 0
				for _, bi := range wb.depthBand(depthLo, depthHi) {
					cand := wb.highestWord(bi, depthLo, depthHi)
					if cand == nil {
						continue
					}
					gap := horizontalGap(cand, line.words[len(line.words)-1])
					if gap < -minOverlap {
						break scanBands
					}
					if !noBreaks && gap > maxGap {
						continue
					}
					if next != nil && compareLlx(cand, next) >= 0 {
						continue
					}
					next = cand
					nextIdx = bi
				}
				if next == nil {
					break
				}
				line.pullWord(wb, next, nextIdx)
			}
			line.markWordBoundaries()
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return nil
	}
	sort.Slice(lines, func(i, j int) bool { return compareDepthThenLlx(lines[i], lines[j]) < 0 })
	para := newTextPara(wb.PdfRectangle, lines)
	if debugWordBags {
		common.Log.Info("arrangeText !!! para=%s", para.String())
		if debugWordBagsPage {
			for i, line := range para.lines {
				fmt.Printf("%4d: %s\n", i, line.String())
				if debugWordBagsArrange {
					for j, word := range line.words {
						fmt.Printf("%8d: %s\n", j, word.String())
						for k, mark := range word.marks {
							fmt.Printf("%12d: %s\n", k, mark.String())
						}
					}
				}
			}
		}
	}
	return para
}
func (wb *wordBag) minDepth() float64 { return wb.pageHeight - (wb.Ury - wb.maxFontSize) }

func (wb *wordBag) removeWord(word *textWord, idx int) {
	if wb.removed != nil {
		if _, ok := wb.removed[word]; !ok {
			wb.removed[word] = struct{}{}
			wb.removedCount[idx]++
		}
		return
	}
	bin := wb.bins[idx]
	bin = removeWordFromSlice(bin, word)
	if len(bin) == 0 {
		delete(wb.bins, idx)
	} else {
		wb.bins[idx] = bin
	}
}

func (wb *wordBag) scanBand(label string, bag *wordBag, accept func(b *wordBag, w *textWord) bool, lo, hi, maxDiff float64, single, fixed bool) int {
	maxFontSize := bag.maxFontSize
	var removals map[int]map[*textWord]struct{}
	if !single {
		removals = wb.makeRemovals()
	}
	margin := lineMergeFactor * maxFontSize
	numPulled := 0
	for _, bi := range wb.depthBand(lo-margin, hi+margin) {
		if len(wb.bins[bi]) == 0 {
			continue
		}
		for _, w := range wb.bins[bi] {
			if !(lo-margin <= w.depth && w.depth <= hi+margin) {
				continue
			}
			if !accept(bag, w) {
				continue
			}
			relDiff := 2.0 * math.Abs(w.fontSize-bag.maxFontSize) / (w.fontSize + bag.maxFontSize)
			ratio := math.Max(w.fontSize/bag.maxFontSize, bag.maxFontSize/w.fontSize)
			sizeDiff := math.Min(relDiff, ratio)
			if maxDiff > 0 && sizeDiff > maxDiff {
				continue
			}
			if bag.blocked(w) {
				continue
			}
			if !single {
				bag.pullWord(w, bi, removals)
			}
			numPulled++
			if !fixed {
				if w.depth < lo {
					lo = w.depth
				}
				if w.depth > hi {
					hi = w.depth
				}
			}
			if single {
				break
			}
		}
	}
	if !single {
		wb.applyRemovals(removals)
	}
	return numPulled
}

func (wb *wordBag) getDepthIdx(depth float64) int {
	idxs := wb.depthIndexes()
	idx := depthBinIndex(depth)
	if idx < idxs[0] {
		return idxs[0]
	}
	if idx > idxs[len(idxs)-1] {
		return idxs[len(idxs)-1]
	}
	return idx
}

func (wb *wordBag) highestWord(idx int, lo, hi float64) *textWord {
	bin := wb.bins[idx]
	start := 0
	if wb.removed != nil {
		start = wb.advanceBinStart(idx)
	}
	for i := start; i < len(bin); i++ {
		w := bin[i]
		if wb.removed != nil {
			if _, ok := wb.removed[w]; ok {
				continue
			}
		}
		if lo <= w.depth && w.depth <= hi {
			return w
		}
	}
	return nil
}

func (wb *wordBag) firstWord(idx int) *textWord {
	bin := wb.bins[idx]
	if wb.removed == nil {
		return bin[0]
	}
	start := wb.advanceBinStart(idx)
	if start >= len(bin) {
		return nil
	}
	return bin[start]
}

func (wb *wordBag) empty(idx int) bool {
	words, ok := wb.bins[idx]
	if !ok {
		return true
	}
	if wb.removed != nil {
		return wb.removedCount[idx] >= len(words)
	}
	return false
}

func (wb *wordBag) depthRange(lo, hi int) []int {
	var idxs []int
	for idx := range wb.bins {
		if lo <= idx && idx <= hi {
			idxs = append(idxs, idx)
		}
	}
	if len(idxs) == 0 {
		return nil
	}
	sort.Ints(idxs)
	return idxs
}

func (wb *wordBag) absorb(bag *wordBag) {
	removals := bag.makeRemovals()
	for idx, bin := range bag.bins {
		for _, w := range bin {
			wb.pullWord(w, idx, removals)
		}
	}
	bag.applyRemovals(removals)
}

func (wb *wordBag) makeRemovals() map[int]map[*textWord]struct{} {
	removals := make(map[int]map[*textWord]struct{}, len(wb.bins))
	for i := range wb.bins {
		removals[i] = make(map[*textWord]struct{})
	}
	return removals
}

func (wb *wordBag) endDeferredRemoval() {
	if wb.removed == nil {
		return
	}
	for i, removedCount := range wb.removedCount {
		if removedCount == 0 {
			continue
		}
		bin := wb.bins[i]
		if removedCount >= len(bin) {
			delete(wb.bins, i)
			continue
		}
		kept := 0
		for _, bin2 := range bin {
			if _, ok := wb.removed[bin2]; !ok {
				bin[kept] = bin2
				kept++
			}
		}
		for j := kept; j < len(bin); j++ {
			bin[j] = nil
		}
		wb.bins[i] = bin[:kept]
	}
	wb.removed = nil
	wb.removedCount = nil
	wb.binStart = nil
}

func splitWordBag(src *wordBag, pageHeight float64, vertRulings, horzRulings rulingList, noBreaks bool) []*wordBag {
	var out []*wordBag
	for _, binIdx := range src.depthIndexes() {
		grew := false
		for !src.empty(binIdx) {
			seedIdx := src.firstReadingIndex(binIdx)
			seed := src.firstWord(seedIdx)
			bag := newWordBag(seed, pageHeight, vertRulings, horzRulings)
			src.removeWord(seed, seedIdx)
			if debugSplitBags {
				common.Log.Info("firstWord ^^^^ %s", seed.String())
			}
			for loop := true; loop; loop = grew {
				grew = false
				adjGap := otherScanOffsetFactor * bag.maxFontSize
				readingGap := horizontalScanOffsetFactor * bag.maxFontSize
				if noBreaks {
					readingGap = math.MaxFloat64
				}
				depthGap := scanMarginFactor * bag.maxFontSize
				if debugSplitBags {
					common.Log.Info("paraWords depth %.2f - %.2f maxIntraDepthGap=%.2f maxIntraReadingGap=%.2f", bag.minDepth(), bag.maxDepth(), depthGap, readingGap)
				}
				if src.scanBand("vertical", bag, withGap(overlapsHorizontally, 0), bag.minDepth()-depthGap, bag.maxDepth()+depthGap, verticalGapFraction, false, false) > 0 {
					grew = true
				}
				if src.scanBand("horizontal", bag, withGap(overlapsHorizontally, readingGap), bag.minDepth(), bag.maxDepth(), horizontalGapFraction, false, false) > 0 {
					grew = true
				}
				if grew {
					continue
				}
				numPulled := src.scanBand("", bag, withGap(isAdjacentRight, adjGap), bag.minDepth(), bag.maxDepth(), otherGapFraction, true, false)
				if numPulled > 0 {
					numLines := (bag.maxDepth() - bag.minDepth()) / bag.maxFontSize
					if (numPulled > 1 && float64(numPulled) > 0.3*numLines) || numPulled <= 10 {
						if src.scanBand("other", bag, withGap(isAdjacentRight, adjGap), bag.minDepth(), bag.maxDepth(), otherGapFraction, false, true) > 0 {
							grew = true
						}
					}
				}
			}
			out = append(out, bag)
		}
	}
	return out
}

func (wb *wordBag) depthBand(lo, hi float64) []int {
	if len(wb.bins) == 0 {
		return nil
	}
	return wb.depthRange(wb.getDepthIdx(lo), wb.getDepthIdx(hi))
}

func newWordBag(word *textWord, pageHeight float64, vertRulings, horzRulings rulingList) *wordBag {
	idx := depthBinIndex(word.depth)
	words := []*textWord{word}
	bag := wordBag{bins: map[int][]*textWord{idx: words}, PdfRectangle: word.PdfRectangle, maxFontSize: word.fontSize, pageHeight: pageHeight, vertRulings: vertRulings, horzRulings: horzRulings}
	return &bag
}

type wordBag struct {
	model.PdfRectangle
	maxFontSize              float64
	vertRulings, horzRulings rulingList
	pageHeight               float64
	bins                     map[int][]*textWord
	removed                  map[*textWord]struct{}
	removedCount             map[int]int
	binStart                 map[int]int
}

func removeWordAt(words []*textWord, idx int) []*textWord {
	numWords := len(words)
	copy(words[idx:], words[idx+1:])
	return words[:numWords-1]
}

func (wb *wordBag) firstReadingIndex(idx int) int {
	fontSize := wb.firstWord(idx).fontSize
	binBottom := float64(idx+1) * depthBinWidth
	limit := binBottom + readingOrderFontFactor*fontSize
	best := idx
	for _, bi := range wb.depthBand(binBottom, limit) {
		w := wb.firstWord(bi)
		if w == nil {
			continue
		}
		if compareLlx(w, wb.firstWord(best)) < 0 {
			best = bi
		}
	}
	return best
}

func (wb *wordBag) sort() {
	for _, bin := range wb.bins {
		sort.Slice(bin, func(i, j int) bool { return compareLlx(bin[i], bin[j]) < 0 })
	}
}

func (wb *wordBag) beginDeferredRemoval() {
	wb.removed = make(map[*textWord]struct{})
	wb.removedCount = make(map[int]int, len(wb.bins))
	wb.binStart = make(map[int]int, len(wb.bins))
}
