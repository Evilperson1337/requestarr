// Package migrations embeds the SQL migration files so the application binary
// can apply them without reading files from disk at runtime. The embed
// directive must live beside the .sql files because Go embed patterns cannot
// reach outside the package directory.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
