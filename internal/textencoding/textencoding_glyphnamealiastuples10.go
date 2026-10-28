// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package textencoding

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/OpenMellon/openpdf/internal/cmap"
	"github.com/OpenMellon/openpdf/internal/strutils"
	"github.com/OpenMellon/openpdf/internal/textencoding/internal/syncmap"
)

var glyphNameAliasTuples10 = []syncmap.StringsTuple{{Key: "afii59762", Value: "twothai"}, {Key: "gravesubnosp", Value: "ubar"}, {Key: "deltaturn", Value: "ubreve"}, {Key: "uhungarumlaut", Value: "udblacute"}, {Key: "eshshortrev", Value: "udblgrave"}, {Key: "Aacutering", Value: "udieresiscaron"}, {Key: "ihacek", Value: "uhorn"}, {Key: "tturn", Value: "uinvertedbreve"}, {Key: "nwquadarc", Value: "ularc"}, {Key: "dbllowline", Value: "underscoredbl"}, {Key: "twolowline", Value: "underscoredbl"}, {Key: "midhorizellipsis", Value: "unicodecdots"}, {Key: "cup", Value: "union"}, {Key: "Cup", Value: "uniondbl"}, {Key: "unionmultidisplay", Value: "unionmulti"}, {Key: "unionmultitext", Value: "unionmulti"}, {Key: "uplus", Value: "unionmulti"}, {Key: "sqcup", Value: "unionsq"}, {Key: "unionsqdisplay", Value: "unionsq"}, {Key: "unionsqtext", Value: "unionsq"}, {Key: "bigcup", Value: "uniontext"}, {Key: "naryunion", Value: "uniontext"}, {Key: "uniondisplay", Value: "uniontext"}, {Key: "forall", Value: "universal"}, {Key: "blockuphalf", Value: "upblock"}, {Key: "gekarev", Value: "updigamma"}, {Key: "enrtdes", Value: "upkoppa"}, {Key: "Kavertbar", Value: "upoldKoppa"}, {Key: "kavertbar", Value: "upoldkoppa"}, {Key: "enge", Value: "upsampi"}, {Key: "upupsilon", Value: "upsilon"}, {Key: "acutesubnosp", Value: "upsilonlatin"}, {Key: "xsol", Value: "upslope"}, {Key: "kabar", Value: "upstigma"}, {Key: "Upsilon1dieresis", Value: "uptackbelowcmb"}, {Key: "Upsilon1diaeresis", Value: "uptackbelowcmb"}, {Key: "Chevertbar", Value: "upvarTheta"}, {Key: "nequadarc", Value: "urarc"}, {Key: "Dbar1", Value: "utilde"}, {Key: "perspcorrespond", Value: "vardoublebarwedge"}, {Key: "clwcontintegral", Value: "varointclockwise"}, {Key: "triangleright", Value: "vartriangleleft"}, {Key: "triangleleft", Value: "vartriangleright"}, {Key: "afii57669", Value: "vav"}, {Key: "vavhebrew", Value: "vav"}, {Key: "afii57723", Value: "vavdageshhebrew"}, {Key: "vavdagesh", Value: "vavdageshhebrew"}, {Key: "vavdagesh65", Value: "vavdageshhebrew"}, {Key: "vavwithdagesh", Value: "vavdageshhebrew"}, {Key: "afii57700", Value: "vavholam"}, {Key: "vavholamhebrew", Value: "vavholam"}, {Key: "vavwithholam", Value: "vavholam"}, {Key: "vec", Value: "vector"}, {Key: "equiangular", Value: "veeeq"}, {Key: "afii57505", Value: "veharabic"}, {Key: "veh", Value: "veharabic"}, {Key: "vehfinal", Value: "vehfinalarabic"}, {Key: "vehinitial", Value: "vehinitialarabic"}, {Key: "vehmedial", Value: "vehmedialarabic"}, {Key: "Sampi", Value: "verticallinebelowcmb"}, {Key: "arrowlongbothv", Value: "vewarmenian"}, {Key: "tackleftsubnosp", Value: "vhook"}, {Key: "vertrectangle", Value: "vrectangle"}, {Key: "filledvertrect", Value: "vrectangleblack"}, {Key: "tackrightsubnosp", Value: "vturned"}, {Key: "openbullet1", Value: "vysmwhtcircle"}, {Key: "ringmath", Value: "vysmwhtcircle"}, {Key: "afii57448", Value: "wawarabic"}, {Key: "waw", Value: "wawarabic"}, {Key: "wawfinal", Value: "wawfinalarabic"}, {Key: "wawwithhamzaabovefinal", Value: "wawhamzaabovefinalarabic"}, {Key: "estimates", Value: "wedgeq"}, {Key: "Pscript", Value: "weierstrass"}, {Key: "wp", Value: "weierstrass"}, {Key: "openbullet", Value: "whitebullet"}, {Key: "smwhtcircle", Value: "whitebullet"}, {Key: "circle", Value: "whitecircle"}, {Key: "mdlgwhtcircle", Value: "whitecircle"}, {Key: "diamondrhomb", Value: "whitediamond"}, {Key: "mdlgwhtdiamond", Value: "whitediamond"}, {Key: "blackinwhitediamond", Value: "whitediamondcontainingblacksmalldiamond"}, {Key: "diamondrhombnested", Value: "whitediamondcontainingblacksmalldiamond"}, {Key: "smalltriangleinv", Value: "whitedownpointingsmalltriangle"}, {Key: "triangledown", Value: "whitedownpointingsmalltriangle"}, {Key: "bigtriangledown", Value: "whitedownpointingtriangle"}, {Key: "triangleinv", Value: "whitedownpointingtriangle"}, {Key: "smalltriangleleft", Value: "whiteleftpointingsmalltriangle"}, {Key: "triangleleft1", Value: "whiteleftpointingtriangle"}, {Key: "triaglfopen", Value: "whitepointerleft"}, {Key: "triagrtopen", Value: "whitepointerright"}, {Key: "smalltriangleright", Value: "whiterightpointingsmalltriangle"}, {Key: "triangleright1", Value: "whiterightpointingtriangle"}, {Key: "H18551", Value: "whitesmallsquare"}, {Key: "smallbox", Value: "whitesmallsquare"}, {Key: "smwhtsquare", Value: "whitesmallsquare"}, {Key: "bigwhitestar", Value: "whitestar"}, {Key: "smalltriangle", Value: "whiteuppointingsmalltriangle"}, {Key: "vartriangle", Value: "whiteuppointingsmalltriangle"}, {Key: "bigtriangleup", Value: "whiteuppointingtriangle"}, {Key: "triangle", Value: "whiteuppointingtriangle"}, {Key: "afii59719", Value: "wowaenthai"}, {Key: "wr", Value: "wreathproduct"}, {Key: "diaeresistonosnosp", Value: "wsuperior"}, {Key: "anglesupnosp", Value: "wturned"}, {Key: "upxi", Value: "xi"}, {Key: "afii59758", Value: "yamakkanthai"}, {Key: "afii10194", Value: "yatcyrillic"}, {Key: "Ibar", Value: "ycircumflex"}, {Key: "afii57450", Value: "yeharabic"}, {Key: "yeh", Value: "yeharabic"}, {Key: "afii57519", Value: "yehbarreearabic"}, {Key: "yehfinal", Value: "yehfinalarabic"}, {Key: "afii57414", Value: "yehhamzaabovearabic"}, {Key: "yehwithhamzaabove", Value: "yehhamzaabovearabic"}, {Key: "yehwithhamzaabovefinal", Value: "yehhamzaabovefinalarabic"}, {Key: "yehwithhamzaaboveinitial", Value: "yehhamzaaboveinitialarabic"}, {Key: "yehwithhamzaabovemedial", Value: "yehhamzaabovemedialarabic"}, {Key: "alefmaksurainitialarabic", Value: "yehinitialarabic"}, {Key: "yehinitial", Value: "yehinitialarabic"}, {Key: "yehwithmeeminitial", Value: "yehmeeminitialarabic"}, {Key: "yehwithmeemisolated", Value: "yehmeemisolatedarabic"}, {Key: "yehwithnoonfinal", Value: "yehnoonfinalarabic"}, {Key: "Yen", Value: "yen"}, {Key: "auxiliaryon", Value: "yerahbenyomohebrew"}, {Key: "devcon1", Value: "yerahbenyomohebrew"}, {Key: "yerahbenyomolefthebrew", Value: "yerahbenyomohebrew"}, {Key: "afii10093", Value: "yericyrillic"}, {Key: "yeri", Value: "yericyrillic"}, {Key: "startofhead", Value: "yetivhebrew"}, {Key: "uhacek", Value: "yhook"}, {Key: "afii10104", Value: "yicyrillic"}, {Key: "yi", Value: "yicyrillic"}, {Key: "arrowsouthwest", Value: "yiwnarmenian"}, {Key: "yoddagesh", Value: "yoddageshhebrew"}, {Key: "yodwithdagesh", Value: "yoddageshhebrew"}, {Key: "afii57718", Value: "yodyodhebrew"}, {Key: "yoddbl", Value: "yodyodhebrew"}, {Key: "afii57705", Value: "yodyodpatahhebrew"}, {Key: "doubleyodpatah", Value: "yodyodpatahhebrew"}, {Key: "doubleyodpatahhebrew", Value: "yodyodpatahhebrew"}, {Key: "chertdes", Value: "yotgreek"}, {Key: "afii59714", Value: "yoyakthai"}, {Key: "afii59693", Value: "yoyingthai"}, {Key: "dzhacek", Value: "yr"}, {Key: "iotasubnosp", Value: "ysuperior"}, {Key: "hornnosp", Value: "yturned"}, {Key: "afii57432", Value: "zaharabic"}, {Key: "zah", Value: "zaharabic"}, {Key: "zahfinal", Value: "zahfinalarabic"}, {Key: "zahinitial", Value: "zahinitialarabic"}, {Key: "zahmedial", Value: "zahmedialarabic"}, {Key: "afii57426", Value: "zainarabic"}, {Key: "zain", Value: "zainarabic"}, {Key: "zainfinal", Value: "zainfinalarabic"}, {Key: "arrowloopright", Value: "zaqefgadolhebrew"}, {Key: "arrowloopleft", Value: "zaqefqatanhebrew"}, {Key: "arrowzigzag", Value: "zarqahebrew"}, {Key: "zayindagesh", Value: "zayindageshhebrew"}, {Key: "zayinwithdagesh", Value: "zayindageshhebrew"}, {Key: "nleg", Value: "zcaron"}, {Key: "tackdownsubnosp", Value: "zcurl"}, {Key: "mcapturn", Value: "zdotaccent"}, {Key: "zdot", Value: "zdotaccent"}, {Key: "zerodot", Value: "zero"}, {Key: "zeroslash", Value: "zero"}, {Key: "afii57392", Value: "zerohackarabic"}, {Key: "arabicindicdigitzero", Value: "zerohackarabic"}, {Key: "zeroarabic", Value: "zerohackarabic"}, {Key: "zerosub", Value: "zeroinferior"}, {Key: "extendedarabicindicdigitzero", Value: "zeropersian"}, {Key: "afii59760", Value: "zerothai"}, {Key: "bom", Value: "zerowidthjoiner"}, {Key: "zerowidthnobreakspace", Value: "zerowidthjoiner"}, {Key: "zerospace", Value: "zerowidthspace"}, {Key: "upzeta", Value: "zeta"}, {Key: "afii10072", Value: "zhecyrillic"}, {Key: "zhe", Value: "zhecyrillic"}, {Key: "negacknowledge", Value: "zinorhebrew"}, {Key: "tackupsubnosp", Value: "zretroflexhook"}}

