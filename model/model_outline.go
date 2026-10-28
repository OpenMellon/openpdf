package model

import (
	"errors"
	"fmt"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
)

// Outline represents a PDF outline dictionary (Table 152 - p. 376).
// Currently, the Outline object can only be used to construct PDF outlines.
type Outline struct {
	Entries []*OutlineItem `json:"entries,omitempty"`
}

// NewOutline returns a new outline instance.
func NewOutline() *Outline { return &Outline{} }

// ToPdfOutline returns a low level PdfOutline object, based on the current
// instance.
func (o *Outline) ToPdfOutline() *PdfOutline {
	outline := NewPdfOutline()
	var items []*PdfOutlineItem
	var n int64
	var item *PdfOutlineItem
	for _, entry := range o.Entries {
		item2, count := entry.ToPdfOutlineItem()
		item2.Parent = &outline.PdfOutlineTreeNode
		if item != nil {
			item.Next = &item2.PdfOutlineTreeNode
			item2.Prev = &item.PdfOutlineTreeNode
		}
		items = append(items, item2)
		n += count
		item = item2
	}
	count := int64(len(items))
	n += count
	if count > 0 {
		outline.First = &items[0].PdfOutlineTreeNode
		outline.Last = &items[count-1].PdfOutlineTreeNode
		outline.Count = &n
	}
	return outline
}

// ToPdfObject returns a PDF object representation of the outline.
func (o *Outline) ToPdfObject() core.PdfObject { return o.ToPdfOutline().ToPdfObject() }

// Insert adds a top level outline item in the outline,
// at the specified index.
func (o *Outline) Insert(index uint, item *OutlineItem) {
	n := uint(len(o.Entries))
	if index > n {
		index = n
	}
	o.Entries = append(o.Entries[:index], append([]*OutlineItem{item}, o.Entries[index:]...)...)
}

// Items returns all children outline items.
func (o *Outline) Items() []*OutlineItem { return o.Entries }

// NewOutlineBookmark returns an initialized PdfOutlineItem for a given bookmark title and page.
func NewOutlineBookmark(title string, page *core.PdfIndirectObject) *PdfOutlineItem {
	item := PdfOutlineItem{primitive: core.MakeIndirectObject(core.MakeDict())}
	item.context = &item
	item.Title = core.MakeString(title)
	array := core.MakeArray()
	array.Append(page)
	array.Append(core.MakeName("Fit"))
	item.Dest = array
	return &item
}

// ToOutlineTree returns a low level PdfOutlineTreeNode object, based on
// the current instance.
func (o *Outline) ToOutlineTree() *PdfOutlineTreeNode {
	return &o.ToPdfOutline().PdfOutlineTreeNode
}

// Add appends a top level outline item to the outline.
func (o *Outline) Add(item *OutlineItem) { o.Entries = append(o.Entries, item) }

// OutlineDest represents the destination of an outline item.
// It holds the page and the position on the page an outline item points to.
type OutlineDest struct {
	PageObj *core.PdfIndirectObject `json:"-"`
	Page    int64                   `json:"page"`
	Mode    string                  `json:"mode"`
	X       float64                 `json:"x"`
	Y       float64                 `json:"y"`
	Zoom    float64                 `json:"zoom"`
}

// ToPdfObject returns a PDF object representation of the outline destination.
func (d OutlineDest) ToPdfObject() core.PdfObject {
	if (d.PageObj == nil && d.Page < 0) || d.Mode == "" {
		return core.MakeNull()
	}
	array := core.MakeArray()
	if d.PageObj != nil {
		array.Append(d.PageObj)
	} else {
		array.Append(core.MakeInteger(d.Page))
	}
	array.Append(core.MakeName(d.Mode))
	switch d.Mode {
	case "Fit", "FitB":
	case "FitH", "FitBH":
		array.Append(core.MakeFloat(d.Y))
	case "FitV", "FitBV":
		array.Append(core.MakeFloat(d.X))
	case "XYZ":
		array.Append(core.MakeFloat(d.X))
		array.Append(core.MakeFloat(d.Y))
		array.Append(core.MakeFloat(d.Zoom))
	default:
		array.Set(1, core.MakeName("Fit"))
	}
	return array
}

// NewOutlineDest returns a new outline destination which can be used
// with outline items.
func NewOutlineDest(page int64, x, y float64) OutlineDest {
	return OutlineDest{Page: page, Mode: "XYZ", X: x, Y: y}
}

