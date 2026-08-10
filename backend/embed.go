// Package backend aggregates the embedded web frontend.
package backend

import "embed"

//go:embed web/dist/*
var WebFS embed.FS