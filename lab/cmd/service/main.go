package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"example.com/go-service-engineering/lab/internal/service"
)

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger) error {
	cfg := service.DefaultConfig()
	cfg.Logger = logger
	var err error
	if cfg.Workers, err = strconv.Atoi(env("WORKERS", "2")); err != nil {
		return fmt.Errorf("WORKERS: %w", err)
	}
	if cfg.QueueCapacity, err = strconv.Atoi(env("QUEUE_CAPACITY", "8")); err != nil {
		return fmt.Errorf("QUEUE_CAPACITY: %w", err)
	}
	if cfg.TaskTimeout, err = time.ParseDuration(env("TASK_TIMEOUT", "5s")); err != nil {
		return fmt.Errorf("TASK_TIMEOUT: %w", err)
	}
	if cfg.StreamWriteTimeout, err = time.ParseDuration(env("STREAM_WRITE_TIMEOUT", "250ms")); err != nil {
		return fmt.Errorf("STREAM_WRITE_TIMEOUT: %w", err)
	}
	grace, err := time.ParseDuration(env("SHUTDOWN_TIMEOUT", "5s"))
	if err != nil || grace <= 0 {
		return errors.New("SHUTDOWN_TIMEOUT must be a positive duration")
	}
	app, err := service.New(cfg)
	if err != nil {
		return err
	}
	defer app.Fetcher.Close()
	defer func() {
		cleanup, cancel := context.WithCancel(context.Background())
		cancel()
		_ = app.Pool.Shutdown(cleanup)
	}()
	listener, err := net.Listen("tcp", env("LISTEN_ADDR", "127.0.0.1:8080"))
	if err != nil {
		return err
	}
	defer listener.Close()
	adminListener, err := net.Listen("tcp", env("ADMIN_ADDR", "127.0.0.1:6060"))
	if err != nil {
		return err
	}
	defer adminListener.Close()
	server := &http.Server{Handler: app.Handler(), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16 << 10}
	admin := &http.Server{Handler: http.DefaultServeMux, ReadHeaderTimeout: 2 * time.Second, IdleTimeout: 30 * time.Second}
	defer server.Close()
	defer admin.Close()
	serveErrors := make(chan error, 2)
	go func() { serveErrors <- server.Serve(listener) }()
	go func() { serveErrors <- admin.Serve(adminListener) }()
	logger.Info("listening", "address", listener.Addr().String(), "admin", adminListener.Addr().String())
	select {
	case <-ctx.Done():
	case err := <-serveErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	logger.Info("shutdown started")
	shutdown, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	// Shutdown first stops admission at the listener and waits for request
	// handlers. Only then close task admission and drain accepted background work.
	httpErr := server.Shutdown(shutdown)
	if httpErr != nil {
		_ = server.Close()
	}
	_ = admin.Close()
	poolErr := app.Pool.Shutdown(shutdown)
	// Closing sockets cancels request contexts, but Server.Close does not wait
	// for their handlers. Explicitly wait for cooperative cleanup as well.
	cleanup, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
	defer cleanupCancel()
	requestErr := app.DrainRequests(cleanup)
	select {
	case <-app.Pool.Done():
	case <-cleanup.Done():
		return errors.New("workers ignored cancellation for one second")
	}
	logger.Info("shutdown complete", "stats", app.Pool.Stats(), "http_error", httpErr, "pool_error", poolErr, "request_error", requestErr)
	return errors.Join(httpErr, poolErr, requestErr)
}
