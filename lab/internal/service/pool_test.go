package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"sync"
	"testing"
	"time"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func receive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for completion")
	}
}

func pool(t *testing.T, workers, capacity int, timeout time.Duration) *Pool {
	t.Helper()
	p, err := NewPool(workers, capacity, timeout, quiet())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = p.Shutdown(ctx)
		receive(t, p.Done())
	})
	return p
}

func TestPoolQueueBound(t *testing.T) {
	p := pool(t, 1, 2, time.Second)
	started, release := make(chan struct{}), make(chan struct{})
	_, err := p.Submit(context.Background(), func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	for range 2 {
		if _, err := p.Submit(context.Background(), func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Submit(context.Background(), func(context.Context) error { return nil }); !errors.Is(err, ErrFull) {
		t.Fatalf("got %v, want ErrFull", err)
	}
	if s := p.Stats(); s.Queued != 2 || s.Active != 1 || s.Accepted != 3 {
		t.Fatalf("unexpected stats: %+v", s)
	}
	close(release)
	p.Close()
	receive(t, p.Done())
	if s := p.Stats(); s.Completed != 3 || s.Active != 0 || s.Queued != 0 {
		t.Fatalf("not drained: %+v", s)
	}
}

func TestAcceptedTaskOutlivesCaller(t *testing.T) {
	p := pool(t, 1, 1, time.Second)
	caller, cancel := context.WithCancel(context.Background())
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	_, err := p.Submit(caller, func(ctx context.Context) error {
		close(started)
		select {
		case <-release:
			close(finished)
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	cancel()
	close(release)
	receive(t, finished)
	if _, err := p.Submit(caller, func(context.Context) error { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission: %v", err)
	}
}

func TestPoolBlockedCancellationReleases(t *testing.T) {
	p := pool(t, 1, 2, 5*time.Second)
	started, released := make(chan struct{}), make(chan struct{})
	_, err := p.Submit(context.Background(), func(ctx context.Context) error { close(started); <-ctx.Done(); close(released); return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	receive(t, started)
	// A queued function must never execute after forced cancellation.
	if _, err := p.Submit(context.Background(), func(context.Context) error { t.Error("cancelled queued work executed"); return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := p.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("shutdown: %v", err)
	}
	receive(t, released)
	receive(t, p.Done())
	if s := p.Stats(); s.Completed != 2 || s.Cancelled != 2 || s.Active != 0 {
		t.Fatalf("resources not released: %+v", s)
	}
}

func TestPoolTaskDeadline(t *testing.T) {
	p := pool(t, 1, 1, 20*time.Millisecond)
	_, err := p.Submit(context.Background(), func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() })
	if err != nil {
		t.Fatal(err)
	}
	p.Close()
	receive(t, p.Done())
	if s := p.Stats(); s.Cancelled != 1 {
		t.Fatalf("deadline not observed: %+v", s)
	}
}

func TestPoolConcurrentCloseAndSubmit(t *testing.T) {
	p := pool(t, 4, 8, time.Second)
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_, err := p.Submit(context.Background(), func(context.Context) error { return nil })
				if err != nil && !errors.Is(err, ErrFull) && !errors.Is(err, ErrClosed) {
					t.Errorf("submit: %v", err)
				}
			}
		}()
	}
	p.Close()
	wg.Wait()
	receive(t, p.Done())
	if s := p.Stats(); s.Completed != s.Accepted {
		t.Fatalf("accepted tasks lost: %+v", s)
	}
}

func BenchmarkPoolSubmit(b *testing.B) {
	p, _ := NewPool(4, 128, time.Second, quiet())
	defer func() { p.Close(); <-p.Done() }()
	b.ReportAllocs()
	for b.Loop() {
		for {
			_, err := p.Submit(context.Background(), func(context.Context) error { return nil })
			if err == nil {
				break
			}
			if !errors.Is(err, ErrFull) {
				b.Fatal(err)
			}
			runtime.Gosched()
		}
	}
}
