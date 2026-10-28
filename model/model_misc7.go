package model

import (
	"encoding/asn1"
	"fmt"
	"image/color"
	_ "image/gif"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/imageutil"
	"github.com/OpenMellon/openpdf/internal/sampling"
)

func (w lazyStreamWriter) Write(p []byte) (int, error) {
	w.writer.writeBytes(p)
	if w.writer.writeErr != nil {
		return 0, w.writer.writeErr
	}
	return len(p), nil
}

func libraryVersion() string { return common.Version }

// AddCustomInfo adds a custom info into document info dictionary.
func (i *PdfInfo) AddCustomInfo(name string, value string) error {
	if i.dict == nil {
		i.dict = core.MakeDict()
	}
	if _, ok := infoDictKeys[name]; ok {
		return fmt.Errorf("cannot use standard info key %s as custom field key", name)
	}
	i.dict.SetIfNotNil(*core.MakeName(name), core.MakeString(value))
	return nil
}

// GetSamples converts the raw byte slice into samples which are stored in a uint32 bit array.
// Each sample is represented by BitsPerComponent consecutive bits in the raw data.
// NOTE: The method resamples the image byte data before returning the result and
// this could lead to high memory usage, especially on large images. It should
// be avoided, when possible. It is recommended to access the Data field of the
// image directly or use the ColorAt method to extract individual pixels.
func (img *Image) GetSamples() []uint32 {
	resampleBytes := sampling.ResampleBytes(img.Data, int(img.BitsPerComponent))
	if img.BitsPerComponent < 8 {
		resampleBytes = img.samplesTrimPadding(resampleBytes)
	}
	n := int(img.Width) * int(img.Height) * img.ColorComponents
	if len(resampleBytes) < n {
		common.Log.Debug("Error: Too few samples (got %d, expecting %d)", len(resampleBytes), n)
		return resampleBytes
	} else if len(resampleBytes) > n {
		common.Log.Debug("Error: Too many samples (got %d, expecting %d", len(resampleBytes), n)
		resampleBytes = resampleBytes[:n]
	}
	return resampleBytes
}

// NewPdfInfoFromObject creates a new PdfInfo from the input core.PdfObject.
func NewPdfInfoFromObject(obj core.PdfObject) (*PdfInfo, error) {
	var info PdfInfo
	dict, ok := obj.(*core.PdfObjectDictionary)
	if !ok {
		return nil, fmt.Errorf("invalid pdf object type: %T", obj)
	}
	for _, name := range dict.Keys() {
		switch name {
		case "Title":
			info.Title, _ = core.GetString(dict.Get("Title"))
		case "Author":
			info.Author, _ = core.GetString(dict.Get("Author"))
		case "Subject":
			info.Subject, _ = core.GetString(dict.Get("Subject"))
		case "Keywords":
			info.Keywords, _ = core.GetString(dict.Get("Keywords"))
		case "Creator":
			info.Creator, _ = core.GetString(dict.Get("Creator"))
		case "Producer":
			info.Producer, _ = core.GetString(dict.Get("Producer"))
		case "Trapped":
			info.Trapped, _ = core.GetName(dict.Get("Trapped"))
		case "CreationDate":
			if creationDate, ok := core.GetString(dict.Get("CreationDate")); ok && creationDate.String() != "" {
				date, err := NewPdfDate(creationDate.String())
				if err != nil {
					return nil, fmt.Errorf("invalid CreationDate field: %w", err)
				}
				info.CreationDate = &date
			}
		case "ModDate":
			if modDate, ok := core.GetString(dict.Get("ModDate")); ok && modDate.String() != "" {
				date, err := NewPdfDate(modDate.String())
				if err != nil {
					return nil, fmt.Errorf("invalid ModDate field: %w", err)
				}
				info.ModifiedDate = &date
			}
		default:
			str, _ := core.GetString(dict.Get(name))
			if info.dict == nil {
				info.dict = core.MakeDict()
			}
			info.dict.Set(name, str)
		}
	}
	return &info, nil
}

