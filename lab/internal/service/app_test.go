package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func app(t *testing.T, cfg Config) *App {
	t.Helper()
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = a.Pool.Shutdown(ctx)
		receive(t, a.Pool.Done())
		a.Fetcher.Close()
	})
	return a
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("condition did not become true")
		}
	}
}

func TestHTTPOverloadAndTaskOwnership(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers, cfg.QueueCapacity, cfg.Logger = 1, 1, quiet()
	a := app(t, cfg)
	srv := httptest.NewServer(a.Handler())
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/tasks?delay=150ms", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("first task: %d", resp.StatusCode)
	}
	cancel() // Accepted task is owned by the pool, not this now-ended request.
	eventually(t, func() bool { return a.Pool.Stats().Active == 1 })
	for _, want := range []int{http.StatusAccepted, http.StatusServiceUnavailable} {
		resp, err := srv.Client().Post(srv.URL+"/tasks?delay=150ms", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("got %d, want %d", resp.StatusCode, want)
		}
		if want == http.StatusServiceUnavailable && resp.Header.Get("Retry-After") != "1" {
			t.Fatal("missing retry hint")
		}
	}
	a.Pool.Close()
	receive(t, a.Pool.Done())
	if s := a.Pool.Stats(); s.Completed != 2 || s.Failed != 0 {
		t.Fatalf("request cancellation leaked into task: %+v", s)
	}
}

type lockedBuffer struct {
	mu   sync.Mutex
	text strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.text.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.text.String() }

func TestSlowStreamDeadlineReleasesHandler(t *testing.T) {
	var logs lockedBuffer
	cfg := DefaultConfig()
	cfg.StreamWriteTimeout = 80 * time.Millisecond
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	a := app(t, cfg)
	finished := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.Handler().ServeHTTP(w, r)
		close(finished)
	}))
	defer srv.Close()
	conn, err := net.DialTimeout("tcp", srv.Listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.SetReadBuffer(1024)
	}
	// A real peer sends its request and never reads the 256 MiB response.
	if _, err := fmt.Fprint(conn, "GET /stream?bytes=268435456 HTTP/1.1\r\nHost: localhost\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	receive(t, finished)
	if text := logs.String(); !strings.Contains(text, "stream finished") || !strings.Contains(text, "i/o timeout") {
		t.Fatalf("stream did not hit its write deadline: %s", text)
	}
}

func TestHTTPInvalidInput(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Logger = quiet()
	a := app(t, cfg)
	for _, path := range []string{"/tasks?delay=-1s", "/tasks?url=file:///etc/passwd", "/stream?bytes=999999999"} {
		method := http.MethodPost
		if strings.HasPrefix(path, "/stream") {
			method = http.MethodGet
		}
		rr := httptest.NewRecorder()
		a.Handler().ServeHTTP(rr, httptest.NewRequest(method, path, nil))
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", path, rr.Code)
		}
	}
}

func TestDrainRequestsWaitsForHandlerAndGatesAdmission(t *testing.T) {
	var logs lockedBuffer
	cfg := DefaultConfig()
	cfg.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	a := app(t, cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan struct{})
	go func() {
		a.Handler().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/work?delay=5s", nil).WithContext(ctx))
		close(finished)
	}()
	eventually(t, func() bool { return strings.Contains(logs.String(), "request started") })
	short, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	if err := a.DrainRequests(short); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("drain returned while handler still owned resources: %v", err)
	}
	cancel()
	receive(t, finished)
	long, end := context.WithTimeout(context.Background(), time.Second)
	defer end()
	if err := a.DrainRequests(long); err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	a.Handler().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("new handler admitted during drain: %d", rr.Code)
	}
}