func newOutlineDestFromPdfObject(obj core.PdfObject, reader *PdfReader) (*OutlineDest, error) {
	arr, ok := core.GetArray(obj)
	if !ok {
		return nil, errors.New("outline destination object must be an array")
	}
	arrLen := arr.Len()
	if arrLen < 2 {
		return nil, fmt.Errorf("invalid outline destination array length: %d", arrLen)
	}
	dest := &OutlineDest{Mode: "Fit"}
	obj2 := arr.Get(0)
	if indirect, ok := core.GetIndirect(obj2); ok {
		if _, n, err := reader.PageFromIndirectObject(indirect); err == nil {
			dest.Page = int64(n - 1)
		} else {
			common.Log.Debug("WARN: could not get page index for page %+v", indirect)
		}
		dest.PageObj = indirect
	} else if n, ok := core.GetIntVal(obj2); ok {
		if n >= 0 && n < len(reader.PageList) {
			dest.PageObj = reader.PageList[n].GetPageAsIndirectObject()
		} else {
			common.Log.Debug("WARN: could not get page container for page %d", n)
		}
		dest.Page = int64(n)
	} else {
		return nil, fmt.Errorf("invalid outline destination page: %T", obj2)
	}
	s, ok := core.GetNameVal(arr.Get(1))
	if !ok {
		common.Log.Debug("invalid outline destination magnification mode: %v", arr.Get(1))
		return dest, nil
	}
	switch s {
	case "Fit", "FitB":
	case "FitH", "FitBH":
		if arrLen > 2 {
			dest.Y, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
		}
	case "FitV", "FitBV":
		if arrLen > 2 {
			dest.X, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
		}
	case "XYZ":
		if arrLen > 4 {
			dest.X, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(2)))
			dest.Y, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(3)))
			dest.Zoom, _ = core.GetNumberAsFloat(core.TraceToDirectObject(arr.Get(4)))
		}
	default:
		s = "Fit"
	}
	dest.Mode = s
	return dest, nil
}

// PdfOutline represents a PDF outline dictionary (Table 152 - p. 376).
type PdfOutline struct {
	PdfOutlineTreeNode
	Parent    *PdfOutlineTreeNode
	Count     *int64
	primitive *core.PdfIndirectObject
}

// NewPdfOutlineTree returns an initialized PdfOutline tree.
func NewPdfOutlineTree() *PdfOutline {
	outline := NewPdfOutline()
	outline.context = &outline
	return outline
}

func newPdfOutlineFromIndirectObject(indirect *core.PdfIndirectObject) (*PdfOutline, error) {
	dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("outline object not a dictionary")
	}
	outline := NewPdfOutline()
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if ok {
			if *name != "Outlines" {
				common.Log.Debug("ERROR Type != Outlines (%s)", *name)
			}
		}
	}
	if count := dict.Get("Count"); count != nil {
		n, err := core.GetNumberAsInt64(count)
		if err != nil {
			return nil, err
		}
		outline.Count = &n
	}
	return outline, nil
}

// ToPdfObject recursively builds the Outline tree PDF object.
func (o *PdfOutline) ToPdfObject() core.PdfObject {
	indirect := o.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Type", core.MakeName("Outlines"))
	if o.First != nil {
		dict.Set("First", o.First.ToPdfObject())
	}
	if o.Last != nil {
		dict.Set("Last", o.Last.GetContext().GetContainingPdfObject())
	}
	if o.Parent != nil {
		dict.Set("Parent", o.Parent.GetContext().GetContainingPdfObject())
	}
	if o.Count != nil {
		dict.Set("Count", core.MakeInteger(*o.Count))
	}
	return indirect
}

// NewPdfOutline returns an initialized PdfOutline.
func NewPdfOutline() *PdfOutline {
	outline := &PdfOutline{primitive: core.MakeIndirectObject(core.MakeDict())}
	outline.context = outline
	return outline
}

// GetContainingPdfObject returns the container of the outline (indirect object).
func (o *PdfOutline) GetContainingPdfObject() core.PdfObject { return o.primitive }

// PdfOutlineTreeNode contains common fields used by the outline and outline
// item objects.
type PdfOutlineTreeNode struct {
	context any
	First   *PdfOutlineTreeNode
	Last    *PdfOutlineTreeNode
}

// GetContainingPdfObject returns the container of the outline tree node (indirect object).
func (n *PdfOutlineTreeNode) GetContainingPdfObject() core.PdfObject {
	return n.GetContext().GetContainingPdfObject()
}

// ToPdfObject returns the PDF representation of the outline tree node.
func (n *PdfOutlineTreeNode) ToPdfObject() core.PdfObject {
	return n.GetContext().ToPdfObject()
}

// GetContext returns the context of the outline tree node, which is either a
// *PdfOutline or a *PdfOutlineItem. The method returns nil for uninitialized
// tree nodes.
func (n *PdfOutlineTreeNode) GetContext() PdfModel {
	if outline, ok := n.context.(*PdfOutline); ok {
		return outline
	}
	if item, ok := n.context.(*PdfOutlineItem); ok {
		return item
	}
	common.Log.Debug("ERROR Invalid outline tree node item")
	return nil
}

