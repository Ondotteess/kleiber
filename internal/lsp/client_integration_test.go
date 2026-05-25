//go:build integration

package lsp_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/lsp"
)

// helloFixture walks upward from the test's working directory looking
// for fixtures/hello/go.mod and returns the containing directory. This
// mirrors the helper used by project's integration test so behavior
// stays consistent across packages.
func helloFixture(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir, err := filepath.Abs(wd)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "fixtures", "hello", "go.mod")
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Dir(candidate)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate fixtures/hello from %s", wd)
		}
		dir = parent
	}
}

// requireGopls skips the test cleanly when no gopls is on PATH. The
// integration suite is meant to run on developer machines and on the
// dedicated integration CI lane; the default `go test ./...` lane does
// not exercise it.
func requireGopls(t *testing.T) string {
	t.Helper()
	bin, err := exec.LookPath("gopls")
	if err != nil {
		t.Skipf("gopls not found on PATH; skipping integration test")
	}
	return bin
}

func uriFromPath(t *testing.T, p string) lsp.DocumentURI {
	t.Helper()
	uri, err := lsp.DocumentURIFromPath(p)
	if err != nil {
		t.Fatalf("DocumentURIFromPath: %v", err)
	}
	return uri
}

// TestClient_Integration_OpenHelloFixture drives the full LSP handshake
// against a real gopls, opens fixtures/hello/main.go, and asserts that
// at minimum one diagnostics notification arrives (gopls publishes one
// per opened document even when there are zero diagnostics).
func TestClient_Integration_OpenHelloFixture(t *testing.T) {
	binary := requireGopls(t)
	root := helloFixture(t)
	rootURI := uriFromPath(t, root)

	client := lsp.NewClient(lsp.ClientOptions{
		GoplsPath:     binary,
		ClientName:    "kleiber-test",
		ClientVersion: "integration",
		RootURI:       rootURI,
		WorkspaceFolders: []lsp.WorkspaceFolder{
			{URI: rootURI, Name: "hello"},
		},
	})

	diagCh, diagCancel := client.Diagnostics().Subscribe(8)
	defer diagCancel()

	startCtx, startCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer startCancel()
	if err := client.Start(startCtx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = client.Stop(stopCtx)
	})

	mainPath := filepath.Join(root, "main.go")
	mainURI := uriFromPath(t, mainPath)
	source, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	openCtx, openCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer openCancel()
	if err := client.DidOpen(openCtx, mainURI, "go", string(source)); err != nil {
		t.Fatalf("DidOpen: %v", err)
	}

	// Wait up to 15s for at least one diagnostics event. gopls sends
	// publishDiagnostics even when the document is clean.
	select {
	case ev := <-diagCh:
		if ev.URI == "" {
			t.Errorf("event URI is empty")
		}
		t.Logf("diagnostics received: uri=%s count=%d", ev.URI, len(ev.Diagnostics))
	case <-time.After(15 * time.Second):
		t.Fatal("did not receive any diagnostics within 15s")
	}
}
