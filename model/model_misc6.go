package model

import (
	"bytes"
	"crypto/x509"
	"errors"
	"fmt"
	_ "image/gif"
	"sort"
	"strings"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core"
	"github.com/OpenMellon/openpdf/internal/transform"
	"golang.org/x/crypto/ocsp"
)

// GetContainingPdfObject implements interface PdfModel.
func (f *PdfFilespec) GetContainingPdfObject() core.PdfObject { return f.primitive }

// FileRelationship represents a attachment file relationship type.
type (
	FileRelationship int
	ocspKey          struct{ responseHash, certHash, issuerHash [32]byte }
)

func filterFieldsRecursive(fields *[]*PdfField, filter FieldFilterFunc, ok bool) []*PdfField {
	if fields == nil {
		return nil
	}
	fields2 := *fields
	if len(*fields) == 0 {
		return nil
	}
	fields3 := fields2[:0]
	if filter == nil {
		filter = func(*PdfField) bool { return true }
	}
	var fields4 []*PdfField
	for _, field := range fields2 {
		found := filter(field)
		if found {
			fields4 = append(fields4, field)
			if len(field.Kids) > 0 {
				fields4 = append(fields4, filterFieldsRecursive(&field.Kids, filter, ok)...)
			}
		}
		if !ok || !found || len(field.Kids) > 0 {
			fields3 = append(fields3, field)
		}
	}
	*fields = fields3
	return fields4
}

