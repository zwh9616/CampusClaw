// Package migrations embeds the ordered SQL migrations and applies them.
package migrations

import "embed"

// files holds every migration script, compiled into the binary so the API
// image needs no separate schema directory.
//
//go:embed *.sql
var files embed.FS
