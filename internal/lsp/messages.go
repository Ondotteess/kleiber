package lsp

// This file defines the subset of LSP types Kleiber uses today. Each
// type maps directly to a structure in the Language Server Protocol
// specification (microsoft/language-server-protocol). Field names mirror
// the spec exactly via json tags so that wire format stays predictable.
//
// Scope is intentionally narrow: we only declare what PR-2 needs
// (initialize/initialized handshake, didOpen, publishDiagnostics, one
// hover request to prove correlation). Adding new methods is additive —
// declare more types here, never mutate existing ones in incompatible
// ways (see docs/agents/forbidden-actions.md §6).

import "encoding/json"

// DocumentURI is the URI of a document, e.g., "file:///home/u/main.go".
// LSP declares it as a typedef of string; we name the type for clarity.
type DocumentURI string

// Position is a zero-based (line, character) location in a document.
// Both axes are zero-indexed per LSP spec §"Position".
//
// Character offsets are UTF-16 code units by default in LSP; gopls
// follows that convention. Callers converting between byte offsets and
// LSP positions must handle the encoding mismatch separately.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open interval [Start, End) in a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location identifies a position within a document.
type Location struct {
	URI   DocumentURI `json:"uri"`
	Range Range       `json:"range"`
}

// TextDocumentIdentifier identifies a document by URI only.
type TextDocumentIdentifier struct {
	URI DocumentURI `json:"uri"`
}

// TextDocumentItem is a full snapshot of a document the client sends to
// the server on didOpen.
type TextDocumentItem struct {
	URI        DocumentURI `json:"uri"`
	LanguageID string      `json:"languageId"`
	Version    int         `json:"version"`
	Text       string      `json:"text"`
}

// TextDocumentSyncKind enumerates how the client tells the server about
// document changes. Kleiber currently negotiates Full (resend the entire
// text on every change), since the editor engine that produces
// incremental edits is not yet in place.
type TextDocumentSyncKind int

// TextDocumentSyncKind constants.
const (
	// TextDocumentSyncNone means the server does not want change
	// notifications.
	TextDocumentSyncNone TextDocumentSyncKind = 0

	// TextDocumentSyncFull means the client sends the complete text
	// on each change.
	TextDocumentSyncFull TextDocumentSyncKind = 1

	// TextDocumentSyncIncremental means the client sends only the
	// changed regions.
	TextDocumentSyncIncremental TextDocumentSyncKind = 2
)

// DiagnosticSeverity classifies a diagnostic by urgency. Values follow
// LSP spec §"Diagnostic" exactly so JSON marshaling is a no-op.
type DiagnosticSeverity int

// DiagnosticSeverity constants.
const (
	// DiagnosticSeverityError is a hard error — code is broken.
	DiagnosticSeverityError DiagnosticSeverity = 1

	// DiagnosticSeverityWarning is something probably wrong.
	DiagnosticSeverityWarning DiagnosticSeverity = 2

	// DiagnosticSeverityInformation is purely informational.
	DiagnosticSeverityInformation DiagnosticSeverity = 3

	// DiagnosticSeverityHint is the lowest severity — usually a
	// stylistic or refactoring suggestion.
	DiagnosticSeverityHint DiagnosticSeverity = 4
)

// String renders a DiagnosticSeverity in lower-case for logs.
func (s DiagnosticSeverity) String() string {
	switch s {
	case DiagnosticSeverityError:
		return "error"
	case DiagnosticSeverityWarning:
		return "warning"
	case DiagnosticSeverityInformation:
		return "info"
	case DiagnosticSeverityHint:
		return "hint"
	default:
		return "unknown"
	}
}

// Diagnostic is one problem detected by the server in a document. The
// codec leaves Code and CodeDescription opaque (the spec allows string
// or int for Code); callers that need them can decode the JSON further.
type Diagnostic struct {
	Range    Range              `json:"range"`
	Severity DiagnosticSeverity `json:"severity,omitempty"`
	Code     json.RawMessage    `json:"code,omitempty"`
	Source   string             `json:"source,omitempty"`
	Message  string             `json:"message"`
}

// ClientInfo identifies the editor to the server, optionally with a
// version string.
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerInfo identifies the language server back to the client.
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// WorkspaceFolder describes one of the workspace roots opened in the
// editor.
type WorkspaceFolder struct {
	URI  DocumentURI `json:"uri"`
	Name string      `json:"name"`
}

// TextDocumentSyncClientCapabilities lists which document-sync features
// the client can handle. We omit fields we do not yet use.
type TextDocumentSyncClientCapabilities struct {
	// DynamicRegistration is false: we negotiate capabilities once
	// at initialize and do not support runtime re-registration.
	DynamicRegistration bool `json:"dynamicRegistration"`
}

// PublishDiagnosticsClientCapabilities tells the server what the client
// can do with diagnostics. Trimmed to bare minimum for v1.
type PublishDiagnosticsClientCapabilities struct {
	// VersionSupport reports diagnostics tagged with a document
	// version. We accept them but do nothing special yet.
	VersionSupport bool `json:"versionSupport,omitempty"`
}

// HoverClientCapabilities advertises what the client can render in a
// hover popup. We accept plaintext only for now; gopls will downgrade
// markdown content automatically.
type HoverClientCapabilities struct {
	ContentFormat []MarkupKind `json:"contentFormat,omitempty"`
}

