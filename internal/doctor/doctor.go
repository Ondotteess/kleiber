package doctor

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/Ondotteess/kleiber/internal/logging"
)

// Doctor runs a fixed set of Checks against a project root and returns
// their Findings.
//
// A Doctor is created with an immutable list of Checks; to use a
// different set, construct a new Doctor. The order of Checks at
// construction is the order of Findings in the result.
type Doctor struct {
	logger *slog.Logger
	checks []Check
}

// New constructs a Doctor with the given checks. A nil logger maps to
// a discard logger so the Doctor is safe to use in tests.
func New(logger *slog.Logger, checks ...Check) *Doctor {
	if logger == nil {
		logger = logging.Discard()
	}
	return &Doctor{logger: logger, checks: checks}
}

// Run executes each registered Check sequentially against root. If ctx
// is canceled between checks, Run returns the findings collected so
// far along with ctx.Err(). Individual checks are responsible for
// honoring ctx during their own work.
//
// root may be a relative path; Run resolves it to an absolute one
// before invoking checks, so individual checks always see absolute paths.
func (d *Doctor) Run(ctx context.Context, root string) ([]Finding, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("doctor: resolving root %q: %w", root, err)
	}

	findings := make([]Finding, 0, len(d.checks))
	for _, check := range d.checks {
		if err := ctx.Err(); err != nil {
			return findings, err
		}
		f := check.Run(ctx, abs)
		if f.CheckName == "" {
			f.CheckName = check.Name()
		}
		d.logger.Debug("doctor check completed",
			"check", check.Name(),
			"severity", f.Severity.String(),
			"title", f.Title,
		)
		findings = append(findings, f)
	}
	return findings, nil
}

// DefaultChecks returns the built-in checks in canonical order:
// toolchain, tools, workspace. Callers that want a different subset
// can pass selected Checks to New directly.
func DefaultChecks() []Check {
	return []Check{
		NewToolchainCheck(),
		NewToolsCheck(),
		NewWorkspaceCheck(),
	}
}
