package config

// Config is the in-memory representation of Kleiber's settings.
//
// New fields must be added at the bottom of their sub-struct to keep JSON
// diffs stable. Removing or renaming a field is a breaking change to the
// on-disk format and requires the same human approval as a public API
// change (see docs/agents/forbidden-actions.md §6).
type Config struct {
	Editor   EditorConfig      `json:"editor"`
	LSP      LSPConfig         `json:"lsp"`
	AI       AIConfig          `json:"ai"`
	Logging  LoggingConfig     `json:"logging"`
	KeyBinds map[string]string `json:"keybinds,omitempty"`
}

// EditorConfig holds Editor-Engine preferences.
type EditorConfig struct {
	TabSize      int    `json:"tabSize"`
	InsertSpaces bool   `json:"insertSpaces"`
	FontFamily   string `json:"fontFamily"`
	FontSize     int    `json:"fontSize"`
	Theme        string `json:"theme"`
}

// LSPConfig holds gopls-related settings.
type LSPConfig struct {
	// Path is the absolute path to the gopls binary. Empty means
	// "use exec.LookPath on PATH".
	Path string `json:"path,omitempty"`

	// InitOptions are forwarded verbatim as gopls.initializationOptions.
	InitOptions map[string]any `json:"initializationOptions,omitempty"`
}

// AIConfig holds AI Bridge preferences. The default in v1 is no provider
// configured; AI features stay dormant until the user opts in.
type AIConfig struct {
	DefaultProvider string                      `json:"defaultProvider,omitempty"`
	Providers       map[string]AIProviderConfig `json:"providers,omitempty"`
}

// AIProviderConfig holds settings shared across providers. Provider-
// specific fields live in Extra to keep the schema open.
type AIProviderConfig struct {
	Model       string         `json:"model,omitempty"`
	BaseURL     string         `json:"baseUrl,omitempty"`
	Temperature float64        `json:"temperature,omitempty"`
	Extra       map[string]any `json:"extra,omitempty"`
}

// LoggingConfig configures the slog handler the binary builds at start.
type LoggingConfig struct {
	Level     string `json:"level"`
	Format    string `json:"format"`
	AddSource bool   `json:"addSource"`
}

// Default returns a Config populated with conservative defaults. The
// returned value is a fresh copy; callers may mutate it freely without
// affecting future Default calls.
func Default() Config {
	return Config{
		Editor: EditorConfig{
			TabSize:      4,
			InsertSpaces: false,
			FontFamily:   "monospace",
			FontSize:     13,
			Theme:        "default",
		},
		LSP: LSPConfig{},
		AI: AIConfig{
			Providers: map[string]AIProviderConfig{},
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		KeyBinds: map[string]string{},
	}
}