// IDTree represents the ID tree dictionary where the format of the content
// is using Name Trees as described in chapter 7.9.6.
type IDTree struct {
	// Limits shall be an array of two strings, that shall specify the (lexically) least and greatest keys included in the Names array.
	Limits *core.PdfObjectArray

	// Names shall be an array of the form
	//
	// [ key1 value1 key2 value2 … keyn valuen]
	//
	// where each keyi shall be a string and the corresponding valuei shall be the object
	// associated with that key. The keys shall be sorted in lexical order, as described below.
	Names *core.PdfObjectArray

	// Kids Shall be an array of indirect references to the immediate children of this node.
	Kids []*IDTree
}

// SetSamples convert samples to byte-data and sets for the image.
// NOTE: The method resamples the data and this could lead to high memory usage,
// especially on large images. It should be used only when it is not possible
// to work with the image byte data directly.
func (img *Image) SetSamples(samples []uint32) {
	if img.BitsPerComponent < 8 {
		samples = img.samplesAddPadding(samples)
	}
	resampleUint32 := sampling.ResampleUint32(samples, int(img.BitsPerComponent), 8)
	acquireBytes := core.AcquireBytes(len(resampleUint32))
	for i, n := range resampleUint32 {
		acquireBytes[i] = byte(n)
	}
	img.Data = acquireBytes
	img.clearSourceMetadata()
}

type issuingDistributionPoint struct {
	DistributionPoint          distributionPointName `asn1:"optional,tag:0"`
	OnlyContainsUserCerts      bool                  `asn1:"optional,tag:1"`
	OnlyContainsCACerts        bool                  `asn1:"optional,tag:2"`
	OnlySomeReasons            asn1.BitString        `asn1:"optional,tag:3"`
	IndirectCRL                bool                  `asn1:"optional,tag:4"`
	OnlyContainsAttributeCerts bool                  `asn1:"optional,tag:5"`
}

func getPdfKeywords() string {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	return pdfKeywords
}

func inheritReaderOpts(reader *PdfReader, opts *ReaderOpts) *ReaderOpts {
	if reader.parser == nil || reader.parser.Opts == nil {
		return opts
	}
	readerOpts := NewReaderOpts()
	if opts != nil {
		*readerOpts = *opts
	}
	if readerOpts.MaxDecodedStreamSize == 0 {
		readerOpts.MaxDecodedStreamSize = reader.parser.Opts.MaxDecodedStreamSize
	}
	if readerOpts.MaxDecodedTotalSize == 0 {
		readerOpts.MaxDecodedTotalSize = reader.parser.Opts.MaxDecodedTotalSize
	}
	return readerOpts
}

// SetPdfKeywords sets the Keywords attribute of the output PDF.
func SetPdfKeywords(keywords string) {
	metadataMu.Lock()
	defer metadataMu.Unlock()
	pdfKeywords = keywords
}

func iccProfileNumComponents(data []byte) int {
	if len(data) < 128 {
		return 0
	}
	switch string(data[16:20]) {
	case "GRAY":
		return 1
	case "RGB ":
		return 3
	case "CMYK":
		return 4
	default:
		return 0
	}
}

func newICCBasedColorspaceFromProfile(iccProfile []byte, cs PdfColorspace) *PdfColorspaceICCBased {
	if cs == nil {
		return nil
	}
	numComponents := cs.GetNumComponents()
	if numComponents != 1 && numComponents != 3 && numComponents != 4 {
		return nil
	}
	if iccProfileNumComponents(iccProfile) != numComponents {
		return nil
	}
	iccBasedCS, err := NewPdfColorspaceICCBased(numComponents)
	if err != nil {
		return nil
	}
	iccBasedCS.Data = iccProfile
	iccBasedCS.Alternate = cs
	return iccBasedCS
}

