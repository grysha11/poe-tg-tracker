// Package migrations holds the goose SQL migrations, embedded so the migrate
// binary always carries exactly the schema its image version expects.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
