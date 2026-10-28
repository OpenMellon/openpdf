package creator

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"text/template"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/model"
	chartrender "github.com/unidoc/unichart/render"
)

type commands struct {
	names       []string
	paramCounts map[string]int
	moveSymbol  string
	closeSymbol string
}

func (com *commands) isCommand(s string) bool {
	for _, name := range com.names {
		if strings.ToLower(s) == name {
			return true
		}
	}
	return false
}

func parseCommands(tokens []token) ([]*Command, error) {
	var (
		commands []*Command
		floats   []float64
	)
	for _, token := range slices.Backward(tokens) {
		if token.isCommand {
			paramCount := svgCommands.paramCounts[strings.ToLower(token.text)]
			floatsLen := len(floats)
			if paramCount == 0 && floatsLen == 0 {
				command := &Command{Symbol: token.text}
				commands = append([]*Command{command}, commands...)
			} else if paramCount != 0 && floatsLen%paramCount == 0 {
				floatsLen2 := floatsLen / paramCount
				for i := 0; i < floatsLen2; i++ {
					text := token.text
					if text == "m" && i < floatsLen2-1 {
						text = "l"
					}
					if text == "M" && i < floatsLen2-1 {
						text = "L"
					}
					command := &Command{text, reverseFloats(floats[:paramCount])}
					commands = append([]*Command{command}, commands...)
					floats = floats[paramCount:]
				}
			} else {
				pathParserError2 := pathParserError{"Incorrect number of parameters for " + token.text}
				return nil, pathParserError2
			}
		} else {
			svgLength, err := parseSVGLength(token.text, 64)
			if err != nil {
				return nil, err
			}
			floats = append(floats, svgLength)
		}
	}
	return commands, nil
}

// Command is a representation of an SVG path command and its parameters.
type Command struct {
	Symbol string
	Params []float64
}

func (co *Command) isAbsolute() bool { return co.Symbol == strings.ToUpper(co.Symbol) }

func (co *Command) compare(command *Command) bool {
	if co.Symbol != command.Symbol {
		return false
	}
	for i, param := range co.Params {
		if param != command.Params[i] {
			return false
		}
	}
	return true
}

// ParsedTemplate is a template read and compiled once that can be rendered many
// times, into different creators, with different data. Compiling (reading the
// source, compiling the text/template and its subtemplates, and installing the
// escaping helpers) is done up front by ParseTemplate; a creator's RenderTemplate
// or ValidateTemplate (or the Block equivalents) executes the compiled template
// with the caller's data and builds the resulting XML tree, so the read and
// compile cost is not paid per render. A parsed template is not bound to a
// creator and may be reused across documents.
//
// Fonts referenced by a path are loaded per render (once per RenderTemplate
// call), so the same font is embedded independently in each document.
type ParsedTemplate struct {
	options *TemplateOptions
	tmpl    *template.Template
	source  string
}

func (pat *ParsedTemplate) renderInto(creator *Creator, renderer componentRenderer, data any) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	data2, err := pat.execute(data)
	if err != nil {
		return err
	}
	return newTemplateProcessor(creator, pat.source, data2, pat.options, renderer).render()
}

func (pat *ParsedTemplate) execute(data any) ([]byte, error) {
	buffer := bytes.NewBuffer(nil)
	if err := pat.tmpl.Execute(buffer, data); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (pat *ParsedTemplate) validateWith(creator *Creator, renderer componentRenderer, data any) error {
	if creator == nil {
		common.Log.Error("Creator instance cannot be nil.")
		return errInvalidCreator
	}
	data2, err := pat.execute(data)
	if err != nil {
		return err
	}
	return newTemplateProcessor(creator, pat.source, data2, pat.options, renderer).validate()
}

type token struct {
	text      string
	isCommand bool
}

func tokenizePathData(s2 string) []token {
	var (
		tokens []token
		s      string
	)
	for _, r := range s2 {
		text := string(r)
		switch {
		case svgCommands.isCommand(text):
			tokens, s = flushToken(tokens, s)
			tokens = append(tokens, token{text, true})
		case text == ".":
			if s == "" {
				s = "0"
			}
			if strings.Contains(s, text) {
				tokens = append(tokens, token{s, false})
				s = "0"
			}
			fallthrough
		case text >= "0" && text <= "9" || text == "e":
			s += text
		case text == "-":
			if strings.HasSuffix(s, "e") {
				s += text
			} else {
				tokens, _ = flushToken(tokens, s)
				s = text
			}
		default:
			tokens, s = flushToken(tokens, s)
		}
	}
	tokens, _ = flushToken(tokens, s)
	return tokens
}

func templateAdd(v, v2 any) (any, error) {
	v3, err := normalizeTemplateNumber(v)
	if err != nil {
		return nil, err
	}
	switch n := v3.(type) {
	case int64:
		v, err := normalizeTemplateNumber(v2)
		if err != nil {
			return nil, err
		}
		switch n2 := v.(type) {
		case int64:
			return n + n2, nil
		case float64:
			return float64(n) + n2, nil
		}
	case float64:
		v, err := normalizeTemplateNumber(v2)
		if err != nil {
			return nil, err
		}
		switch n2 := v.(type) {
		case int64:
			return n + float64(n2), nil
		case float64:
			return n + n2, nil
		}
	}
	return nil, fmt.Errorf("failed to add %v and %v", v, v2)
}

func templateExtendDict(templateExtendDict map[string]any, vals ...any) (map[string]any, error) {
	n := len(vals)
	if n%2 != 0 {
		return nil, core.ErrRangeError
	}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			return nil, core.ErrTypeError
		}
		templateExtendDict[s] = vals[i+1]
	}
	return templateExtendDict, nil
}

