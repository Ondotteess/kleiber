package ui

import (
	"context"
	"time"

	"github.com/Ondotteess/kleiber/internal/project"
)

// watchDebounce is how long the watcher waits after the last filesystem
// event before refreshing the tree. Editors and go commands produce
// bursts of events (temp files, renames); one refresh per burst is
// enough for an explorer.
const watchDebounce = 400 * time.Millisecond

// StartWatching begins watching the project for filesystem changes and
// refreshes the file tree (then calls onChange, typically the window's
// invalidate) after each debounced burst of events. It returns
// immediately; the watch goroutine runs until ctx is canceled or the
// watcher closes.
//
// Watching requires an open project (the watcher belongs to the project
// model); when the session has none — e.g. analysis failed on broken
// code — StartWatching is a documented no-op returning nil, and the tree
// refreshes only manually.
func (w *Workbench) StartWatching(ctx context.Context, onChange func()) error {
	proj := w.session.Project()
	if proj == nil {
		return nil
	}
	events, err := proj.Watch(ctx)
	if err != nil {
		return err
	}
	if onChange == nil {
		onChange = func() {}
	}
	go w.watchLoop(ctx, events, onChange)
	return nil
}

// watchLoop debounces watcher events into tree refreshes. It exits when
// ctx is canceled or the events channel closes (the project watcher
// closes it on ctx cancellation).
func (w *Workbench) watchLoop(ctx context.Context, events <-chan project.FSEvent, onChange func()) {
	timer := time.NewTimer(watchDebounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	pending := false
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-events:
			if !ok {
				return
			}
			pending = true
			// Reset without draining is safe here: a spurious fire is
			// ignored by the pending flag.
			timer.Reset(watchDebounce)
		case <-timer.C:
			if !pending {
				continue
			}
			pending = false
			if err := w.RefreshTree(ctx); err != nil && ctx.Err() == nil {
				continue
			}
			onChange()
		}
	}
}
