package model

import (
	"bytes"
	"errors"
	"fmt"
	_ "image/gif"
	"math"
	"strings"
	"sync"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
)

// PdfPage represents a page in a PDF document. (7.7.3.3 - Table 30).
type PdfPage struct {
	Parent               core.PdfObject
	LastModified         *PdfDate
	Resources            *PdfPageResources
	CropBox              *PdfRectangle
	MediaBox             *PdfRectangle
	BleedBox             *PdfRectangle
	TrimBox              *PdfRectangle
	ArtBox               *PdfRectangle
	BoxColorInfo         core.PdfObject
	Contents             core.PdfObject
	Rotate               *int64
	Group                core.PdfObject
	Thumb                core.PdfObject
	B                    core.PdfObject
	Dur                  core.PdfObject
	Trans                core.PdfObject
	AA                   core.PdfObject
	Metadata             core.PdfObject
	PieceInfo            core.PdfObject
	StructParents        core.PdfObject
	ID                   core.PdfObject
	PZ                   core.PdfObject
	SeparationInfo       core.PdfObject
	Tabs                 core.PdfObject
	TemplateInstantiated core.PdfObject
	PresSteps            core.PdfObject
	UserUnit             core.PdfObject
	VP                   core.PdfObject
	Annots               core.PdfObject
	annotations          []*PdfAnnotation
	mu                   *sync.Mutex
	pageDict             *core.PdfObjectDictionary
	primitive            *core.PdfIndirectObject
	originalDict         *core.PdfObjectDictionary
	reader               *PdfReader
}

// AppendContentBytes creates a PDF stream from `cs` and appends it to the
// array of streams specified by the pages's Contents entry.
// If `wrapContents` is true, the content stream of the page is wrapped using
// a `q/Q` operator pair, so that its state does not affect the appended
// content stream.
func (p *PdfPage) AppendContentBytes(cs []byte, wrapContents bool) error {
	contentStreamObjs := p.GetContentStreamObjs()
	wrapContents = wrapContents && len(contentStreamObjs) > 0
	flateEncoder := core.NewFlateEncoder()
	array := core.MakeArray()
	if wrapContents {
		stream, err := core.MakeStream([]byte("q\n"), flateEncoder)
		if err != nil {
			return err
		}
		array.Append(stream)
	}
	array.Append(contentStreamObjs...)
	if wrapContents {
		stream, err := core.MakeStream([]byte("\nQ\n"), flateEncoder)
		if err != nil {
			return err
		}
		array.Append(stream)
	}
	stream, err := core.MakeStream(cs, flateEncoder)
	if err != nil {
		return err
	}
	array.Append(stream)
	p.Contents = array
	return nil
}

func (p *PdfPage) getAnnotationsLocked() ([]*PdfAnnotation, error) {
	if p.annotations != nil {
		return p.annotations, nil
	}
	if p.Annots == nil {
		p.annotations = []*PdfAnnotation{}
		return nil, nil
	}
	if p.reader == nil {
		p.annotations = []*PdfAnnotation{}
		return nil, nil
	}
	annotations, err := p.reader.loadAnnotations(p.Annots)
	if err != nil {
		return nil, err
	}
	if annotations == nil {
		p.annotations = []*PdfAnnotation{}
	}
	p.annotations = annotations
	return p.annotations, nil
}

