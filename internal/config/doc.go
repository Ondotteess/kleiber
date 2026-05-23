// Package config loads and persists Kleiber's user and project
// configuration. Per ADR-005 (docs/architecture/decisions.md) the on-disk
// format is JSON.
//
// User-level config lives in the platform-specific user config directory
// reported by os.UserConfigDir:
//
//   - Linux:   $XDG_CONFIG_HOME/kleiber/config.json (or ~/.config/kleiber/...)
//   - macOS:   $HOME/Library/Application Support/kleiber/config.json
//   - Windows: %AppData%\kleiber\config.json
//
// Project-level config lives at <project_root>/.kleiber/config.json and
// overlays the user config; the Project Model loads it when a workspace
// is opened. This package itself only handles a single config file at a
// time — callers compose the user/project merge.
//
// Writes are atomic: SaveFile writes to a sibling temp file and renames
// it into place, so a process killed mid-write cannot corrupt the prior
// contents.
package config
