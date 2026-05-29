package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Ondotteess/kleiber/internal/ui"
)

func TestRun_Version_PrintsVersion(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"long flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tc.args, &stdout, &stderr); err != nil {
				t.Fatalf("run returned error: %v", err)
			}
			got := stdout.String()
			if !strings.HasPrefix(got, "kleiber ") {
				t.Errorf("stdout = %q, want prefix %q", got, "kleiber ")
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRun_NoArgs_PrintsPreAlphaNotice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "pre-alpha") {
		t.Errorf("stderr = %q, want it to mention pre-alpha", stderr.String())
	}
	if !strings.Contains(stderr.String(), "doctor") {
		t.Errorf("stderr = %q, want it to advertise the doctor subcommand", stderr.String())
	}
}

func TestRun_Help_PrintsUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	for _, want := range []string{"kleiber", "doctor", "experimental-ui", "Usage"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("help output missing %q; got %q", want, stdout.String())
		}
	}
}

func TestRun_Help_MarksExperimentalUIUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	if err := runWithOptions([]string{"help"}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run help: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "experimental-ui") {
		t.Fatalf("help output missing experimental-ui: %q", out)
	}
	if !strings.Contains(out, "-tags=gio") {
		t.Fatalf("help output missing -tags=gio caveat: %q", out)
	}
}

func TestRun_NoArgs_MarksExperimentalUIUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	if err := runWithOptions(nil, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run no args: %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "experimental-ui") {
		t.Fatalf("pre-alpha output missing experimental-ui: %q", out)
	}
	if !strings.Contains(out, "-tags=gio") {
		t.Fatalf("pre-alpha output missing -tags=gio caveat: %q", out)
	}
}

func TestRun_ExperimentalUIHelp_MarksUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	if err := runWithOptions([]string{"experimental-ui", "--help"}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run experimental-ui --help: %v", err)
	}
	out := stderr.String()
	if !strings.Contains(out, "experimental-ui") {
		t.Fatalf("experimental-ui help missing command: %q", out)
	}
	if !strings.Contains(out, "-tags=gio") {
		t.Fatalf("experimental-ui help missing -tags=gio caveat: %q", out)
	}
	if !strings.Contains(out, "--smoke") {
		t.Fatalf("experimental-ui help missing --smoke: %q", out)
	}
}

func TestRun_Help_AvailableExperimentalUIDoesNotSayUnavailable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{
		gioUIAvailable: func() bool { return true },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			t.Fatal("launcher should not be called by help")
			return nil
		},
	}
	if err := runWithOptions([]string{"help"}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run help: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "experimental-ui") {
		t.Fatalf("help output missing experimental-ui: %q", out)
	}
	if strings.Contains(out, "-tags=gio") {
		t.Fatalf("available help should not claim Gio tag is required: %q", out)
	}
}

func TestRun_UnknownSubcommand_Errors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"bogus"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("run returned nil error for unknown subcommand")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("err = %q, want it to mention the unknown subcommand", err.Error())
	}
}

func TestRun_DoctorSubcommand_NoGoMod(t *testing.T) {
	// An empty temp dir has no go.mod, no multi-module, and the
	// tools check runs against the real PATH. Whatever the result,
	// run should succeed with non-empty stdout.
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error("stdout is empty; doctor should print findings")
	}
	// Confirm the report contains at least one of the canonical
	// check names.
	if !strings.Contains(stdout.String(), "[toolchain]") {
		t.Errorf("stdout does not mention [toolchain]; got %q", stdout.String())
	}
}

func TestRun_DoctorSubcommand_ValidGoProject(t *testing.T) {
	// Build a tiny one-module project and run the doctor on it.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/test\n\ngo 1.20\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor", dir}, &stdout, &stderr); err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "[workspace]") {
		t.Errorf("output missing [workspace] section: %q", out)
	}
	if !strings.Contains(out, "single Go module") {
		t.Errorf("expected single-module workspace finding; got %q", out)
	}
}

func TestRun_DoctorSubcommand_DefaultCwd(t *testing.T) {
	// `kleiber doctor` with no path argument should use cwd. We can
	// run it against the test's cwd — whatever findings come back,
	// the call should succeed.
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor"}, &stdout, &stderr); err != nil {
		t.Fatalf("run doctor: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error("stdout empty; doctor should print findings even for cwd")
	}
}

