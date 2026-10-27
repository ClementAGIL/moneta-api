// Package ml is a small text classifier written from scratch: multinomial Naive Bayes over label tokens, trained per
// user on their categorized transactions. features.go turns a transaction into tokens.
package ml

import (
	"fmt"
	"math"
	"strings"

	"github.com/ClementAGIL/moneta-api/internal/labels"
)

const trigramSize = 3

// Features returns the tokens of a transaction: label words and word pairs, character trigrams of the merchant
// (robust to truncated or misspelled names), the direction, and the order of magnitude of the amount.
func Features(label string, amount int64) []string {
	norm := labels.Normalize(label)
	words := strings.Fields(norm)
	features := make([]string, 0, len(words)*3+8)
	for i, w := range words {
		features = append(features, "w:"+w)
		if i > 0 {
			features = append(features, "b:"+words[i-1]+"_"+w)
		}
	}
	merchant := strings.ReplaceAll(labels.Merchant(norm), " ", "")
	for i := 0; i+trigramSize <= len(merchant); i++ {
		features = append(features, "c:"+merchant[i:i+trigramSize])
	}
	sign := "out"
	if amount > 0 {
		sign = "in"
	}
	features = append(features, "s:"+sign, fmt.Sprintf("a:%s%d", sign, magnitude(amount)))
	return features
}

// magnitude buckets an amount on a log scale (≈ 1 €, 2 €, 4 €, 8 €…), so the model learns "small" vs "rent-sized".
func magnitude(cents int64) int {
	euros := math.Abs(float64(cents)) / 100
	if euros < 1 {
		return 0
	}
	return int(math.Log2(euros)) + 1
}
