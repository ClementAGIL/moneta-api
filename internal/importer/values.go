// Cell value parsing: dates in the formats French banks use, and Excel serial dates.
package importer

import (
	"strconv"
	"strings"
	"time"
)

// FormatExcelSerial means the date column holds Excel serial day numbers (raw XLSX values).
const FormatExcelSerial = "excel"

// DateFormats are the Go layouts tried when guessing, most common first.
var DateFormats = []string{"02/01/2006", "2006-01-02", "02-01-2006", "02/01/06", "02.01.2006", "2006/01/02",
	"02-01-06", "2/1/2006"}

var excelEpoch = time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC)

const (
	minSerial = 20000 // 1954
	maxSerial = 80000 // 2119
)

// ParseDate reads a date cell with layout (or FormatExcelSerial). Time parts after the date are ignored.
func ParseDate(cell, layout string) (time.Time, bool) {
	cell = strings.TrimSpace(cell)
	if layout == FormatExcelSerial {
		return parseSerial(cell)
	}
	if len(cell) > len(layout) && strings.ContainsAny(cell[len(layout):len(layout)+1], " T") {
		cell = cell[:len(layout)]
	}
	t, err := time.Parse(layout, cell)
	return t, err == nil && t.Year() >= 1970 && t.Year() < 2100
}

func parseSerial(cell string) (time.Time, bool) {
	f, err := strconv.ParseFloat(cell, 64)
	if err != nil || f < minSerial || f > maxSerial {
		return time.Time{}, false
	}
	return excelEpoch.AddDate(0, 0, int(f)), true
}

// guessDateFormat returns the first layout that parses every non-empty sample (needs at least one).
func guessDateFormat(samples []string) (string, bool) {
	for _, layout := range append([]string{FormatExcelSerial}, DateFormats...) {
		if allParse(samples, layout) {
			return layout, true
		}
	}
	return "", false
}

func allParse(samples []string, layout string) bool {
	seen := 0
	for _, s := range samples {
		if s == "" {
			continue
		}
		if _, ok := ParseDate(s, layout); !ok {
			return false
		}
		seen++
	}
	return seen > 0
}

func isDate(cell string) bool {
	_, ok := guessDateFormat([]string{cell})
	return ok
}
