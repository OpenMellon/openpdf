package extractor

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
)

func truncate(s string, maxLen int) string {
	if len(s) < maxLen {
		return s
	}
	return s[:maxLen]
}

var rulingKindNames = map[rulingKind]string{rulingNone: "none", rulingHorz: "horizontal", rulingVert: "vertical"}

const (
	ExtractionModeLayout = iota
	ExtractionModePlain
	ExtractionModeLayoutNoBreaks
	ExtractionModeGrid
)

var (
	errTypeCheck  = errors.New("type check error")
	errRangeCheck = errors.New("range check error")
)

// Options extractor options.
type Options struct {
	// DisableDocumentTags specifies whether to use the document tags during list extraction.
	DisableDocumentTags bool

	// ApplyCropBox will extract page text based on page cropbox if set to `true`.
	ApplyCropBox bool

	// Text extraction mode, default is ExtractionModeLayout.
	ExtractionMode ExtractionMode

	// IncludeAnnotations specifies whether to include annotations in the extraction process, default value is `false`.
	IncludeAnnotations bool

	// RelaxedMode specifies whether to use relaxed mode for processing the objects,
	// If enabled OpenPDF will automatically try to fix invalid parameters length and value.
	// Default is `false`.
	RelaxedMode bool

	// DisableDehyphenation specifies whether to disable de-hyphenation of words that are split across lines.
	// If `true`, words ending in a hyphen will not be combined with the next line.
	// Default is `false` (de-hyphenation enabled).
	DisableDehyphenation bool
}

// Box represents the bounding box of a given textMark on pdf page.
// This might be used for different kinds of high lighting after doing the search
type Box struct{ BBox model.PdfRectangle }

// ExtractionMode defines different types of extraction mode.
type ExtractionMode int

// New returns an Extractor instance for extracting content from the input PDF page.
func New(page *model.PdfPage) (*Extractor, error) { return NewWithOptions(page, nil) }

var blackColorKey = newColorStateKey(model.NewPdfColorspaceDeviceGray(), model.NewPdfColorDeviceGray(0))

// Extractor stores and offers functionality for extracting content from PDF pages.
type Extractor struct {
	contents          string
	resources         *model.PdfPageResources
	mediaBox          model.PdfRectangle
	cropBox           *model.PdfRectangle
	structParentsKey  int
	fonts             map[string]fontEntry
	parsedForms       map[*core.PdfObjectStream]parsedForm
	formResults       map[formCacheKey]textResult
	annotationResults map[string]textResult
	formExtractions   int
	fontClock         int64
	options           *Options
	structTreeRoot    *model.StructTreeRoot
	pageObject        core.PdfObject
	annotations       []*model.PdfAnnotation
	collectPaths      bool
}

const (
	removeDuplicateWords = true
	mergeDiacritics      = true
	applyEnlargedBBoxes  = false
	useGridTables        = true
	useTextTables        = true
	useStrokeRulings     = true
	useFillRulings       = true
	checkGridCorners     = false
)

type markKind int

const (
	debugEBBoxes          = false
	debugWords            = false
	debugOperands         = false
	debugTextRender       = false
	debugShapes           = false
	debugTextState        = false
	debugSplitBags        = false
	debugTopoOrder        = false
	debugWordBags         = false
	debugWordBagsPage     = debugWordBags && true
	debugWordBagsArrange  = debugWordBagsPage && false
	debugRemoveDuplicates = debugWordBags && true
	debugTables           = false
	debugGrowTable        = debugTables && false
	debugTablesVerbose    = debugTables && true
	debugRulings          = false
	debugSubpaths         = debugRulings && false
	debugMergeRulings     = debugRulings && false
	debugGrids            = debugRulings && true
	debugRectRulings      = debugRulings && false
	debugBlocked          = debugRulings && false
)

var romanNumeralPattern string = "(?i)^(M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})(\\)|\\.)|^\\((M{0,3})(C(?:D|M)|D?C{0,3})(X(?:L|C)|L?X{0,3})(I(?:V|X)|V?I{0,3})\\)"

var enumeratorPattern string = "^[a-zA-Z](\\)|\\.)|^[\\d]+(\\)|\\.)|^\\([a-zA-Z]\\)|^\\([\\d]+\\)"

var listBulletRegexp *regexp.Regexp = regexp.MustCompile(romanNumeralPattern + "|" + enumeratorPattern)

const (
	colorKindOther byte = iota
	colorKindGray
	colorKindRGB
	colorKindCMYK
	colorKindPattern
)

// PageTextOptions holds various options available in extraction process.
type PageTextOptions struct {
	disableDocumentTags  bool
	mode                 ExtractionMode
	disableDehyphenation bool
}

// String returns a description of `k`.
func (mk markKind) String() string {
	name, ok := markKindNames[mk]
	if !ok {
		return fmt.Sprintf("Not a mark: %d", mk)
	}
	return name
}

var markKindNames = map[markKind]string{markKindStroke: "stroke", markKindFill: "fill", markKindAugment: "augment"}

const glyphSpaceUnit = 1.0 / 1000.0

const (
	rulingNone rulingKind = iota
	rulingHorz
	rulingVert
)

// NewFromContents creates a new extractor from contents and page resources.
func NewFromContents(contents string, resources *model.PdfPageResources) (*Extractor, error) {
	ex := &Extractor{contents: contents, resources: resources, fonts: map[string]fontEntry{}, parsedForms: map[*core.PdfObjectStream]parsedForm{}, formResults: map[formCacheKey]textResult{}}
	return ex, nil
}

