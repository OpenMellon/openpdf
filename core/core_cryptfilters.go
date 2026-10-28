package core

import (
	"crypto/md5"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/OpenMellon/openpdf/common"
	"github.com/OpenMellon/openpdf/core/security"
	"github.com/OpenMellon/openpdf/core/security/crypt"
)

func parseCryptFilterDict(filterDict *crypt.FilterDict, dict *PdfObjectDictionary) error {
	if typeName, ok := dict.Get("Type").(*PdfObjectName); ok {
		if typeStr := string(*typeName); typeStr != "CryptFilter" {
			common.Log.Debug("Invalid CF dict type: %s (should be CryptFilter)", typeStr)
		}
	}
	cfm, ok := dict.Get("CFM").(*PdfObjectName)
	if !ok {
		return fmt.Errorf("unsupported crypt filter (None)")
	}
	filterDict.CFM = string(*cfm)
	if authEvent, isName := dict.Get("AuthEvent").(*PdfObjectName); isName {
		filterDict.AuthEvent = security.AuthEvent(*authEvent)
	} else {
		filterDict.AuthEvent = security.EventDocOpen
	}
	if length, isNum := dict.Get("Length").(*PdfObjectInteger); isNum {
		filterDict.Length = int(*length)
	}
	return nil
}

const stdCryptFilterName = "StdCF"

func defaultCryptFilters(length int) cryptFilters {
	return cryptFilters{stdCryptFilterName: crypt.NewFilterV2(length)}
}

func (crypt *PdfCrypt) makeKey(filterName string, objNum, genNum uint32, fileKey []byte) ([]byte, error) {
	filter, ok := crypt.cryptFilters[filterName]
	if !ok {
		return nil, fmt.Errorf("unknown crypt filter (%s)", filterName)
	}
	return filter.MakeKey(objNum, genNum, fileKey)
}

func (crypt *PdfCrypt) cryptFilterDict(encryptDict *PdfObjectDictionary, filterName string) *PdfObjectDictionary {
	if filterName == "" {
		return nil
	}
	traceDirect := func(obj PdfObject) PdfObject {
		obj = TraceToDirectObject(obj)
		if ref, ok := obj.(*PdfObjectReference); ok && crypt.parser != nil {
			if byReference, err := crypt.parser.LookupByReference(*ref); err == nil {
				obj = TraceToDirectObject(byReference)
			}
		}
		return obj
	}
	cfDict, ok := traceDirect(encryptDict.Get("CF")).(*PdfObjectDictionary)
	if !ok {
		return nil
	}
	dict, _ := traceDirect(cfDict.Get(PdfObjectName(filterName))).(*PdfObjectDictionary)
	return dict
}

