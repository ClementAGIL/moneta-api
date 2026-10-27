// Package migrations embeds the goose SQL files so the binary migrates the database on startup.
package migrations

import "embed"

// FS holds every *.sql migration, applied in file name order.
//
//go:embed *.sql
var FS embed.FS
