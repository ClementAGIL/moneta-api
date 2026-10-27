// Package forecast projects the balance to the end of the month: today's balance, plus the recurring payments and
// income still expected this month, plus everyday spending simulated by resampling the last 90 days (bootstrap),
// which gives a median path and a p25–p75 band.
package forecast

import (
	"context"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/vault"
)

const (
	historyDays = 90
	simulations = 500
	seed        = 42 // same data → same forecast
)

// Day is the projected balance at the end of a day.
type Day struct {
	Date string `json:"date"`
	P25  int64  `json:"p25Cents"`
	P50  int64  `json:"p50Cents"`
	P75  int64  `json:"p75Cents"`
}

// Expected is a recurring item still due this month.
type Expected struct {
	Name   string `json:"name"`
	Date   string `json:"date"`
	Amount int64  `json:"amountCents"`
}

// Forecast is the GET /forecast response.
type Forecast struct {
	AccountID *uuid.UUID `json:"accountId"`
	Today     string     `json:"today"`
	Balance   int64      `json:"balanceCents"`
	Expected  []Expected `json:"expected"`
	Days      []Day      `json:"days"`
	End       Day        `json:"endOfMonth"`
}

type KeySource interface {
	Keys(ctx context.Context, userID uuid.UUID) (*vault.UserKeys, error)
}

type Service struct {
	db   *pgxpool.Pool
	keys KeySource
}

func NewService(db *pgxpool.Pool, keys KeySource) *Service { return &Service{db: db, keys: keys} }

// Month forecasts one account, or every current/card account when accountID is nil.
func (s *Service) Month(ctx context.Context, userID uuid.UUID, accountID *uuid.UUID, today time.Time) (Forecast, error) {
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	f := Forecast{AccountID: accountID, Today: today.Format(time.DateOnly), Expected: []Expected{}}
	var err error
	if f.Balance, err = s.balance(ctx, userID, accountID); err != nil {
		return f, err
	}
	if f.Expected, err = s.expected(ctx, userID, accountID, today); err != nil {
		return f, err
	}
	daily, err := s.dailyNet(ctx, userID, accountID, today)
	if err != nil {
		return f, err
	}
	f.Days = simulate(f.Balance, today, f.Expected, daily)
	if len(f.Days) > 0 {
		f.End = f.Days[len(f.Days)-1]
	} else {
		f.End = Day{Date: f.Today, P25: f.Balance, P50: f.Balance, P75: f.Balance}
	}
	return f, nil
}

// simulate runs the bootstrap: each remaining day draws one past day's net amount, recurring items are added on
// their date, and each day keeps the 25th, 50th and 75th percentile of the simulated balances.
func simulate(balance int64, today time.Time, expected []Expected, daily []int64) []Day {
	end := time.Date(today.Year(), today.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	byDate := map[string]int64{}
	for _, e := range expected {
		byDate[e.Date] += e.Amount
	}
	rng := rand.New(rand.NewPCG(seed, seed))
	var days []Day
	paths := make([]int64, simulations)
	for i := range paths {
		paths[i] = balance
	}
	for d := today.AddDate(0, 0, 1); d.Before(end); d = d.AddDate(0, 0, 1) {
		date := d.Format(time.DateOnly)
		for i := range paths {
			if len(daily) > 0 {
				paths[i] += daily[rng.IntN(len(daily))]
			}
			paths[i] += byDate[date]
		}
		days = append(days, percentiles(date, paths))
	}
	return days
}

func percentiles(date string, paths []int64) Day {
	sorted := slices.Clone(paths)
	slices.Sort(sorted)
	at := func(p float64) int64 { return sorted[int(p*float64(len(sorted)-1))] }
	return Day{Date: date, P25: at(0.25), P50: at(0.5), P75: at(0.75)}
}
