// Package app composes Kleiber's core services for command-driven callers.
//
// The package is intentionally UI-free. It owns cross-component command
// registration and small orchestration policies, such as config-gated
// format-on-save, so editor, project, and LSP packages can stay focused on
// their own state and protocols.
package app
