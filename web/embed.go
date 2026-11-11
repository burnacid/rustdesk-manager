// Package web embeds the built React admin UI (web/dist).
package web

import "embed"

// Dist holds the Vite build output. Run `npm run build` in web/ before `go build`.
//
//go:embed all:dist
var Dist embed.FS
