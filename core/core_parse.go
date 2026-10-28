package core

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strconv"

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

func parsePubSecRecipients(dict *PdfObjectDictionary) ([][]byte, error) {
	recipients, ok := GetArray(dict.Get("Recipients"))
	if !ok || recipients.Len() == 0 {
		return nil, errors.New("public-key encrypt dictionary missing Recipients")
	}
	datas := make([][]byte, 0, recipients.Len())
	for i, elem := range recipients.Elements() {
		stringBytes, hasStringBytes := GetStringBytes(elem)
		if !hasStringBytes || len(stringBytes) == 0 {
			return nil, fmt.Errorf("invalid Recipients entry %d", i)
		}
		datas = append(datas, stringBytes)
	}
	return datas, nil
}

func parsePubSecEncryptDict(crypt *PdfCrypt, ed *PdfObjectDictionary) (*security.PubSecEncryptDict, error) {
	pubSecDict := &security.PubSecEncryptDict{SubFilter: crypt.encrypt.SubFilter}
	switch pubSecDict.SubFilter {
	case security.PubSecSubFilterS4, security.PubSecSubFilterS5:
	case "":
		return nil, errors.New("public-key encrypt dictionary missing SubFilter")
	default:
		return nil, fmt.Errorf("unsupported public-key SubFilter (%s)", pubSecDict.SubFilter)
	}
	switch crypt.encrypt.V {
	case 1, 2, 4, 5:
	default:
		return nil, fmt.Errorf("unsupported V (%d) for public-key encryption", crypt.encrypt.V)
	}
	dict := ed
	crypt.recipientFilter = ""
	if crypt.encrypt.V >= 4 {
		findRecipientFilter := func(filterNames []string) bool {
			for _, name := range filterNames {
				if name == "" || name == "Identity" {
					continue
				}
				if filterDict := crypt.cryptFilterDict(ed, name); filterDict != nil && filterDict.Get("Recipients") != nil {
					dict = filterDict
					crypt.recipientFilter = name
					return true
				}
			}
			return false
		}
		strs := []string{crypt.streamFilter, crypt.stringFilter}
		if eff, ok := GetName(ed.Get("EFF")); ok {
			strs = append(strs, string(*eff))
		}
		if !findRecipientFilter(strs) && ed.Get("Recipients") == nil {
			var sortedKeys []string
			for sKey := range crypt.cryptFilters {
				sortedKeys = append(sortedKeys, sKey)
			}
			sort.Strings(sortedKeys)
			findRecipientFilter(sortedKeys)
		}
	}
	var err error
	pubSecDict.Recipients, err = parsePubSecRecipients(dict)
	if err != nil {
		return nil, err
	}
	if crypt.encrypt.V >= 4 {
		recipientFilter := crypt.recipientFilter
		if recipientFilter == "" {
			recipientFilter = "top-level /Recipients"
		}
		for sKey := range crypt.cryptFilters {
			if sKey == "Identity" || sKey == crypt.recipientFilter {
				continue
			}
			filterDict := crypt.cryptFilterDict(ed, sKey)
			if filterDict == nil || filterDict.Get("Recipients") == nil {
				continue
			}
			pubSecRecipients, err2 := parsePubSecRecipients(filterDict)
			if err2 != nil {
				return nil, err2
			}
			if !slices.EqualFunc(pubSecDict.Recipients, pubSecRecipients, bytes.Equal) {
				return nil, fmt.Errorf("unsupported public-key encryption: %s and crypt filter %s have different recipient lists", recipientFilter, sKey)
			}
		}
	}
	pubSecDict.EncryptMetadata = true
	if encryptMetadata, ok := GetBool(dict.Get("EncryptMetadata")); ok {
		pubSecDict.EncryptMetadata = bool(*encryptMetadata)
	} else if boolObj, hasEncryptMetadata := GetBool(ed.Get("EncryptMetadata")); hasEncryptMetadata && dict != ed {
		pubSecDict.EncryptMetadata = bool(*boolObj)
	}
	return pubSecDict, nil
}

func parseObjSignature(line string) (int, int, error) {
	stringSubmatch := objHeaderRegexp.FindStringSubmatch(line)
	if len(stringSubmatch) < 3 {
		return 0, 0, errors.New("unable to detect indirect object signature")
	}
	objNum, _ := strconv.Atoi(stringSubmatch[1])
	genNum, _ := strconv.Atoi(stringSubmatch[2])
	return objNum, genNum, nil
}

func parseReferenceText(text string) (PdfObjectReference, error) {
	reference := PdfObjectReference{}
	matches := referencePattern.FindStringSubmatch(text)
	if len(matches) < 3 {
		common.Log.Debug("Error parsing reference")
		return reference, errors.New("unable to parse reference")
	}
	objectNumber, _ := strconv.Atoi(matches[1])
	generationNumber, _ := strconv.Atoi(matches[2])
	reference.ObjectNumber = int64(objectNumber)
	reference.GenerationNumber = int64(generationNumber)
	return reference, nil
}

// ParseNumber parses a numeric objects from a buffered stream.
// Section 7.3.3.
// Integer or Float.
//
// An integer shall be written as one or more decimal digits optionally
// preceded by a sign. The value shall be interpreted as a signed
// decimal integer and shall be converted to an integer object.
//
// A real value shall be written as one or more decimal digits with an
// optional sign and a leading, trailing, or embedded PERIOD (2Eh)
// (decimal point). The value shall be interpreted as a real number
// and shall be converted to a real object.
//
// Regarding exponential numbers: 7.3.3 Numeric Objects:
// A conforming writer shall not use the PostScript syntax for numbers
// with non-decimal radices (such as 16#FFFE) or in exponential format
// (such as 6.02E23).
// Nonetheless, we sometimes get numbers with exponential format, so
// we will support it in the reader (no confusion with other types, so
// no compromise).
func ParseNumber(buf *bufio.Reader) (PdfObject, error) {
	ok := false
	ok2 := true
	var numBuf bytes.Buffer
	for {
		if common.Log.IsLogLevel(common.LogLevelTrace) {
			common.Log.Trace("Parsing number \"%s\"", numBuf.String())
		}
		data, err := buf.Peek(1)
		if err == io.EOF {
			break
		}
		if err != nil {
			common.Log.Debug("ERROR %s", err)
			return nil, err
		}
		if ok2 && (data[0] == '-' || data[0] == '+') {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok2 = false
		} else if IsDecimalDigit(data[0]) {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
		} else if data[0] == '.' {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok = true
		} else if data[0] == 'e' || data[0] == 'E' {
			b, _ := buf.ReadByte()
			numBuf.WriteByte(b)
			ok = true
			ok2 = true
		} else {
			break
		}
	}
	var obj PdfObject
	if ok {
		float, err := strconv.ParseFloat(numBuf.String(), 64)
		if err != nil {
			common.Log.Debug("Error parsing number %v err=%v. Using 0.0. Output may be incorrect", numBuf.String(), err)
			float = 0.0
		}
		num := PdfObjectFloat(float)
		obj = &num
	} else {
		val, err := strconv.ParseInt(numBuf.String(), 10, 64)
		if err != nil {
			common.Log.Debug("Error parsing number %v err=%v. Using 0. Output may be incorrect", numBuf.String(), err)
			val = 0
		}
		num := PdfObjectInteger(val)
		obj = &num
	}
	return obj, nil
}
