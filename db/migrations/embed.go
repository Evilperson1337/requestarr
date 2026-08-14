package migrations

import "embed"

// Files contains the immutable migration assets compiled into the binary.
//
//go:embed *.sql
var Files embed.FS
