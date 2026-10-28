package core

import (
	"crypto"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"golang.org/x/sync/singleflight"
)

// PdfParser parses a PDF file and provides access to the object structure of the PDF.
type PdfParser struct {
	version              Version
	reader               io.ReaderAt
	fileSize             int64
	xrefs                atomic.Pointer[XrefTable]
	xrefOffset           int64
	xrefType             *xrefType
	objStreams           objectStreams
	trailer              *PdfObjectDictionary
	crypter              *PdfCrypt
	encryptObj           *PdfIndirectObject
	repairOnce           sync.Once
	repair               repairResult
	repairedXrefs        atomic.Pointer[XrefTable]
	pendingXrefRepair    bool
	objCache             sync.Map
	lookupGroup          singleflight.Group
	objStreamMu          sync.Mutex
	metadata             ParserMetadata
	compliance           bool
	xrefOffsets          []int64
	numRevisions         int
	startxrefOffset      int64
	xrefSectionEnds      []int64
	linearized           bool
	linearizedFileLength int64
	linearizationDict    *PdfObjectDictionary
	linearization        *LinearizationInfo
	linearizationObjNum  int
	firstTrailerOnce     sync.Once
	firstTrailer         *PdfObjectDictionary
	firstTrailerErr      error
	firstXrefOffset      int64
	revisionParsers      map[*PdfParser]*PdfParser
	revisionEndsOnce     sync.Once
	revisionEnds         []int64
	revisionEndsErr      error
	revisionEndsValid    bool
	revisions            []*PdfParser

	// Opts holds different parsing options.
	Opts         *ParserOpts
	decodedTotal *atomic.Int64
}

// LoadCachedObject returns the cached PdfObject for the given object number,
// if any.
func (parser *PdfParser) LoadCachedObject(objNumber int) (PdfObject, bool) {
	cached, ok := parser.objCache.Load(objNumber)
	if !ok {
		return nil, false
	}
	return cached.(PdfObject), true
}

// GetReaderAt returns the source the parser reads the document through. For
// a parser returned by GetPreviousRevisionParser it is the section of the
// parent document holding that revision.
func (parser *PdfParser) GetReaderAt() io.ReaderAt { return parser.reader }

// Size returns the effective byte length of the PDF document. Offsets used
// with ReadBytesAt are relative to the same coordinate space: when garbage
// bytes precede the %PDF header, the parser reads through an offset-adjusted
// source and Size reflects the effective extent rather than the raw source
// length. Note that a signature ByteRange written by an external tool against
// the raw (prefixed) bytes is shifted relative to this space — such
// signatures fail digest verification for the same reason.
func (parser *PdfParser) Size() int64 { return parser.fileSize }

func (parser *PdfParser) clearObjCache() {
	parser.objCache.Clear()
	parser.objStreamMu.Lock()
	parser.objStreams = make(objectStreams)
	parser.objStreamMu.Unlock()
}

// GetUpdatedObjects returns pdf objects which were updated from the specific version (from prevParser).
func (parser *PdfParser) GetUpdatedObjects(prevParser *PdfParser) (map[int64]PdfObject, error) {
	if prevParser == nil {
		return nil, errors.New("previous parser can't be null")
	}
	numbersOfUpdatedObjects, err := parser.getNumbersOfUpdatedObjects(prevParser)
	if err != nil {
		return nil, err
	}
	m := make(map[int64]PdfObject)
	for _, num := range numbersOfUpdatedObjects {
		if byNumber, err2 := parser.LookupByNumber(num); err2 == nil {
			m[int64(num)] = byNumber
		} else {
			return nil, err2
		}
	}
	return m, nil
}

// Inspect analyzes the document object structure. Returns a map of object types (by name) with the instance count
// as value.
func (parser *PdfParser) Inspect() (map[string]int, error) { return parser.inspect() }

