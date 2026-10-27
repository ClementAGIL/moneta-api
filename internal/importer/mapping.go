// Mapping: which column holds what, and parsing the grid's data rows with it.
package importer

import (
	"strings"
	"time"

	"github.com/ClementAGIL/moneta-api/internal/apperr"
	"github.com/ClementAGIL/moneta-api/internal/money"
)

const (
	AmountSingle      = "single"       // one signed column
	AmountDebitCredit = "debit_credit" // two columns; debit is money out whatever its sign
	maxLabelLen       = 300
)

// Mapping tells how to read the data rows. Column indexes are 0-based; -1 means unused.
type Mapping struct {
	HeaderRow  int    `json:"headerRow"` // -1 when the file has no header
	DateCol    int    `json:"dateCol"`
	DateFormat string `json:"dateFormat"` // a Go layout from DateFormats, or "excel"
	AmountMode string `json:"amountMode"`
	AmountCol  int    `json:"amountCol"`
	DebitCol   int    `json:"debitCol"`
	CreditCol  int    `json:"creditCol"`
	DecimalSep string `json:"decimalSep"` // "," or "."
	LabelCols  []int  `json:"labelCols"`  // joined with a space
}

// Row is one parsed data row, or its error.
type Row struct {
	Index  int // row index in the grid
	Date   time.Time
	Amount int64
	Label  string
	Err    string
}

// Validate checks the mapping against a grid with cols columns.
func (m Mapping) Validate(cols int) error {
	in := func(c int) bool { return c >= 0 && c < cols }
	switch {
	case m.HeaderRow < -1:
		return apperr.Invalid("headerRow", "must be -1 or a row index")
	case !in(m.DateCol):
		return apperr.Invalid("dateCol", "pick the date column")
	case m.DateFormat != FormatExcelSerial && !knownLayout(m.DateFormat):
		return apperr.Invalid("dateFormat", "unknown date format")
	case m.AmountMode == AmountSingle && !in(m.AmountCol):
		return apperr.Invalid("amountCol", "pick the amount column")
	case m.AmountMode == AmountDebitCredit && (!in(m.DebitCol) || !in(m.CreditCol)):
		return apperr.Invalid("debitCol", "pick the debit and credit columns")
	case m.AmountMode != AmountSingle && m.AmountMode != AmountDebitCredit:
		return apperr.Invalid("amountMode", "must be single or debit_credit")
	case m.DecimalSep != "," && m.DecimalSep != ".":
		return apperr.Invalid("decimalSep", "must be , or .")
	case len(m.LabelCols) == 0:
		return apperr.Invalid("labelCols", "pick at least one label column")
	}
	for _, c := range m.LabelCols {
		if !in(c) {
			return apperr.Invalid("labelCols", "unknown label column")
		}
	}
	return nil
}

// Parse reads every row after the header. Rows without a valid date (sub-totals, "Solde au …") are invalid.
func (m Mapping) Parse(g Grid) []Row {
	var rows []Row
	for i := m.HeaderRow + 1; i < len(g.Rows); i++ {
		rows = append(rows, m.parseRow(i, g.Rows[i]))
	}
	return rows
}

func (m Mapping) parseRow(i int, cells []string) Row {
	row := Row{Index: i}
	date, ok := ParseDate(cell(cells, m.DateCol), m.DateFormat)
	if !ok {
		row.Err = "no valid date"
		return row
	}
	amount, err := m.amount(cells)
	if err != nil {
		row.Err = "no valid amount"
		return row
	}
	row.Date, row.Amount, row.Label = date, amount, m.label(cells)
	if row.Label == "" {
		row.Err = "empty label"
	}
	return row
}

func (m Mapping) amount(cells []string) (int64, error) {
	sep := []rune(m.DecimalSep)[0]
	if m.AmountMode == AmountSingle {
		return money.Parse(cell(cells, m.AmountCol), sep)
	}
	debit, credit := cell(cells, m.DebitCol), cell(cells, m.CreditCol)
	if debit != "" {
		v, err := money.Parse(debit, sep)
		return -abs(v), err
	}
	v, err := money.Parse(credit, sep)
	return abs(v), err
}

func (m Mapping) label(cells []string) string {
	var parts []string
	for _, c := range m.LabelCols {
		if v := cell(cells, c); v != "" {
			parts = append(parts, v)
		}
	}
	label := strings.Join(parts, " ")
	if r := []rune(label); len(r) > maxLabelLen {
		label = string(r[:maxLabelLen])
	}
	return label
}

func cell(cells []string, i int) string {
	if i < 0 || i >= len(cells) {
		return ""
	}
	return cells[i]
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

func knownLayout(layout string) bool {
	for _, l := range DateFormats {
		if l == layout {
			return true
		}
	}
	return false
}
