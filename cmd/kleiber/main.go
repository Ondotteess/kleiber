// Command kleiber is the entry point for the Kleiber IDE binary.
//
// Per docs/agents/codebase-map.md, this package contains no business
// logic: it parses command-line flags, dispatches subcommands, and
// (in future milestones) hands off to internal/ui for the actual
// editor experience. Anything more substantial belongs under internal/.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Ondotteess/kleiber/internal/doctor"
	"github.com/Ondotteess/kleiber/pkg/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kleiber:", err)
		os.Exit(1)
	}
}

// run is the testable entry point. It dispatches to subcommands when
// the first positional argument is not a flag.
func run(args []string, stdout, stderr io.Writer) error {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		switch args[0] {
		case "doctor":
			return runDoctor(args[1:], stdout, stderr)
		case "help":
			return runHelp(args[1:], stdout)
		default:
			return fmt.Errorf("unknown subcommand %q (try `kleiber help`)", args[0])
		}
	}
	return runTop(args, stdout, stderr)
}

// runTop handles the top-level (no subcommand) invocation: flag
// parsing for --version / -v, and otherwise the pre-alpha notice.
func runTop(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("kleiber", flag.ContinueOnError)
	fs.SetOutput(stderr)

	showVersion := fs.Bool("version", false, "print version information and exit")
	fs.BoolVar(showVersion, "v", false, "print version information and exit (shorthand)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *showVersion {
		fmt.Fprintln(stdout, "kleiber", version.Current())
		return nil
	}

	// Pre-alpha: no UI yet. The roadmap (docs/product/roadmap.md)
	// describes when this changes. Until Phase 4 lands, the binary
	// is informational and exposes the doctor subcommand.
	fmt.Fprintf(stderr,
		"kleiber %s (pre-alpha)\n"+
			"No UI yet — see docs/product/roadmap.md for milestones.\n"+
			"Available subcommands:\n"+
			"  kleiber doctor [path]   diagnose a Go project\n"+
			"  kleiber help            show this message\n",
		version.Current(),
	)
	return nil
}

// runHelp prints a brief usage summary to stdout.
func runHelp(_ []string, stdout io.Writer) error {
	fmt.Fprintf(stdout,
		"kleiber %s\n\n"+
			"Usage:\n"+
			"  kleiber [--version|-v]\n"+
			"  kleiber doctor [path]\n"+
			"  kleiber help\n",
		version.Current(),
	)
	return nil
}

// runDoctor implements the `kleiber doctor [path]` subcommand.
//
// It runs the default check set against the given path (default: cwd)
// and prints a human-readable report to stdout. Exit code is zero even
// when issues are found — Findings are observations, not runtime
// errors. Critical failures (bad path, cancelled context) do return a
// non-nil error.
func runDoctor(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("kleiber doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return err
	}

	root := "."
	if rest := fs.Args(); len(rest) > 0 {
		root = rest[0]
	}

	d := doctor.New(nil, doctor.DefaultChecks()...)
	findings, err := d.Run(context.Background(), root)
	if err != nil {
		return fmt.Errorf("doctor: %w", err)
	}

	if len(findings) == 0 {
		fmt.Fprintln(stdout, "No checks registered.")
		return nil
	}
	for _, f := range findings {
		printFinding(stdout, f)
	}
	return nil
}

// printFinding emits one Finding in human-readable form.
func printFinding(w io.Writer, f doctor.Finding) {
	fmt.Fprintf(w, "%s [%s] %s\n", severityBadge(f.Severity), f.CheckName, f.Title)
	if f.Detail != "" {
		for _, line := range strings.Split(f.Detail, "\n") {
			fmt.Fprintln(w, "   "+line)
		}
	}
	if f.Hint != "" {
		fmt.Fprintln(w, "   hint: "+f.Hint)
	}
	for _, fix := range f.Fixes {
		fmt.Fprintf(w, "   fix : %s — %s\n", fix.Label, fix.Command)
	}
	fmt.Fprintln(w)
}

// severityBadge returns a short fixed-width prefix marker for a
// Finding. The badge is plain ASCII so it renders on every terminal,
// including basic CI logs.
func severityBadge(s doctor.Severity) string {
	switch s {
	case doctor.SeverityOK:
		return "OK "
	case doctor.SeverityInfo:
		return "i  "
	case doctor.SeverityWarning:
		return "!  "
	case doctor.SeverityError:
		return "X  "
	default:
		return "?  "
	}
}