// TextDocumentClientCapabilities collects per-document capabilities.
type TextDocumentClientCapabilities struct {
	Synchronization    *TextDocumentSyncClientCapabilities   `json:"synchronization,omitempty"`
	PublishDiagnostics *PublishDiagnosticsClientCapabilities `json:"publishDiagnostics,omitempty"`
	Hover              *HoverClientCapabilities              `json:"hover,omitempty"`
}

// WorkspaceClientCapabilities is currently empty — we do not yet
// negotiate workspace-wide features. Declared so that the struct shape
// matches the spec and so we can extend it without renaming.
type WorkspaceClientCapabilities struct {
	// WorkspaceFolders advertises that we send workspaceFolders at
	// initialize time. gopls expects this to be true.
	WorkspaceFolders bool `json:"workspaceFolders,omitempty"`
}

// ClientCapabilities is the client's full capability bundle sent in
// initialize.
type ClientCapabilities struct {
	Workspace    *WorkspaceClientCapabilities    `json:"workspace,omitempty"`
	TextDocument *TextDocumentClientCapabilities `json:"textDocument,omitempty"`
}

// TextDocumentSyncOptions is one of the two valid shapes for the
// server's textDocumentSync capability (the other is a bare
// TextDocumentSyncKind integer; we decode both — see ServerCapabilities).
type TextDocumentSyncOptions struct {
	OpenClose bool                 `json:"openClose"`
	Change    TextDocumentSyncKind `json:"change"`
}

// ServerCapabilities is what the server advertises in its initialize
// response. We pull out only what we drive against today; new fields
// can be added without breaking older callers.
type ServerCapabilities struct {
	// TextDocumentSync is left as RawMessage because the spec allows
	// either an integer (TextDocumentSyncKind) or an object
	// (TextDocumentSyncOptions). Helpers below decode either form.
	TextDocumentSync json.RawMessage `json:"textDocumentSync,omitempty"`

	// HoverProvider is either a boolean or an options object. We
	// keep it opaque; Client only checks for truthiness.
	HoverProvider json.RawMessage `json:"hoverProvider,omitempty"`
}

// InitializeParams are the params sent on the initialize request.
type InitializeParams struct {
	// ProcessID is the parent process id (i.e., this editor's PID) or
	// nil to mean "no parent". Pointer so we can emit JSON null.
	ProcessID *int `json:"processId"`

	// ClientInfo advertises Kleiber's identity to the server.
	ClientInfo *ClientInfo `json:"clientInfo,omitempty"`

	// RootURI is deprecated by the spec in favor of WorkspaceFolders
	// but gopls still inspects it on some code paths. Pointer so we
	// can serialize null when absent.
	RootURI *DocumentURI `json:"rootUri"`

	// Capabilities is the negotiated client feature set.
	Capabilities ClientCapabilities `json:"capabilities"`

	// WorkspaceFolders is the list of open project roots. Nil means
	// no workspace.
	WorkspaceFolders []WorkspaceFolder `json:"workspaceFolders"`

	// Trace controls verbose protocol logging on the server side
	// ("off" / "messages" / "verbose"). Empty defaults to "off".
	Trace string `json:"trace,omitempty"`
}

// InitializeResult is the server's response to initialize.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   *ServerInfo        `json:"serverInfo,omitempty"`
}

// InitializedParams is the (empty) payload of the "initialized"
// notification.
type InitializedParams struct{}

// DidOpenTextDocumentParams is the payload for textDocument/didOpen.
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// PublishDiagnosticsParams is the payload of
// textDocument/publishDiagnostics — server pushes these when it has new
// findings for a document.
type PublishDiagnosticsParams struct {
	URI         DocumentURI  `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// HoverParams is the payload for textDocument/hover.
type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// MarkupKind identifies the format of hover and similar content.
type MarkupKind string

// MarkupKind constants.
const (
	// MarkupKindPlainText is utf-8 plaintext.
	MarkupKindPlainText MarkupKind = "plaintext"

	// MarkupKindMarkdown is CommonMark plus extensions per the LSP
	// spec — code fences carry hover-snippet language tags.
	MarkupKindMarkdown MarkupKind = "markdown"
)

// MarkupContent is a (kind, value) pair holding hover, signature-help,
// or code-action explanation text.
type MarkupContent struct {
	Kind  MarkupKind `json:"kind"`
	Value string     `json:"value"`
}

// Hover is the result of a successful textDocument/hover request.
//
// LSP also allows the legacy MarkedString / MarkedString[] shapes for
// Contents; gopls emits MarkupContent today, so we decode only that.
// If we ever encounter a server that still emits legacy shapes we will
// add a custom UnmarshalJSON; for now decoding will fail clearly.
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// LSP method names we send or receive. Constants are cheaper to grep
// for and easier to keep consistent than string literals scattered
// across the call sites.
const (
	MethodInitialize            = "initialize"
	MethodInitialized           = "initialized"
	MethodShutdown              = "shutdown"
	MethodExit                  = "exit"
	MethodTextDocumentDidOpen   = "textDocument/didOpen"
	MethodTextDocumentDidChange = "textDocument/didChange"
	MethodTextDocumentDidClose  = "textDocument/didClose"
	MethodTextDocumentHover     = "textDocument/hover"
	MethodPublishDiagnostics    = "textDocument/publishDiagnostics"
	MethodWindowLogMessage      = "window/logMessage"
	MethodWindowShowMessage     = "window/showMessage"
	MethodClientRegisterCap     = "client/registerCapability"
	MethodClientUnregisterCap   = "client/unregisterCapability"
)
