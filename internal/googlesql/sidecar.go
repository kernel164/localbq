// Package googlesql manages the GoogleSQL gRPC sidecar process.
//
// Architecture:
//
//   localbq process                    googlesql sidecar
//   ┌──────────────┐                  ┌──────────────────┐
//   │              │   gRPC (TCP)     │  execute_query    │
//   │  Sidecar     │ ──────────────>  │  + server main    │
//   │  Manager     │                  │                    │
//   │              │  RegisterCatalog │  GoogleSqlLocal    │
//   │  - start     │  Analyze         │  ServiceGrpcImpl   │
//   │  - health    │  Parse           │                    │
//   │  - restart   │  FormatSql       │  (C++ / Bazel)     │
//   │  - shutdown  │                  │                    │
//   └──────────────┘                  └──────────────────┘
//
// The sidecar binary is bundled with localbq. It starts lazily on first
// query and is kept alive for the session. On crash, it auto-restarts
// and the catalog is re-registered from the metadata store.
//
// Port assignment: the sidecar prints "GOOGLESQL_PORT=NNNN" to stdout
// on startup. The Go process reads this to know where to connect.
package googlesql

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// Sidecar manages the googlesql gRPC server process lifecycle.
type Sidecar struct {
	binaryPath string
	timeout    time.Duration

	mu   sync.Mutex
	cmd  *exec.Cmd
	conn *grpc.ClientConn
	port int
}

// NewSidecar creates a new sidecar manager. binaryPath is the path to the
// googlesql_server binary. timeout is how long to wait for startup.
func NewSidecar(binaryPath string, timeout time.Duration) *Sidecar {
	return &Sidecar{
		binaryPath: binaryPath,
		timeout:    timeout,
	}
}

// Start launches the sidecar process and waits for it to be ready.
// Returns the gRPC connection. Safe to call multiple times (idempotent).
func (s *Sidecar) Start(ctx context.Context) (*grpc.ClientConn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Already running and healthy
	if s.conn != nil && s.isHealthy(ctx) {
		return s.conn, nil
	}

	// Clean up any dead process
	s.stopLocked()

	slog.Info("starting googlesql sidecar", "binary", s.binaryPath)

	// Start the process with --port=0 (OS assigns a free port)
	cmd := exec.CommandContext(ctx, s.binaryPath, "--port=0")
	cmd.Stderr = os.Stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("googlesql: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("googlesql: start binary: %w", err)
	}
	s.cmd = cmd

	// Read the port from stdout: "GOOGLESQL_PORT=NNNN"
	portCh := make(chan int, 1)
	errCh := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "GOOGLESQL_PORT=") {
				p, err := strconv.Atoi(strings.TrimPrefix(line, "GOOGLESQL_PORT="))
				if err != nil {
					errCh <- fmt.Errorf("googlesql: bad port line: %q", line)
					return
				}
				portCh <- p
				return
			}
		}
		errCh <- fmt.Errorf("googlesql: process exited without printing port")
	}()

	// Wait for port with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	select {
	case port := <-portCh:
		s.port = port
	case err := <-errCh:
		s.stopLocked()
		return nil, err
	case <-timeoutCtx.Done():
		s.stopLocked()
		return nil, fmt.Errorf("googlesql: startup timeout after %s", s.timeout)
	}

	// Connect via gRPC
	addr := fmt.Sprintf("localhost:%d", s.port)
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		s.stopLocked()
		return nil, fmt.Errorf("googlesql: grpc dial %s: %w", addr, err)
	}
	s.conn = conn

	// Wait for health check
	if err := s.waitHealthy(timeoutCtx); err != nil {
		s.stopLocked()
		return nil, fmt.Errorf("googlesql: health check failed: %w", err)
	}

	slog.Info("googlesql sidecar ready", "port", s.port)
	return s.conn, nil
}

// Conn returns the gRPC connection, starting the sidecar if needed.
func (s *Sidecar) Conn(ctx context.Context) (*grpc.ClientConn, error) {
	return s.Start(ctx)
}

// Stop shuts down the sidecar process and closes the gRPC connection.
func (s *Sidecar) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
}

func (s *Sidecar) stopLocked() {
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- s.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			s.cmd.Process.Kill()
			<-done
		}
		s.cmd = nil
	}
	s.port = 0
}

func (s *Sidecar) isHealthy(ctx context.Context) bool {
	if s.conn == nil {
		return false
	}
	client := grpc_health_v1.NewHealthClient(s.conn)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	resp, err := client.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	return err == nil && resp.GetStatus() == grpc_health_v1.HealthCheckResponse_SERVING
}

func (s *Sidecar) waitHealthy(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.isHealthy(ctx) {
				return nil
			}
		}
	}
}

// Port returns the port the sidecar is listening on, or 0 if not started.
func (s *Sidecar) Port() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.port
}
