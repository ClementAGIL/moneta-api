// Package dedup classifies imported rows against the account's existing transactions: exact duplicates (same
// date, amount and normalized label, counted per occurrence) are skipped, near-matches go to the user for review.
package dedup

import (
	"time"

	"github.com/google/uuid"
)

const (
	nearDays = 3 // banks move a payment's date between the "pending" and "booked" exports

	StatusNew       = "new"
	StatusDuplicate = "duplicate"
	StatusReview    = "review"
)

// Row is an imported row: Occurrence is its rank among identical rows (same Key) in the file, from 1.
type Row struct {
	Date       time.Time
	Amount     int64
	Key        string
	Occurrence int
}

// Existing is a transaction already in the account, around the file's period.
type Existing struct {
	ID     uuid.UUID
	Date   time.Time
	Amount int64
	Key    string
}

// Result is the decision for one row; Match is set for reviews.
type Result struct {
	Status string
	Match  *uuid.UUID
}

// Classify decides each row. Identical rows are legitimate (two coffees the same day), so row (key, n) is a duplicate
// only when the account already holds at least n transactions with that key.
func Classify(rows []Row, existing []Existing) []Result {
	counts := map[string]int{}
	inFile := map[string]bool{}
	for _, e := range existing {
		counts[e.Key]++
	}
	for _, r := range rows {
		inFile[r.Key] = true
	}
	start, end := period(rows)
	used := map[uuid.UUID]bool{}
	results := make([]Result, len(rows))
	for i, r := range rows {
		if r.Occurrence <= counts[r.Key] {
			results[i] = Result{Status: StatusDuplicate}
			continue
		}
		results[i] = Result{Status: StatusNew}
		if m := nearMatch(r, existing, inFile, used, start, end); m != nil {
			used[*m] = true
			results[i] = Result{Status: StatusReview, Match: m}
		}
	}
	return results
}

// nearMatch finds an existing transaction this file should contain but does not reproduce exactly: same amount, a
// close date inside the file's period, and no identical row in the file. Such a transaction was probably re-labelled
// or re-dated by the bank, so the row may be the same payment.
func nearMatch(r Row, existing []Existing, inFile map[string]bool, used map[uuid.UUID]bool, start, end time.Time) *uuid.UUID {
	for _, e := range existing {
		if used[e.ID] || inFile[e.Key] || e.Amount != r.Amount || e.Date.Before(start) || e.Date.After(end) {
			continue
		}
		if days := e.Date.Sub(r.Date).Hours() / 24; days >= -nearDays && days <= nearDays {
			id := e.ID
			return &id
		}
	}
	return nil
}

func period(rows []Row) (time.Time, time.Time) {
	var start, end time.Time
	for i, r := range rows {
		if i == 0 || r.Date.Before(start) {
			start = r.Date
		}
		if i == 0 || r.Date.After(end) {
			end = r.Date
		}
	}
	return start, end
}
