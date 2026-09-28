package migrations

import "embed"

//go:embed *.sql atlas.sum
var Files embed.FS
