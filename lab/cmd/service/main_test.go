package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// The helper is a real child process running the production main and signal
// handler. It also runs under the race runtime when invoked by go test -race.
func TestProcessHelper(t *testing.T) {
	if os.Getenv("GO_SERVICE_PROCESS_HELPER") != "1" {
		return
	}
	main()
	os.Exit(0)
}

type process struct {
	cmd    *exec.Cmd
	events chan map[string]any
	done   chan error
	mu     sync.Mutex
	logs   bytes.Buffer
}

func startProcess(t *testing.T, grace string) (*process, string) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestProcessHelper$")
	cmd.Env = append(os.Environ(), "GO_SERVICE_PROCESS_HELPER=1", "LISTEN_ADDR=127.0.0.1:0", "ADMIN_ADDR=127.0.0.1:0", "WORKERS=1", "QUEUE_CAPACITY=2", "TASK_TIMEOUT=10s", "SHUTDOWN_TIMEOUT="+grace)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	p := &process{cmd: cmd, events: make(chan map[string]any, 128), done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			p.mu.Lock()
			_, _ = fmt.Fprintln(&p.logs, scanner.Text())
			p.mu.Unlock()
			var event map[string]any
			if json.Unmarshal(scanner.Bytes(), &event) == nil {
				p.events <- event
			}
		}
		p.done <- cmd.Wait()
	}()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	event := p.waitEvent(t, "listening")
	return p, "http://" + event["address"].(string)
}

func (p *process) output() string { p.mu.Lock(); defer p.mu.Unlock(); return p.logs.String() }

func (p *process) waitEvent(t *testing.T, message string) map[string]any {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-p.events:
			if event["msg"] == message {
				return event
			}
		case <-timer.C:
			t.Fatalf("missing event %q; logs:\n%s", message, p.output())
			return nil
		}
	}
}

func postTask(t *testing.T, url string) {
	t.Helper()
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Post(url, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("task status: %d", resp.StatusCode)
	}
}

func TestSIGTERMDrainsRequestAndAcceptedTasks(t *testing.T) {
	if testing.Short() {
		t.Skip("real process integration test")
	}
	p, url := startProcess(t, "3s")
	postTask(t, url+"/tasks?delay=350ms")
	p.waitEvent(t, "task started")
	postTask(t, url+"/tasks?delay=150ms") // queued, also must drain
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(url + "/work?delay=150ms")
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				err = fmt.Errorf("request status %d", resp.StatusCode)
			}
		}
		requestDone <- err
	}()
	p.waitEvent(t, "request started")
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	p.waitEvent(t, "shutdown started")
	select {
	case err := <-requestDone:
		if err != nil {
			t.Fatalf("in-flight request lost: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("request did not finish")
	}
	event := p.waitEvent(t, "shutdown complete")
	stats := event["stats"].(map[string]any)
	if stats["accepted"] != float64(2) || stats["completed"] != float64(2) || stats["failed"] != float64(0) || stats["active"] != float64(0) {
		t.Fatalf("task boundary violated: %v", stats)
	}
	select {
	case err := <-p.done:
		if err != nil {
			t.Fatalf("process: %v\n%s", err, p.output())
		}
	case <-time.After(4 * time.Second):
		t.Fatal("process did not exit")
	}
	// Listener must be gone after shutdown, not merely return a health error.
	client := &http.Client{Timeout: 200 * time.Millisecond}
	if resp, err := client.Get(url + "/healthz"); err == nil {
		resp.Body.Close()
		t.Fatal("listener still accepts after exit")
	}
	logs := p.output()
	if strings.Index(logs, "request finished") > strings.Index(logs, "shutdown complete") {
		t.Fatal("shutdown returned before handler completion")
	}
}

func TestSIGTERMDeadlineCancelsBlockedTask(t *testing.T) {
	if testing.Short() {
		t.Skip("real process integration test")
	}
	p, url := startProcess(t, "80ms")
	postTask(t, url+"/tasks?delay=5s")
	p.waitEvent(t, "task started")
	requestDone := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(url + "/work?delay=5s")
		if err == nil {
			_, err = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		requestDone <- err
	}()
	p.waitEvent(t, "request started")
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	event := p.waitEvent(t, "shutdown complete")
	stats := event["stats"].(map[string]any)
	if stats["cancelled"] != float64(1) || stats["completed"] != float64(1) || stats["active"] != float64(0) {
		t.Fatalf("cancelled task did not release: %v", stats)
	}
	select {
	case err := <-requestDone:
		if err == nil {
			t.Fatal("deadline-expired request unexpectedly succeeded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closed request did not return")
	}
	logs := p.output()
	if !strings.Contains(logs, `"msg":"request finished","path":"/work","error":"context canceled"`) {
		t.Fatalf("request cleanup was not observed before shutdown completion:\n%s", logs)
	}
	select {
	case err := <-p.done:
		if err == nil {
			t.Fatal("deadline must produce nonzero exit status")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("process did not exit after cancellation")
	}
}
