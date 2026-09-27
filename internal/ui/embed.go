// Package ui embeds the admin UI that frontend/ builds into build/; run
// `make ui` (or the Docker build) to fill it.
package ui

import "embed"

//go:embed all:build
var Build embed.FS
