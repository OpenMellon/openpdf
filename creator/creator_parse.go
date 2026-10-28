package creator

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"
	"text/template"
	"unicode"

	"github.com/OpenMellon/openpdf/common"
	"golang.org/x/net/html/charset"
)

func parseBool(str string) (bool, error) { return strconv.ParseBool(str) }

func parseFloat64(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

// ParseFromSVGFile creates a GraphicSVG instance from file.
func ParseFromSVGFile(path string) (*GraphicSVGElement, error) {
	source, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	return ParseFromSVGStream(source)
}

// ParseFromSVGStream creates a GraphicSVG instance from SVG stream input.
func ParseFromSVGStream(source io.Reader) (*GraphicSVGElement, error) {
	decoder := xml.NewDecoder(source)
	decoder.CharsetReader = charset.NewReaderLabel
	svgElementTree, err := parseSVGElementTree(decoder)
	if err != nil {
		return nil, err
	}
	if err := svgElementTree.Decode(decoder); err != nil && err != io.EOF {
		return nil, err
	}
	return svgElementTree, nil
}

func parseSVGLength(value string, bitSize int) (float64, error) {
	s, unit := splitUnitSuffix(value)
	float, err := strconv.ParseFloat(s, bitSize)
	if err != nil {
		return 0, err
	}
	if unitToPoint, ok := unitToPoints[unit]; ok {
		float = float * unitToPoint
	} else {
		float = float * pxToPt
	}
	return float, nil
}

func parseSVGPoints(s string) ([]float64, error) {
	n := -1
	var svgPoints []float64
	r2 := ' '
	for i, r := range s {
		if !unicode.IsNumber(r) && r != '.' && !(r == '-' && r2 == 'e') && r != 'e' {
			if n != -1 {
				numbers, err := parseNumberList(s[n:i])
				if err != nil {
					return svgPoints, err
				}
				svgPoints = append(svgPoints, numbers...)
			}
			if r == '-' {
				n = i
			} else {
				n = -1
			}
		} else if n == -1 {
			n = i
		}
		r2 = r
	}
	if n != -1 && n != len(s) {
		numbers, err := parseNumberList(s[n:])
		if err != nil {
			return svgPoints, err
		}
		svgPoints = append(svgPoints, numbers...)
	}
	return svgPoints, nil
}

func parseInt64List(s string) ([]int64, error) {
	fields := strings.Fields(s)
	nums := make([]int64, len(fields))
	var err error
	for i, field := range fields {
		i2, err2 := strconv.ParseInt(field, 10, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			i2 = 0
		}
		nums[i] = i2
	}
	return nums, err
}

func parseInt64(s string) (int64, error) { return strconv.ParseInt(s, 10, 64) }

func parseSVGPath(s string) (*Path, error) {
	svgCommands = newPathCommands()
	commands, err := parseCommands(tokenizePathData(s))
	if err != nil {
		return nil, err
	}
	return buildPath(commands), nil
}

// ParseFromSVGString creates a GraphicSVG instance from string SVG.
func ParseFromSVGString(svgStr string) (*GraphicSVGElement, error) {
	return ParseFromSVGStream(strings.NewReader(svgStr))
}

func parsePercentOrFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	var percentOrFloat float64
	if before, ok := strings.CutSuffix(s, "%"); ok {
		float, err := strconv.ParseFloat(before, 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float / 100.0
	} else {
		float, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, err
		}
		percentOrFloat = float
	}
	return percentOrFloat, nil
}

func parseFloatFields(s string) ([]float64, error) {
	fields := strings.Fields(s)
	floats := make([]float64, len(fields))
	var err error
	for i, field := range fields {
		float, err2 := strconv.ParseFloat(field, 64)
		if err2 != nil {
			setFirstError(&err, err2)
			float = 0
		}
		floats[i] = float
	}
	return floats, err
}

func parseSVGElementTree(decoder *xml.Decoder) (*GraphicSVGElement, error) {
	for {
		token, err := decoder.Token()
		if token == nil && err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		switch token2 := token.(type) {
		case xml.StartElement:
			return newGraphicSVGElement(token2), nil
		}
	}
	return &GraphicSVGElement{}, nil
}

