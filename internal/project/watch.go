package project

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// FSEventKind enumerates the filesystem change types Watch reports.
type FSEventKind int

// FSEventKind values.
const (
	// FSEventUnknown represents an event we did not classify; produced
	// only for internal completeness and never sent on the channel.
	FSEventUnknown FSEventKind = iota
	FSEventCreate
	FSEventWrite
	FSEventRemove
	FSEventRename
	FSEventChmod
)

// String renders an FSEventKind in lower-case for logs.
func (k FSEventKind) String() string {
	switch k {
	case FSEventCreate:
		return "create"
	case FSEventWrite:
		return "write"
	case FSEventRemove:
		return "remove"
	case FSEventRename:
		return "rename"
	case FSEventChmod:
		return "chmod"
	default:
		return "unknown"
	}
}

// FSEvent describes a filesystem change observed inside the project root.
type FSEvent struct {
	// Path is the absolute path of the file or directory that changed.
	Path string

	// Kind classifies the change.
	Kind FSEventKind
}

// watchBuffer sizes the channel returned by Watch. Picked to absorb a
// reasonable burst (e.g., a `git checkout` rewriting dozens of files)
// without making fsnotify's internal goroutine drop events.
const watchBuffer = 256

// Watch starts a recursive fsnotify watch over the project root and
// returns a channel of FSEvents. The watch lives until ctx is canceled,
// at which point the channel is closed and the watcher is released.
//
// Watch skips:
//   - directories whose base name starts with '.' (e.g., .git, .kleiber),
//   - the conventional output dirs "bin", "dist", "vendor", "node_modules".
//
// Newly created subdirectories under the root are added to the watch on
// the fly so callers see events from them immediately.
func (p *Project) Watch(ctx context.Context) (<-chan FSEvent, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("starting fsnotify watcher: %w", err)
	}
	if err := addRecursive(w, p.root); err != nil {
		_ = w.Close()
		return nil, err
	}
	out := make(chan FSEvent, watchBuffer)
	go p.pumpWatch(ctx, w, out)
	return out, nil
}

func (p *Project) pumpWatch(ctx context.Context, w *fsnotify.Watcher, out chan<- FSEvent) {
	defer close(out)
	defer func() {
		if err := w.Close(); err != nil {
			p.logger.Warn("closing fsnotify watcher", "err", err)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			p.logger.Warn("fsnotify error", "err", err)
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			kind := mapOp(ev.Op)
			if kind == FSEventUnknown {
				continue
			}
			if kind == FSEventCreate {
				if info, statErr := os.Stat(ev.Name); statErr == nil && info.IsDir() && !skipDir(filepath.Base(ev.Name)) {
					if addErr := addRecursive(w, ev.Name); addErr != nil {
						p.logger.Warn("adding new dir to watch", "path", ev.Name, "err", addErr)
					}
				}
			}
			select {
			case out <- FSEvent{Path: ev.Name, Kind: kind}:
			case <-ctx.Done():
				return
			}
		}
	}
}

func mapOp(op fsnotify.Op) FSEventKind {
	switch {
	case op&fsnotify.Create != 0:
		return FSEventCreate
	case op&fsnotify.Write != 0:
		return FSEventWrite
	case op&fsnotify.Remove != 0:
		return FSEventRemove
	case op&fsnotify.Rename != 0:
		return FSEventRename
	case op&fsnotify.Chmod != 0:
		return FSEventChmod
	default:
		return FSEventUnknown
	}
}

func addRecursive(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("walking %s: %w", path, err)
		}
		if !d.IsDir() {
			return nil
		}
		name := d.Name()
		if path != root && skipDir(name) {
			return filepath.SkipDir
		}
		if err := w.Add(path); err != nil {
			return fmt.Errorf("adding %s to watch: %w", path, err)
		}
		return nil
	})
}

// skipDir reports whether a directory base name should be excluded from
// the recursive watch.
func skipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor", "bin", "dist":
		return true
	}
	return false
}
