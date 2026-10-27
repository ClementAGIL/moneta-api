// Package recurring finds payments and income that repeat (subscriptions, bills, rent, salary). detect.go is pure:
// one merchant's dated amounts in, a series (or nothing) out.
package recurring

import (
	"math"
	"slices"
	"time"
)

const (
	minOccurrences       = 3
	minYearlyOccurrences = 2
	minShare             = 0.7  // share of intervals / amounts that must fit the pattern
	amountTolerance      = 0.15 // ±15 % around the median amount
	priceChangeMin       = 0.03 // a change under 3 % is noise (rounding, currency)
	stableTolerance      = 0.01
	maxSubscriptionCents = 10000
)

type cadenceRule struct {
	name             string
	minDays, maxDays float64
	step             func(time.Time) time.Time
}

var cadences = []cadenceRule{
	{"weekly", 5, 9, func(t time.Time) time.Time { return t.AddDate(0, 0, 7) }},
	{"monthly", 26, 35, func(t time.Time) time.Time { return t.AddDate(0, 1, 0) }},
	{"quarterly", 82, 100, func(t time.Time) time.Time { return t.AddDate(0, 3, 0) }},
	{"yearly", 350, 380, func(t time.Time) time.Time { return t.AddDate(1, 0, 0) }},
}

// Point is one transaction of a merchant.
type Point struct {
	Date   time.Time
	Amount int64
}

// Series is a detected recurring payment or income.
type Series struct {
	Cadence        string
	Amount         int64 // latest
	PreviousAmount *int64
	PriceChangedOn *time.Time
	Average        int64
	Occurrences    int
	LastDate       time.Time
	NextDate       time.Time
	Stable         bool // every amount within 1 % of the median
}

// Detect returns the series formed by points (sorted by date), or false when they do not repeat regularly.
func Detect(points []Point) (Series, bool) {
	if len(points) < minYearlyOccurrences {
		return Series{}, false
	}
	rule, ok := findCadence(points)
	if !ok || (rule.name != "yearly" && len(points) < minOccurrences) {
		return Series{}, false
	}
	amounts := make([]int64, len(points))
	for i, p := range points {
		amounts[i] = p.Amount
	}
	if !regularAmounts(amounts) {
		return Series{}, false
	}
	median := medianOf(amounts)
	last := points[len(points)-1]
	s := Series{Cadence: rule.name, Amount: last.Amount, Average: mean(amounts), Occurrences: len(points),
		LastDate: last.Date, NextDate: rule.step(last.Date),
		Stable: share(amounts, func(a int64) bool { return near(a, median, stableTolerance) }) == 1}
	s.PreviousAmount, s.PriceChangedOn = priceChange(points)
	return s, true
}

// LooksLikeSubscription guesses from the amounts alone: outgoing, monthly or yearly, stable and small. Used only when
// the category does not tell (see Service.upsert).
func (s Series) LooksLikeSubscription() bool {
	return s.Amount < 0 && s.Stable && (s.Cadence == "monthly" || s.Cadence == "yearly") && -s.Amount <= maxSubscriptionCents
}

// Ended reports a series whose next occurrence is overdue by more than half a period.
func (s Series) Ended(now time.Time) bool {
	half := s.NextDate.Sub(s.LastDate) / 2
	return now.After(s.NextDate.Add(half))
}

// MonthlyCost converts an amount at a cadence to its monthly equivalent.
func MonthlyCost(cadence string, amount int64) int64 {
	switch cadence {
	case "weekly":
		return amount * 52 / 12
	case "quarterly":
		return amount / 3
	case "yearly":
		return amount / 12
	default:
		return amount
	}
}

// regularAmounts accepts a stable amount, possibly with one level change: the trailing run of amounts close to the
// latest one (a price increase) plus earlier amounts close to their own median must cover 70 % of the points.
func regularAmounts(amounts []int64) bool {
	latest := amounts[len(amounts)-1]
	start := len(amounts) - 1
	for start > 0 && near(amounts[start-1], latest, amountTolerance) {
		start--
	}
	fits := len(amounts) - start
	if before := amounts[:start]; len(before) > 0 {
		median := medianOf(before)
		for _, a := range before {
			if near(a, median, amountTolerance) {
				fits++
			}
		}
	}
	return float64(fits)/float64(len(amounts)) >= minShare
}

func findCadence(points []Point) (cadenceRule, bool) {
	intervals := make([]float64, len(points)-1)
	for i := 1; i < len(points); i++ {
		intervals[i-1] = points[i].Date.Sub(points[i-1].Date).Hours() / 24
	}
	for _, rule := range cadences {
		fits := 0
		for _, d := range intervals {
			if d >= rule.minDays && d <= rule.maxDays {
				fits++
			}
		}
		if float64(fits)/float64(len(intervals)) >= minShare {
			return rule, true
		}
	}
	return cadenceRule{}, false
}

// priceChange returns the amount before the latest change and the date of that change, if it changed by > 3 %.
func priceChange(points []Point) (*int64, *time.Time) {
	last := points[len(points)-1]
	for i := len(points) - 2; i >= 0; i-- {
		if points[i].Amount != last.Amount {
			if near(points[i].Amount, last.Amount, priceChangeMin) {
				return nil, nil
			}
			previous, on := points[i].Amount, points[i+1].Date
			return &previous, &on
		}
	}
	return nil, nil
}

func near(a, b int64, tolerance float64) bool {
	return math.Abs(float64(a-b)) <= math.Abs(float64(b))*tolerance
}

func share(values []int64, ok func(int64) bool) float64 {
	n := 0
	for _, v := range values {
		if ok(v) {
			n++
		}
	}
	return float64(n) / float64(len(values))
}

func medianOf(values []int64) int64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

func mean(values []int64) int64 {
	var sum int64
	for _, v := range values {
		sum += v
	}
	return sum / int64(len(values))
}
