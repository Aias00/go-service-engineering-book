package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestConnectionExhaustionAndRecovery(t *testing.T) {
	released := make(chan struct{})
	var okRequests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(released)
			return
		}
		okRequests.Add(1)
		_, _ = io.WriteString(w, "recovered")
	}))
	defer srv.Close()
	f := NewFetcher(1)
	defer f.Close()
	resp, err := f.Client.Get(srv.URL + "/hold")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.Fetch(ctx, srv.URL+"/ok"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiting for occupied connection: %v", err)
	}
	if got := okRequests.Load(); got != 0 {
		t.Fatalf("request bypassed connection bound: %d", got)
	}
	_ = resp.Body.Close()
	receive(t, released)
	recovery, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if n, err := f.Fetch(recovery, srv.URL+"/ok"); err != nil || n != 9 {
		t.Fatalf("recovery: bytes=%d error=%v", n, err)
	}
}

func TestFetcherReusesConnectionIncludingErrorStatus(t *testing.T) {
	var connections atomic.Int64
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/error" {
			w.WriteHeader(http.StatusBadGateway)
		}
		_, _ = io.WriteString(w, "small body")
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()
	f := NewFetcher(1)
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := f.Fetch(ctx, srv.URL+"/error"); err == nil {
		t.Fatal("expected status error")
	}
	for range 3 {
		if _, err := f.Fetch(ctx, srv.URL); err != nil {
			t.Fatal(err)
		}
	}
	if n := connections.Load(); n != 1 {
		t.Fatalf("body was not drained/reused: connections=%d", n)
	}
}

func TestFetcherBlockedBodyCancellationReleasesConnection(t *testing.T) {
	released := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/blocked" {
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			close(released)
			return
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	f := NewFetcher(1)
	defer f.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := f.Fetch(ctx, srv.URL+"/blocked"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked body: %v", err)
	}
	receive(t, released)
	recovery, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if _, err := f.Fetch(recovery, srv.URL); err != nil {
		t.Fatalf("connection slot not recovered: %v", err)
	}
}
