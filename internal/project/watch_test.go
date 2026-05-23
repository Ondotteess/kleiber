package project

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Ondotteess/kleiber/internal/logging"
)

func TestWatch_FileCreate_EmitsEvent(t *testing.T) {
	root := t.TempDir()
	p := &Project{root: root, logger: logging.Discard()}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := p.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Give fsnotify a beat to install the watch.
	time.Sleep(150 * time.Millisecond)

	target := filepath.Join(root, "new.go")
	writeFile(t, target, "package main\n")

	if !awaitPath(events, target, 5*time.Second) {
		t.Fatalf("did not observe an event for %s", target)
	}
}

func TestWatch_SkipsHiddenDir(t *testing.T) {
	root := t.TempDir()
	hidden := filepath.Join(root, ".git")
	if err := os.MkdirAll(hidden, 0o755); err != nil {
		t.Fatal(err)
	}

	p := &Project{root: root, logger: logging.Discard()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := p.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	writeFile(t, filepath.Join(hidden, "HEAD"), "ref: ...\n")

	select {
	case ev := <-events:
		// We may still see chmod-like events on parent dirs on some
		// platforms; only fail if the event explicitly references our
		// hidden file.
		if strings.Contains(ev.Path, filepath.Join(".git", "HEAD")) {
			t.Errorf("received event for hidden dir: %+v", ev)
		}
	case <-time.After(time.Second):
		// expected: no event from hidden dir
	}
}

func TestWatch_CtxCancel_ClosesChannel(t *testing.T) {
	root := t.TempDir()
	p := &Project{root: root, logger: logging.Discard()}
	ctx, cancel := context.WithCancel(context.Background())
	events, err := p.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-events:
			if !ok {
				return // channel closed; success
			}
		case <-deadline:
			t.Fatal("events channel was not closed after ctx cancel")
		}
	}
}

// awaitPath waits up to timeout for an FSEvent whose Path matches target
// (case-insensitively on Windows, exact otherwise).
func awaitPath(ch <-chan FSEvent, target string, timeout time.Duration) bool {
	target = filepath.Clean(target)
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return false
			}
			if pathsEqual(ev.Path, target) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}
