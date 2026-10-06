// Package version exposes build metadata for the gabitotl binary.
package version

// Version is overridden at build time via -ldflags "-X .../internal/version.Version=...".
var Version = "0.1.0-dev"

// String returns the human-readable version string.
func String() string { return "gabitotl " + Version }
