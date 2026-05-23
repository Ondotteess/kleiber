package commands

import "context"

// Func wraps a function as a Command. It is the simplest way to register
// behavior; for commands with non-trivial state, define a dedicated type
// that implements Command directly.
type Func struct {
	NameStr        string
	DescriptionStr string
	Fn             func(ctx context.Context, args map[string]any) error
}

// Name returns the command name.
func (f Func) Name() string { return f.NameStr }

// Description returns the command's palette description.
func (f Func) Description() string { return f.DescriptionStr }

// Execute invokes the wrapped function.
func (f Func) Execute(ctx context.Context, args map[string]any) error {
	if f.Fn == nil {
		return nil
	}
	return f.Fn(ctx, args)
}
