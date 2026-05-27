package app

import (
	"context"
	"log/slog"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/config"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/logging"
	"github.com/Ondotteess/kleiber/internal/lsp"
	"github.com/Ondotteess/kleiber/internal/project"
)

// BufferFormatter is the narrow LSP formatting capability Session needs for
// format-on-save. *lsp.Bridge satisfies it, and tests can provide a lightweight
// fake without starting gopls.
type BufferFormatter interface {
	FormatAndSaveBuffer(ctx context.Context, id editor.BufferID, opts lsp.FormattingOptions) (int, error)
}

// Options wires the core services owned by a Session. Nil services are filled
// with safe defaults where possible; optional services stay nil.
type Options struct {
	Logger     *slog.Logger
	Config     *config.Config
	Dispatcher *commands.Dispatcher
	Editor     *editor.EditorEngine
	Project    *project.Project
	Formatter  BufferFormatter
}

// Session is the app/core composition layer between a future UI and core
// packages. It holds no global state and does not start UI or subprocesses.
type Session struct {
	logger     *slog.Logger
	cfg        config.Config
	dispatcher *commands.Dispatcher
	editor     *editor.EditorEngine
	project    *project.Project
	formatter  BufferFormatter
}

// NewSession constructs a Session. If Config is nil, config.Default is used; if
// Dispatcher or Editor are nil, defaults are constructed with the session
// logger. Project and Formatter are optional capabilities.
func NewSession(opts Options) (*Session, error) {
	logger := opts.Logger
	if logger == nil {
		logger = logging.Discard()
	}

	cfg := config.Default()
	if opts.Config != nil {
		cfg = *opts.Config
	}

	dispatcher := opts.Dispatcher
	if dispatcher == nil {
		dispatcher = commands.New(logger)
	}

	engine := opts.Editor
	if engine == nil {
		engine = editor.NewEngine(editor.EngineOptions{Logger: logger})
	}

	return &Session{
		logger:     logger,
		cfg:        cfg,
		dispatcher: dispatcher,
		editor:     engine,
		project:    opts.Project,
		formatter:  opts.Formatter,
	}, nil
}

// Dispatcher returns the command dispatcher owned by the session.
func (s *Session) Dispatcher() *commands.Dispatcher {
	return s.dispatcher
}

// Dispatch invokes a registered mutation command through the session's
// dispatcher. It is the preferred UI-facing path over exposing the raw
// dispatcher to higher layers.
func (s *Session) Dispatch(ctx context.Context, name string, args map[string]any) error {
	if s == nil || s.dispatcher == nil {
		return ErrCommandDispatcherNil
	}
	return s.dispatcher.Dispatch(ctx, name, args)
}

// Editor returns the editor engine owned by the session.
func (s *Session) Editor() *editor.EditorEngine {
	return s.editor
}

// Config returns the session configuration snapshot.
func (s *Session) Config() config.Config {
	if s == nil {
		return config.Config{}
	}
	return cloneConfig(s.cfg)
}

// Project returns the optional project model attached to the session.
func (s *Session) Project() *project.Project {
	return s.project
}

// ProjectSnapshot returns the current project snapshot when a project is
// attached. It performs no I/O.
func (s *Session) ProjectSnapshot() (project.ProjectSnapshot, bool) {
	if s == nil || s.project == nil {
		return project.ProjectSnapshot{}, false
	}
	return s.project.Snapshot(), true
}

// SubscribeEditorEvents subscribes to editor lifecycle/change events without
// exposing the editor engine itself. The returned cancel function is
// idempotent. A nil session or editor returns a closed channel and no-op cancel.
func (s *Session) SubscribeEditorEvents(buffer int) (<-chan editor.BufferEvent, func()) {
	if s == nil || s.editor == nil {
		ch := make(chan editor.BufferEvent)
		close(ch)
		return ch, func() {}
	}
	return s.editor.Events().Subscribe(buffer)
}

func cloneConfig(src config.Config) config.Config {
	out := src
	if src.LSP.InitOptions != nil {
		out.LSP.InitOptions = make(map[string]any, len(src.LSP.InitOptions))
		for k, v := range src.LSP.InitOptions {
			out.LSP.InitOptions[k] = v
		}
	}
	if src.AI.Providers != nil {
		out.AI.Providers = make(map[string]config.AIProviderConfig, len(src.AI.Providers))
		for name, provider := range src.AI.Providers {
			if provider.Extra != nil {
				extra := make(map[string]any, len(provider.Extra))
				for k, v := range provider.Extra {
					extra[k] = v
				}
				provider.Extra = extra
			}
			out.AI.Providers[name] = provider
		}
	}
	if src.KeyBinds != nil {
		out.KeyBinds = make(map[string]string, len(src.KeyBinds))
		for k, v := range src.KeyBinds {
			out.KeyBinds[k] = v
		}
	}
	return out
}
