package extractor

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func reverseInts(in []int) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[len(in)-1-i] = v
	}
	return out
}

func replaceSingleByteString(marks *TextMarkArray, repl string, offset *int, limit int) error {
	var prevMark TextMark
	for _, mark := range marks.Elements() {
		text := mark.Text
		font := mark.Font
		sub := subTextForMark(repl, *offset, len(text), limit)
		directObject := mark.DirectObject
		if directObject == nil && mark.Text == " " {
			objStr := prevMark.ObjString
			lastObj := objStr[len(objStr)-1]
			if lastObj != mark.Text {
				directObject = prevMark.DirectObject
				font = prevMark.Font
				pdfStr, ok := core.GetString(directObject)
				if !ok {
					return fmt.Errorf("unable to get string Bytes from directObject")
				}
				raw, ok := core.GetStringBytes(directObject)
				if !ok {
					return core.ErrTypeError
				}
				prevText := decodeBytes(raw, font)
				sub = prevText + sub
				setStringFromText(pdfStr, sub, font)
				*offset += len(text)
				continue
			}
		}
		pdfStr, ok2 := core.GetString(directObject)
		if !ok2 {
			return fmt.Errorf("unable to get string Bytes from directObject")
		}
		newText := ""
		raw, ok2 := core.GetStringBytes(directObject)
		if !ok2 {
			return core.ErrTypeError
		}
		oldText := decodeBytes(raw, font)
		newText = strings.Replace(oldText, text, sub, 1)
		setStringFromText(pdfStr, newText, font)
		*offset += len(text)
		prevMark = mark
	}
	return nil
}

func newBidiText(s string, ltr bool, vertical bool) BidiText {
	dir := "ltr"
	if vertical {
		dir = "ttb"
	} else if !ltr {
		dir = "rtl"
	}
	return BidiText{text: s, direction: dir}
}

func decodeBytes(buf []byte, font *model.PdfFont) string {
	codes := font.BytesToCharcodes(buf)
	strs, numChars, numMisses := font.CharcodesToStrings(codes, "")
	if numMisses > 0 {
		common.Log.Debug("renderText: numChars=%d numMisses=%d", numChars, numMisses)
	}
	text := strings.Join(strs, "")
	return text
}

func combiningFor(s string) (string, bool) {
	runes := []rune(s)
	if len(runes) != 1 {
		return "", false
	}
	comb, ok := accentToCombining[runes[0]]
	return comb, ok
}

