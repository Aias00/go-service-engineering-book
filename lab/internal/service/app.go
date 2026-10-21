package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

type Config struct {
	Workers            int
	QueueCapacity      int
	TaskTimeout        time.Duration
	StreamWriteTimeout time.Duration
	Logger             *slog.Logger
}

func DefaultConfig() Config {
	return Config{Workers: 2, QueueCapacity: 8, TaskTimeout: 5 * time.Second, StreamWriteTimeout: 250 * time.Millisecond}
}

type App struct {
	Pool           *Pool
	Fetcher        *Fetcher
	cfg            Config
	log            *slog.Logger
	requestMu      sync.Mutex
	requestsClosed bool
	requests       sync.WaitGroup
}

func New(cfg Config) (*App, error) {
	if cfg.StreamWriteTimeout <= 0 {
		return nil, fmt.Errorf("stream write timeout must be positive")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	pool, err := NewPool(cfg.Workers, cfg.QueueCapacity, cfg.TaskTimeout, cfg.Logger)
	if err != nil {
		return nil, err
	}
	return &App{Pool: pool, Fetcher: NewFetcher(2), cfg: cfg, log: cfg.Logger}, nil
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", a.tasks)
	mux.HandleFunc("GET /work", a.work)
	mux.HandleFunc("GET /stream", a.stream)
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, http.StatusOK, a.Pool.Stats()) })
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.requestMu.Lock()
		if a.requestsClosed {
			a.requestMu.Unlock()
			http.Error(w, "requests draining", http.StatusServiceUnavailable)
			return
		}
		a.requests.Add(1)
		a.requestMu.Unlock()
		defer a.requests.Done()
		mux.ServeHTTP(w, r)
	})
}

// DrainRequests gates new handlers and waits for existing handlers to return.
// Call it after stopping the listener. The gate prevents Add racing with Wait
// when the previous request count is zero. It does not itself cancel requests.
func (a *App) DrainRequests(ctx context.Context) error {
	a.requestMu.Lock()
	a.requestsClosed = true
	a.requestMu.Unlock()
	done := make(chan struct{})
	go func() { a.requests.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func duration(r *http.Request) (time.Duration, error) {
	s := r.URL.Query().Get("delay")
	if s == "" {
		return 100 * time.Millisecond, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 || d > 30*time.Second {
		return 0, errors.New("delay must be between 0 and 30s")
	}
	return d, nil
}

func wait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *App) tasks(w http.ResponseWriter, r *http.Request) {
	d, err := duration(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target := r.URL.Query().Get("url")
	if target != "" {
		u, parseErr := url.Parse(target)
		if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "url must be an absolute http(s) URL", http.StatusBadRequest)
			return
		}
	}
	// Capture values only. Do not retain r, ResponseWriter, or r.Context().
	id, err := a.Pool.Submit(r.Context(), func(ctx context.Context) error {
		if target != "" {
			_, err := a.Fetcher.Fetch(ctx, target)
			return err
		}
		return wait(ctx, d)
	})
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, context.Canceled) {
			status = http.StatusRequestTimeout
		}
		w.Header().Set("Retry-After", "1")
		http.Error(w, err.Error(), status)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]uint64{"id": id})
}

func (a *App) work(w http.ResponseWriter, r *http.Request) {
	d, err := duration(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	a.log.Info("request started", "path", "/work")
	err = wait(r.Context(), d)
	a.log.Info("request finished", "path", "/work", "error", err)
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "done"})
}

func (a *App) stream(w http.ResponseWriter, r *http.Request) {
	n := int64(64 << 20)
	if raw := r.URL.Query().Get("bytes"); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 1 || value > 256<<20 {
			http.Error(w, "bytes must be 1..268435456", http.StatusBadRequest)
			return
		}
		n = value
	}
	controller := http.NewResponseController(w)
	// One absolute deadline bounds the entire stream, including blocked writes.
	if err := controller.SetWriteDeadline(time.Now().Add(a.cfg.StreamWriteTimeout)); err != nil {
		http.Error(w, "write deadlines unavailable", http.StatusInternalServerError)
		return
	}
	// Keep the deadline active through net/http's final buffered flush. This
	// HTTP/1.1 demo closes streaming connections rather than reusing one with a
	// stale deadline. Other endpoints retain ordinary keep-alive behavior.
	w.Header().Set("Connection", "close")
	w.Header().Set("Content-Type", "application/octet-stream")
	chunk := make([]byte, 32<<10)
	var sent int64
	for sent < n {
		if err := r.Context().Err(); err != nil {
			a.log.Info("stream finished", "bytes", sent, "error", err)
			return
		}
		size := min(int64(len(chunk)), n-sent)
		written, err := w.Write(chunk[:size])
		sent += int64(written)
		if err != nil {
			a.log.Info("stream finished", "bytes", sent, "error", err)
			return
		}
	}
	a.log.Info("stream finished", "bytes", sent)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
