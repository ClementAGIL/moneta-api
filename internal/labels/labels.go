// Package labels cleans bank transaction labels and extracts the merchant. Dedup, rules, recurring detection and the
// classifier all work on these, so changes here change every downstream result: tune on real exports.
package labels

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const maxMerchantWords = 3

// Words after which a label only holds the payment reason or a reference.
var cutWords = map[string]bool{"MOTIF": true, "REF": true, "REFERENCE": true, "LIB": true, "ECHEANCE": true,
	"MANDAT": true, "ID": true}

// Prefixes French banks put before the useful part of a label, longest first so "PRLV SEPA" wins over "PRLV".
var prefixes = []string{
	"PAIEMENT PAR CARTE", "PAIEMENT CB", "ACHAT CB", "FACTURE CARTE", "CARTE BANCAIRE", "PRLV SEPA", "PRELEVEMENT",
	"VIREMENT EN VOTRE FAVEUR", "VIREMENT DE", "VIR SEPA RECU", "VIR SEPA EMIS", "VIR SEPA", "VIREMENT SEPA",
	"VIREMENT", "VIR INST", "VIR",
	"RETRAIT DAB", "RETRAIT", "PAIEMENT", "CARTE", "PRLV", "CB", "DU",
}

var (
	cardNumber = regexp.MustCompile(`\bX+\d{2,}\b|\b\d{4}X+\d*\b|\*+\d+`)
	dateLike   = regexp.MustCompile(`\b\d{1,2}[/.-]\d{1,2}([/.-]\d{2,4})?\b|\b\d{6}\b|\b\d{8}\b`)
	longNumber = regexp.MustCompile(`\b\w*\d{5,}\w*\b`) // references, mandate ids, phone numbers
	spaces     = regexp.MustCompile(`\s+`)
	noise      = regexp.MustCompile(`[^A-Z0-9&' ]`)
)

// Words dropped when looking for the merchant: legal forms, countries, glue words, common city names.
var stopWords = map[string]bool{
	"SAS": true, "SARL": true, "SA": true, "EURL": true, "SASU": true, "FR": true, "FRA": true, "FRANCE": true,
	"EUR": true, "CB": true, "MME": true, "MR": true, "M": true, "MLLE": true, "DE": true, "LA": true, "LE": true,
	"LES": true, "DES": true, "ET": true, "WWW": true, "COM": true,
	"PARIS": true, "LYON": true, "MARSEILLE": true, "TOULOUSE": true, "NANTES": true, "LILLE": true, "BORDEAUX": true,
	"NICE": true, "RENNES": true, "STRASBOURG": true, "MONTPELLIER": true, "GRENOBLE": true, "CEDEX": true,
}

var stripAccents = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

// Normalize uppercases, removes accents, card numbers, dates and references, and collapses spaces.
func Normalize(label string) string {
	s, _, _ := transform.String(stripAccents, strings.ToUpper(label))
	s = cardNumber.ReplaceAllString(s, " ")
	s = dateLike.ReplaceAllString(s, " ")
	s = longNumber.ReplaceAllString(s, " ")
	s = noise.ReplaceAllString(s, " ")
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// Merchant returns the first meaningful words of a normalized label ("CB CARREFOUR CITY PARIS" → "CARREFOUR CITY").
func Merchant(normalized string) string {
	s := trimPrefixes(normalized)
	var words []string
	for _, w := range strings.Fields(s) {
		if cutWords[w] {
			break
		}
		if stopWords[w] || len(w) < 2 && !unicode.IsLetter(rune(w[0])) {
			continue
		}
		words = append(words, w)
		if len(words) == maxMerchantWords {
			break
		}
	}
	if len(words) == 0 {
		return normalized
	}
	return strings.Join(words, " ")
}

func trimPrefixes(s string) string {
	for changed := true; changed; {
		changed = false
		for _, p := range prefixes {
			if rest, ok := strings.CutPrefix(s, p+" "); ok {
				s, changed = rest, true
				break
			}
		}
	}
	return s
}
