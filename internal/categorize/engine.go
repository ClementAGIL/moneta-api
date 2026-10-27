// Package categorize assigns categories: the user's rules first, then built-in merchant keywords, then (once trained)
// the classifier. Transactions the user categorized by hand are never touched.
package categorize

import (
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/ClementAGIL/moneta-api/internal/labels"
)

const (
	SourceRule    = "rule"
	SourceBuiltin = "builtin"
	SourceModel   = "model"
	SourceUser    = "user"

	ruleConfidence    = 1.0
	builtinConfidence = 0.9
)

// Rule is a decrypted user rule.
type Rule struct {
	ID         uuid.UUID `json:"id"`
	Pattern    string    `json:"pattern"`
	Match      string    `json:"match"` // merchant (equals the merchant) or contains (anywhere in the label)
	Sign       string    `json:"sign"`  // any, in, out
	CategoryID uuid.UUID `json:"categoryId"`
	Priority   int       `json:"priority"`
	Origin     string    `json:"origin"`
}

// Txn is what the engine needs from a transaction.
type Txn struct {
	Label  string // decrypted, raw
	Amount int64
}

// Decision is the engine's answer; CategoryID nil means uncategorized.
type Decision struct {
	CategoryID *uuid.UUID
	Source     *string
	Confidence *float32
}

// Predictor is the trained classifier (nil until active).
type Predictor interface {
	Predict(label string, amount int64) (uuid.UUID, float32, bool)
}

// Engine categorizes one user's transactions.
type Engine struct {
	rules []Rule
	byKey map[string]uuid.UUID
	model Predictor
}

// NewEngine sorts rules by priority (then corrections before manual rules, as they are more specific).
func NewEngine(rules []Rule, byKey map[string]uuid.UUID, model Predictor) *Engine {
	sort.SliceStable(rules, func(i, j int) bool {
		if rules[i].Priority != rules[j].Priority {
			return rules[i].Priority > rules[j].Priority
		}
		return rules[i].Origin == "correction" && rules[j].Origin != "correction"
	})
	return &Engine{rules: rules, byKey: byKey, model: model}
}

// Categorize applies the pipeline to one transaction.
func (e *Engine) Categorize(t Txn) Decision {
	norm := labels.Normalize(t.Label)
	merchant := labels.Merchant(norm)
	for _, r := range e.rules {
		if signMatches(r.Sign, t.Amount) && ruleMatches(r, norm, merchant) {
			return decision(r.CategoryID, SourceRule, ruleConfidence)
		}
	}
	padded := " " + norm + " "
	for _, b := range builtins {
		if id, ok := e.byKey[b.key]; ok && signMatches(b.sign, t.Amount) && strings.Contains(padded, " "+b.keyword+" ") {
			return decision(id, SourceBuiltin, builtinConfidence)
		}
	}
	if e.model != nil {
		if id, confidence, ok := e.model.Predict(t.Label, t.Amount); ok {
			return decision(id, SourceModel, confidence)
		}
	}
	return Decision{}
}

// Matches reports whether rule r applies to t (used by the rule preview).
func Matches(r Rule, t Txn) bool {
	norm := labels.Normalize(t.Label)
	return signMatches(r.Sign, t.Amount) && ruleMatches(r, norm, labels.Merchant(norm))
}

func ruleMatches(r Rule, norm, merchant string) bool {
	pattern := labels.Normalize(r.Pattern)
	if pattern == "" {
		return false
	}
	if r.Match == "merchant" {
		return merchant == pattern
	}
	return strings.Contains(norm, pattern)
}

func signMatches(sign string, amount int64) bool {
	return sign == "any" || sign == "out" && amount < 0 || sign == "in" && amount > 0
}

func decision(id uuid.UUID, source string, confidence float32) Decision {
	return Decision{CategoryID: &id, Source: &source, Confidence: &confidence}
}
