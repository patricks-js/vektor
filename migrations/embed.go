package migrations

import "embed"

// FS embeds versioned SQL migrations shipped inside the binary.
//
//go:embed *.sql
var FS embed.FS
