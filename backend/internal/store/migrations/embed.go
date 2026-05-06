package migrations

import "embed"

// FS holds all goose migration files. Used by cmd/migrate to apply schema
// changes against a Postgres URL without depending on the filesystem layout
// at runtime.
//
//go:embed *.sql
var FS embed.FS
