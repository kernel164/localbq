package googlesql

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// FindBinary locates the googlesql_server binary.
// Search order:
//  1. GOOGLESQL_SERVER_PATH environment variable
//  2. Next to the localbq binary (./googlesql_server)
//  3. In the bin/ directory relative to the localbq binary
//  4. In PATH
func FindBinary() (string, error) {
	// 1. Explicit env var
	if p := os.Getenv("GOOGLESQL_SERVER_PATH"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
		return "", fmt.Errorf("GOOGLESQL_SERVER_PATH=%q: file not found", p)
	}

	// Determine the platform-specific binary name
	name := "googlesql_server"
	if runtime.GOOS == "darwin" {
		name = "googlesql_server_darwin"
	} else if runtime.GOOS == "linux" {
		name = "googlesql_server_linux"
	}

	// 2. Next to the running binary
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		for _, candidate := range []string{name, "googlesql_server"} {
			p := filepath.Join(dir, candidate)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}

		// 3. In bin/ relative to exe
		for _, candidate := range []string{name, "googlesql_server"} {
			p := filepath.Join(dir, "bin", candidate)
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}

	// 4. In PATH
	if p, err := findInPath(name); err == nil {
		return p, nil
	}
	if p, err := findInPath("googlesql_server"); err == nil {
		return p, nil
	}

	return "", fmt.Errorf("googlesql_server binary not found. Set GOOGLESQL_SERVER_PATH or place it next to the localbq binary")
}

func findInPath(name string) (string, error) {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	for _, dir := range dirs {
		p := filepath.Join(dir, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", fmt.Errorf("%s not in PATH", name)
}
