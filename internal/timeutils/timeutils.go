// Copyright 2020 FoxyUtils ehf. Licensed under the MIT License.
// See the LICENSE file in the repository root.

package timeutils

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

func parsePart(text string, fallback int64) int64 {
	if len(text) == 0 {
		return fallback
	}
	value, _ := strconv.ParseInt(text, 10, 32)
	return value
}
func ParsePdfTime(pdfTime string) (time.Time, error) {
	matches := pdfDatePattern.FindAllStringSubmatch(pdfTime, 1)
	if len(matches) < 1 {
		if len(pdfTime) > 0 && pdfTime[0] != 'D' {
			pdfTime = fmt.Sprintf("D:%s", pdfTime)
			return ParsePdfTime(pdfTime)
		}
		return time.Time{}, fmt.Errorf("invalid date string (%s)", pdfTime)
	}
	if len(matches[0]) != 10 {
		return time.Time{}, errors.New("invalid regexp group match length != 10")
	}
	year, _ := strconv.ParseInt(matches[0][1], 10, 32)
	month := parsePart(matches[0][2], 1)
	day := parsePart(matches[0][3], 1)
	hour := parsePart(matches[0][4], 0)
	minute := parsePart(matches[0][5], 0)
	second := parsePart(matches[0][6], 0)
	var (
		zoneSign    byte
		zoneHours   int64
		zoneMinutes int64
	)
	zoneSign = '+'
	if len(matches[0][7]) > 0 {
		switch matches[0][7] {
		case "-":
			zoneSign = '-'
		case "Z":
			zoneSign = 'Z'
		}
	}
	if len(matches[0][8]) > 0 {
		zoneHours, _ = strconv.ParseInt(matches[0][8], 10, 32)
	} else {
		zoneHours = 0
	}
	if len(matches[0][9]) > 0 {
		zoneMinutes, _ = strconv.ParseInt(matches[0][9], 10, 32)
	} else {
		zoneMinutes = 0
	}
	offsetSeconds := int(zoneHours*60*60 + zoneMinutes*60)
	switch zoneSign {
	case '-':
		offsetSeconds = -offsetSeconds
	case 'Z':
		offsetSeconds = 0
	}
	zoneName := fmt.Sprintf("UTC%c%.2d%.2d", zoneSign, zoneHours, zoneMinutes)
	location := time.FixedZone(zoneName, offsetSeconds)
	return time.Date(int(year), time.Month(month), int(day), int(hour), int(minute), int(second), 0, location), nil
}

var pdfDatePattern = regexp.MustCompile("\\s*D\\s*:\\s*(\\d{4})(\\d{2})?(\\d{2})?(\\d{2})?(\\d{2})?(\\d{2})?([+-Z])?(\\d{2})?'?(\\d{2})?")

func FormatPdfTime(in time.Time) string {
	offset := in.Format("-07:00")
	offsetHours, _ := strconv.ParseInt(offset[1:3], 10, 32)
	offsetMinutes, _ := strconv.ParseInt(offset[4:6], 10, 32)
	year := int64(in.Year())
	month := int64(in.Month())
	day := int64(in.Day())
	hour := int64(in.Hour())
	minute := int64(in.Minute())
	second := int64(in.Second())
	sign := offset[0]
	return fmt.Sprintf("D:%.4d%.2d%.2d%.2d%.2d%.2d%c%.2d'%.2d'", year, month, day, hour, minute, second, sign, offsetHours, offsetMinutes)
}
