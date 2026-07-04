package doctor

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

const goBinaryCheckName = "go-binary"

// GoBinaryCheck verifies that the `go` command is on PATH. Kleiber shells
// out to `go` for nearly everything — go/packages project loading, gopls,
// and the build/test commands — so its absence is the most common and
// most confusing first-run failure, and it deserves an explicit,
// actionable finding rather than a downstream "go list" error.
//
// lookPath and versionOf are test seams; production leaves them nil and
// the check uses exec.LookPath plus a `go version` probe.
type GoBinaryCheck struct {
	lookPath  func(name string) (string, error)
	versionOf func(ctx context.Context, path string) (string, error)
}

// NewGoBinaryCheck builds the default `go`-on-PATH check.
func NewGoBinaryCheck() *GoBinaryCheck { return &GoBinaryCheck{} }

// Name returns the canonical check name.
func (*GoBinaryCheck) Name() string { return goBinaryCheckName }

// Run probes for the go binary. The project root is irrelevant here.
func (c *GoBinaryCheck) Run(ctx context.Context, _ string) Finding {
	lookPath := c.lookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	bin, err := lookPath("go")
	if err != nil {
		return Finding{
			CheckName: c.Name(),
			Severity:  SeverityError,
			Title:     "go command not found on PATH",
			Detail:    "Kleiber needs the Go toolchain to load projects, run gopls, and build and test code.",
			Hint:      "install Go and make sure its bin directory is on your PATH",
			Fixes: []FixAction{{
				Label:   "Install Go",
				Command: "https://go.dev/dl/",
			}},
		}
	}

	versionOf := c.versionOf
	if versionOf == nil {
		versionOf = defaultGoVersion
	}
	v, _ := versionOf(ctx, bin) // version probe failure is non-fatal
	detail := "go (at " + bin + ")"
	if v != "" {
		detail = v + " (at " + bin + ")"
	}
	return Finding{
		CheckName: c.Name(),
		Severity:  SeverityOK,
		Title:     "go command available",
		Detail:    detail,
	}
}

// defaultGoVersion runs `go version` and returns the goX.Y.Z token, e.g.
// "go1.26.3" from "go version go1.26.3 windows/386". Returns empty on any
// probe failure.
func defaultGoVersion(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, path, "version")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return "", err
	}
	for _, field := range strings.Fields(buf.String()) {
		if strings.HasPrefix(field, "go1") || strings.HasPrefix(field, "go2") {
			return field, nil
		}
	}
	return "", nil
}