func alignToRuneStart(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// BidiText represents a bidi text organized in its visual order
// with base direction of the text.
type BidiText struct {
	text      string
	direction string
}

func reverseStrings(strs []string, from int, to int) {
	for i, j := from, to-1; i < j; i, j = i+1, j-1 {
		tmp := strs[i]
		strs[i] = strs[j]
		strs[j] = tmp
	}
}

var accentToCombining = map[rune]string{0x0060: "̀", 0x02CB: "̀", 0x0027: "́", 0x00B4: "́", 0x02B9: "́", 0x02CA: "́", 0x005E: "̂", 0x02C6: "̂", 0x007E: "̃", 0x02DC: "̃", 0x00AF: "̄", 0x02C9: "̄", 0x02D8: "̆", 0x02D9: "̇", 0x00A8: "̈", 0x00B0: "̊", 0x02DA: "̊", 0x02BA: "̋", 0x02DD: "̋", 0x02C7: "̌", 0x02C8: "̍", 0x0022: "̎", 0x02BB: "̒", 0x02BC: "̓", 0x0486: "̓", 0x055A: "̓", 0x02BD: "̔", 0x0485: "̔", 0x0559: "̔", 0x02D4: "̝", 0x02D5: "̞", 0x02D6: "̟", 0x02D7: "̠", 0x02B2: "̡", 0x00B8: "̧", 0x02CC: "̩", 0x02B7: "̫", 0x02CD: "̱", 0x005F: "̲", 0x204E: "͙"}

func replaceMultiByteString(marks *TextMarkArray, repl string, offset *int, limit int) error {
	obj := marks.Elements()[0].DirectObject
	pdfStr, ok := core.GetString(obj)
	if !ok {
		return fmt.Errorf("unable to get string Bytes from directObject")
	}
	markText := marksToString(marks)
	raw, ok := core.GetStringBytes(obj)
	if !ok {
		return core.ErrTypeError
	}
	font := marks.Elements()[0].Font
	strText := decodeBytes(raw, font)
	sub := subTextForMark(repl, *offset, len(markText), limit)
	newText := ""
	words := strings.Split(strText, " ")
	lastWord := words[len(words)-1]
	if lastWord == markText && *offset == 0 {
		pos := strings.LastIndex(strText, markText)
		newText = replaceRange(strText, pos, len(markText)+pos, sub)
	} else {
		newText = strings.Replace(strText, markText, sub, 1)
	}
	setStringFromText(pdfStr, newText, font)
	*offset += len(markText)
	return nil
}

var bidiTypesArabic = []string{"AN", "AN", "AN", "AN", "AN", "AN", "ON", "ON", "AL", "ET", "ET", "AL", "CS", "AL", "ON", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "AN", "ET", "AN", "AN", "AL", "AL", "AL", "NSM", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "AL", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AN", "ON", "NSM", "NSM", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "NSM", "NSM", "ON", "NSM", "NSM", "NSM", "NSM", "AL", "AL", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "AL", "AL", "AL", "AL", "AL", "AL"}

func reorderBidi(runes []rune) BidiText {
	baseLevel := -1
	vertical := false
	ltr := true
	numRunes := len(runes)
	chars := make([]string, numRunes)
	types := make([]string, numRunes)
	if numRunes == 0 || vertical {
		return newBidiText(string(runes), ltr, vertical)
	}
	numRTL := 0
	for i, runeV := range runes {
		chars[i] = string(runeV)
		typ := "L"
		if runeV <= 0x00ff {
			typ = bidiTypesLatin[runeV]
		} else if 0x0590 <= runeV && runeV <= 0x05f4 {
			typ = "R"
		} else if 0x0600 <= runeV && runeV <= 0x06ff {
			low := runeV & 0xff
			if int(low) >= len(bidiTypesArabic) {
				common.Log.Debug("Bidi: invalid Unicode character " + string(runeV))
			}
			typ = bidiTypesArabic[runeV&0xff]
		} else if (0x0700 <= runeV && runeV <= 0x08ac) || (0xfb50 <= runeV && runeV <= 0xfdff) || (0xfe70 <= runeV && runeV <= 0xfeff) {
			typ = "AL"
		}
		if typ == "R" || typ == "AL" || typ == "AN" {
			numRTL++
		}
		types[i] = typ
	}
	if numRTL == 0 {
		ltr = true
		return newBidiText(string(runes), ltr, false)
	}
	if baseLevel == -1 {
		if float64(numRTL)/float64(numRunes) < 0.3 && numRunes > 4 {
			ltr = true
			baseLevel = 0
		} else {
			ltr = false
			baseLevel = 1
		}
	}
	var levels []int
	for range runes {
		levels = append(levels, baseLevel)
	}
	baseType := "L"
	if isOdd(baseLevel) {
		baseType = "R"
	}
	baseDir := baseType
	prevType := baseDir
	lastStrong := baseDir
	for i := range runes {
		if types[i] == "NSM" {
			types[i] = lastStrong
		} else {
			lastStrong = types[i]
		}
	}
	lastStrong = baseDir
	var s string
	for i := range runes {
		s = types[i]
		switch s {
		case "EN":
			if lastStrong == "AL" {
				types[i] = "AN"
			} else {
				types[i] = "EN"
			}
		case "R", "L", "AL":
			lastStrong = s
		}
	}
	for i := range runes {
		typeV := types[i]
		if typeV == "AL" {
			types[i] = "R"
		}
	}
	for i := 1; i < (len(runes) - 1); i++ {
		if types[i] == "ES" && types[i-1] == "EN" && types[i+1] == "EN" {
			types[i] = "EN"
		}
		if types[i] == "CS" && (types[i-1] == "EN" || types[i-1] == "AN") && types[i+1] == types[i-1] {
			types[i] = types[i-1]
		}
	}
	for i := range runes {
		if types[i] == "EN" {
			for j := i - 1; j >= 0; j-- {
				if types[j] != "ET" {
					break
				}
				types[j] = "EN"
			}
			for j := i + 1; j < numRunes; j++ {
				if types[j] != "ET" {
					break
				}
				types[j] = "EN"
			}
		}
	}
	for i := range runes {
		typeV := types[i]
		if typeV == "WS" || typeV == "ES" || typeV == "ET" || typeV == "CS" {
			types[i] = "ON"
		}
	}
	lastStrong = "sor"
	for i := range runes {
		typeV := types[i]
		switch typeV {
		case "EN":
			if lastStrong == "L" {
				types[i] = "L"
			} else {
				types[i] = "EN"
			}
		case "R", "L":
			lastStrong = typeV
		}
	}
	for i := 0; i < len(runes); i++ {
		if types[i] == "ON" {
			runEnd := nextIndexNot(types, i+1, "ON")
			before := prevType
			if i > 0 {
				before = types[i-1]
			}
			after := prevType
			if runEnd+1 < numRunes {
				after = types[runEnd+1]
			}
			if before != "L" {
				before = "R"
			}
			if after != "L" {
				after = "R"
			}
			if before == after {
				fillStrings(types, i, runEnd, before)
			}
			i = runEnd - 1
		}
	}
	for i := range runes {
		if types[i] == "ON" {
			types[i] = baseType
		}
	}
	for i := range runes {
		typeV := types[i]
		if isEven(levels[i]) {
			switch typeV {
			case "R":
				levels[i]++
			case "AN", "EN":
				levels[i] += 2
			}
		} else if typeV == "L" || typeV == "AN" || typeV == "EN" {
			levels[i]++
		}
	}
	maxLevel := -1
	minOdd := 99
	var lvl int
	for i := 0; i < len(levels); i++ {
		lvl = levels[i]
		if maxLevel < lvl {
			maxLevel = lvl
		}
		if minOdd > lvl && isOdd(lvl) {
			minOdd = lvl
		}
	}
	for i := maxLevel; i >= minOdd; i-- {
		runStart := -1
		for j := 0; j < len(levels); j++ {
			if levels[j] < i {
				if runStart >= 0 {
					reverseStrings(chars, runStart, j)
					runStart = -1
				}
			} else if runStart < 0 {
				runStart = j
			}
		}
		if runStart >= 0 {
			reverseStrings(chars, runStart, len(levels))
		}
	}
	for i := 0; i < len(chars); i++ {
		char := chars[i]
		if char == "<" || char == ">" {
			chars[i] = ""
		}
	}
	return newBidiText(strings.Join(chars, ""), ltr, false)
}

var bidiTypesLatin = []string{"BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "S", "B", "S", "WS", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "B", "B", "B", "S", "WS", "ON", "ON", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "ON", "ES", "CS", "ES", "CS", "CS", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "EN", "CS", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "ON", "ON", "ON", "BN", "BN", "BN", "BN", "BN", "BN", "B", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "BN", "CS", "ON", "ET", "ET", "ET", "ET", "ON", "ON", "ON", "ON", "L", "ON", "ON", "BN", "ON", "ON", "ET", "ET", "EN", "EN", "ON", "L", "ON", "ON", "ON", "EN", "L", "ON", "ON", "ON", "ON", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "L", "ON", "L", "L", "L", "L", "L", "L", "L", "L"}

func isBlank(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