const (
	zeroTolerance              = 1.0e-6
	coordGranularity           = 1.0e-4
	angleRoundingStep          = 10
	depthBinWidth              = 6
	lineMergeFactor            = 0.5
	maxWordGapFactor           = 0.12
	maxOverlapFactor           = 0.19
	maxDepthDeltaFactor        = 0.04
	scanMarginFactor           = 1.0
	verticalGapFraction        = 0.04
	minFontSize                = 12
	horizontalScanOffsetFactor = 0.4
	horizontalGapFraction      = 0.7
	otherScanOffsetFactor      = 1.0
	otherGapFraction           = 0.1
	lineHeightMax              = 1.4
	lineHeightMin              = 0.46
	wordBoundaryGapFactor      = 0.02
	duplicateToleranceFactor   = 0.2
	diacriticAreaFactor        = 0.5
	minHyphenatedLen           = 4
	readingOrderFontFactor     = 4.0
	minTableParas              = 6
	maxEmptyFraction           = 0.3
	xNeighbourTolerance        = 0.01
	yNeighbourTolerance        = 0.02
	minGridCols                = 2
	minGridRows                = 2
	maxGridRulings             = 500
	minRulingLength            = 4.0
	maxSlope                   = 0.05
	slopeEpsilon               = 0.1
	approxTolerance            = 2.0
	alignTolerance             = 2.0
	containTolerance           = 1.5
	reduceTilingTolerance      = 3.0
	alignedFraction            = 0.25
)

const (
	msgFontConvert       = "ERROR: Can't convert font object, invalid type"
	msgFontPropsNotFound = "ERROR: Can't get font properties, font not found"
	msgFontStreamInvalid = "ERROR: Can't get font stream, invalid type"
	msgNoFontFound       = "ERROR: No font found"
)

const (
	gapFactor               = 1.5
	rowToleranceFactor      = 0.5
	rowHeightFactor         = 0.35
	maxIndexGap             = 3
	rowSearchDepth          = 5
	splitGapFactor          = 10.0
	mergeGapFactor          = 1.0
	medianSpacingFactor     = 1.8
	maxGapSpaces            = 40
	maxOffsetSpaces         = 60
	defaultCharWidth        = 6.0
	defaultRowSpacingFactor = 5
	maxDedupeRunes          = 3
	dupPositionTolerance    = 2.0
	dupLookback             = 30
	minDupRunes             = 2
)

const maxFormDepth = 20

const (
	markKindNone markKind = iota
	markKindStroke
	markKindFill
	markKindAugment
)

func isPatternColorspace(cs model.PdfColorspace) bool {
	_, ok := cs.(*model.PdfColorspaceSpecialPattern)
	return ok
}

const maxCachedFonts = 10

// NewWithOptions an Extractor instance for extracting content from the input PDF page with options.
func NewWithOptions(page *model.PdfPage, options *Options) (*Extractor, error) {
	contents, err := page.GetAllContentStreams()
	if err != nil {
		return nil, err
	}
	var treeRoot *model.StructTreeRoot
	rootObj, ok := page.GetStructTreeRoot()
	if !ok {
		common.Log.Debug("The pdf document is not tagged. StructTreeRoot doesn't exist.")
	} else {
		treeRoot, err = model.NewStructTreeRootFromPdfObject(*rootObj)
		if err != nil {
			return nil, fmt.Errorf("error loading struct tree root: %v", err)
		}
	}
	pageObj := page.GetContainingPdfObject()
	mediaBox, err := page.GetMediaBox()
	if err != nil {
		return nil, fmt.Errorf("extractor requires mediaBox. %v", err)
	}
	ex := &Extractor{contents: contents, resources: page.Resources, mediaBox: *mediaBox, cropBox: page.CropBox, structParentsKey: page.GetStructParentsKey(), fonts: map[string]fontEntry{}, parsedForms: map[*core.PdfObjectStream]parsedForm{}, formResults: map[formCacheKey]textResult{}, annotationResults: map[string]textResult{}, options: options, structTreeRoot: treeRoot, pageObject: pageObj}
	if ex.mediaBox.Llx > ex.mediaBox.Urx {
		common.Log.Info("MediaBox has X coordinates reversed. %.2f Fixing.", ex.mediaBox)
		ex.mediaBox.Llx, ex.mediaBox.Urx = ex.mediaBox.Urx, ex.mediaBox.Llx
	}
	if ex.mediaBox.Lly > ex.mediaBox.Ury {
		common.Log.Info("MediaBox has Y coordinates reversed. %.2f Fixing.", ex.mediaBox)
		ex.mediaBox.Lly, ex.mediaBox.Ury = ex.mediaBox.Ury, ex.mediaBox.Lly
	}
	if ex.options != nil {
		if ex.options.IncludeAnnotations {
			ex.annotations, err = page.GetAnnotations()
			if err != nil {
				common.Log.Debug("Error getting annotations: %v", err)
			}
		}
	}
	return ex, nil
}

var bulletChars = []string{"❖", "➢", "✓", "•", "\uf0a7", "□", "−", "■", "▪", "o"}

var bareNumberRegexp = regexp.MustCompile("^\\s*(\\d+\\.?|[Iiv]+)\\s*\\)?$")