// CustomKeys returns all custom info keys as list.
func (i *PdfInfo) CustomKeys() []string {
	if i.dict == nil {
		return nil
	}
	strs := make([]string, len(i.dict.Keys()))
	for _, name := range i.dict.Keys() {
		strs = append(strs, name.String())
	}
	return strs
}

// ToPdfObject converts the ID tree to a PDF object.
func (t *IDTree) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	if t.Names != nil && t.Names.Len() > 0 {
		dict.Set("Names", t.Names)
		dict.Set("Limits", t.Limits)
	}
	if len(t.Kids) > 0 {
		array := core.MakeArray()
		for _, kid := range t.Kids {
			array.Append(kid.ToPdfObject())
		}
		dict2 := core.MakeDict()
		dict2.Set("Kids", array)
		dict.Set("Kids", dict2)
	}
	return dict
}

// ToPdfObject convert PdfInfo to pdf object.
func (i *PdfInfo) ToPdfObject() core.PdfObject {
	dict := core.MakeDict()
	dict.SetIfNotNil("Title", i.Title)
	dict.SetIfNotNil("Author", i.Author)
	dict.SetIfNotNil("Subject", i.Subject)
	dict.SetIfNotNil("Keywords", i.Keywords)
	dict.SetIfNotNil("Creator", i.Creator)
	dict.SetIfNotNil("Producer", i.Producer)
	dict.SetIfNotNil("Trapped", i.Trapped)
	if i.CreationDate != nil {
		dict.SetIfNotNil("CreationDate", i.CreationDate.ToPdfObject())
	}
	if i.ModifiedDate != nil {
		dict.SetIfNotNil("ModDate", i.ModifiedDate.ToPdfObject())
	}
	for _, name := range i.dict.Keys() {
		dict.SetIfNotNil(name, i.dict.Get(name))
	}
	return dict
}

func (img *Image) samplesAddPadding(samples []uint32) []uint32 {
	n := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents) * (8 / int(img.BitsPerComponent))
	count := n * int(img.Height)
	if len(samples) == count {
		return samples
	}
	nums := make([]uint32, count)
	num := int(img.Width) * img.ColorComponents
	for i := 0; i < int(img.Height); i++ {
		size := i * int(img.Width)
		length := i * n
		for i := 0; i < num; i++ {
			nums[length+i] = samples[size+i]
		}
	}
	return nums
}

var infoDictKeys = map[string]struct{}{"Title": {}, "Author": {}, "Subject": {}, "Keywords": {}, "Creator": {}, "Producer": {}, "Trapped": {}, "CreationDate": {}, "ModDate": {}}

// GetCustomInfo returns a custom info value for the specified name.
func (i *PdfInfo) GetCustomInfo(name string) *core.PdfObjectString {
	var str *core.PdfObjectString
	if i.dict == nil {
		return str
	}
	if str2, ok := i.dict.Get(*core.MakeName(name)).(*core.PdfObjectString); ok {
		str = str2
	}
	return str
}

type lazyStreamWriter struct{ writer *PdfWriter }

// PdfInfo holds document information that will overwrite
// document information global variables defined above.
type PdfInfo struct {
	Title        *core.PdfObjectString
	Author       *core.PdfObjectString
	Subject      *core.PdfObjectString
	Keywords     *core.PdfObjectString
	Creator      *core.PdfObjectString
	Producer     *core.PdfObjectString
	CreationDate *PdfDate
	ModifiedDate *PdfDate
	Trapped      *core.PdfObjectName
	dict         *core.PdfObjectDictionary
}

