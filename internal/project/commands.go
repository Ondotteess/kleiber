package project

import (
	"context"
	"errors"

	"github.com/Ondotteess/kleiber/internal/commands"
)

const (
	// CommandRefresh reloads project module/package metadata from disk.
	CommandRefresh = "project.refresh"
)

var (
	// ErrCommandDispatcherNil is returned when command registration is
	// attempted without a dispatcher.
	ErrCommandDispatcherNil = errors.New("project: command dispatcher is nil")

	// ErrCommandProjectNil is returned when command registration is attempted
	// without a project.
	ErrCommandProjectNil = errors.New("project: command project is nil")
)

// RegisterCommands registers project-owned commands with d. Project metadata
// reads use Project.Snapshot directly until the dispatcher grows result values.
func RegisterCommands(d *commands.Dispatcher, p *Project) error {
	if d == nil {
		return ErrCommandDispatcherNil
	}
	if p == nil {
		return ErrCommandProjectNil
	}
	return d.Register(commands.Func{
		NameStr:        CommandRefresh,
		DescriptionStr: "Refresh project metadata",
		Fn: func(ctx context.Context, args map[string]any) error {
			return p.Refresh(ctx)
		},
	})
}
