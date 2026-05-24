// Package lsp implements Kleiber's bridge to gopls.
//
// The package is layered so each piece is independently testable:
//
//   - Conn (jsonrpc.go) is a JSON-RPC 2.0 codec over the LSP base-protocol
//     framing (Content-Length header + CRLF + UTF-8 body). It owns no
//     subprocess, no LSP semantics, and no high-level state — just
//     bytes-in / Message-out and Message-in / bytes-out. Read is single-
//     goroutine; Write is safe for concurrent use.
//
//   - Process (process.go) is a generic supervised subprocess that exposes
//     its stdin/stdout as io streams. It knows nothing about JSON-RPC or
//     LSP — Conn wraps Process's stdio to form the wire.
//
//   - Client (planned, separate PR) layers LSP semantics on top of Conn:
//     initialize/initialized handshake, textDocument/didOpen, dispatch of
//     publishDiagnostics into internal/events, and request/response
//     correlation. See docs/architecture/components.md.
//
// On crash isolation: a dead gopls must not take the editor down with
// it (docs/architecture/overview.md §"Design principles"). Process
// surfaces exits through Wait and Stop; supervision policy (restart,
// circuit-breaker) is a Client concern handled in a later PR.
package lsp
