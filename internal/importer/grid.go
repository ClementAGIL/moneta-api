// Package importer turns a bank export (CSV or XLSX) into a grid of cells, guesses its layout, and parses rows with a
// column mapping. It is pure: no database, no user data kept.
package importer

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/xuri/excelize/v2"
	"golang.org/x/text/encoding/charmap"
)

const (
	maxRows        = 20000
	sniffLines     = 50
	KindCSV        = "csv"
	KindXLSX       = "xlsx"
	EncodingUTF8   = "utf-8"
	EncodingLatin1 = "windows-1252"
)

var (
	ErrEmpty    = errors.New("the file has no rows")
	ErrTooBig   = fmt.Errorf("the file has more than %d rows", maxRows)
	ErrOldExcel = errors.New("old .xls files are not supported: save it as .xlsx or CSV")
	zipMagic    = []byte("PK\x03\x04")
	oleMagic    = []byte{0xD0, 0xCF, 0x11, 0xE0}
	utf8BOM     = []byte{0xEF, 0xBB, 0xBF}
)

// Grid is the raw content of a file: rows of trimmed cells.
type Grid struct {
	Kind      string     `json:"kind"`
	Encoding  string     `json:"encoding,omitempty"`
	Delimiter string     `json:"delimiter,omitempty"`
	Rows      [][]string `json:"rows"`
}

// ReadGrid detects the file type, encoding and delimiter, and returns its cells.
func ReadGrid(data []byte) (Grid, error) {
	var g Grid
	var err error
	switch {
	case bytes.HasPrefix(data, oleMagic):
		return Grid{}, ErrOldExcel
	case bytes.HasPrefix(data, zipMagic):
		g, err = readXLSX(data)
	default:
		g, err = readCSV(data)
	}
	if err != nil {
		return Grid{}, err
	}
	g.Rows = dropEmptyRows(g.Rows)
	if len(g.Rows) == 0 {
		return Grid{}, ErrEmpty
	}
	if len(g.Rows) > maxRows {
		return Grid{}, ErrTooBig
	}
	return g, nil
}

func readXLSX(data []byte) (Grid, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{RawCellValue: true})
	if err != nil {
		return Grid{}, fmt.Errorf("reading the Excel file: %w", err)
	}
	defer f.Close()
	sheets := f.GetSheetList()
	if len(sheets) == 0 {
		return Grid{}, ErrEmpty
	}
	rows, err := f.GetRows(sheets[0]) // raw values: dates are serial numbers, amounts use '.'
	if err != nil {
		return Grid{}, fmt.Errorf("reading the Excel sheet: %w", err)
	}
	return Grid{Kind: KindXLSX, Rows: trimCells(rows)}, nil
}

func readCSV(data []byte) (Grid, error) {
	text, encoding := decode(data)
	delim := sniffDelimiter(text)
	r := csv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.FieldsPerRecord = -1 // banks add title lines with fewer columns above the table
	r.LazyQuotes = true
	var rows [][]string
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Grid{}, fmt.Errorf("reading the CSV file: %w", err)
		}
		rows = append(rows, row)
		if len(rows) > maxRows {
			return Grid{}, ErrTooBig
		}
	}
	return Grid{Kind: KindCSV, Encoding: encoding, Delimiter: string(delim), Rows: trimCells(rows)}, nil
}

// decode returns UTF-8 text: valid UTF-8 is kept, anything else is read as Windows-1252 (French bank default).
func decode(data []byte) (string, string) {
	data = bytes.TrimPrefix(data, utf8BOM)
	if utf8.Valid(data) {
		return string(data), EncodingUTF8
	}
	text, _ := charmap.Windows1252.NewDecoder().Bytes(data)
	return string(text), EncodingLatin1
}

// sniffDelimiter picks the separator giving the most lines with the same (>1) number of fields.
func sniffDelimiter(text string) rune {
	lines := strings.Split(text, "\n")
	lines = lines[:min(len(lines), sniffLines)]
	best, bestScore := ';', -1
	for _, d := range []rune{';', ',', '\t', '|'} {
		counts := map[int]int{}
		for _, line := range lines {
			if n := strings.Count(line, string(d)); n > 0 {
				counts[n]++
			}
		}
		for _, c := range counts {
			if c > bestScore {
				best, bestScore = d, c
			}
		}
	}
	return best
}

func trimCells(rows [][]string) [][]string {
	for _, row := range rows {
		for i, cell := range row {
			row[i] = strings.TrimSpace(strings.ReplaceAll(cell, " ", " "))
		}
	}
	return rows
}

func dropEmptyRows(rows [][]string) [][]string {
	kept := rows[:0]
	for _, row := range rows {
		if strings.Join(row, "") != "" {
			kept = append(kept, row)
		}
	}
	return kept
}