func (parser *PdfParser) getNumbersOfUpdatedObjects(prevParser *PdfParser) ([]int, error) {
	if prevParser == nil {
		return nil, errors.New("previous parser can't be null")
	}
	fileSize := prevParser.fileSize
	nums := make([]int, 0)
	m := make(map[int]any)
	offsets := make(map[int]int64)
	for i, xrefObj := range parser.currentXrefs().ObjectMap {
		if xrefObj.Offset == 0 {
			if xrefObj.OsObjNumber != 0 {
				if prev, ok := parser.currentXrefs().ObjectMap[xrefObj.OsObjNumber]; ok {
					m[xrefObj.OsObjNumber] = struct{}{}
					offsets[i] = prev.Offset
				} else {
					return nil, errors.New("undefined xref table")
				}
			}
		} else {
			offsets[i] = xrefObj.Offset
		}
	}
	for i, n := range offsets {
		if _, ok := m[i]; ok {
			continue
		}
		if n >= fileSize {
			nums = append(nums, i)
		}
	}
	return nums, nil
}

// GetObjectNums returns a sorted list of object numbers of the PDF objects in the file.
func (parser *PdfParser) GetObjectNums() []int {
	var nums []int
	for _, xrefObj := range parser.currentXrefs().ObjectMap {
		nums = append(nums, xrefObj.ObjectNumber)
	}
	sort.Ints(nums)
	return nums
}

// StoreCachedObject stores obj in the parser's object cache under objNumber.
func (parser *PdfParser) StoreCachedObject(objNumber int, obj PdfObject) {
	parser.objCache.Store(objNumber, obj)
}

// ReadBytesAt reads length bytes at the given offset within the PDF. It
// returns an error on a short read (fewer than length bytes available), so
// callers never receive silently zero-padded data.
func (parser *PdfParser) ReadBytesAt(offset, length int64) ([]byte, error) {
	data := make([]byte, length)
	at, err := parser.reader.ReadAt(data, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if int64(at) != length {
		return nil, fmt.Errorf("core.ReadBytesAt: short read at offset %d: got %d of %d bytes", offset, at, length)
	}
	return data, nil
}

// RangeCachedObjects iterates over every cached object, invoking fn for each
// (object number, object) pair. Iteration stops early if fn returns false.
// Iteration order is unspecified.
func (parser *PdfParser) RangeCachedObjects(fn func(objNum int, obj PdfObject) bool) {
	parser.objCache.Range(func(k, v any) bool { return fn(k.(int), v.(PdfObject)) })
}

func (parser *PdfParser) inspect() (map[string]int, error) {
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("--------INSPECT ----------")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Xref table:")
	}
	m := map[string]int{}
	count := 0
	failed := 0
	var nums []int
	for i := range parser.currentXrefs().ObjectMap {
		nums = append(nums, i)
	}
	sort.Ints(nums)
	total := 0
	for _, num := range nums {
		xrefObj := parser.currentXrefs().ObjectMap[num]
		if xrefObj.ObjectNumber == 0 {
			continue
		}
		count++
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("==========")
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Looking up object number: %d", xrefObj.ObjectNumber)
		}
		byNumber, err := parser.LookupByNumber(xrefObj.ObjectNumber)
		if err != nil {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("ERROR: Fail to lookup obj %d (%s)", xrefObj.ObjectNumber, err)
			}
			failed++
			continue
		}
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("obj: %s", byNumber)
		}
		indirect, ok := byNumber.(*PdfIndirectObject)
		if ok {
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("IND OOBJ %d: %s", xrefObj.ObjectNumber, indirect)
			}
			dict, isDict := indirect.PdfObject.(*PdfObjectDictionary)
			if isDict {
				if typeName, isName := dict.Get("Type").(*PdfObjectName); isName {
					typeStr := string(*typeName)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("---> Obj type: %s", typeStr)
					}
					_, ok2 := m[typeStr]
					if ok2 {
						m[typeStr]++
					} else {
						m[typeStr] = 1
					}
				} else if subtype, ok2 := dict.Get("Subtype").(*PdfObjectName); ok2 {
					subtypeStr := string(*subtype)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("---> Obj subtype: %s", subtypeStr)
					}
					_, ok3 := m[subtypeStr]
					if ok3 {
						m[subtypeStr]++
					} else {
						m[subtypeStr] = 1
					}
				}
				if sName, isName := dict.Get("S").(*PdfObjectName); isName && *sName == "JavaScript" {
					_, ok2 := m["JavaScript"]
					if ok2 {
						m["JavaScript"]++
					} else {
						m["JavaScript"] = 1
					}
				}
			}
		} else if stream, isStream := byNumber.(*PdfObjectStream); isStream {
			if typeName, isName := stream.Get("Type").(*PdfObjectName); isName {
				if common.Log.IsLogLevel(common.LogLevelTrace) {
					common.Log.Trace("--> Stream object type: %s", *typeName)
				}
				typeStr := string(*typeName)
				m[typeStr]++
			}
		} else {
			dict, isDict := byNumber.(*PdfObjectDictionary)
			if isDict {
				typeName, isName := dict.Get("Type").(*PdfObjectName)
				if isName {
					typeStr := string(*typeName)
					if common.Log.IsLogLevel(common.LogLevelTrace) {
						common.Log.Trace("--- obj type %s", typeStr)
					}
					m[typeStr]++
				}
			}
			if common.Log.IsLogLevel(common.LogLevelTrace) {
				common.Log.Trace("DIRECT OBJ %d: %s", xrefObj.ObjectNumber, byNumber)
			}
		}
		total++
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("--------EOF INSPECT ----------")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("=======")
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Object count: %d", count)
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Failed lookup: %d", failed)
	}
	for sKey, count2 := range m {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("%s: %d", sKey, count2)
		}
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("=======")
	}
	if len(parser.currentXrefs().ObjectMap) < 1 {
		common.Log.Debug("ERROR: This document is invalid (xref table missing!)")
		return nil, fmt.Errorf("invalid document (xref table missing)")
	}
	fontCount, ok := m["Font"]
	if !ok || fontCount < 2 {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("This document is probably scanned!")
		}
	} else {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("This document is valid for extraction!")
		}
	}
	return m, nil
}

