package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

const toolsCheckName = "tools"

// ToolSpec describes one external binary the check probes for.
type ToolSpec struct {
	// Name is the binary name as it should appear on PATH.
	Name string

	// InstallCmd is a copy-paste shell command that installs the
	// tool. Shown to the user as a Fix when the tool is missing.
	InstallCmd string

	// VersionArgs are the arguments passed to the binary to print
	// its version. Empty means call the binary with no args (some
	// tools print version by default; most use "version").
	VersionArgs []string
}

// ToolsCheck verifies that the external binaries Kleiber relies on are
// available on PATH and reports their versions if found.
//
// lookPath and versionOf are exposed so tests can stub them; production
// callers leave them nil and the check uses exec.LookPath plus a
// subprocess version probe.
type ToolsCheck struct {
	tools []ToolSpec

	lookPath  func(name string) (string, error)
	versionOf func(ctx context.Context, path string, args []string) (string, error)
}

// NewToolsCheck builds the default check for gopls and dlv.
func NewToolsCheck() *ToolsCheck {
	return &ToolsCheck{
		tools: defaultToolSpecs(),
	}
}

// defaultToolSpecs is the canonical list. Exposed only to tests via
// the package because callers should usually go through NewToolsCheck.
func defaultToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "gopls",
			InstallCmd:  "go install golang.org/x/tools/gopls@latest",
			VersionArgs: []string{"version"},
		},
		{
			Name:        "dlv",
			InstallCmd:  "go install github.com/go-delve/delve/cmd/dlv@latest",
			VersionArgs: []string{"version"},
		},
	}
}

// Name returns the canonical check name.
func (*ToolsCheck) Name() string { return toolsCheckName }

// Run probes each tool. The project root is irrelevant for this check.
func (c *ToolsCheck) Run(ctx context.Context, _ string) Finding {
	lookPath := c.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	versionOf := c.versionOf
	if versionOf == nil {
		versionOf = defaultVersionOf
	}

	var present []string
	var missing []ToolSpec
	for _, t := range c.tools {
		bin, err := lookPath(t.Name)
		if err != nil {
			missing = append(missing, t)
			continue
		}
		v, _ := versionOf(ctx, bin, t.VersionArgs) // version probe failure is non-fatal
		if v == "" {
			present = append(present, fmt.Sprintf("%s (at %s)", t.Name, bin))
		} else {
			present = append(present, fmt.Sprintf("%s %s", t.Name, v))
		}
	}

	if len(missing) == 0 {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityOK,
			Title:     "external tools available",
			Detail:    strings.Join(present, "\n"),
		}
	}

	var detail strings.Builder
	if len(present) > 0 {
		detail.WriteString("found:\n  - ")
		detail.WriteString(strings.Join(present, "\n  - "))
		detail.WriteString("\n\n")
	}
	detail.WriteString("missing:\n")
	fixes := make([]FixAction, 0, len(missing))
	missingNames := make([]string, 0, len(missing))
	for _, t := range missing {
		detail.WriteString("  - " + t.Name + "\n")
		missingNames = append(missingNames, t.Name)
		fixes = append(fixes, FixAction{
			Label:   "Install " + t.Name,
			Command: t.InstallCmd,
		})
	}

	return Finding{
		CheckName: c.Name(),
		Severity:  SeverityWarning,
		Title:     "missing tools: " + strings.Join(missingNames, ", "),
		Detail:    strings.TrimRight(detail.String(), "\n"),
		Hint:      "install the listed tools to unlock LSP / debugger features",
		Fixes:     fixes,
	}
}

// defaultVersionOf runs `path <args...>` and extracts a version token
// from the combined stdout+stderr. Returns empty string if no version
// token is identifiable.
func defaultVersionOf(ctx context.Context, path string, args []string) (string, error) {
	cmd := exec.CommandContext(ctx, path, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return extractVersion(buf.String()), nil
}

// extractVersion pulls the first version-shaped token out of tool
// version output. A version token is one or more digit-or-dot
// characters (optionally with a leading "v"), terminated cleanly or
// by a "-" / "+" pre-release marker.
//
// Examples handled:
//
//	gopls: "golang.org/x/tools/gopls v0.22.0"  →  "v0.22.0"
//	dlv:   "Delve Debugger\nVersion: 1.21.0..." →  "1.21.0"
//
// Returns "" if nothing version-shaped is found.
func extractVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		for _, tok := range strings.Fields(line) {
			tok = strings.TrimRight(tok, ".,;:")
			if isVersionToken(tok) {
				return tok
			}
		}
	}
	return ""
}

// isVersionToken reports whether s looks like a semantic-version
// fragment: optional leading "v", at least one digit, at least one
// dot, and no other characters before any "-" / "+" suffix.
func isVersionToken(s string) bool {
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return false
	}
	seenDigit := false
	seenDot := false
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
		case r == '.':
			seenDot = true
		case r == '-' || r == '+':
			return seenDigit && seenDot
		default:
			return false
		}
	}
	return seenDigit && seenDot
}
