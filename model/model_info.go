package model

import (
	"fmt"
	_ "image/gif"
	"strconv"
	"time"

	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/timeutils"
)

// PdfInfoTrapped specifies pdf trapped information.
type PdfInfoTrapped string

const (
	TrappedUnknown PdfInfoTrapped = "Unknown"
	TrappedTrue    PdfInfoTrapped = "True"
	TrappedFalse   PdfInfoTrapped = "False"
)

// PdfDate represents a date, which is a PDF string of the form:
// (D:YYYYMMDDHHmmSSOHH'mm)
type PdfDate struct {
	year            int64
	month           int64
	day             int64
	hour            int64
	minute          int64
	second          int64
	utOffsetSign    byte
	utOffsetHours   int64
	utOffsetMinutes int64
}

// ToGoTime returns the date in time.Time format.
func (d PdfDate) ToGoTime() time.Time {
	n := int(d.utOffsetHours*60*60 + d.utOffsetMinutes*60)
	switch d.utOffsetSign {
	case '-':
		n = -n
	case 'Z':
		n = 0
	}
	s := fmt.Sprintf("UTC%c%.2d%.2d", d.utOffsetSign, d.utOffsetHours, d.utOffsetMinutes)
	fixedZone := time.FixedZone(s, n)
	return time.Date(int(d.year), time.Month(d.month), int(d.day), int(d.hour), int(d.minute), int(d.second), 0, fixedZone)
}

// ToPdfObject converts date to a PDF string object.
func (d *PdfDate) ToPdfObject() core.PdfObject {
	s := fmt.Sprintf("D:%.4d%.2d%.2d%.2d%.2d%.2d%c%.2d'%.2d'", d.year, d.month, d.day, d.hour, d.minute, d.second, d.utOffsetSign, d.utOffsetHours, d.utOffsetMinutes)
	return core.MakeString(s)
}

// NewPdfDateFromTime will create a PdfDate based on the given time
func NewPdfDateFromTime(timeObj time.Time) (PdfDate, error) {
	format := timeObj.Format("-07:00")
	n, _ := strconv.ParseInt(format[1:3], 10, 32)
	count, _ := strconv.ParseInt(format[4:6], 10, 32)
	return PdfDate{year: int64(timeObj.Year()), month: int64(timeObj.Month()), day: int64(timeObj.Day()), hour: int64(timeObj.Hour()), minute: int64(timeObj.Minute()), second: int64(timeObj.Second()), utOffsetSign: format[0], utOffsetHours: n, utOffsetMinutes: count}, nil
}

// NewPdfDate returns a new PdfDate object from a PDF date string (see 7.9.4 Dates).
// format: "D: YYYYMMDDHHmmSSOHH'mm"
func NewPdfDate(dateStr string) (PdfDate, error) {
	pdfTime, err := timeutils.ParsePdfTime(dateStr)
	if err != nil {
		return PdfDate{}, err
	}
	return NewPdfDateFromTime(pdfTime)
}

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

var infoDictKeys = map[string]struct{}{"Title": {}, "Author": {}, "Subject": {}, "Keywords": {}, "Creator": {}, "Producer": {}, "Trapped": {}, "CreationDate": {}, "ModDate": {}}