func (p *PdfPage) flattenFieldsWithOpts(appgen FieldAppearanceGenerator, fieldFlattenOpts *FieldFlattenOpts, set map[*PdfAnnotation]bool) error {
	var annots []*PdfAnnotation
	if appgen != nil {
		if err := appgen.WrapContentStream(p); err != nil {
			return err
		}
	}
	annotations, err := p.GetAnnotations()
	if err != nil {
		return err
	}
	for _, annotation := range annotations {
		ok, found := set[annotation]
		if !found && fieldFlattenOpts.AnnotFilterFunc != nil {
			if _, valid := annotation.GetContext().(*PdfAnnotationWidget); !valid {
				found = fieldFlattenOpts.AnnotFilterFunc(annotation)
			}
		}
		if !found {
			annots = append(annots, annotation)
			continue
		}
		switch annotation.GetContext().(type) {
		case *PdfAnnotationPopup:
			continue
		case *PdfAnnotationLink:
			continue
		case *PdfAnnotationProjection:
			continue
		}
		form, rect, err := appearanceFormAndRect(annotation)
		if err != nil {
			if !ok {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Field without V -> annotation without appearance stream - skipping over")
				}
				continue
			}
			common.Log.Debug("ERROR Annotation without appearance stream, err : %v - skipping over", err)
			continue
		}
		if form == nil {
			continue
		}
		generateXObjectName := p.Resources.GenerateXObjectName()
		p.Resources.SetXObjectFormByName(generateXObjectName, form)
		rect2, done, err := formBBox(form)
		if err != nil {
			common.Log.Debug("Error converting appearance Matrix, skipping xform bbox adjustment: %v", err)
		} else {
			identityMatrix := transform.IdentityMatrix()
			identityMatrix = identityMatrix.Translate(-rect2.Llx, -rect2.Lly)
			if done {
				f := 1.0
				if rect2.Width() > 0 {
					f = math.Round(rect.Width() / rect2.Width())
				}
				v := 1.0
				if rect2.Height() > 0 {
					v = math.Round(rect.Height() / rect2.Height())
				}
				identityMatrix = identityMatrix.Scale(f, v)
			}
			rect.Transform(identityMatrix)
		}
		f := math.Min(rect.Llx, rect.Urx)
		v := math.Min(rect.Lly, rect.Ury)
		var strs []string
		strs = append(strs, "q")
		strs = append(strs, fmt.Sprintf("%.6f %.6f %.6f %.6f %.6f %.6f cm", 1.0, 0.0, 0.0, 1.0, f, v))
		strs = append(strs, fmt.Sprintf("/%s Do", generateXObjectName.String()))
		strs = append(strs, "Q")
		join := strings.Join(strs, "\n")
		err = p.AppendContentStream(join)
		if err != nil {
			return err
		}
		if form.Resources != nil {
			fontDict, ok := core.GetDict(form.Resources.Font)
			if ok {
				for _, name := range fontDict.Keys() {
					if !p.Resources.HasFontByName(name) {
						p.Resources.SetFontByName(name, fontDict.Get(name))
					}
				}
			}
		}
	}
	if len(annots) > 0 {
		p.annotations = annots
	} else {
		p.annotations = []*PdfAnnotation{}
	}
	return nil
}

func (p *PdfPage) annotationsSnapshot() []*PdfAnnotation {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	return p.annotations
}

// AppendContentStream adds content stream by string.  Appends to the last
// contentstream instance if many.
func (p *PdfPage) AppendContentStream(contentStr string) error {
	contentStreams, err := p.GetContentStreams()
	if err != nil {
		return err
	}
	if len(contentStreams) == 0 {
		contentStreams = []string{contentStr}
		return p.SetContentStreams(contentStreams, core.NewFlateEncoder())
	}
	var buf bytes.Buffer
	buf.WriteString(contentStreams[len(contentStreams)-1])
	buf.WriteString("\n")
	buf.WriteString(contentStr)
	contentStreams[len(contentStreams)-1] = buf.String()
	return p.SetContentStreams(contentStreams, core.NewFlateEncoder())
}

// HasExtGState checks if ExtGState name is available.
func (p *PdfPage) HasExtGState(name core.PdfObjectName) bool {
	if p.Resources == nil {
		return false
	}
	if p.Resources.ExtGState == nil {
		return false
	}
	dict, ok := core.TraceToDirectObject(p.Resources.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected ExtGState dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.ExtGState))
		return false
	}
	obj := dict.Get(name)
	found := obj != nil
	return found
}

func (p *PdfPage) setContainer(indirect *core.PdfIndirectObject) {
	indirect.PdfObject = p.pageDict
	p.primitive = indirect
}

