// Package buildinfo stores build-time metadata shared across packages.
package buildinfo

// Version is set via ldflags during build. Defaults to "dev".
var Version = "dev"

// Commit is set via ldflags during build. Defaults to "none".
var Commit = "none"
