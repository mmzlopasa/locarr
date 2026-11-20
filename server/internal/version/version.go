// Package version holds build information set with -ldflags at build time.
package version

// Version and Commit are replaced at build time:
//
//	-X github.com/mmzlopasa/locarr/server/internal/version.Version=1.2.3
//	-X github.com/mmzlopasa/locarr/server/internal/version.Commit=<sha>
var (
	Version = "dev"
	Commit  = "unknown"
)
