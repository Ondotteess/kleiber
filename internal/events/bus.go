package events

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
)

// ErrTopicClosed is returned by Publish on a Topic whose Close has been
// called. It is a sentinel; compare with errors.Is.
var ErrTopicClosed = errors.New("events: topic closed")

// Topic is a typed publish/subscribe channel.
//
// The zero value is not usable; construct topics with NewTopic.
type Topic[T any] struct {
	name   string
	logger *slog.Logger

	mu     sync.Mutex
	subs   []*subscription[T]
	closed bool
	done   chan struct{}
}

type subscription[T any] struct {
	ch   chan T
	done chan struct{}
}

// NewTopic creates a Topic identified by name. The name appears in log
// messages and has no semantic effect. If logger is nil, log records are
// discarded — convenient for tests but unusual in production.
func NewTopic[T any](name string, logger *slog.Logger) *Topic[T] {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	return &Topic[T]{
		name:   name,
		logger: logger,
		done:   make(chan struct{}),
	}
}

// Subscribe registers a new subscriber and returns a receive-only channel
// plus a cancel function. The channel has the supplied buffer; choose one
// large enough to absorb short bursts without making publishers wait.
//
// The cancel function is idempotent. After cancel, the channel is no
// longer fed; existing buffered events remain readable until drained.
func (t *Topic[T]) Subscribe(buffer int) (<-chan T, func()) {
	if buffer < 0 {
		buffer = 0
	}
	sub := &subscription[T]{
		ch:   make(chan T, buffer),
		done: make(chan struct{}),
	}

	t.mu.Lock()
	t.subs = append(t.subs, sub)
	t.mu.Unlock()

	cancel := sync.OnceFunc(func() {
		t.mu.Lock()
		for i, s := range t.subs {
			if s == sub {
				t.subs = append(t.subs[:i], t.subs[i+1:]...)
				break
			}
		}
		t.mu.Unlock()
		close(sub.done)
	})
	return sub.ch, cancel
}

// Publish delivers event to every subscriber that was registered at the
// moment of the call. It blocks until each subscriber accepts the event
// or the caller's context is canceled. Subscribers that have already been
// canceled are skipped without error.
//
// Publish returns ErrTopicClosed if the Topic has been closed.
func (t *Topic[T]) Publish(ctx context.Context, event T) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return ErrTopicClosed
	}
	subs := make([]*subscription[T], len(t.subs))
	copy(subs, t.subs)
	t.mu.Unlock()

	for _, sub := range subs {
		select {
		case sub.ch <- event:
		case <-sub.done:
			// Subscriber unsubscribed while we waited; skip.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Close marks the Topic as closed. Subsequent Publish calls return
// ErrTopicClosed. All current subscribers receive a signal on the channel
// returned by Done; in-flight Publish calls observe their per-subscription
// done channel and return promptly.
//
// Close is idempotent and safe for concurrent use.
func (t *Topic[T]) Close() {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return
	}
	t.closed = true
	subs := t.subs
	t.subs = nil
	t.mu.Unlock()

	close(t.done)
	for _, sub := range subs {
		select {
		case <-sub.done:
		default:
			close(sub.done)
		}
	}
	t.logger.Debug("topic closed", "topic", t.name)
}

// Done returns a channel that is closed when the Topic is closed.
// Subscribers should select on it alongside their receive channel to
// react to topic shutdown.
func (t *Topic[T]) Done() <-chan struct{} {
	return t.done
}

// Name returns the topic's identifier.
func (t *Topic[T]) Name() string { return t.name }

// SubscriberCount returns the current number of active subscribers.
// Intended for diagnostics and tests, not for steering control flow.
func (t *Topic[T]) SubscriberCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.subs)
}