// FlattenFieldsWithOpts flattens the form field widgets and annotations of the
// page using the provided field appearance generator and the specified options.
// If opts is nil, all annotations on the page are flattened.
// Widget (form field) annotations are always flattened. Other annotations are
// flattened only when they pass opts.AnnotFilterFunc; when no filter is
// provided, every annotation is flattened. Annotations that are not flattened
// are preserved on the page.
func (p *PdfPage) FlattenFieldsWithOpts(appgen FieldAppearanceGenerator, opts *FieldFlattenOpts) error {
	if opts == nil {
		opts = &FieldFlattenOpts{}
	}
	annotations, err := p.GetAnnotations()
	if err != nil {
		return err
	}
	set := map[*PdfAnnotation]bool{}
	for _, annotation := range annotations {
		_, ok := annotation.GetContext().(*PdfAnnotationWidget)
		if ok || opts.AnnotFilterFunc == nil || opts.AnnotFilterFunc(annotation) {
			set[annotation] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	fieldFlattenOpts := *opts
	fieldFlattenOpts.AnnotFilterFunc = nil
	return p.flattenFieldsWithOpts(appgen, &fieldFlattenOpts, set)
}

// SetContentStreams sets the content streams based on a string array. Will make
// 1 object stream for each string and reference from the page Contents.
// Each stream will be encoded using the encoding specified by the StreamEncoder,
// if empty, will use identity encoding (raw data).
func (p *PdfPage) SetContentStreams(cStreams []string, encoder core.StreamEncoder) error {
	if len(cStreams) == 0 {
		p.Contents = nil
		return nil
	}
	if encoder == nil {
		encoder = core.NewRawEncoder()
	}
	var streams []*core.PdfObjectStream
	for _, cStream := range cStreams {
		stream := &core.PdfObjectStream{}
		streamDict := encoder.MakeStreamDict()
		encodeBytes, err := encoder.EncodeBytes([]byte(cStream))
		if err != nil {
			return err
		}
		streamDict.Set("Length", core.MakeInteger(int64(len(encodeBytes))))
		stream.PdfObjectDictionary = streamDict
		stream.Stream = encodeBytes
		streams = append(streams, stream)
	}
	if len(streams) == 1 {
		p.Contents = streams[0]
	} else {
		array := core.MakeArray()
		for _, stream := range streams {
			array.Append(stream)
		}
		p.Contents = array
	}
	return nil
}

// GetAllContentStreams gets all the content streams for a page as one string.
func (p *PdfPage) GetAllContentStreams() (string, error) {
	contentStreams, err := p.GetContentStreams()
	if err != nil {
		return "", err
	}
	return strings.Join(contentStreams, " "), nil
}

// GetContentStreams returns the content stream as an array of strings.
func (p *PdfPage) GetContentStreams() ([]string, error) {
	contentStreamObjs := p.GetContentStreamObjs()
	var strs []string
	for _, contentStreamObj := range contentStreamObjs {
		s, err := pdfObjectToString(contentStreamObj)
		if err != nil {
			return nil, err
		}
		strs = append(strs, s)
	}
	return strs, nil
}

// AddContentStreamByString adds content stream by string. Puts the content
// string into a stream object and points the content stream towards it.
func (p *PdfPage) AddContentStreamByString(contentStr string) error {
	stream, err := core.MakeStream([]byte(contentStr), core.NewFlateEncoder())
	if err != nil {
		return err
	}
	if p.Contents == nil {
		p.Contents = stream
	} else {
		obj := core.TraceToDirectObject(p.Contents)
		arr, ok := obj.(*core.PdfObjectArray)
		if !ok {
			arr = core.MakeArray(obj)
		}
		arr.Append(stream)
		p.Contents = arr
	}
	return nil
}

// AddExtGState adds a graphics state to the XObject resources.
func (p *PdfPage) AddExtGState(name core.PdfObjectName, egs *core.PdfObjectDictionary) error {
	if p.Resources == nil {
		p.Resources = NewPdfPageResources()
	}
	if p.Resources.ExtGState == nil {
		p.Resources.ExtGState = core.MakeDict()
	}
	dict, ok := core.TraceToDirectObject(p.Resources.ExtGState).(*core.PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Expected ExtGState dictionary is not a dictionary: %v", core.TraceToDirectObject(p.Resources.ExtGState))
		return errors.New("type check error")
	}
	dict.Set(name, egs)
	return nil
}

// NewPdfPage returns a new PDF page.
func NewPdfPage() *PdfPage {
	page := PdfPage{}
	page.mu = &sync.Mutex{}
	page.pageDict = core.MakeDict()
	page.Resources = NewPdfPageResources()
	indirect := core.PdfIndirectObject{
		PdfObject: page.pageDict,
	}
	page.primitive = &indirect
	page.originalDict = page.pageDict.ShallowCopy()
	return &page
}

// AddAnnotation appends `annot` to the list of page annotations.
func (p *PdfPage) AddAnnotation(annot *PdfAnnotation) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	if p.annotations == nil {
		p.getAnnotationsLocked()
	}
	p.annotations = append(p.annotations, annot)
}

// SetAnnotations sets the annotations list.
func (p *PdfPage) SetAnnotations(annotations []*PdfAnnotation) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	p.annotations = annotations
}

