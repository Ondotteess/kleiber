package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

const toolchainCheckName = "toolchain"

// ToolchainCheck compares the running Go toolchain against the version
// required by the project's go.mod file. It walks up from the project
// root looking for the nearest go.mod.
//
// runtimeVersion is exposed for tests; production callers leave it nil
// and the check uses runtime.Version() from the standard library.
type ToolchainCheck struct {
	// runtimeVersion overrides runtime.Version() when non-nil. The
	// returned value is parsed exactly like runtime.Version(), i.e.,
	// "go1.25.0" or "go1.21rc1".
	runtimeVersion func() string
}

// NewToolchainCheck constructs the toolchain check.
func NewToolchainCheck() *ToolchainCheck { return &ToolchainCheck{} }

// Name returns the canonical check name.
func (*ToolchainCheck) Name() string { return toolchainCheckName }

// Run locates the project's go.mod, parses its go directive, and
// compares with the running toolchain.
func (c *ToolchainCheck) Run(_ context.Context, root string) Finding {
	goMod, err := findGoMod(root)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Finding{
				CheckName: c.Name(),
				Severity:  SeverityWarning,
				Title:     "no go.mod found at or above " + root,
				Hint:      "run `go mod init <module-path>` if this is a new project",
			}
		}
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     "locating go.mod",
			Detail:    err.Error(),
		}
	}

	data, err := os.ReadFile(goMod)
	if err != nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     "reading go.mod",
			Detail:    err.Error(),
		}
	}

	parsed, err := modfile.Parse(goMod, data, nil)
	if err != nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     "parsing go.mod",
			Detail:    err.Error(),
		}
	}

	declared := ""
	if parsed.Go != nil {
		declared = parsed.Go.Version
	}

	runningVer := strings.TrimPrefix(c.runtime(), "go")

	if declared == "" {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityInfo,
			Title:     "go.mod has no go directive",
			Detail:    "the Go toolchain will assume the oldest compatible version",
			Hint:      "add a go directive matching your toolchain",
			Fixes: []FixAction{{
				Label:   "Add go directive",
				Command: fmt.Sprintf("go mod edit -go=%s", runningVer),
			}},
		}
	}

	cmp, err := compareGoVersion(runningVer, declared)
	if err != nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityWarning,
			Title:     "unable to compare Go versions",
			Detail:    fmt.Sprintf("running %q vs declared %q: %v", runningVer, declared, err),
		}
	}

	switch {
	case cmp < 0:
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     fmt.Sprintf("Go toolchain too old: have %s, go.mod requires %s", runningVer, declared),
			Hint:      "upgrade Go to " + declared + " or newer",
			Fixes: []FixAction{{
				Label:   "Download Go " + declared,
				Command: "https://go.dev/dl/",
			}},
		}
	case cmp > 0:
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityOK,
			Title:     fmt.Sprintf("Go %s (go.mod requires %s; newer is fine)", runningVer, declared),
		}
	default:
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityOK,
			Title:     fmt.Sprintf("Go %s matches go.mod", runningVer),
		}
	}
}

// runtime returns the running Go version, overridable for tests.
func (c *ToolchainCheck) runtime() string {
	if c.runtimeVersion != nil {
		return c.runtimeVersion()
	}
	return runtime.Version()
}

// findGoMod walks up from root looking for go.mod. Returns fs.ErrNotExist
// (sentinel-compatible via errors.Is) when none is found.
func findGoMod(root string) (string, error) {
	dir := root
	for {
		candidate := filepath.Join(dir, "go.mod")
		info, err := os.Stat(candidate)
		switch {
		case err == nil && !info.IsDir():
			return candidate, nil
		case err != nil && !errors.Is(err, fs.ErrNotExist):
			return "", fmt.Errorf("stat %s: %w", candidate, err)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fs.ErrNotExist
		}
		dir = parent
	}
}

// compareGoVersion compares two Go version strings ("1.25" vs "1.23",
// or "1.25.0" vs "1.25.1"). Returns negative if a < b, zero if equal,
// positive if a > b.
//
// Versions are dot-separated integer components. Any pre-release suffix
// ("rc1", "-beta", "+anything") on either side is ignored — Kleiber's
// goal here is "is the toolchain new enough", which is a coarse
// numeric comparison.
func compareGoVersion(a, b string) (int, error) {
	aParts, err := parseGoVersion(a)
	if err != nil {
		return 0, err
	}
	bParts, err := parseGoVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < len(aParts) || i < len(bParts); i++ {
		ap, bp := 0, 0
		if i < len(aParts) {
			ap = aParts[i]
		}
		if i < len(bParts) {
			bp = bParts[i]
		}
		if ap != bp {
			return ap - bp, nil
		}
	}
	return 0, nil
}

// parseGoVersion turns "1.25.0" or "1.25rc1" into a slice of integer
// components ([1,25,0] or [1,25] respectively). A trailing pre-release
// segment is dropped.
func parseGoVersion(v string) ([]int, error) {
	// Drop anything from the first non-digit, non-dot character on.
	for i, r := range v {
		if (r >= '0' && r <= '9') || r == '.' {
			continue
		}
		v = v[:i]
		break
	}
	if v == "" {
		return nil, fmt.Errorf("empty version")
	}
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("empty component in %q", v)
		}
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("component %q: %w", p, err)
		}
		out = append(out, n)
	}
	return out, nil
}