var glyphNameAliasTuples = slices.Concat(glyphNameAliasTuples1, glyphNameAliasTuples2, glyphNameAliasTuples3, glyphNameAliasTuples4, glyphNameAliasTuples5, glyphNameAliasTuples6, glyphNameAliasTuples7, glyphNameAliasTuples8, glyphNameAliasTuples9, glyphNameAliasTuples10)

func (enc *TrueTypeFontEncoder) String() string {
	parts := []string{fmt.Sprintf("%d entries", enc.runeToGID.Length())}
	type runeGID struct {
		r   rune
		gid GID
	}
	entries := make([]runeGID, 0, enc.runeToGID.Length())
	enc.runeToGID.Range(func(r rune, gid uint16) (stop bool) {
		entries = append(entries, runeGID{r: r, gid: GID(gid)})
		return false
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].r < entries[j].r })
	n := min(len(entries), maxStringEntries)
	for i := 0; i < n; i++ {
		e := entries[i]
		parts = append(parts, fmt.Sprintf("%d=0x%02x: %q", e.r, e.r, rune(e.gid)))
	}
	return fmt.Sprintf("TRUETYPE_ENCODER{%s}", strings.Join(parts, ", "))
}

type IdentityEncoder struct {
	name      string
	usedGIDs  *syncmap.Uint16Set
	toUnicode *cmap.CMap
	cidToGID  []uint16
}