// GetContainingPdfObject returns the page as a dictionary within an PdfIndirectObject.
func (p *PdfPage) GetContainingPdfObject() core.PdfObject { return p.primitive }

// GetAnnotations returns the list of page annotations for `page`. If not loaded attempts to load the
// annotations, otherwise returns the loaded list. Safe for concurrent use: the
// lazy load is guarded so the same page may be read from multiple goroutines.
func (p *PdfPage) GetAnnotations() ([]*PdfAnnotation, error) {
	if p.mu != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	return p.getAnnotationsLocked()
}

// Duplicate creates a duplicate page based on the current one and returns it.
func (p *PdfPage) Duplicate() *PdfPage {
	page := *p
	page.mu = &sync.Mutex{}
	page.pageDict = core.MakeDict()
	page.primitive = core.MakeIndirectObject(page.pageDict)
	page.originalDict = page.pageDict.ShallowCopy()
	return &page
}

// GetContentStreamObjs returns a slice of PDF objects containing the content
// streams of the page.
func (p *PdfPage) GetContentStreamObjs() []core.PdfObject {
	if p.Contents == nil {
		return nil
	}
	obj := core.TraceToDirectObject(p.Contents)
	if arr, ok := obj.(*core.PdfObjectArray); ok {
		return arr.Elements()
	}
	return []core.PdfObject{obj}
}

func (p *PdfPage) getParentResources() (*PdfPageResources, error) {
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			common.Log.Debug("ERROR: invalid parent node")
			return nil, errors.New("invalid parent object")
		}
		if resources := parentDict.Get("Resources"); resources != nil {
			resourcesDict, ok := core.GetDict(resources)
			if !ok {
				return nil, errors.New("invalid resource dict")
			}
			resources, err := NewPdfPageResourcesFromDict(resourcesDict)
			if err != nil {
				return nil, err
			}
			return resources, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, nil
}

