// Package export writes everything a user owns as a ZIP (data.json + transactions.csv), decrypted: the GDPR-style
// "download my data".
package export

import (
	"archive/zip"
	"context"
	"encoding/csv"
	"encoding/json"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ClementAGIL/moneta-api/internal/accounts"
	"github.com/ClementAGIL/moneta-api/internal/auth"
	"github.com/ClementAGIL/moneta-api/internal/budgets"
	"github.com/ClementAGIL/moneta-api/internal/categories"
	"github.com/ClementAGIL/moneta-api/internal/categorize"
	"github.com/ClementAGIL/moneta-api/internal/imports"
	"github.com/ClementAGIL/moneta-api/internal/recurring"
	"github.com/ClementAGIL/moneta-api/internal/transactions"
)

const pageSize = 500

// Data is the content of data.json.
type Data struct {
	ExportedAt   time.Time                  `json:"exportedAt"`
	User         auth.User                  `json:"user"`
	Banks        []accounts.Bank            `json:"banks"`
	Accounts     []accounts.Account         `json:"accounts"`
	Categories   []categories.Category      `json:"categories"`
	Rules        []categorize.Rule          `json:"rules"`
	Budgets      []budgets.Progress         `json:"budgets"`
	Recurring    []recurring.Item           `json:"recurring"`
	Imports      []imports.Summary          `json:"imports"`
	Transactions []transactions.Transaction `json:"transactions"`
}

// Sources are the services that read each part (already wired in the server).
type Sources struct {
	DB           *pgxpool.Pool
	Auth         *auth.Service
	Categorize   *categorize.Service
	Recurring    *recurring.Service
	Imports      *imports.Service
	Transactions *transactions.Service
}

// Write streams the ZIP to w.
func Write(ctx context.Context, src Sources, userID uuid.UUID, w io.Writer) error {
	data, err := collect(ctx, src, userID)
	if err != nil {
		return err
	}
	z := zip.NewWriter(w)
	f, err := z.Create("data.json")
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		return err
	}
	f, err = z.Create("transactions.csv")
	if err != nil {
		return err
	}
	if err := writeCSV(f, data.Transactions); err != nil {
		return err
	}
	return z.Close()
}

func collect(ctx context.Context, src Sources, userID uuid.UUID) (Data, error) {
	d := Data{ExportedAt: time.Now().UTC()}
	var err error
	if d.User, err = src.Auth.Me(ctx, userID); err != nil {
		return d, err
	}
	store := accounts.NewStore(src.DB)
	if d.Banks, err = store.Banks(ctx, userID); err != nil {
		return d, err
	}
	if d.Accounts, err = store.Accounts(ctx, userID); err != nil {
		return d, err
	}
	if d.Categories, err = categories.NewStore(src.DB).List(ctx, userID); err != nil {
		return d, err
	}
	keys, err := src.Auth.Keys(ctx, userID)
	if err != nil {
		return d, err
	}
	if d.Rules, err = src.Categorize.Rules(ctx, userID, keys); err != nil {
		return d, err
	}
	if d.Budgets, err = budgets.NewStore(src.DB).Progress(ctx, userID, time.Now()); err != nil {
		return d, err
	}
	if d.Recurring, err = src.Recurring.List(ctx, userID); err != nil {
		return d, err
	}
	if d.Imports, err = src.Imports.List(ctx, userID); err != nil {
		return d, err
	}
	d.Transactions, err = allTransactions(ctx, src.Transactions, userID)
	return d, err
}

func allTransactions(ctx context.Context, svc *transactions.Service, userID uuid.UUID) ([]transactions.Transaction, error) {
	var all []transactions.Transaction
	cursor := ""
	for {
		page, err := svc.List(ctx, userID, transactions.Filter{Limit: pageSize, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		all = append(all, page.Items...)
		if page.Next == "" {
			return all, nil
		}
		cursor = page.Next
	}
}

func writeCSV(w io.Writer, txns []transactions.Transaction) error {
	c := csv.NewWriter(w)
	c.Comma = ';'
	if err := c.Write([]string{"date", "account_id", "amount", "label", "merchant", "category_id", "note"}); err != nil {
		return err
	}
	for _, t := range txns {
		category := ""
		if t.CategoryID != nil {
			category = t.CategoryID.String()
		}
		amount := strconv.FormatFloat(float64(t.Amount)/100, 'f', 2, 64)
		if err := c.Write([]string{t.Date, t.AccountID.String(), amount, t.Label, t.Merchant, category, t.Note}); err != nil {
			return err
		}
	}
	c.Flush()
	return c.Error()
}
