// Package migrations embeds the SQL files so the API applies them on start.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
