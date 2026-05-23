// Package version reports the running Kleiber binary's version.
//
// It prefers Go's runtime/debug build information (set automatically when the
// binary is built from a tagged module or with VCS metadata embedded by the
// Go toolchain). For local development builds where neither is available it
// falls back to the literal "dev".
//
// The package has no mutable state; callers always obtain the value through
// Current. This avoids the "package-level mutable variable" footgun banned by
// docs/agents/forbidden-actions.md §10.
package version

import "runtime/debug"

// devVersion is the fallback used when neither a module version nor VCS
// metadata is available — typical for `go run` or unstamped `go build`.
const devVersion = "dev"

// shortRevisionLen caps a raw VCS revision to a human-friendly prefix (similar
// to `git rev-parse --short`).
const shortRevisionLen = 12

// Current returns a human-readable version string for the running binary.
//
// Resolution order:
//  1. Main module version reported by runtime/debug.ReadBuildInfo (e.g., "v0.1.0").
//  2. VCS revision recorded in build settings, shortened to the first twelve characters.
//  3. The literal string "dev".
func Current() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && s.Value != "" {
			if len(s.Value) > shortRevisionLen {
				return s.Value[:shortRevisionLen]
			}
			return s.Value
		}
	}
	return devVersion
}
