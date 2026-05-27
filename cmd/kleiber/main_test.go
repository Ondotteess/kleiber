package main

import (
	"bytes"
	"context"
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