func newIDTreeFromPdfObject(obj core.PdfObject) *IDTree {
	obj = core.ResolveReference(obj)
	array := core.MakeArray()
	arr := core.MakeArray()
	idTrees := []*IDTree{}
	if dict, ok := core.GetDict(obj); ok {
		if names := dict.Get("Names"); names != nil {
			names = core.ResolveReference(names)
			if namesArr, ok := core.GetArray(names); ok {
				for _, obj := range namesArr.Elements() {
					array.Append(obj)
				}
			}
		}
		if limits := dict.Get("Limits"); limits != nil {
			limits = core.ResolveReference(limits)
			if limitsArr, ok := core.GetArray(limits); ok {
				for i := 0; i < 2; i++ {
					arr.Append(limitsArr.Get(i))
				}
			}
		}
		if kids := dict.Get("Kids"); kids != nil {
			kids = core.ResolveReference(kids)
			if kidsArr, ok := core.GetArray(kids); ok {
				for _, obj := range kidsArr.Elements() {
					idTree := newIDTreeFromPdfObject(obj)
					idTrees = append(idTrees, idTree)
				}
			}
		}
	}
	idTree := &IDTree{Names: array, Limits: arr}
	if len(idTrees) > 0 {
		idTree.Kids = idTrees
	}
	return idTree
}

func (img *Image) getSuitableEncoder() (core.StreamEncoder, error) {
	var (
		width, height = int(img.Width), int(img.Height)
		set           = make(map[string]bool)
		ok            = true
		found         = false
		fn            = func() *core.DCTEncoder { return core.NewDCTEncoder() }
		fn2           = func() *core.DCTEncoder {
			dctEncoder := core.NewDCTEncoder()
			dctEncoder.BitsPerComponent = 16
			return dctEncoder
		}
	)
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			colorAt, err := img.ColorAt(j, i)
			if err != nil {
				return nil, err
			}
			rgba, n, count, num := colorAt.RGBA()
			if ok && (rgba != n || rgba != count || n != count) {
				ok = false
			}
			if !found {
				switch colorAt.(type) {
				case color.NRGBA:
					found = num > 0
				}
			}
			set[fmt.Sprintf("%d,%d,%d", rgba, n, count)] = true
			if len(set) > 2 && found {
				return fn2(), nil
			}
		}
	}
	if found || len(img.alpha) > 0 {
		return core.NewFlateEncoder(), nil
	}
	if len(set) <= 2 {
		err := img.ConvertToBinary()
		if err != nil {
			return nil, err
		}
		return core.NewJBIG2Encoder(), nil
	}
	if ok {
		return fn(), nil
	}
	if img.ColorComponents == 1 {
		if img.BitsPerComponent == 1 {
			return core.NewJBIG2Encoder(), nil
		} else if img.BitsPerComponent == 8 {
			dctEncoder := core.NewDCTEncoder()
			dctEncoder.ColorComponents = 1
			return dctEncoder, nil
		}
	} else if img.ColorComponents == 3 {
		if img.BitsPerComponent == 8 {
			return fn(), nil
		} else if img.BitsPerComponent == 16 {
			return fn2(), nil
		}
	} else if img.ColorComponents == 4 {
		dctEncoder := fn2()
		dctEncoder.ColorComponents = 4
		return dctEncoder, nil
	}
	return fn2(), nil
}

// PdfInfoTrapped specifies pdf trapped information.
type PdfInfoTrapped string

func (img *Image) samplesTrimPadding(nums []uint32) []uint32 {
	n := img.ColorComponents * int(img.Width) * int(img.Height)
	if len(nums) == n {
		return nums
	}
	nums2 := make([]uint32, n)
	count := int(img.Width) * img.ColorComponents
	var num, size, length, length2 int
	bytesPerLine := imageutil.BytesPerLine(int(img.Width), int(img.BitsPerComponent), img.ColorComponents)
	for num = 0; num < int(img.Height); num++ {
		size = num * int(img.Width)
		length = num * bytesPerLine
		for length2 = 0; length2 < count; length2++ {
			nums2[size+length2] = nums[length+length2]
		}
	}
	return nums2
}
