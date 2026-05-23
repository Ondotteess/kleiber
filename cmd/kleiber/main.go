// Command kleiber is the entry point for the Kleiber IDE binary.
//
// Per docs/agents/codebase-map.md, this package contains no business logic:
// it parses command-line flags, prints version information, and (in future
// milestones) hands off to internal/ui for the actual editor experience.
// Anything more substantial belongs under internal/.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Ondotteess/kleiber/pkg/version"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "kleiber:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
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

	// Pre-alpha: no UI yet. The roadmap (docs/product/roadmap.md) describes
	// when this changes. Until Phase 4 lands, the binary is informational.
	fmt.Fprintf(stderr,
		"kleiber %s (pre-alpha, Phase 0)\n"+
			"No UI yet — see docs/product/roadmap.md for milestones.\n",
		version.Current(),
	)
	return nil
}