func parseSVGStyle(m map[string]string, value2 float64) (*GraphicSVGStyle, error) {
	graphicSVGStyle := newGraphicSVGStyle()
	fillColor, ok := m["fill"]
	if ok {
		graphicSVGStyle.FillColor = fillColor
		if fillColor == "none" {
			graphicSVGStyle.FillColor = ""
		}
	}
	s, ok2 := m["fill-opacity"]
	if ok2 {
		percentOrFloat, err := parsePercentOrFloat(s)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.FillOpacity = percentOrFloat
	}
	strokeColor, ok3 := m["stroke"]
	if ok3 {
		graphicSVGStyle.StrokeColor = strokeColor
		if strokeColor == "none" {
			graphicSVGStyle.StrokeColor = ""
		}
	}
	value, ok4 := m["stroke-width"]
	if ok4 {
		svgLength, err := parseSVGLength(value, 64)
		if err != nil {
			return nil, err
		}
		graphicSVGStyle.StrokeWidth = svgLength * value2
	}
	return graphicSVGStyle, nil
}

func parseNumberList(s string) (numbers []float64, err error) {
	var value float64
	n := 0
	ok := true
	for i, r := range s {
		if r == '.' {
			if ok {
				ok = false
				continue
			}
			value, err = parseSVGLength(s[n:i], 64)
			if err != nil {
				return
			}
			numbers = append(numbers, value)
			n = i
		}
	}
	value, err = parseSVGLength(s[n:], 64)
	if err != nil {
		return
	}
	numbers = append(numbers, value)
	return
}

func parseFloatArray(s string) ([]float64, error) { return parseFloatFields(s) }

func parseFiniteFloat(s string) (float64, error) {
	float, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	if math.IsNaN(float) || math.IsInf(float, 0) {
		return 0, fmt.Errorf("%q is not a finite number", s)
	}
	return float, nil
}

// ParseTemplate reads and compiles the template from the specified reader using
// the specified `options`, returning a handle that Creator.RenderTemplate or
// Creator.ValidateTemplate (or the Block equivalents) render or validate with
// data. Reading and compiling are done once, so a template rendered many times,
// including across documents, does not pay that cost each render. The `options`
// parameter can be nil.
func ParseTemplate(r io.Reader, options *TemplateOptions) (*ParsedTemplate, error) {
	if r == nil || isNilValue(reflect.ValueOf(r)) {
		common.Log.Error("Template reader cannot be nil.")
		return nil, errInvalidReader
	}
	if options == nil {
		options = &TemplateOptions{}
	}
	options.init()
	fileName := ""
	if options.FileName != "" {
		fileName = options.FileName
	} else if file, ok := r.(*os.File); ok {
		fileName = file.Name()
	}
	buffer := bytes.NewBuffer(nil)
	if _, err := io.Copy(buffer, r); err != nil {
		return nil, err
	}
	ok := !options.DisableAutoEscape
	funcMap := template.FuncMap{"dict": templateDict, "add": templateAdd, "array": templateArray, "extendDict": templateExtendDict, "makeSeq": templateMakeSeq}
	if ok {
		funcMap[rawFuncName] = templateValueToString
		funcMap[xmlEscapeFuncName] = templateXMLEscape
	}
	if options.HelperFuncMap != nil {
		for key, v := range options.HelperFuncMap {
			if _, ok := funcMap[key]; ok {
				common.Log.Error("Helper function `%s` is a reserved template helper and cannot be overridden; using the built-in.", key)
				continue
			}
			funcMap[key] = v
		}
	}
	funcs := template.New("").Funcs(funcMap)
	if options.Strict {
		funcs.Option("missingkey=error")
	}
	funcs, err := funcs.Parse(buffer.String())
	if err != nil {
		return nil, err
	}
	if options.SubtemplateMap != nil {
		for key, src := range options.SubtemplateMap {
			if key == "" {
				common.Log.Debug("Subtemplate name cannot be empty. Skipping. Output may be incorrect.")
				continue
			}
			if src == nil {
				common.Log.Debug("Subtemplate content cannot be nil. Skipping. Output may be incorrect.")
				continue
			}
			buffer := bytes.NewBuffer(nil)
			if _, err := io.Copy(buffer, src); err != nil {
				return nil, err
			}
			if _, err := funcs.New(key).Parse(buffer.String()); err != nil {
				return nil, err
			}
		}
	}
	if ok {
		for _, template := range funcs.Templates() {
			if template.Tree != nil {
				addEscapeToTemplate(template.Root)
			}
		}
	}
	return &ParsedTemplate{options: options, tmpl: funcs, source: fileName}, nil
}
