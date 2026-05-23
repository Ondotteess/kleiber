// Package lsp implements Kleiber's LSP client and gopls subprocess
// supervisor. It speaks JSON-RPC 2.0 over stdio, restarts gopls on crash,
// and surfaces diagnostics, hovers, completions, and navigation to the
// editor without ever blocking the UI thread.
//
// The intended public API is sketched in docs/architecture/components.md
// (section "LSP Client"). Implementation lands in Milestone 1, Phase 3.
package lsp
