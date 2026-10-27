// Package money parses bank amounts into signed integer cents ("1 234,56 €", "-12.30", "(5,00)", "12,50-").
package money

import (
	"errors"
	"strings"
	"unicode"
)

var ErrAmount = errors.New("not an amount")

// Parse reads s with the given decimal separator ('.' or ','); the other one is treated as a thousands separator.
func Parse(s string, decimalSep rune) (int64, error) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '€' || r == '\'' {
			return -1
		}
		return r
	}, strings.ToUpper(s))
	s = strings.TrimSuffix(strings.TrimPrefix(s, "EUR"), "EUR")
	negative := false
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		negative, s = true, s[1:len(s)-1]
	}
	if strings.HasSuffix(s, "-") {
		negative, s = true, s[:len(s)-1]
	}
	if strings.HasPrefix(s, "-") {
		negative, s = !negative, s[1:]
	} else {
		s = strings.TrimPrefix(s, "+")
	}
	cents, err := parseUnsigned(s, decimalSep)
	if negative {
		cents = -cents
	}
	return cents, err
}

func parseUnsigned(s string, decimalSep rune) (int64, error) {
	thousands := ','
	if decimalSep == ',' {
		thousands = '.'
	}
	s = strings.ReplaceAll(s, string(thousands), "")
	whole, frac, _ := strings.Cut(s, string(decimalSep))
	if whole == "" && frac == "" || len(whole) > 15 {
		return 0, ErrAmount
	}
	cents, err := digits(whole)
	if err != nil {
		return 0, err
	}
	cents *= 100
	switch len(frac) {
	case 0:
	case 1, 2:
		f, err := digits(frac)
		if len(frac) == 1 {
			f *= 10
		}
		return cents + f, err
	default: // Excel raw values can carry float noise ("12.300000000000001"): round to the cent
		f, err := digits(frac[:2])
		if err != nil {
			return 0, err
		}
		if _, err := digits(frac[2:]); err != nil {
			return 0, err
		}
		if frac[2] >= '5' {
			f++
		}
		return cents + f, nil
	}
	return cents, nil
}

func digits(s string) (int64, error) {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, ErrAmount
		}
		n = n*10 + int64(r-'0')
	}
	return n, nil
}
