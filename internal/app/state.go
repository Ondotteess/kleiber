package app

import (
	"sort"

	"github.com/Ondotteess/kleiber/internal/editor"
)

// CommandDescriptor is the UI-friendly read model for one registered command.
// It deliberately omits Execute so callers cannot mutate command behavior
// through the state API.
type CommandDescriptor struct {
	Name        string
	Description string
}

// CommandPalette returns a stable, defensive snapshot of registered commands.
func (s *Session) CommandPalette() []CommandDescriptor {
	if s == nil || s.dispatcher == nil {
		return nil
	}
	cmds := s.dispatcher.Palette()
	out := make([]CommandDescriptor, len(cmds))
	for i, cmd := range cmds {
		out[i] = CommandDescriptor{
			Name:        cmd.Name(),
			Description: cmd.Description(),
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Buffers returns a read-only snapshot of registered editor buffers.
func (s *Session) Buffers() []editor.BufferRef {
	if s == nil || s.editor == nil {
		return nil
	}
	return s.editor.Buffers()
}

// Views returns a read-only snapshot of registered editor views. A zero
// bufferID returns views for all buffers; non-zero filters to one buffer.
func (s *Session) Views(bufferID editor.BufferID) []editor.ViewRef {
	if s == nil || s.editor == nil {
		return nil
	}
	return s.editor.Views(bufferID)
}
