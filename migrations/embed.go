package migrations

import "embed"

// FS contains explicitly executed SQL migrations; API startup never migrates.
//
//go:embed *.sql
var FS embed.FS
