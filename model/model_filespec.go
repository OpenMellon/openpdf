package model

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	_ "image/gif"
	"os"
	"path/filepath"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/h2non/filetype"
)

// FileRelationship represents a attachment file relationship type.
type (
	FileRelationship int
	ocspKey          struct{ responseHash, certHash, issuerHash [32]byte }
)

const (
	RelationshipSource FileRelationship = iota
	RelationshipData
	RelationshipAlternative
	RelationshipSupplement
	RelationshipUnspecified
)

// EmbeddedFile represents an embedded file.
type EmbeddedFile struct {
	Name         string
	Content      []byte
	FileType     string
	Description  string
	Relationship FileRelationship
	Hash         string
	CreationTime time.Time
	ModTime      time.Time
}

// NewEmbeddedFile constructs a new EmbeddedFile object from supplied file.
// The file type of the file would be detected automatically.
func NewEmbeddedFile(path string) (*EmbeddedFile, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	match, err := filetype.Match(file)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(file)
	file2 := &EmbeddedFile{Name: filepath.Base(path), Content: file, FileType: match.MIME.Value, Hash: hex.EncodeToString(data[:])}
	return file2, nil
}

// NewEmbeddedFileFromObject construct a new EmbeddedFile from supplied object.
func NewEmbeddedFileFromObject(obj core.PdfObject) (*EmbeddedFile, error) {
	obj2 := core.TraceToDirectObject(obj)
	dict, ok := obj2.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid object")
	}
	f := core.TraceToDirectObject(dict.Get("F"))
	if f == nil {
		return nil, errors.New("invalid object structure")
	}
	stream, ok := f.(*core.PdfObjectStream)
	if !ok {
		return nil, errors.New("invalid stream")
	}
	dict2 := stream.PdfObjectDictionary
	params := dict2.Get("Params")
	if params == nil {
		return nil, errors.New("params object not available")
	}
	dict3, ok := params.(*core.PdfObjectDictionary)
	if !ok {
		return nil, errors.New("invalid Params object")
	}
	s := ""
	checkSum := dict3.Get("CheckSum")
	if checkSum != nil {
		s = checkSum.(*core.PdfObjectString).Str()
	}
	data, err := core.DecodeStream(stream)
	if err != nil {
		return nil, err
	}
	file := &EmbeddedFile{Content: data, Hash: s}
	return file, nil
}