// GetPageDict converts the Page to a PDF object dictionary.
func (p *PdfPage) GetPageDict() *core.PdfObjectDictionary {
	pageDict := p.pageDict
	pageDict.Clear()
	pageDict.Set("Type", core.MakeName("Page"))
	pageDict.Set("Parent", p.Parent)
	if p.LastModified != nil {
		pageDict.Set("LastModified", p.LastModified.ToPdfObject())
	}
	if p.Resources != nil {
		pageDict.Set("Resources", p.Resources.ToPdfObject())
	}
	if p.CropBox != nil {
		pageDict.Set("CropBox", p.CropBox.ToPdfObject())
	}
	if p.MediaBox != nil {
		pageDict.Set("MediaBox", p.MediaBox.ToPdfObject())
	}
	if p.BleedBox != nil {
		pageDict.Set("BleedBox", p.BleedBox.ToPdfObject())
	}
	if p.TrimBox != nil {
		pageDict.Set("TrimBox", p.TrimBox.ToPdfObject())
	}
	if p.ArtBox != nil {
		pageDict.Set("ArtBox", p.ArtBox.ToPdfObject())
	}
	pageDict.SetIfNotNil("BoxColorInfo", p.BoxColorInfo)
	pageDict.SetIfNotNil("Contents", p.Contents)
	if p.Rotate != nil {
		pageDict.Set("Rotate", core.MakeInteger(*p.Rotate))
	}
	pageDict.SetIfNotNil("Group", p.Group)
	pageDict.SetIfNotNil("Thumb", p.Thumb)
	pageDict.SetIfNotNil("B", p.B)
	pageDict.SetIfNotNil("Dur", p.Dur)
	pageDict.SetIfNotNil("Trans", p.Trans)
	pageDict.SetIfNotNil("AA", p.AA)
	pageDict.SetIfNotNil("Metadata", p.Metadata)
	pageDict.SetIfNotNil("PieceInfo", p.PieceInfo)
	pageDict.SetIfNotNil("StructParents", p.StructParents)
	pageDict.SetIfNotNil("ID", p.ID)
	pageDict.SetIfNotNil("PZ", p.PZ)
	pageDict.SetIfNotNil("SeparationInfo", p.SeparationInfo)
	pageDict.SetIfNotNil("Tabs", p.Tabs)
	pageDict.SetIfNotNil("TemplateInstantiated", p.TemplateInstantiated)
	pageDict.SetIfNotNil("PresSteps", p.PresSteps)
	pageDict.SetIfNotNil("UserUnit", p.UserUnit)
	pageDict.SetIfNotNil("VP", p.VP)
	if annotationsSnapshot := p.annotationsSnapshot(); annotationsSnapshot != nil {
		array := core.MakeArray()
		for _, annot := range annotationsSnapshot {
			if model := annot.GetContext(); model != nil {
				array.Append(model.ToPdfObject())
			} else {
				array.Append(annot.ToPdfObject())
			}
		}
		if array.Len() > 0 {
			pageDict.Set("Annots", array)
		}
	} else if p.Annots != nil {
		pageDict.SetIfNotNil("Annots", p.Annots)
	}
	return pageDict
}

// GetPageAsIndirectObject returns the page as a dictionary within an PdfIndirectObject.
func (p *PdfPage) GetPageAsIndirectObject() *core.PdfIndirectObject { return p.primitive }

// GetMediaBox gets the inheritable media box value, either from the page
// or a higher up page/pages struct.
func (p *PdfPage) GetMediaBox() (*PdfRectangle, error) {
	if p.MediaBox != nil {
		return p.MediaBox, nil
	}
	parent := p.Parent
	for parent != nil {
		parentDict, ok := core.GetDict(parent)
		if !ok {
			return nil, errors.New("invalid parent objects dictionary")
		}
		if mediaBox := parentDict.Get("MediaBox"); mediaBox != nil {
			mediaBoxArr, ok := core.GetArray(mediaBox)
			if !ok {
				return nil, errors.New("invalid media box")
			}
			rect, err := NewPdfRectangle(*mediaBoxArr)
			if err != nil {
				return nil, err
			}
			return rect, nil
		}
		parent = parentDict.Get("Parent")
	}
	return nil, errors.New("media box not defined")
}

// AddImageResource adds an image to the XObject resources.
func (p *PdfPage) AddImageResource(name core.PdfObjectName, ximg *XObjectImage) error {
	var dict *core.PdfObjectDictionary
	if p.Resources.XObject == nil {
		dict = core.MakeDict()
		p.Resources.XObject = dict
	} else {
		var ok bool
		dict, ok = p.Resources.XObject.(*core.PdfObjectDictionary)
		if !ok {
			return errors.New("invalid xres dict type")
		}
	}
	dict.Set(name, ximg.ToPdfObject())
	return nil
}

// RemveTabOrder removes the tab order for the page.
func (p *PdfPage) RemoveTabOrder() { p.Tabs = nil }