func TestRun_ExperimentalUI_ConstructsShellAndUsesLauncher(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module example.com/ui\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"),
		[]byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}

	var launched bool
	var gotTitle string
	var gotProjectOpen bool
	opts := runOptions{
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			launched = true
			gotTitle = opts.Title
			if err := shell.Refresh(ctx); err != nil {
				return err
			}
			gotProjectOpen = shell.State().Project.Open
			shell.Close()
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	if err := runWithOptions([]string{"experimental-ui", dir}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run experimental-ui: %v", err)
	}
	if !launched {
		t.Fatal("launcher was not called")
	}
	if gotTitle != "Kleiber experimental UI" {
		t.Fatalf("title = %q, want Kleiber experimental UI", gotTitle)
	}
	if !gotProjectOpen {
		t.Fatal("project was not opened for experimental UI")
	}
	if !strings.Contains(stderr.String(), "experimental UI") {
		t.Fatalf("stderr = %q, want experimental UI notice", stderr.String())
	}
}

func TestRun_ExperimentalUI_NoArgsAccepted(t *testing.T) {
	dir := t.TempDir()
	writeMiniGoProject(t, dir, "example.com/noargs")
	t.Chdir(dir)

	var launched bool
	opts := runOptions{
		gioUIAvailable: func() bool { return true },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			_ = ctx
			_ = opts
			_ = stderr
			launched = true
			shell.Close()
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	if err := runWithOptions([]string{"experimental-ui"}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run experimental-ui: %v", err)
	}
	if !launched {
		t.Fatal("launcher was not called")
	}
}

func TestRun_ExperimentalUI_RejectsExtraArgs(t *testing.T) {
	opts := runOptions{
		gioUIAvailable: func() bool { return true },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			t.Fatal("launcher should not be called for invalid args")
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	err := runWithOptions([]string{"experimental-ui", ".", "extra"}, &stdout, &stderr, opts)
	if err == nil {
		t.Fatal("run returned nil error for extra args")
	}
	if !strings.Contains(err.Error(), "at most one path") {
		t.Fatalf("err = %q, want path count error", err.Error())
	}
	if !strings.Contains(stderr.String(), "Usage: kleiber experimental-ui [--smoke] [path]") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestRun_ExperimentalUI_HelpSucceeds(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"experimental-ui", "--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("run experimental-ui --help: %v", err)
	}
	if !strings.Contains(stderr.String(), "Usage: kleiber experimental-ui [--smoke] [path]") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
	if !strings.Contains(stderr.String(), "--smoke") {
		t.Fatalf("stderr = %q, want --smoke help", stderr.String())
	}
}

func TestRun_ExperimentalUI_NonGioUnavailableFailsBeforeProjectSetup(t *testing.T) {
	opts := runOptions{
		gioUIAvailable: func() bool { return false },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			t.Fatal("launcher should not be called when Gio is unavailable")
			return nil
		},
	}

	var stdout, stderr bytes.Buffer
	err := runWithOptions([]string{"experimental-ui", `Z:\definitely-missing-kleiber-path`}, &stdout, &stderr, opts)
	if !errors.Is(err, errExperimentalUIUnavailable) {
		t.Fatalf("err = %v, want %v", err, errExperimentalUIUnavailable)
	}
	if strings.Contains(stderr.String(), "Starting") {
		t.Fatalf("stderr = %q, should not print start notice before Gio availability check", stderr.String())
	}
}

func TestRun_ExperimentalUI_SmokeNoArgsUsesCwdWithoutGio(t *testing.T) {
	dir := t.TempDir()
	writeMiniGoProject(t, dir, "example.com/smokecwd")
	t.Chdir(dir)

	opts := runOptions{
		gioUIAvailable: func() bool { return false },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			t.Fatal("launcher should not be called in smoke mode")
			return nil
		},
	}
	var stdout, stderr bytes.Buffer
	if err := runWithOptions([]string{"experimental-ui", "--smoke"}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run experimental-ui --smoke: %v", err)
	}
	out := stdout.String()
	for _, want := range []string{
		"experimental-ui smoke",
		"project: " + dir,
		"modules: 1",
		"packages: 1",
		"buffers: 0",
		"commands: ",
		ui.ExperimentalUIShortcutSummary,
		"window: skipped (smoke mode)",
		"gopls: not auto-started",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("smoke output missing %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRun_ExperimentalUI_SmokeDotPrintsSummaryWithoutGio(t *testing.T) {
	dir := t.TempDir()
	writeMiniGoProject(t, dir, "example.com/smokedot")
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	if err := runWithOptions([]string{"experimental-ui", "--smoke", "."}, &stdout, &stderr, opts); err != nil {
		t.Fatalf("run experimental-ui --smoke .: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "project: "+dir) || !strings.Contains(out, "window: skipped (smoke mode)") {
		t.Fatalf("unexpected smoke output:\n%s", out)
	}
}

func TestRun_ExperimentalUI_SmokeMissingProjectReturnsProjectError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	err := runWithOptions([]string{"experimental-ui", "--smoke", `Z:\definitely-missing-kleiber-path`}, &stdout, &stderr, opts)
	if err == nil {
		t.Fatal("run returned nil error for missing smoke project")
	}
	if errors.Is(err, errExperimentalUIUnavailable) || strings.Contains(err.Error(), "-tags=gio") {
		t.Fatalf("err = %v, want project error not Gio tag error", err)
	}
	if !strings.Contains(err.Error(), "opening project") {
		t.Fatalf("err = %v, want project open error", err)
	}
}

func TestRun_ExperimentalUI_SmokeRejectsExtraArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	opts := runOptions{gioUIAvailable: func() bool { return false }}
	err := runWithOptions([]string{"experimental-ui", "--smoke", ".", "extra"}, &stdout, &stderr, opts)
	if err == nil {
		t.Fatal("run returned nil error for extra smoke args")
	}
	if !strings.Contains(err.Error(), "at most one path") {
		t.Fatalf("err = %q, want path count error", err.Error())
	}
	if !strings.Contains(stderr.String(), "Usage: kleiber experimental-ui [--smoke] [path]") {
		t.Fatalf("stderr = %q, want usage", stderr.String())
	}
}

func TestExperimentalUISmokeSummary_NoProjectDeterministic(t *testing.T) {
	summary := experimentalUISmokeSummary(ui.ShellState{
		Title: "Kleiber Smoke",
		State: ui.State{
			Commands: []ui.CommandItem{{Name: "editor.openFile"}},
		},
	})
	want := strings.Join([]string{
		"experimental-ui smoke",
		"title: Kleiber Smoke",
		"project: no project",
		"modules: 0",
		"packages: 0",
		"buffers: 0",
		"commands: 1",
		ui.ExperimentalUIShortcutSummary,
		"render-lines: 14",
		"window: skipped (smoke mode)",
		"gopls: not auto-started",
		"",
	}, "\n")
	if summary != want {
		t.Fatalf("summary =\n%s\nwant =\n%s", summary, want)
	}
}

func TestRun_ExperimentalUI_NonGioValidPathStillFailsFast(t *testing.T) {
	dir := t.TempDir()
	writeMiniGoProject(t, dir, "example.com/failfast")

	opts := runOptions{
		gioUIAvailable: func() bool { return false },
		launchUI: func(ctx context.Context, shell *ui.Shell, opts ui.GioWindowOptions, stderr io.Writer) error {
			t.Fatal("launcher should not be called when Gio is unavailable")
			return nil
		},
	}
	var stdout, stderr bytes.Buffer
	err := runWithOptions([]string{"experimental-ui", dir}, &stdout, &stderr, opts)
	if !errors.Is(err, errExperimentalUIUnavailable) {
		t.Fatalf("err = %v, want %v", err, errExperimentalUIUnavailable)
	}
	if strings.Contains(stderr.String(), "Starting") {
		t.Fatalf("stderr = %q, should not print start notice before Gio availability check", stderr.String())
	}
}

func writeMiniGoProject(t *testing.T, dir, module string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module "+module+"\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\n\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
}
