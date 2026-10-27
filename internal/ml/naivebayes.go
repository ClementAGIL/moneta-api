// Multinomial Naive Bayes with Laplace smoothing. Classes are category ids (strings so the model is gob-friendly).
package ml

import (
	"math"
	"sort"
	"strings"
)

const explainTop = 3

// Sample is one training example.
type Sample struct {
	Features []string
	Class    string
}

// Model holds token counts per class.
type Model struct {
	Docs        map[string]int            // documents per class
	TokenTotals map[string]int            // tokens per class
	Counts      map[string]map[string]int // class → token → count
	Vocabulary  map[string]bool
	Total       int
}

// Train builds a model from samples.
func Train(samples []Sample) *Model {
	m := &Model{Docs: map[string]int{}, TokenTotals: map[string]int{}, Counts: map[string]map[string]int{},
		Vocabulary: map[string]bool{}}
	for _, s := range samples {
		m.Add(s)
	}
	return m
}

// Add learns one more example (cheap: corrections update the model without a full retrain).
func (m *Model) Add(s Sample) {
	if m.Counts[s.Class] == nil {
		m.Counts[s.Class] = map[string]int{}
	}
	m.Docs[s.Class]++
	m.Total++
	for _, f := range s.Features {
		m.Counts[s.Class][f]++
		m.TokenTotals[s.Class]++
		m.Vocabulary[f] = true
	}
}

// Predict returns the most likely class and its probability (softmax of the log scores).
func (m *Model) Predict(features []string) (string, float64) {
	if m.Total == 0 {
		return "", 0
	}
	scores := map[string]float64{}
	best, bestScore := "", math.Inf(-1)
	for class := range m.Docs {
		score := m.logScore(class, features)
		scores[class] = score
		if score > bestScore || score == bestScore && class < best {
			best, bestScore = class, score
		}
	}
	var sum float64
	for _, score := range scores {
		sum += math.Exp(score - bestScore)
	}
	return best, 1 / sum
}

// Explain returns the features that pushed most towards class compared with the other classes.
func (m *Model) Explain(features []string, class string) []string {
	type weight struct {
		feature string
		value   float64
	}
	var weights []weight
	seen := map[string]bool{}
	for _, f := range features {
		if seen[f] || !m.Vocabulary[f] {
			continue
		}
		seen[f] = true
		weights = append(weights, weight{f, m.logLikelihood(class, f) - m.otherLogLikelihood(class, f)})
	}
	// Words read better than character trigrams: they come first, then the strongest of the rest.
	sort.Slice(weights, func(i, j int) bool {
		if wi, wj := isWord(weights[i].feature), isWord(weights[j].feature); wi != wj {
			return wi
		}
		return weights[i].value > weights[j].value
	})
	var reasons []string
	for _, w := range weights {
		if w.value > 0 {
			reasons = append(reasons, readable(w.feature))
		}
		if len(reasons) == explainTop {
			break
		}
	}
	return reasons
}

func (m *Model) logScore(class string, features []string) float64 {
	score := math.Log(float64(m.Docs[class]) / float64(m.Total))
	for _, f := range features {
		score += m.logLikelihood(class, f)
	}
	return score
}

func (m *Model) logLikelihood(class, feature string) float64 {
	return math.Log(float64(m.Counts[class][feature]+1) / float64(m.TokenTotals[class]+len(m.Vocabulary)))
}

// otherLogLikelihood is the feature's likelihood in all the other classes pooled together.
func (m *Model) otherLogLikelihood(class, feature string) float64 {
	count, total := 0, 0
	for c, counts := range m.Counts {
		if c != class {
			count += counts[feature]
			total += m.TokenTotals[c]
		}
	}
	return math.Log(float64(count+1) / float64(total+len(m.Vocabulary)))
}

func isWord(feature string) bool {
	return strings.HasPrefix(feature, "w:") || strings.HasPrefix(feature, "b:")
}

// readable turns a feature into what a user understands ("w:CARREFOUR" → "CARREFOUR", "a:out4" → "montant").
func readable(feature string) string {
	kind, value, _ := strings.Cut(feature, ":")
	switch kind {
	case "b":
		return strings.ReplaceAll(value, "_", " ")
	case "c":
		return "…" + value + "…"
	case "s":
		return map[string]string{"in": "entrée d'argent", "out": "dépense"}[value]
	case "a":
		return "montant habituel"
	default:
		return value
	}
}