func templateXMLEscape(i any) (string, error) {
	valueToString2, err := valueToString(i)
	if err != nil {
		return "", err
	}
	var w bytes.Buffer
	xml.EscapeText(&w, []byte(valueToString2))
	return w.String(), nil
}

func templateArray(templateArray ...any) []any { return templateArray }

func templateValueToString(i any) (string, error) { return valueToString(i) }

func templateDict(vals ...any) (map[string]any, error) {
	n := len(vals)
	if n%2 != 0 {
		common.Log.Error("Invalid number of parameters for creating map: %d.", n)
		return nil, core.ErrRangeError
	}
	m := map[string]any{}
	for i := 0; i < n; i += 2 {
		s, ok := vals[i].(string)
		if !ok {
			common.Log.Error("Invalid map key type (%T). Expected string.", vals[i])
			return nil, core.ErrTypeError
		}
		m[s] = vals[i+1]
	}
	return m, nil
}

func templateMakeSeq(i2, i3, n int) []int {
	nums := []int{}
	for i := i2; i <= n; i += i3 {
		nums = append(nums, i)
	}
	return nums
}

type templateText struct {
	text   string
	line   int
	col    int
	offset int64
}

func newTemplateText(data []byte, text string, line, col int, offset int64) *templateText {
	text2 := text[:len(text)-len(strings.TrimLeft(text, whitespaceChars))]
	offset2 := offset + int64(len(text2))
	if offset2 > int64(len(data)) || string(data[offset:offset2]) != text2 {
		return &templateText{text: text, line: line, col: col, offset: offset}
	}
	if line != 0 {
		for _, r := range text2 {
			if r == '\n' {
				line, col = line+1, 1
			} else {
				col++
			}
		}
	}
	return &templateText{text: text, line: line, col: col, offset: offset + int64(len(text2))}
}

func (text *templateText) pos() *templateNode {
	return &templateNode{line: text.line, col: text.col, offset: text.offset}
}

type templateTag struct {
	allowedParents        map[string]struct{}
	parse                 func(*templateProcessor, *templateNode) (any, error)
	attrs                 map[string]attrApplyFunc
	singleChild           bool
	allowMultipleChildren bool
}

var templateTags = map[string]*templateTag{"paragraph": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseStyledParagraphTag, attrs: styledParagraphAttrs}, "text-chunk": {allowedParents: map[string]struct{}{"paragraph": {}}, parse: parseTextChunkTag, attrs: textChunkAttrs}, "division": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseDivisionTag, attrs: divisionAttrs}, "table": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseTableTag, attrs: tableAttrs}, "table-cell": {allowedParents: map[string]struct{}{"table": {}}, parse: parseTableCellTag, attrs: tableCellAttrs, singleChild: true}, "line": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseLineTag, attrs: lineAttrs}, "rectangle": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseRectangleTag, attrs: rectangleAttrs}, "ellipse": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseEllipseTag, attrs: ellipseAttrs}, "image": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseImageTag, attrs: imageAttrs}, "chapter": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "chapter": {}}, parse: parseChapterTag, attrs: chapterAttrs}, "chapter-heading": {allowedParents: map[string]struct{}{"chapter": {}}, parse: parseChapterHeadingTag, attrs: chapterHeadingAttrs}, "chart": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}}, parse: parseChartTag, attrs: chartAttrs}, "page-break": {allowedParents: map[string]struct{}{"creator": {}, "chapter": {}, "division": {}}, parse: parsePageBreakTag, attrs: emptyAttrs}, "background": {allowedParents: map[string]struct{}{"block": {}, "division": {}, "chapter": {}}, parse: parseBackgroundTag, attrs: backgroundAttrs}, "list": {allowedParents: map[string]struct{}{"creator": {}, "block": {}, "division": {}, "table-cell": {}, "chapter": {}, "list-item": {}}, parse: parseListTag, attrs: listAttrs}, "list-item": {allowedParents: map[string]struct{}{"list": {}}, parse: parseListItemTag, attrs: emptyAttrs, singleChild: true}, "list-marker": {allowedParents: map[string]struct{}{"list": {}, "list-item": {}}, parse: parseListMarkerTag, attrs: textChunkAttrs, allowMultipleChildren: true}}

