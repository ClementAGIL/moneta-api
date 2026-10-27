// Layout guessing: header row, column roles (from header names, then from content), date format, decimal separator,
// and the header fingerprint that recognizes a known bank format.
package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/ClementAGIL/moneta-api/internal/labels"
	"github.com/ClementAGIL/moneta-api/internal/money"
)

const (
	headerScanRows = 30
	sampleRows     = 40
)

// Header words per role, on normalized names (uppercase, no accents). Order matters: first hit wins.
var (
	dateWords   = []string{"DATE OPERATION", "DATE COMPTABLE", "DATE DE COMPTABILISATION", "DATE"}
	labelWords  = []string{"LIBELLE", "LABEL", "DESCRIPTION", "INTITULE", "DETAIL", "NATURE", "OPERATION", "MOTIF"}
	amountWords = []string{"MONTANT", "AMOUNT", "SOMME", "VALEUR"}
)

// Detect guesses a mapping for g. Unknown roles are -1; the user fixes them in the wizard.
func Detect(g Grid) Mapping {
	header := findHeader(g.Rows)
	m := Mapping{HeaderRow: header, DateCol: -1, AmountMode: AmountSingle, AmountCol: -1, DebitCol: -1, CreditCol: -1,
		DecimalSep: ","}
	if g.Kind == KindXLSX {
		m.DecimalSep = "."
	}
	if header >= 0 {
		m.byHeaderNames(g.Rows[header])
	}
	data := g.Rows[header+1 : min(len(g.Rows), header+1+sampleRows)]
	m.byContent(data)
	if m.DateCol >= 0 {
		m.DateFormat, _ = guessDateFormat(column(data, m.DateCol))
	}
	if m.AmountMode == AmountSingle && m.AmountCol >= 0 {
		m.DecimalSep = guessDecimalSep(column(data, m.AmountCol), m.DecimalSep)
	}
	return m
}

// Fingerprint identifies a file layout by its header (or its column count when there is none).
func Fingerprint(g Grid, headerRow int) string {
	var key string
	if headerRow >= 0 && headerRow < len(g.Rows) {
		names := make([]string, len(g.Rows[headerRow]))
		for i, name := range g.Rows[headerRow] {
			names[i] = labels.Normalize(name)
		}
		key = strings.Join(names, "|")
	}
	if headerRow+1 < len(g.Rows) {
		key += "#" + strings.Repeat("c", len(g.Rows[headerRow+1]))
	}
	sum := sha256.Sum256([]byte(g.Kind + "/" + key))
	return hex.EncodeToString(sum[:])
}

// findHeader returns the last mostly-text row right before the first row holding a date, or -1.
func findHeader(rows [][]string) int {
	for i := 0; i < min(len(rows), headerScanRows); i++ {
		if rowHasDate(rows[i]) {
			if i > 0 && isTextRow(rows[i-1]) {
				return i - 1
			}
			return -1
		}
	}
	return -1
}

func (m *Mapping) byHeaderNames(header []string) {
	names := make([]string, len(header))
	for i, h := range header {
		names[i] = labels.Normalize(h)
	}
	m.DateCol = findName(names, dateWords, "VALEUR")
	m.DebitCol = findName(names, []string{"DEBIT"}, "")
	m.CreditCol = findName(names, []string{"CREDIT"}, "")
	m.AmountCol = findName(names, amountWords, "DATE")
	if m.AmountCol < 0 && m.DebitCol >= 0 && m.CreditCol >= 0 {
		m.AmountMode = AmountDebitCredit
	}
	if c := findName(names, labelWords, "DATE"); c >= 0 {
		m.LabelCols = []int{c}
	}
}

// byContent fills roles still unknown: first date-like column, last amount-like column, longest text column.
func (m *Mapping) byContent(data [][]string) {
	cols := 0
	for _, row := range data {
		cols = max(cols, len(row))
	}
	for c := 0; c < cols; c++ {
		values := column(data, c)
		switch {
		case m.DateCol < 0 && share(values, isDate) > 0.8:
			m.DateCol = c
		case m.AmountMode == AmountSingle && c != m.DateCol && share(values, isAmount) > 0.8:
			if m.AmountCol < 0 || m.AmountCol == c {
				m.AmountCol = c
			}
		}
	}
	if len(m.LabelCols) == 0 {
		if c := longestTextColumn(data, cols, m.DateCol, m.AmountCol); c >= 0 {
			m.LabelCols = []int{c}
		}
	}
}

// findName returns the first column whose name contains one of words (in priority order) and not exclude.
func findName(names, words []string, exclude string) int {
	for _, w := range words {
		for i, n := range names {
			if strings.Contains(n, w) && (exclude == "" || !strings.Contains(n, exclude)) {
				return i
			}
		}
	}
	return -1
}

func guessDecimalSep(values []string, fallback string) string {
	for _, v := range values {
		if i := strings.LastIndexAny(v, ",."); i >= 0 && len(strings.TrimRight(v[i+1:], " €")) <= 2 {
			return v[i : i+1]
		}
	}
	return fallback
}

func longestTextColumn(data [][]string, cols int, skip ...int) int {
	best, bestLen := -1, 0
	for c := 0; c < cols; c++ {
		if contains(skip, c) {
			continue
		}
		total := 0
		for _, v := range column(data, c) {
			if !isAmount(v) && !isDate(v) {
				total += len(v)
			}
		}
		if total > bestLen {
			best, bestLen = c, total
		}
	}
	return best
}

func rowHasDate(row []string) bool {
	for _, c := range row {
		if isDate(c) {
			return true
		}
	}
	return false
}

func isTextRow(row []string) bool {
	text := 0
	for _, c := range row {
		if c != "" && !isAmount(c) && !isDate(c) {
			text++
		}
	}
	return text >= 2
}

func isAmount(s string) bool {
	if s == "" {
		return false
	}
	_, errComma := money.Parse(s, ',')
	_, errDot := money.Parse(s, '.')
	return errComma == nil || errDot == nil
}

func column(rows [][]string, c int) []string {
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		values = append(values, cell(row, c))
	}
	return values
}

// share is the fraction of non-empty values matching ok.
func share(values []string, ok func(string) bool) float64 {
	hit, total := 0, 0
	for _, v := range values {
		if v == "" {
			continue
		}
		total++
		if ok(v) {
			hit++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(hit) / float64(total)
}

func contains(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
