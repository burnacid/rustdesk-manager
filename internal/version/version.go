// Package version holds the release version, stamped in at build time:
//
//	go build -ldflags "-X rustdesk-manager/internal/version.Version=$(cat VERSION)" ./cmd/server
//
// The Dockerfile does this from the VERSION file. Plain `go run` reports "dev".
package version

// Version is the release, e.g. "0.1.0".
var Version = "dev"
