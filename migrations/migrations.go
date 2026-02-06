// Package migrations embeds SQL migration files for goose.
//
// Migration files follow the naming convention: YYYYMMDDHHMMSS_description.sql
// They are applied in order during database initialization.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
