// Package service demonstrates bounded concurrency and explicit task ownership.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrFull   = errors.New("task queue full")
	ErrClosed = errors.New("task pool closed")
)

type Work func(context.Context) error

type task struct {
	id   uint64
	work Work
}

type Stats struct {
	Accepted  uint64 `json:"accepted"`
	Started   uint64 `json:"started"`
	Completed uint64 `json:"completed"`
	Failed    uint64 `json:"failed"`
	Cancelled uint64 `json:"cancelled"`
	Active    int64  `json:"active"`
	Queued    int    `json:"queued"`
}

// Pool owns every accepted task. A caller's context controls admission only;
// after admission, only the pool lifetime and per-task deadline cancel the task.
type Pool struct {
	mu        sync.Mutex
	closed    bool
	queue     chan task
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	timeout   time.Duration
	log       *slog.Logger
	next      atomic.Uint64
	accepted  atomic.Uint64
	started   atomic.Uint64
	completed atomic.Uint64
	failed    atomic.Uint64
	cancelled atomic.Uint64
	active    atomic.Int64
}

func NewPool(workers, capacity int, timeout time.Duration, logger *slog.Logger) (*Pool, error) {
	if workers < 1 || capacity < 1 || timeout <= 0 {
		return nil, fmt.Errorf("workers, capacity, and timeout must be positive")
	}
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &Pool{queue: make(chan task, capacity), ctx: ctx, cancel: cancel, done: make(chan struct{}), timeout: timeout, log: logger}
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for t := range p.queue {
				p.run(t)
			}
		}()
	}
	go func() { wg.Wait(); cancel(); close(p.done) }()
	return p, nil
}

func (p *Pool) run(t task) {
	ctx, cancel := context.WithTimeout(p.ctx, p.timeout)
	defer cancel()
	p.active.Add(1)
	p.started.Add(1)
	p.log.Info("task started", "id", t.id)
	var err error
	if err = ctx.Err(); err == nil {
		err = t.work(ctx)
	}
	if err != nil {
		p.failed.Add(1)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			p.cancelled.Add(1)
		}
	}
	p.completed.Add(1)
	p.active.Add(-1)
	p.log.Info("task finished", "id", t.id, "error", err)
}

// Submit never waits for queue space. The queue bound excludes running tasks.
func (p *Pool) Submit(ctx context.Context, work Work) (uint64, error) {
	if work == nil {
		return 0, errors.New("nil work")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if p.closed {
		return 0, ErrClosed
	}
	id := p.next.Add(1)
	select {
	case p.queue <- task{id: id, work: work}:
		p.accepted.Add(1)
		p.log.Info("task accepted", "id", id)
		return id, nil
	default:
		return 0, ErrFull
	}
}

// Close stops admission and lets workers drain the queue. It is idempotent.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.closed {
		p.closed = true
		close(p.queue)
	}
}

// Shutdown drains until the deadline, then signals cancellation. It cannot
// force a Work function that ignores context to stop. Done reports actual exit.
func (p *Pool) Shutdown(ctx context.Context) error {
	p.Close()
	select {
	case <-p.done:
		return nil
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	}
}

func (p *Pool) Done() <-chan struct{} { return p.done }

func (p *Pool) Stats() Stats {
	return Stats{Accepted: p.accepted.Load(), Started: p.started.Load(), Completed: p.completed.Load(), Failed: p.failed.Load(), Cancelled: p.cancelled.Load(), Active: p.active.Load(), Queued: len(p.queue)}
}