func flattenFieldTree(field *PdfField) []*PdfField {
	fields := []*PdfField{field}
	for _, kid := range field.Kids {
		fields = append(fields, flattenFieldTree(kid)...)
	}
	return fields
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

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func newPdfFilespecFromPdfObject(obj core.PdfObject) (*PdfFilespec, error) {
	if obj == nil {
		return nil, nil
	}
	return NewPdfFilespecFromObj(obj)
}

func findRevocationEntry(crls []crlEvidence, cert *x509.Certificate, t time.Time) *x509.RevocationListEntry {
	for _, crl := range crls {
		entryFor := crl.entryFor(cert)
		if entryFor == nil || entryFor.ReasonCode == crlReasonCertificateHold || entryFor.ReasonCode == crlReasonRemoveFromCRL {
			continue
		}
		if !entryFor.RevocationTime.After(t) {
			return entryFor
		}
	}
	return nil
}

func (h holdReport) precedes(thisUpdate time.Time, crl *parsedCRL) bool {
	if h.crl != nil && crl != nil {
		return crl.newerThan(h.crl)
	}
	return thisUpdate.After(h.thisUpdate)
}

func findHoldReport(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) (holdReport, bool) {
	var report holdReport
	ok := false
	fn := func(report2 holdReport) {
		if !ok || report.precedes(report2.thisUpdate, report2.crl) {
			report, ok = report2, true
		}
	}
	for _, answer := range answers {
		if answer.Status == ocsp.Revoked && answer.RevocationReason == ocsp.CertificateHold && !answer.RevokedAt.After(t) {
			fn(holdReport{revokedAt: answer.RevokedAt, thisUpdate: answer.ThisUpdate})
		}
	}
	for _, crl := range crls {
		if entryFor := crl.entryFor(cert); entryFor != nil && entryFor.ReasonCode == crlReasonCertificateHold && !entryFor.RevocationTime.After(t) {
			fn(holdReport{revokedAt: entryFor.RevocationTime, thisUpdate: crl.ThisUpdate, crl: crl.parsedCRL})
		}
	}
	return report, ok
}

// Hasher is the interface that wraps the basic Write method.
type Hasher interface {
	Write(data []byte) (n int, err error)
}

func findChainWithValidIntermediates(certsList [][]*x509.Certificate, fn func(*x509.Certificate) bool) []*x509.Certificate {
	for _, certs := range certsList {
		ok := true
		for i := 1; i < len(certs)-1; i++ {
			if !fn(certs[i]) {
				ok = false
				break
			}
		}
		if ok {
			return certs
		}
	}
	return nil
}

func (h holdReport) lifted(answers []ocspAnswer, crls []crlEvidence, cert *x509.Certificate, t time.Time) bool {
	fn := func(t2 time.Time) bool { return t.IsZero() || !t2.After(t) }
	for _, answer := range answers {
		if answer.Status == ocsp.Good && answer.authorised && fn(answer.ThisUpdate) && !answer.ThisUpdate.After(cert.NotAfter) && h.precedes(answer.ThisUpdate, nil) {
			return true
		}
	}
	for _, crl := range crls {
		if !fn(crl.ThisUpdate) || crl.ThisUpdate.After(cert.NotAfter) || !h.precedes(crl.ThisUpdate, crl.parsedCRL) {
			continue
		}
		entryFor := crl.entryFor(cert)
		if entryFor != nil && entryFor.ReasonCode == crlReasonRemoveFromCRL {
			return true
		}
		if entryFor == nil && !crl.isDelta() && crl.covers(cert) {
			return true
		}
	}
	return false
}

type holdReport struct {
	revokedAt  time.Time
	thisUpdate time.Time
	crl        *parsedCRL
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

func finalizeLinearizationDict(writer *PdfWriter, plan *linearizedPlan, inputs convergedPlanInputs) error {
	plan.linDict = buildLinearizationDict(linearizationDictObjNum2, plan.fileLength, plan.hintOffset, int64(inputs.hintLen), plan.firstPageEnd, inputs.layout.pageCount, inputs.xrefFirstEntryOffset, inputs.layout.firstPageObjNum)
	if len(plan.linDict) != inputs.linDictLen {
		return fmt.Errorf("plan: linearization dict size changed (placeholder=%d, final=%d)", inputs.linDictLen, len(plan.linDict))
	}
	if inputs.objStreams == nil {
		plan.firstXref = buildFirstXrefTable(writer, inputs.firstXrefObjs, inputs.objects, inputs.hintObjNum, plan.hintOffset, plan.mainXrefOffset, plan.linDictOffset, inputs.trailerSize)
		if len(plan.firstXref) != inputs.firstXrefLen {
			return fmt.Errorf("plan: first xref size changed (placeholder=%d, final=%d)", inputs.firstXrefLen, len(plan.firstXref))
		}
	}
	var buf bytes.Buffer
	for _, firstSection := range inputs.layout.firstSection {
		buf.Write(inputs.objects[firstSection].data)
	}
	plan.firstSection = buf.Bytes()
	if int64(len(plan.firstSection)) != plan.firstPageEnd-inputs.firstSectionStart {
		return fmt.Errorf("plan: first section byte count mismatch")
	}
	var buf2 bytes.Buffer
	for _, mainSection := range inputs.layout.mainSection {
		buf2.Write(inputs.objects[mainSection].data)
	}
	plan.mainSection = buf2.Bytes()
	if int64(len(plan.mainSection)) != plan.mainXrefOffset-inputs.mainSectionStart {
		return fmt.Errorf("plan: main section byte count mismatch")
	}
	if inputs.objStreams != nil {
		s := fmt.Sprintf("startxref\r\n%010d\r\n%%%%EOF\r\n", plan.firstXrefOffset)
		plan.mainXref = append(plan.mainXref, []byte(s)...)
	}
	return nil
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

func formBBox(form *XObjectForm) (*PdfRectangle, bool, error) {
	if arr, ok := form.BBox.(*core.PdfObjectArray); ok {
		rect, err := NewPdfRectangle(*arr)
		if err != nil {
			return nil, false, err
		}
		if arr, ok := form.Matrix.(*core.PdfObjectArray); ok {
			floats, err := arr.ToFloat64Array()
			if err != nil {
				return nil, false, err
			}
			identityMatrix := transform.IdentityMatrix()
			if len(floats) == 6 {
				identityMatrix = transform.NewMatrix(floats[0], floats[1], floats[2], floats[3], floats[4], floats[5])
			}
			rect.Transform(identityMatrix)
			return rect, true, nil
		}
		return rect, false, nil
	}
	return nil, false, errors.New("invalid appearance BBox type")
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

func firstPageHintOffset(layout *linearizationLayout, table map[int64]int64, n, count int64) (uint32, error) {
	if len(layout.firstShared) == 0 {
		return 0, nil
	}
	return adjustHintOffset(table[layout.firstShared[0]], n, count)
}

func fixFontWidthsRange(dict *core.PdfObjectDictionary) {
	widths, ok := core.GetArray(dict.Get("Widths"))
	firstChar, isFirstChar := core.GetIntVal(dict.Get("FirstChar"))
	lastChar, isLastChar := core.GetIntVal(dict.Get("LastChar"))
	if ok && isFirstChar && isLastChar {
		widthsLen := widths.Len()
		if widthsLen != lastChar-firstChar+1 {
			common.Log.Debug("Unexpected Widths length: %v, LastChar: %v", widthsLen, lastChar)
			num := core.PdfObjectInteger(firstChar + widthsLen - 1)
			dict.Set("LastChar", &num)
		}
	}
}

// NewPdfFilespec returns an initialized generic PDF filespec model.
func NewPdfFilespec() *PdfFilespec {
	filespec := &PdfFilespec{}
	filespec.primitive = core.MakeIndirectObject(core.MakeDict())
	return filespec
}

func fillSignatureCoverage(parser *core.PdfParser, nums []int64, contents *core.PdfObjectString, result *SignatureValidationResult) {
	result.byteRangeChecked = true
	n := nums[len(nums)-2] + nums[len(nums)-1]
	result.IsCoveringEntireFile = n == parser.Size()
	result.ContentsGapOnly = isContentsGapOnly(parser, nums, contents)
	revisionEndOffsets, err := parser.RevisionEndOffsets()
	if err != nil || len(revisionEndOffsets) == 0 {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile
		return
	}
	search := sort.Search(len(revisionEndOffsets), func(count int) bool { return revisionEndOffsets[count] >= n })
	if search == len(revisionEndOffsets) {
		result.IsCoveringEntireRevision = result.IsCoveringEntireFile || isWhitespaceGap(parser, revisionEndOffsets[len(revisionEndOffsets)-1], n)
		return
	}
	if n == revisionEndOffsets[search] {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if isWhitespaceGap(parser, n, revisionEndOffsets[search]) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - 1 - search
		return
	}
	if search > 0 && isWhitespaceGap(parser, revisionEndOffsets[search-1], n) {
		result.IsCoveringEntireRevision = true
		result.RevisionsAfterSignature = len(revisionEndOffsets) - search
	}
}
