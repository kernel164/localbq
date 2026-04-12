package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func pidFile() string {
	return filepath.Join(*dataDir, "localbq.pid")
}

func logFile() string {
	return filepath.Join(*dataDir, "localbq.log")
}

// daemonStart re-executes localbq in the background with logs redirected to a file.
func daemonStart() {
	if err := os.MkdirAll(*dataDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Check if already running
	if pid, running := daemonRunning(); running {
		fmt.Fprintf(os.Stderr, "LocalBQ is already running (PID %d)\n", pid)
		os.Exit(1)
	}

	// Build args: strip -d/--daemon flag, keep everything else
	var args []string
	for _, arg := range os.Args[1:] {
		if arg == "-d" || arg == "--daemon" {
			continue
		}
		args = append(args, arg)
	}

	// Open log file
	lf, err := os.OpenFile(logFile(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening log file: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(os.Args[0], args...)
	cmd.Stdout = lf
	cmd.Stderr = lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		lf.Close()
		fmt.Fprintf(os.Stderr, "Error starting daemon: %v\n", err)
		os.Exit(1)
	}
	lf.Close()

	// Write PID file
	if err := os.WriteFile(pidFile(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to write PID file: %v\n", err)
	}

	// Wait briefly and check if process is still alive
	time.Sleep(500 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		fmt.Fprintf(os.Stderr, "Error: daemon exited immediately. Check %s\n", logFile())
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "LocalBQ started in background (PID %d)\n", cmd.Process.Pid)
	fmt.Fprintf(os.Stderr, "  REST API: http://localhost:%d\n", *port)
	fmt.Fprintf(os.Stderr, "  Logs:     %s\n", logFile())
	fmt.Fprintf(os.Stderr, "  Stop:     localbq stop\n")
}

// runStop stops a running daemon.
func runStop() {
	pid, running := daemonRunning()
	if !running {
		fmt.Fprintf(os.Stderr, "LocalBQ is not running\n")
		os.Exit(1)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding process %d: %v\n", pid, err)
		os.Exit(1)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		fmt.Fprintf(os.Stderr, "Error stopping LocalBQ (PID %d): %v\n", pid, err)
		os.Exit(1)
	}

	// Wait for process to exit
	for i := 0; i < 30; i++ {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	os.Remove(pidFile())
	fmt.Fprintf(os.Stderr, "LocalBQ stopped (PID %d)\n", pid)
}

// runStatus shows whether the daemon is running.
func runStatus() {
	pid, running := daemonRunning()
	if !running {
		fmt.Fprintf(os.Stderr, "LocalBQ is not running\n")
		os.Exit(1)
	}

	// Try to hit the status endpoint
	resp, err := http.Get(fmt.Sprintf("http://localhost:%d/", *port))
	if err != nil {
		fmt.Fprintf(os.Stderr, "LocalBQ process running (PID %d) but not responding on port %d\n", pid, *port)
		os.Exit(1)
	}
	resp.Body.Close()

	fmt.Fprintf(os.Stderr, "LocalBQ is running (PID %d) on port %d\n", pid, *port)
}

// daemonRunning checks if a daemon is running by reading the PID file.
func daemonRunning() (int, bool) {
	data, err := os.ReadFile(pidFile())
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}

	// Check if process is alive
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		os.Remove(pidFile()) // stale PID file
		return 0, false
	}

	return pid, true
}
