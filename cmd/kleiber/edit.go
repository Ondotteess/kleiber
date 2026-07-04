package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	appcore "github.com/Ondotteess/kleiber/internal/app"
	"github.com/Ondotteess/kleiber/internal/config"
	"github.com/Ondotteess/kleiber/internal/logging"
	"github.com/Ondotteess/kleiber/internal/ui"
)

// errIDEUnavailable is returned when `kleiber edit` runs against a binary
// built without the gio tag, which has no window backend.
var errIDEUnavailable = errors.New("edit requires a build with -tags=gio")

// runEdit implements `kleiber edit [--debug] [--log-file PATH] [path]`: it
// opens a project directory (or the parent of a file) as an IDE workspace
// and launches the Gio window. A file path also opens that file in a tab.
func runEdit(args []string, stdout, stderr io.Writer, opts runOptions) error {
	fs := flag.NewFlagSet("kleiber edit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	debug := fs.Bool("debug", false, "enable debug logging (includes the gopls JSON-RPC trace)")
	logFile := fs.String("log-file", "", "write logs to this file (default: the user cache directory)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: kleiber edit [--debug] [--log-file PATH] [path]")
		fmt.Fprintln(stderr, "  path   a project directory (default \".\") or a file to open")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	target := "."
	if rest := fs.Args(); len(rest) > 1 {
		fs.Usage()
		return fmt.Errorf("edit: expected at most one path argument")
	} else if len(rest) == 1 {
		target = rest[0]
	}

	root, openPath, err := resolveEditTarget(target)
	if err != nil {
		return err
	}

	logger, closeLog, err := setupEditLogging(*debug, *logFile, stderr)
	if err != nil {
		return err
	}
	defer closeLog()

	ctx := context.Background()
	wb, err := buildEditWorkbench(ctx, root, openPath, logger)
	if err != nil {
		return err
	}

	if opts.launchIDE == nil {
		return errIDEUnavailable
	}
	fmt.Fprintf(stdout, "Opening %s\n", root)
	return opts.launchIDE(ctx, wb, ui.IDEWindowOptions{
		Title:    "Kleiber — " + filepath.Base(root),
		WidthDP:  1100,
		HeightDP: 740,
	}, stderr)
}

// resolveEditTarget turns a path argument into a workspace root and an
// optional file to open. A directory is the root; a file resolves to its
// parent directory as root plus the file to open.
func resolveEditTarget(target string) (root, openPath string, err error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", "", fmt.Errorf("edit: resolving %q: %w", target, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", "", fmt.Errorf("edit: %w", err)
	}
	if info.IsDir() {
		return abs, "", nil
	}
	return filepath.Dir(abs), abs, nil
}

// setupEditLogging builds the IDE's logger. Logs go to a file (so a
// windowed process still leaves a diagnosable trail); --debug raises the
// level and enables the JSON-RPC trace. If the log file cannot be opened,
// it falls back to stderr rather than failing to launch.
func setupEditLogging(debug bool, logFile string, stderr io.Writer) (*slog.Logger, func(), error) {
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}

	path := logFile
	if path == "" {
		cacheDir, err := config.UserCachePath()
		if err == nil {
			path = filepath.Join(cacheDir, "logs", "kleiber.log")
		}
	}

	if path != "" {
		if f, err := logging.OpenFile(path); err == nil {
			logger := logging.New(logging.Options{Level: level, Format: logging.FormatText, Writer: f})
			logger.Info("kleiber edit starting", "logFile", path, "debug", debug)
			return logger, func() { _ = f.Close() }, nil
		}
		fmt.Fprintf(stderr, "kleiber: could not open log file %s; logging to stderr\n", path)
	}

	logger := logging.New(logging.Options{Level: level, Format: logging.FormatText, Writer: stderr})
	return logger, func() {}, nil
}

// buildEditWorkbench constructs the session and workbench, loads the file
// tree, and opens the initial file if one was given. Project analysis
// failures (e.g. code that does not compile) are non-fatal: the IDE opens
// anyway so the user can fix the code.
func buildEditWorkbench(ctx context.Context, root, openPath string, logger *slog.Logger) (*ui.Workbench, error) {
	session, err := appcore.NewDefaultSession(ctx, appcore.DefaultSessionOptions{
		ProjectRoot: root,
		Logger:      logger,
	})
	if err != nil {
		// Project analysis failed (commonly: the code does not build).
		// Open the workspace without it so the tree and editor still work.
		logger.Warn("opening project analysis failed; continuing without it", "root", root, "err", err)
		session, err = appcore.NewDefaultSession(ctx, appcore.DefaultSessionOptions{Logger: logger})
		if err != nil {
			return nil, err
		}
	}

	wb, err := ui.NewWorkbench(session)
	if err != nil {
		return nil, err
	}
	wb.SetRoot(root)
	if err := wb.RefreshTree(ctx); err != nil {
		logger.Warn("loading file tree failed", "root", root, "err", err)
	}
	if openPath != "" {
		if _, err := wb.OpenFile(ctx, openPath); err != nil {
			logger.Warn("opening initial file failed", "path", openPath, "err", err)
		}
	}
	return wb, nil
}