// ToPdfObject implements interface PdfModel.
func (f *EmbeddedFile) ToPdfObject() core.PdfObject {
	flateEncoder := core.NewFlateEncoder()
	stream, err := core.MakeStream(f.Content, flateEncoder)
	if err != nil {
		common.Log.Debug("Failed to create embedded file stream: %v", err)
		return nil
	}
	dict := stream.PdfObjectDictionary
	dict.Set("Type", core.MakeName("EmbeddedFile"))
	dict.Set("Subtype", core.MakeEncodedString("text/plain", true))
	dict2 := core.MakeDict()
	dict2.Set("CheckSum", core.MakeString(f.Hash[:]))
	dict2.Set("Size", core.MakeInteger(int64(len(f.Content))))
	creationTime := f.CreationTime
	if creationTime.IsZero() {
		creationTime = time.Now()
	}
	modTime := f.ModTime
	if modTime.IsZero() {
		modTime = creationTime
	}
	date, err := NewPdfDateFromTime(creationTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	date2, err := NewPdfDateFromTime(modTime)
	if err != nil {
		common.Log.Debug("Failed to create embedded file creation date: %v", err)
		return nil
	}
	dict2.Set("CreationDate", date.ToPdfObject())
	dict2.Set("ModDate", date2.ToPdfObject())
	dict.Set("Params", dict2)
	dict3 := core.MakeDict()
	dict3.Set(*core.MakeName("F"), stream)
	return dict3
}

// NewEmbeddedFileFromContent construct a new EmbeddedFile from supplied file content.
func NewEmbeddedFileFromContent(content []byte) (*EmbeddedFile, error) {
	match, err := filetype.Match(content)
	if err != nil {
		common.Log.Debug("Failed to detect file type: %v", err)
	}
	data := md5.Sum(content)
	file := &EmbeddedFile{Name: "attachment", Content: content, FileType: match.MIME.Type, Hash: hex.EncodeToString(data[:])}
	return file, nil
}

// PdfFilespec represents a file specification which can either refer to an external or embedded file.
type PdfFilespec struct {
	Type core.PdfObject
	FS   core.PdfObject
	F    core.PdfObject
	UF   core.PdfObject

	// DOS, Mac and Unix are obsolete as of PDF 2.0 (deprecated by PDF 1.7); the F and UF entries
	// should be used instead. They are still read and written here because they may appear in
	// existing files produced by older writers.
	DOS            core.PdfObject
	Mac            core.PdfObject
	Unix           core.PdfObject
	ID             core.PdfObject
	V              core.PdfObject
	EF             core.PdfObject
	RF             core.PdfObject
	Desc           core.PdfObject
	CI             core.PdfObject
	AFRelationship core.PdfObject
	primitive      core.PdfObject
}

// GetContainingPdfObject implements interface PdfModel.
func (f *PdfFilespec) GetContainingPdfObject() core.PdfObject { return f.primitive }

func (f *PdfFilespec) getDict() *core.PdfObjectDictionary {
	if indirect, ok := f.primitive.(*core.PdfIndirectObject); ok {
		dict, ok := indirect.PdfObject.(*core.PdfObjectDictionary)
		if !ok {
			return nil
		}
		return dict
	} else if dict, ok := f.primitive.(*core.PdfObjectDictionary); ok {
		return dict
	} else {
		common.Log.Debug("Trying to access Filespec dictionary of invalid object type (%T)", f.primitive)
		return nil
	}
}

func newPdfFilespecFromPdfObject(obj core.PdfObject) (*PdfFilespec, error) {
	if obj == nil {
		return nil, nil
	}
	return NewPdfFilespecFromObj(obj)
}

// NewPdfFilespecFromObj creates and returns a new PdfFilespec object.
func NewPdfFilespecFromObj(obj core.PdfObject) (*PdfFilespec, error) {
	filespec := &PdfFilespec{}
	var dict *core.PdfObjectDictionary
	if indirect, ok := core.GetIndirect(obj); ok {
		filespec.primitive = indirect
		dict2, ok := core.GetDict(indirect.PdfObject)
		if !ok {
			common.Log.Debug("Object not a dictionary type")
			return nil, core.ErrTypeError
		}
		dict = dict2
	} else if dict2, ok := core.GetDict(obj); ok {
		filespec.primitive = dict2
		dict = dict2
	} else {
		common.Log.Debug("Object type unexpected (%T)", obj)
		return nil, core.ErrTypeError
	}
	if dict == nil {
		common.Log.Debug("Dictionary missing")
		return nil, errors.New("dict missing")
	}
	if obj := dict.Get("Type"); obj != nil {
		name, ok := obj.(*core.PdfObjectName)
		if !ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("Incompatibility! Invalid type of Type (%T) - should be Name", obj)
			}
		} else {
			if *name != "Filespec" {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("Unsuspected Type != Filespec (%s)", *name)
				}
			}
		}
	}
	if fs := dict.Get("FS"); fs != nil {
		filespec.FS = fs
	}
	if f := dict.Get("F"); f != nil {
		filespec.F = f
	}
	if uf := dict.Get("UF"); uf != nil {
		filespec.UF = uf
	}
	if dos := dict.Get("DOS"); dos != nil {
		filespec.DOS = dos
	}
	if mac := dict.Get("Mac"); mac != nil {
		filespec.Mac = mac
	}
	if unix := dict.Get("Unix"); unix != nil {
		filespec.Unix = unix
	}
	if id := dict.Get("ID"); id != nil {
		filespec.ID = id
	}
	if v := dict.Get("V"); v != nil {
		filespec.V = v
	}
	if ef := dict.Get("EF"); ef != nil {
		filespec.EF = ef
	}
	if rf := dict.Get("RF"); rf != nil {
		filespec.RF = rf
	}
	if desc := dict.Get("Desc"); desc != nil {
		filespec.Desc = desc
	}
	if ci := dict.Get("CI"); ci != nil {
		filespec.CI = ci
	}
	if afRelationship := dict.Get("AFRelationship"); afRelationship != nil {
		filespec.AFRelationship = afRelationship
	}
	return filespec, nil
}

// NewPdfFileSpecFromEmbeddedFile construct a new PdfFileSpec that contains an embedded file.
func NewPdfFileSpecFromEmbeddedFile(file *EmbeddedFile) *PdfFilespec {
	filespec := &PdfFilespec{}
	filespec.primitive = core.MakeIndirectObject(core.MakeDict())
	filespec.Desc = core.MakeString(file.Description)
	filespec.EF = file.ToPdfObject()
	filespec.F = core.MakeString(file.Name)
	filespec.UF = core.MakeEncodedString(file.Name, true)
	s := "Unspecified"
	switch file.Relationship {
	case RelationshipSource:
		s = "Source"
	case RelationshipData:
		s = "Data"
	case RelationshipAlternative:
		s = "Alternative"
	case RelationshipSupplement:
		s = "Supplement"
	}
	filespec.AFRelationship = core.MakeName(s)
	return filespec
}

// ToPdfObject implements interface PdfModel.
func (f *PdfFilespec) ToPdfObject() core.PdfObject {
	dict := f.getDict()
	dict.Clear()
	dict.Set("Type", core.MakeName("Filespec"))
	dict.SetIfNotNil("FS", f.FS)
	dict.SetIfNotNil("F", f.F)
	dict.SetIfNotNil("UF", f.UF)
	dict.SetIfNotNil("DOS", f.DOS)
	dict.SetIfNotNil("Mac", f.Mac)
	dict.SetIfNotNil("Unix", f.Unix)
	dict.SetIfNotNil("ID", f.ID)
	dict.SetIfNotNil("V", f.V)
	dict.SetIfNotNil("EF", f.EF)
	dict.SetIfNotNil("RF", f.RF)
	dict.SetIfNotNil("Desc", f.Desc)
	dict.SetIfNotNil("CI", f.CI)
	dict.SetIfNotNil("AFRelationship", f.AFRelationship)
	return f.primitive
}

// NewPdfFilespec returns an initialized generic PDF filespec model.
func NewPdfFilespec() *PdfFilespec {
	filespec := &PdfFilespec{}
	filespec.primitive = core.MakeIndirectObject(core.MakeDict())
	return filespec
}
