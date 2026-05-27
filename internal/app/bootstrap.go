package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/Ondotteess/kleiber/internal/commands"
	"github.com/Ondotteess/kleiber/internal/config"
	"github.com/Ondotteess/kleiber/internal/editor"
	"github.com/Ondotteess/kleiber/internal/logging"
	"github.com/Ondotteess/kleiber/internal/project"
)

// DefaultSessionOptions configures NewDefaultSession. It is the bootstrap
// boundary a future UI can call to receive a command-registered core session.
type DefaultSessionOptions struct {
	Logger      *slog.Logger
	LogWriter   io.Writer
	Config      *config.Config
	ConfigPath  string
	Dispatcher  *commands.Dispatcher
	Editor      *editor.EditorEngine
	Project     *project.Project
	ProjectRoot string
	Formatter   BufferFormatter
}

// NewDefaultSession builds a command-ready Session from default core services.
// It loads user config when Config is nil, opens ProjectRoot when supplied, and
// registers app-owned commands exactly once. It does not start UI or gopls.
func NewDefaultSession(ctx context.Context, opts DefaultSessionOptions) (*Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg, err := bootstrapConfig(opts)
	if err != nil {
		return nil, err
	}
	logger, err := bootstrapLogger(cfg, opts)
	if err != nil {
		return nil, err
	}
	p, err := bootstrapProject(ctx, logger, opts)
	if err != nil {
		return nil, err
	}

	s, err := NewSession(Options{
		Logger:     logger,
		Config:     &cfg,
		Dispatcher: opts.Dispatcher,
		Editor:     opts.Editor,
		Project:    p,
		Formatter:  opts.Formatter,
	})
	if err != nil {
		return nil, err
	}
	if err := s.RegisterCommands(); err != nil {
		return nil, err
	}
	return s, nil
}

func bootstrapConfig(opts DefaultSessionOptions) (config.Config, error) {
	if opts.Config != nil {
		return *opts.Config, nil
	}

	var (
		cfg config.Config
		err error
	)
	if opts.ConfigPath != "" {
		cfg, err = config.LoadFile(opts.ConfigPath)
	} else {
		cfg, err = config.Load()
	}
	if err != nil && !errors.Is(err, config.ErrNotFound) {
		return config.Config{}, err
	}
	return cfg, nil
}

func bootstrapLogger(cfg config.Config, opts DefaultSessionOptions) (*slog.Logger, error) {
	if opts.Logger != nil {
		return opts.Logger, nil
	}
	level, err := logging.ParseLevel(cfg.Logging.Level)
	if err != nil {
		return nil, err
	}
	format, err := logging.ParseFormat(cfg.Logging.Format)
	if err != nil {
		return nil, err
	}
	return logging.New(logging.Options{
		Level:     level,
		Format:    format,
		Writer:    opts.LogWriter,
		AddSource: cfg.Logging.AddSource,
	}), nil
}

func bootstrapProject(ctx context.Context, logger *slog.Logger, opts DefaultSessionOptions) (*project.Project, error) {
	if opts.Project != nil {
		return opts.Project, nil
	}
	if opts.ProjectRoot == "" {
		return nil, nil
	}
	p, err := project.Open(ctx, opts.ProjectRoot, project.Options{Logger: logger})
	if err != nil {
		return nil, fmt.Errorf("app: opening project %s: %w", opts.ProjectRoot, err)
	}
	return p, nil
}