func (parser *PdfParser) afterAuthentication() error {
	parser.clearObjCache()
	var err error
	if parser.pendingXrefRepair {
		parser.pendingXrefRepair = false
		err = parser.checkAndRepairXrefTable(parser.trailer)
	}
	if parser.encryptObj != nil {
		parser.StoreCachedObject(int(parser.encryptObj.ObjectNumber), parser.encryptObj)
	}
	return err
}

// IsEncrypted checks if the document is encrypted. A bool flag is returned indicating the result.
// First time when called, will check if the Encrypt dictionary is accessible through the trailer dictionary.
// If encrypted, prepares a crypt datastructure which can be used to authenticate and decrypt the document.
// On failure, an error is returned.
func (parser *PdfParser) IsEncrypted() (bool, error) {
	if parser.crypter != nil {
		return true, nil
	} else if parser.trailer == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Checking encryption dictionary!")
	}
	encrypt := parser.trailer.Get("Encrypt")
	if encrypt == nil {
		return false, nil
	}
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Is encrypted!")
	}
	var dict *PdfObjectDictionary
	switch v := encrypt.(type) {
	case *PdfObjectDictionary:
		dict = v
	case *PdfObjectReference:
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("0: Look up ref %q", v)
		}
		byReference, err := parser.LookupByReference(*v)
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("1: %q", byReference)
		}
		if err != nil {
			return false, err
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			common.Log.Debug("Encryption object not an indirect object")
			return false, errors.New("type check error")
		}
		indirectDict, ok := indirect.PdfObject.(*PdfObjectDictionary)
		parser.encryptObj = indirect
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("2: %q", indirectDict)
		}
		if !ok {
			return false, errors.New("trailer Encrypt object non dictionary")
		}
		dict = indirectDict
	case *PdfObjectNull:
		common.Log.Debug("Encrypt is a null object. File should not be encrypted.")
		return false, nil
	default:
		return false, fmt.Errorf("unsupported type: %T", v)
	}
	crypt, err := PdfCryptNewDecrypt(parser, dict, parser.trailer)
	if err != nil {
		return false, err
	}
	for _, key := range []string{"Encrypt"} {
		obj := parser.trailer.Get(PdfObjectName(key))
		if obj == nil {
			continue
		}
		switch v := obj.(type) {
		case *PdfObjectReference:
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		case *PdfIndirectObject:
			crypt.decryptedObjects[v] = true
			crypt.encryptedObjNums[int(v.ObjectNumber)] = struct{}{}
		}
	}
	parser.crypter = crypt
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Crypter object %b", crypt)
	}
	return true, nil
}

