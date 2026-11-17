// Package version holds the Kuraka release version.
package version

// Version is the current Kuraka release.
const Version = "0.4.0"

// Commit is the build's 40-hex SHA, set by -ldflags
// -X github.com/InkaNode/kuraka/internal/version.Commit=<40-hex sha>.
var Commit = ""

// LibredeskCommit is the 40-hex SHA of the last commit that changed
// patches/libredesk before Commit, set by -ldflags
// -X github.com/InkaNode/kuraka/internal/version.LibredeskCommit=<40-hex sha>.
var LibredeskCommit = ""
