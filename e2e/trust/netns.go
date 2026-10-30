// netns.go — reads the operator's controller-runtime metrics. The chart binds
// them to 127.0.0.1:8080 inside the operator pod, so the scenario (root on
// the node) enters the pod's network namespace on a dedicated OS thread and
// dials from there.
package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"aquaduct.dev/weft-operator/e2e/e2ekit"
)

// sysSetns is setns(2) on linux/amd64 (the VM's only architecture); the
// syscall package does not name it.
const sysSetns = 308

// operatorMetrics returns the operator's /metrics text.
func operatorMetrics(ctx context.Context) (string, error) {
	pid, err := e2ekit.ContainerPID(ctx, "weft-operator")
	if err != nil {
		return "", err
	}
	type result struct {
		body string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		// Never unlocked: the thread's network namespace is changed, so
		// the runtime must throw the thread away when this goroutine ends.
		runtime.LockOSThread()
		body, err := func() (string, error) {
			fd, err := syscall.Open(fmt.Sprintf("/proc/%d/ns/net", pid), syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
			if err != nil {
				return "", fmt.Errorf("open netns: %w", err)
			}
			defer syscall.Close(fd)
			if _, _, errno := syscall.RawSyscall(sysSetns, uintptr(fd), syscall.CLONE_NEWNET, 0); errno != 0 {
				return "", fmt.Errorf("setns: %w", errno)
			}
			// The socket is created on this thread, so in the pod's netns.
			conn, err := net.DialTimeout("tcp", "127.0.0.1:8080", 5*time.Second)
			if err != nil {
				return "", err
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.WriteString(conn, "GET /metrics HTTP/1.0\r\nHost: localhost\r\n\r\n"); err != nil {
				return "", err
			}
			b, err := io.ReadAll(conn)
			return string(b), err
		}()
		ch <- result{body, err}
	}()
	select {
	case r := <-ch:
		if r.err == nil && !strings.HasPrefix(r.body, "HTTP/1.") {
			return "", fmt.Errorf("unexpected metrics response: %.200s", r.body)
		}
		return r.body, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// webhookRequests sums controller_runtime_webhook_requests_total for the
// trust webhook path over all response codes.
func webhookRequests(ctx context.Context) (int, error) {
	body, err := operatorMetrics(ctx)
	if err != nil {
		return 0, err
	}
	total, seen := 0, false
	sc := bufio.NewScanner(strings.NewReader(body))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "controller_runtime_webhook_requests_total{") || !strings.Contains(line, `webhook="/inject-trust"`) {
			continue
		}
		f := strings.Fields(line)
		v, err := strconv.ParseFloat(f[len(f)-1], 64)
		if err != nil {
			return 0, fmt.Errorf("parse %q: %w", line, err)
		}
		total += int(v)
		seen = true
	}
	if !seen {
		// No request yet: the counter vector has no series.
		return 0, nil
	}
	return total, nil
}