func parseStdEncryptDict(stdEncrypt *security.StdEncryptDict, ed *PdfObjectDictionary) error {
	R, ok := ed.Get("R").(*PdfObjectInteger)
	if !ok {
		return errors.New("encrypt dictionary missing R")
	}
	if *R < 2 || *R > 6 {
		return fmt.Errorf("invalid R (%d)", *R)
	}
	stdEncrypt.R = int(*R)
	O, ok := ed.GetString("O")
	if !ok {
		return errors.New("encrypt dictionary missing O")
	}
	if stdEncrypt.R == 5 || stdEncrypt.R == 6 {
		if len(O) < 48 {
			return fmt.Errorf("Length(O) < 48 (%d)", len(O))
		}
	} else if len(O) != 32 {
		return fmt.Errorf("Length(O) != 32 (%d)", len(O))
	}
	stdEncrypt.O = []byte(O)
	U, ok := ed.GetString("U")
	if !ok {
		return errors.New("encrypt dictionary missing U")
	}
	if stdEncrypt.R == 5 || stdEncrypt.R == 6 {
		if len(U) < 48 {
			return fmt.Errorf("Length(U) < 48 (%d)", len(U))
		}
	} else if len(U) != 32 {
		common.Log.Debug("Warning: Length(U) != 32 (%d)", len(U))
	}
	stdEncrypt.U = []byte(U)
	if stdEncrypt.R >= 5 {
		OE, ok2 := ed.GetString("OE")
		if !ok2 {
			return errors.New("encrypt dictionary missing OE")
		} else if len(OE) != 32 {
			return fmt.Errorf("Length(OE) != 32 (%d)", len(OE))
		}
		stdEncrypt.OE = []byte(OE)
		UE, ok2 := ed.GetString("UE")
		if !ok2 {
			return errors.New("encrypt dictionary missing UE")
		} else if len(UE) != 32 {
			return fmt.Errorf("Length(UE) != 32 (%d)", len(UE))
		}
		stdEncrypt.UE = []byte(UE)
	}
	P, ok := ed.Get("P").(*PdfObjectInteger)
	if !ok {
		return errors.New("encrypt dictionary missing permissions attr")
	}
	stdEncrypt.P = security.Permissions(*P)
	if stdEncrypt.R == 6 {
		Perms, ok2 := ed.GetString("Perms")
		if !ok2 {
			return errors.New("encrypt dictionary missing Perms")
		} else if len(Perms) != 16 {
			return fmt.Errorf("Length(Perms) != 16 (%d)", len(Perms))
		}
		stdEncrypt.Perms = []byte(Perms)
	}
	if encryptMetadata, isBoolObj := ed.Get("EncryptMetadata").(*PdfObjectBool); isBoolObj {
		stdEncrypt.EncryptMetadata = bool(*encryptMetadata)
	} else {
		stdEncrypt.EncryptMetadata = true
	}
	return nil
}

func (crypt *PdfCrypt) newEncryptDict() *PdfObjectDictionary {
	dict := MakeDict()
	dict.Set("Filter", MakeName("Standard"))
	dict.Set("V", MakeInteger(int64(crypt.encrypt.V)))
	dict.Set("Length", MakeInteger(int64(crypt.encrypt.Length)))
	return dict
}

type cryptFilters map[string]crypt.Filter

func (crypt *PdfCrypt) saveCryptFilters(dict *PdfObjectDictionary) error {
	if crypt.encrypt.V < 4 {
		return errors.New("can only be used with V>=4")
	}
	cfDict := MakeDict()
	dict.Set("CF", cfDict)
	for sKey, cryptFilter := range crypt.cryptFilters {
		if sKey == "Identity" {
			continue
		}
		cryptFilterDict := makeCryptFilterDict(cryptFilter, "")
		cfDict.Set(PdfObjectName(sKey), cryptFilterDict)
	}
	dict.Set("StrF", MakeName(crypt.stringFilter))
	dict.Set("StmF", MakeName(crypt.streamFilter))
	return nil
}

func (crypt *PdfCrypt) securityHandler() security.StdHandler {
	if crypt.stdEncrypt.R >= 5 {
		return security.NewHandlerR6()
	}
	return security.NewHandlerR4(crypt.id0, crypt.encrypt.Length)
}

// EncryptInfo contains an information generated by the document encrypter.
type EncryptInfo struct {
	Version

	// Encrypt is an encryption dictionary that contains all necessary parameters.
	// It should be stored in all copies of the document trailer.
	Encrypt *PdfObjectDictionary

	// ID0 and ID1 are IDs used in the trailer. Older algorithms such as RC4 uses them for encryption.
	ID0, ID1 string
}

const defaultCryptFilterKey = "DefaultCryptFilter"

func generateFileIDs() (id0, id1 string) {
	data := md5.Sum([]byte(time.Now().Format(time.RFC850)))
	id0 = string(data[:])
	buf := make([]byte, 100)
	rand.Read(buf)
	data = md5.Sum(buf)
	id1 = string(data[:])
	if common.Log.IsLogLevel(common.LogLevelTrace) {
		common.Log.Trace("Random b: % x", buf)
		common.Log.Trace("Gen Id 0: % x", id0)
	}
	return id0, id1
}

type encryptDict struct {
	Filter    string
	V         int
	SubFilter string
	Length    int
	StmF      string
	StrF      string
	EFF       string
	CF        map[string]crypt.FilterDict
}

