package migrations

import "embed"

// Files contains the ordered SQLite migrations.
//
//go:embed *.sql
var Files embed.FS