// PdfOutlineItem represents an outline item dictionary (Table 153 - pp. 376 - 377).
type PdfOutlineItem struct {
	PdfOutlineTreeNode
	Title     *core.PdfObjectString
	Parent    *PdfOutlineTreeNode
	Prev      *PdfOutlineTreeNode
	Next      *PdfOutlineTreeNode
	Count     *int64
	Dest      core.PdfObject
	A         core.PdfObject
	SE        core.PdfObject
	C         core.PdfObject
	F         core.PdfObject
	primitive *core.PdfIndirectObject
}

// GetContainingPdfObject returns the container of the outline item (indirect object).
func (o *PdfOutlineItem) GetContainingPdfObject() core.PdfObject { return o.primitive }

// ToPdfObject recursively builds the Outline tree PDF object.
func (o *PdfOutlineItem) ToPdfObject() core.PdfObject {
	indirect := o.primitive
	dict := indirect.PdfObject.(*core.PdfObjectDictionary)
	dict.Set("Title", o.Title)
	if o.A != nil {
		dict.Set("A", o.A)
	}
	if se := dict.Get("SE"); se != nil {
		dict.Remove("SE")
	}
	if o.C != nil {
		dict.Set("C", o.C)
	}
	if o.Dest != nil {
		dict.Set("Dest", o.Dest)
	}
	if o.F != nil {
		dict.Set("F", o.F)
	}
	if o.Count != nil {
		dict.Set("Count", core.MakeInteger(*o.Count))
	}
	if o.Next != nil {
		dict.Set("Next", o.Next.ToPdfObject())
	}
	if o.First != nil {
		dict.Set("First", o.First.ToPdfObject())
	}
	if o.Prev != nil {
		dict.Set("Prev", o.Prev.GetContext().GetContainingPdfObject())
	}
	if o.Last != nil {
		dict.Set("Last", o.Last.GetContext().GetContainingPdfObject())
	}
	if o.Parent != nil {
		dict.Set("Parent", o.Parent.GetContext().GetContainingPdfObject())
	}
	return indirect
}

// NewPdfOutlineItem returns an initialized PdfOutlineItem.
func NewPdfOutlineItem() *PdfOutlineItem {
	item := &PdfOutlineItem{primitive: core.MakeIndirectObject(core.MakeDict())}
	item.context = item
	return item
}

// OutlineItem represents a PDF outline item dictionary (Table 153 - pp. 376 - 377).
type OutlineItem struct {
	Title   string         `json:"title"`
	Dest    OutlineDest    `json:"dest"`
	Entries []*OutlineItem `json:"entries,omitempty"`
}

// ToPdfOutlineItem returns a low level PdfOutlineItem object,
// based on the current instance.
func (o *OutlineItem) ToPdfOutlineItem() (*PdfOutlineItem, int64) {
	item := NewPdfOutlineItem()
	item.Title = core.MakeEncodedString(o.Title, true)
	item.Dest = o.Dest.ToPdfObject()
	var items []*PdfOutlineItem
	var n int64
	var item2 *PdfOutlineItem
	for _, entry := range o.Entries {
		item3, count := entry.ToPdfOutlineItem()
		item3.Parent = &item.PdfOutlineTreeNode
		if item2 != nil {
			item2.Next = &item3.PdfOutlineTreeNode
			item3.Prev = &item2.PdfOutlineTreeNode
		}
		items = append(items, item3)
		n += count
		item2 = item3
	}
	count := len(items)
	n += int64(count)
	if count > 0 {
		item.First = &items[0].PdfOutlineTreeNode
		item.Last = &items[count-1].PdfOutlineTreeNode
		item.Count = &n
	}
	return item, n
}

// Insert adds an outline item as a child of the current outline item,
// at the specified index.
func (o *OutlineItem) Insert(index uint, item *OutlineItem) {
	n := uint(len(o.Entries))
	if index > n {
		index = n
	}
	o.Entries = append(o.Entries[:index], append([]*OutlineItem{item}, o.Entries[index:]...)...)
}

// ToPdfObject returns a PDF object representation of the outline item.
func (o *OutlineItem) ToPdfObject() core.PdfObject {
	item, _ := o.ToPdfOutlineItem()
	return item.ToPdfObject()
}

// NewOutlineItem returns a new outline item instance.
func NewOutlineItem(title string, dest OutlineDest) *OutlineItem {
	return &OutlineItem{Title: title, Dest: dest}
}

// Add appends an outline item as a child of the current outline item.
func (o *OutlineItem) Add(item *OutlineItem) { o.Entries = append(o.Entries, item) }

// Items returns all children outline items.
func (o *OutlineItem) Items() []*OutlineItem { return o.Entries }