// GetCrypter returns the PdfCrypt instance which has information about the PDFs encryption.
func (parser *PdfParser) GetCrypter() *PdfCrypt { return parser.crypter }

// IsAuthenticated returns true if the PDF has already been authenticated for accessing.
func (parser *PdfParser) IsAuthenticated() bool { return parser.crypter.authenticated }

// GetEncryptObj returns the PdfIndirectObject which has information about the PDFs encryption details.
func (parser *PdfParser) GetEncryptObj() *PdfIndirectObject { return parser.encryptObj }

func (parser *PdfParser) trailerHasEncrypt(trailer *PdfObjectDictionary) bool {
	if trailer == nil {
		return false
	}
	switch v := trailer.Get("Encrypt").(type) {
	case *PdfObjectDictionary:
		return true
	case *PdfObjectReference:
		byReference, err := parser.LookupByReference(*v)
		if err != nil {
			return false
		}
		indirect, ok := byReference.(*PdfIndirectObject)
		if !ok {
			return false
		}
		_, isDict := indirect.PdfObject.(*PdfObjectDictionary)
		return isDict
	}
	return false
}

// Decrypt attempts to decrypt the PDF file with a specified password.  Also tries to
// decrypt with an empty password.  Returns true if successful, false otherwise.
// An error is returned when there is a problem with decrypting.
//
// Decrypt must not run concurrently with object lookups: on success it
// clears the object cache, and a lookup in flight during authentication
// could re-populate it with a still-encrypted object.
//
// With ParserOpts.AutoRepairXrefTable, the deferred xref table health check
// runs on the first successful authentication. If the table then cannot be
// rebuilt, the password is reported as accepted and an error is returned.
func (parser *PdfParser) Decrypt(password []byte) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticate(password)
	if err != nil {
		return false, err
	}
	if !ok {
		ok, err = parser.crypter.authenticate([]byte(""))
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, err
}

// CheckAccessRights checks access rights and permissions for a specified password. If either user/owner password is
// specified, full rights are granted, otherwise the access rights are specified by the Permissions flag.
//
// The bool flag indicates that the user can access and view the file.
// The AccessPermissions shows what access the user has for editing etc.
// An error is returned if there was a problem performing the authentication.
func (parser *PdfParser) CheckAccessRights(password []byte) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRights(password)
}

// CheckAccessRightsWithCertificate reports whether the certificate can open a
// public-key encrypted document and the permissions its recipient entry
// grants, without authenticating the parser. An unencrypted document grants
// full permissions; a document using the standard security handler returns
// ErrPasswordRequired.
func (parser *PdfParser) CheckAccessRightsWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, security.Permissions, error) {
	if parser.crypter == nil {
		return true, security.PermOwner, nil
	}
	return parser.crypter.checkAccessRightsCertificate(cert, key)
}

// DecryptWithCertificate authenticates a document encrypted with a public-key
// security handler (/Filter /Adobe.PubSec) using a recipient's certificate and
// private key, and builds the document key. The key must be an
// *rsa.PrivateKey: RSA key transport is the only recipient type supported.
// It returns security.ErrNotRecipient when the certificate is not among the
// recipients, security.ErrRecipientNotOpened when the recipient's entry does
// not open with the key, and ErrPasswordRequired when the document uses the
// standard security handler. The deferred xref health check behaves as for
// Decrypt.
func (parser *PdfParser) DecryptWithCertificate(cert *x509.Certificate, key crypto.PrivateKey) (bool, error) {
	if parser.crypter == nil {
		return false, errors.New("check encryption first")
	}
	ok, err := parser.crypter.authenticateCertificate(cert, key)
	if err != nil {
		return false, err
	}
	if ok {
		if err2 := parser.afterAuthentication(); err2 != nil {
			return true, err2
		}
	}
	return ok, nil
}