func (c *PdfCrypt) loadCryptFilters(ed *PdfObjectDictionary) error {
	c.cryptFilters = cryptFilters{}
	cf := ed.Get("CF")
	cf = TraceToDirectObject(cf)
	if ref, ok := cf.(*PdfObjectReference); ok {
		byReference, err := c.parser.LookupByReference(*ref)
		if err != nil {
			common.Log.Debug("Error looking up CF reference")
			return err
		}
		cf = TraceToDirectObject(byReference)
	}
	dict, ok := cf.(*PdfObjectDictionary)
	if !ok {
		common.Log.Debug("Invalid CF, type: %T", cf)
		return errors.New("invalid CF")
	}
	for _, key := range dict.Keys() {
		obj := dict.Get(key)
		if ref, isRef := obj.(*PdfObjectReference); isRef {
			byReference, err := c.parser.LookupByReference(*ref)
			if err != nil {
				common.Log.Debug("Error lookup up dictionary reference")
				return err
			}
			obj = TraceToDirectObject(byReference)
		}
		cfDict, isDict := obj.(*PdfObjectDictionary)
		if !isDict {
			return fmt.Errorf("invalid dict in CF (name %s) - not a dictionary but %T", key, obj)
		}
		if key == "Identity" {
			common.Log.Debug("ERROR - Cannot overwrite the identity filter - Trying next")
			continue
		}
		var filterDict crypt.FilterDict
		if err := parseCryptFilterDict(&filterDict, cfDict); err != nil {
			return err
		}
		filter, err := crypt.NewFilter(filterDict)
		if err != nil {
			return err
		}
		c.cryptFilters[string(key)] = filter
	}
	c.cryptFilters["Identity"] = crypt.NewIdentity()
	c.stringFilter = "Identity"
	if strF, isName := ed.Get("StrF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*strF)]; !ok2 {
			return fmt.Errorf("crypt filter for StrF not specified in CF dictionary (%s)", *strF)
		}
		c.stringFilter = string(*strF)
	}
	c.streamFilter = "Identity"
	if stmF, isName := ed.Get("StmF").(*PdfObjectName); isName {
		if _, ok2 := c.cryptFilters[string(*stmF)]; !ok2 {
			return fmt.Errorf("crypt filter for StmF not specified in CF dictionary (%s)", *stmF)
		}
		c.streamFilter = string(*stmF)
	}
	return nil
}

func (crypt *PdfCrypt) generateParams(userPass, ownerPass []byte) error {
	stdHandler := crypt.securityHandler()
	data, err := stdHandler.GenerateParams(&crypt.stdEncrypt, ownerPass, userPass)
	if err != nil {
		return err
	}
	crypt.encryptionKey = data
	return nil
}

func writeStdEncryptDict(stdDict *security.StdEncryptDict, dict *PdfObjectDictionary) {
	dict.Set("R", MakeInteger(int64(stdDict.R)))
	dict.Set("P", MakeInteger(int64(int32(stdDict.P))))
	dict.Set("O", MakeStringFromBytes(stdDict.O))
	dict.Set("U", MakeStringFromBytes(stdDict.U))
	if stdDict.R >= 5 {
		dict.Set("OE", MakeStringFromBytes(stdDict.OE))
		dict.Set("UE", MakeStringFromBytes(stdDict.UE))
		dict.Set("EncryptMetadata", MakeBool(stdDict.EncryptMetadata))
		if stdDict.R > 5 {
			dict.Set("Perms", MakeStringFromBytes(stdDict.Perms))
		}
	}
}

func makeCryptFilterDict(filter crypt.Filter, event security.AuthEvent) *PdfObjectDictionary {
	if event == "" {
		event = security.EventDocOpen
	}
	dict := MakeDict()
	dict.Set("Type", MakeName("CryptFilter"))
	dict.Set("AuthEvent", MakeName(string(event)))
	dict.Set("CFM", MakeName(filter.Name()))
	dict.Set("Length", MakeInteger(int64(filter.KeyLength())))
	return dict
}

var encryptDictKeys = []PdfObjectName{"V", "R", "O", "U", "P"}
