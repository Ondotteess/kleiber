package commands

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// ErrUnknownCommand is returned by Dispatch when no Command is registered
// under the supplied name. Compare with errors.Is.
var ErrUnknownCommand = errors.New("commands: unknown command")

// ErrDuplicateName is returned by Register when a Command with the same
// Name is already registered.
var ErrDuplicateName = errors.New("commands: duplicate command name")

// ErrEmptyName is returned by Register when a Command's Name is empty.
var ErrEmptyName = errors.New("commands: empty command name")

// ErrNilCommand is returned by Register when called with a nil Command.
var ErrNilCommand = errors.New("commands: nil command")

// Command is a discrete user-invokable action.
//
// Implementations should be cheap to construct: the dispatcher keeps the
// Command value for the lifetime of the registration, so any expensive
// state should live in the Command struct (or be obtained lazily inside
// Execute).
type Command interface {
	// Name returns the unique, stable identifier used to dispatch this
	// command — typically a dot-separated namespace
	// (for example "editor.save", "view.gotoDefinition"). The dispatcher
	// treats names as opaque strings; uniqueness is enforced at Register.
	Name() string

	// Description returns a short user-facing string for the command
	// palette. Keep it under 80 characters.
	Description() string

	// Execute performs the command. Implementations must respect ctx
	// and never block indefinitely. The args map is the caller-supplied
	// parameter bag; implementations that need typed args should
	// validate and extract them here and return a clear error on
	// mismatch.
	Execute(ctx context.Context, args map[string]any) error
}

// Dispatcher routes Commands by name and exposes them as a sorted palette.
type Dispatcher struct {
	logger *slog.Logger

	mu       sync.RWMutex
	commands map[string]Command
}

// New constructs a Dispatcher. If logger is nil the dispatcher silently
// discards log records (convenient for tests).
func New(logger *slog.Logger) *Dispatcher {
	if logger == nil {
		logger = logging.Discard()
	}
	return &Dispatcher{
		logger:   logger,
		commands: map[string]Command{},
	}
}

// Register adds cmd to the dispatcher. It returns:
//   - ErrNilCommand if cmd is nil,
//   - ErrEmptyName if cmd.Name() is "",
//   - ErrDuplicateName (wrapped with the offending name) if a Command
//     with the same name is already registered.
func (d *Dispatcher) Register(cmd Command) error {
	if cmd == nil {
		return ErrNilCommand
	}
	name := cmd.Name()
	if name == "" {
		return ErrEmptyName
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, exists := d.commands[name]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateName, name)
	}
	d.commands[name] = cmd
	d.logger.Debug("command registered", "name", name)
	return nil
}

// Unregister removes a command by name. It is a no-op if no command with
// that name is registered.
func (d *Dispatcher) Unregister(name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.commands[name]; ok {
		delete(d.commands, name)
		d.logger.Debug("command unregistered", "name", name)
	}
}

// Dispatch looks up the command by name and invokes its Execute. Errors
// from Execute are wrapped with the command name for context.
func (d *Dispatcher) Dispatch(ctx context.Context, name string, args map[string]any) error {
	d.mu.RLock()
	cmd, ok := d.commands[name]
	d.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: %s", ErrUnknownCommand, name)
	}
	d.logger.Debug("dispatching command", "name", name)
	if err := cmd.Execute(ctx, args); err != nil {
		return fmt.Errorf("executing %s: %w", name, err)
	}
	return nil
}

// Palette returns all registered commands sorted by Name. The returned
// slice is a snapshot; later registrations do not appear in it. The
// underlying Command values are shared with the dispatcher, so callers
// must not mutate them in ways that change observable behavior across
// concurrent dispatches.
func (d *Dispatcher) Palette() []Command {
	d.mu.RLock()
	cmds := make([]Command, 0, len(d.commands))
	for _, c := range d.commands {
		cmds = append(cmds, c)
	}
	d.mu.RUnlock()
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name() < cmds[j].Name() })
	return cmds
}

// Has reports whether a command with the given name is registered.
func (d *Dispatcher) Has(name string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	_, ok := d.commands[name]
	return ok
}

// Len returns the number of registered commands.
func (d *Dispatcher) Len() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return len(d.commands)
}
