// Package version holds the Kuraka release version.
package version

// Version is the current Kuraka release.
const Version = "0.1.1"

// Commit is the build's 40-hex SHA, set by -ldflags
// -X github.com/InkaNode/kuraka/internal/version.Commit=<40-hex sha>.
var Commit = ""
