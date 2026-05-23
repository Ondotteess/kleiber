package events

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestTopic_Publish_DeliversToSubscriber(t *testing.T) {
	topic := NewTopic[int]("test", nil)
	defer topic.Close()

	ch, cancel := topic.Subscribe(1)
	defer cancel()

	ctx, ctxCancel := context.WithTimeout(context.Background(), time.Second)
	defer ctxCancel()

	if err := topic.Publish(ctx, 42); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-ch:
		if got != 42 {
			t.Errorf("received %d, want 42", got)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event in time")
	}
}

func TestTopic_Publish_MultipleSubscribers(t *testing.T) {
	topic := NewTopic[string]("multi", nil)
	defer topic.Close()

	ch1, cancel1 := topic.Subscribe(1)
	defer cancel1()
	ch2, cancel2 := topic.Subscribe(1)
	defer cancel2()

	ctx, ctxCancel := context.WithTimeout(context.Background(), time.Second)
	defer ctxCancel()
	if err := topic.Publish(ctx, "hello"); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	for i, ch := range []<-chan string{ch1, ch2} {
		select {
		case got := <-ch:
			if got != "hello" {
				t.Errorf("subscriber %d: got %q, want %q", i, got, "hello")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

func TestTopic_Subscribe_CancelRemovesSubscriber(t *testing.T) {
	topic := NewTopic[int]("cancel", nil)
	defer topic.Close()

	_, cancel := topic.Subscribe(0)
	if got := topic.SubscriberCount(); got != 1 {
		t.Fatalf("SubscriberCount = %d, want 1", got)
	}
	cancel()
	if got := topic.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after cancel = %d, want 0", got)
	}
	// Second cancel must be a no-op.
	cancel()
	if got := topic.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after second cancel = %d, want 0", got)
	}
}

func TestTopic_Publish_SkipsCanceledSubscriber(t *testing.T) {
	topic := NewTopic[int]("skip", nil)
	defer topic.Close()

	// Unbuffered subscriber that immediately cancels — Publish must not
	// block waiting on it.
	_, cancel := topic.Subscribe(0)
	cancel()

	ch, cancel2 := topic.Subscribe(1)
	defer cancel2()

	ctx, ctxCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer ctxCancel()
	if err := topic.Publish(ctx, 7); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case got := <-ch:
		if got != 7 {
			t.Errorf("got %d, want 7", got)
		}
	case <-time.After(time.Second):
		t.Fatal("active subscriber did not receive event")
	}
}

func TestTopic_Publish_ContextCanceled(t *testing.T) {
	topic := NewTopic[int]("ctx", nil)
	defer topic.Close()

	// Unbuffered subscriber that never reads — Publish must block until
	// the context is canceled.
	_, cancel := topic.Subscribe(0)
	defer cancel()

	ctx, ctxCancel := context.WithCancel(context.Background())
	ctxCancel()

	err := topic.Publish(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestTopic_Close_PreventsFurtherPublish(t *testing.T) {
	topic := NewTopic[int]("close", nil)
	topic.Close()

	err := topic.Publish(context.Background(), 1)
	if !errors.Is(err, ErrTopicClosed) {
		t.Errorf("err = %v, want ErrTopicClosed", err)
	}
}

func TestTopic_Close_SignalsDone(t *testing.T) {
	topic := NewTopic[int]("done", nil)

	select {
	case <-topic.Done():
		t.Fatal("Done() should not be closed before Close()")
	default:
	}

	topic.Close()
	select {
	case <-topic.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close after Close()")
	}

	// Idempotency.
	topic.Close()
}

func TestTopic_Publish_Concurrent(t *testing.T) {
	topic := NewTopic[int]("concurrent", nil)
	defer topic.Close()

	const subs = 8
	const events = 100

	channels := make([]<-chan int, subs)
	cancels := make([]func(), subs)
	for i := 0; i < subs; i++ {
		channels[i], cancels[i] = topic.Subscribe(events)
	}
	defer func() {
		for _, c := range cancels {
			c()
		}
	}()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for i := 0; i < events; i++ {
			if err := topic.Publish(ctx, i); err != nil {
				t.Errorf("Publish: %v", err)
				return
			}
		}
	}()

	for i := 0; i < subs; i++ {
		wg.Add(1)
		go func(ch <-chan int) {
			defer wg.Done()
			for j := 0; j < events; j++ {
				select {
				case <-ch:
				case <-time.After(5 * time.Second):
					t.Errorf("subscriber timed out at event %d", j)
					return
				}
			}
		}(channels[i])
	}

	wg.Wait()
}

func TestNewTopic_NilLogger_NoPanic(t *testing.T) {
	topic := NewTopic[int]("nil-logger", nil)
	defer topic.Close()
	if topic.Name() != "nil-logger" {
		t.Errorf("Name() = %q, want %q", topic.Name(), "nil-logger")
	}
}