func NewZapfDingbatsEncoder() SimpleEncoder { return zapfDingbatsMapping.NewEncoder() }

type UTF16Encoder struct{ name string }

func (enc *differencesEncoding) String() string {
	return fmt.Sprintf("differences(%s, %v)", enc.base.String(), enc.differences)
}

func init() {
	RegisterSimpleEncoding(macExpertEncodingName, NewMacExpertEncoder)
	RegisterSimpleEncoding(macRomanEncodingName, NewMacRomanEncoder)
}
func (enc UTF16Encoder) Decode(raw []byte) string { return strutils.UTF16ToString(raw) }

type simpleEncoding struct {
	name       string
	runeToByte *syncmap.RuneByteMap
	byteToRune *syncmap.ByteRuneMap
	usedRunes  *syncmap.RuneSet
}
type ShapedGlyphRegistry interface{ RegisteredShapedGIDs() []GID }

func (m *simpleEncodersMap) Write(s string, f func() SimpleEncoder) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.encoders[s] = f
}

type simpleEncoder struct{ runeToByte *syncmap.RuneByteMap }

func (m *simpleMapping) NewEncoder() SimpleEncoder {
	m.once.Do(m.init)
	return &simpleEncoding{name: m.name, runeToByte: m.runeToByte, byteToRune: m.byteToRune}
}

func init() {
	RegisterSimpleEncoding(symbolEncodingName, NewSymbolEncoder)
	RegisterSimpleEncoding(zapfDingbatsEncodingName, NewZapfDingbatsEncoder)
}
func (enc *IdentityEncoder) Encode(str string) []byte { return encodeTwoByte(enc, str) }
