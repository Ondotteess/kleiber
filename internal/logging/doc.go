// Package logging constructs *slog.Logger instances for Kleiber components.
//
// Kleiber follows docs/contributing/coding-standards.md §3.3 "No globals":
// every component accepts a *slog.Logger in its constructor. The helpers
// in this package centralize the choice of handler (JSON or text), level,
// output writer, and the parsing of human-friendly level/format strings
// from configuration files. They do not own a singleton.
package logging