// TemplateOptions contains options and resources to use when rendering
// a template with a Creator instance.
// All the resources in the map fields can be referenced by their
// name/key in the template which is rendered using the options instance.
type TemplateOptions struct {
	// HelperFuncMap is used to define functions which can be accessed
	// inside the rendered templates by their assigned names.
	HelperFuncMap template.FuncMap

	// SubtemplateMap contains templates which can be rendered alongside
	// the main template. They can be accessed using their assigned names
	// in the main template or in the other subtemplates.
	// Subtemplates defined inside the subtemplates specified in the map
	// can be accessed directly.
	// All resources available to the main template are also available
	// to the subtemplates.
	SubtemplateMap map[string]io.Reader

	// FontMap contains pre-loaded fonts which can be accessed
	// inside the rendered templates by their assigned names.
	// The assigned names must not contain commas, as the font
	// attribute is interpreted as a comma-separated fallback list.
	FontMap map[string]*model.PdfFont

	// ImageMap contains pre-loaded images which can be accessed
	// inside the rendered templates by their assigned names.
	ImageMap map[string]*model.Image

	// ColorMap contains colors which can be accessed
	// inside the rendered templates by their assigned names.
	ColorMap map[string]Color

	// ChartMap contains charts which can be accessed
	// inside the rendered templates by their assigned names.
	ChartMap map[string]chartrender.ChartRenderable

	// DisableAutoEscape turns off automatic XML-escaping of template action
	// output.
	//
	// By default, the output of every template action is XML-escaped so that
	// values coming from the template data cannot break the XML stream (e.g. a
	// bare `&`) or inject their own tags. The `raw` helper opts a single action
	// out of escaping; it is honored only as the final command of the action's
	// pipeline (optionally wrapped in parentheses), so `{{raw .v}}` and
	// `{{(raw .v)}}` are unescaped, while `{{raw .v | printf "%s"}}` and
	// `{{printf "[%s]" (raw .v)}}` are still escaped.
	//
	// Set this to true to restore the previous behavior, where action output is
	// written to the XML stream verbatim. This is an upgrade escape hatch for
	// templates whose data supplies pre-escaped XML (for example the "&#xA;"
	// newline idiom) or intentionally emits markup. When disabled, `raw` is not
	// reserved and a HelperFuncMap entry of that name is used as provided.
	DisableAutoEscape bool

	// Strict makes the processor return an error, located at the template file
	// and line:column, on an attribute parse failure, unknown attribute or tag,
	// invalid nesting, invalid table structure, inapplicable attribute or
	// unresolved image or chart resource. Execution also fails when an action
	// references a key missing from map data, including in `{{if .Key}}` and
	// `{{with .Key}}`; read optional keys with `{{with index . "Key"}}`.
	//
	// When false (the default), these problems are logged and skipped, a bad
	// attribute value falls back to its default, and a missing key renders as
	// empty text. With DisableAutoEscape it writes text/template's `<no value>`
	// into the XML, which fails to parse.
	Strict bool

	// FileName sets the template name used in error locations. Use it when the
	// template is read from a buffer or embedded filesystem, which carry no
	// file name of their own.
	FileName string
}

func (teo *TemplateOptions) init() {
	if teo.SubtemplateMap == nil {
		teo.SubtemplateMap = map[string]io.Reader{}
	}
	if teo.FontMap == nil {
		teo.FontMap = map[string]*model.PdfFont{}
	}
	if teo.ImageMap == nil {
		teo.ImageMap = map[string]*model.Image{}
	}
	if teo.ColorMap == nil {
		teo.ColorMap = map[string]Color{}
	}
	if teo.ChartMap == nil {
		teo.ChartMap = map[string]chartrender.ChartRenderable{}
	}
}

type attrApplyFunc func(proc *templateProcessor, node *templateNode, attrName string, target any, value string) error

var (
	attrFuncRe         = regexp.MustCompile("([\\w-]+)\\('(.+)'\\)")
	errInvalidCreator  = errors.New("invalid template creator instance")
	errInvalidParent   = errors.New("invalid template parent node")
	errInvalidChild    = errors.New("invalid template child node")
	errInvalidResource = errors.New("invalid template resource")
	errInvalidReader   = errors.New("invalid template reader")
)
