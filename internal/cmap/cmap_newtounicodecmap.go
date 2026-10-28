// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package cmap

func NewToUnicodeCMap(codeToRune map[CharCode]string) *CMap {
	cmap := &CMap{name: "Adobe-Identity-UCS", ctype: 2, nbits: 16, systemInfo: CIDSystemInfo{Registry: "Adobe", Ordering: "UCS", Supplement: 0}, codespaces: []Codespace{{Low: 0, High: 0xffff}}, codeToUnicode: codeToRune, unicodeToCode: make(map[string]CharCode, len(codeToRune)), codeToCID: make(map[CharCode]CharCode, len(codeToRune)), cidToCode: make(map[CharCode]CharCode, len(codeToRune))}
	return cmap
}

const (
	tokCIDSystemInfo       = "CIDSystemInfo"
	tokBeginCMap           = "begincmap"
	tokEndCMap             = "endcmap"
	tokBeginCodespaceRange = "begincodespacerange"
	tokEndCodespaceRange   = "endcodespacerange"
	tokBeginBfChar         = "beginbfchar"
	tokEndBfChar           = "endbfchar"
	tokBeginBfRange        = "beginbfrange"
	tokEndBfRange          = "endbfrange"
	tokBeginCIDRange       = "begincidrange"
	tokEndCIDRange         = "endcidrange"
	tokUseCMap             = "usecmap"
	tokWMode               = "WMode"
	tokCMapName            = "CMapName"
	tokCMapType            = "CMapType"
	tokCMapVersion         = "CMapVersion"
)
