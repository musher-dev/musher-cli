//go:build unix

package install_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

type runResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func scriptPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("../../install.sh")
	if err != nil {
		t.Fatalf("resolve install.sh path: %v", err)
	}

	return path
}

func runScript(t *testing.T, args, env []string) runResult {
	t.Helper()

	ctx, cancel := contextWithDeadline(t)
	defer cancel()

	cmdArgs := append([]string{scriptPath(t)}, args...)
	cmd := exec.CommandContext(ctx, "sh", cmdArgs...)

	cmd.Env = append(os.Environ(), env...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return collectRunResult(t, err, stdout.String(), stderr.String())
}

func runFunction(t *testing.T, fnCall string, env []string) runResult {
	t.Helper()

	ctx, cancel := contextWithDeadline(t)
	defer cancel()

	script := fmt.Sprintf(". '%s'\n%s", scriptPath(t), fnCall)
	cmd := exec.CommandContext(ctx, "sh", "-c", script)

	cmd.Env = append(os.Environ(), "INSTALL_SH_TESTING=1")
	cmd.Env = append(cmd.Env, env...)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	return collectRunResult(t, err, stdout.String(), stderr.String())
}

func collectRunResult(t *testing.T, err error, stdout, stderr string) runResult {
	t.Helper()

	exitCode := 0

	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run script: %v", err)
		}
	}

	return runResult{
		stdout:   stdout,
		stderr:   stderr,
		exitCode: exitCode,
	}
}

func contextWithDeadline(t *testing.T) (ctx context.Context, cancel func()) {
	t.Helper()

	deadline, ok := t.Deadline()
	if !ok {
		return context.WithTimeout(t.Context(), 30*time.Second)
	}

	return context.WithDeadline(t.Context(), deadline.Add(-time.Second))
}

func makeFakeArchive(t *testing.T) (data []byte, checksum string) {
	t.Helper()

	var buf bytes.Buffer

	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	content := []byte("#!/bin/sh\necho 'musher test build'\n")
	hdr := &tar.Header{
		Name: "musher",
		Mode: 0o755,
		Size: int64(len(content)),
	}

	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}

	if _, err := tw.Write(content); err != nil {
		t.Fatalf("write tar content: %v", err)
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("close tar writer: %v", err)
	}

	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip writer: %v", err)
	}

	data = buf.Bytes()
	sum := sha256.Sum256(data)

	return data, hex.EncodeToString(sum[:])
}

func hostArchiveName(t *testing.T, version string) string {
	t.Helper()

	var osName string

	switch runtime.GOOS {
	case "linux":
		osName = "linux"
	case "darwin":
		osName = "darwin"
	default:
		t.Skipf("unsupported OS %s", runtime.GOOS)
	}

	var archName string

	switch runtime.GOARCH {
	case "amd64":
		archName = "amd64"
	case "arm64":
		archName = "arm64"
	default:
		t.Skipf("unsupported arch %s", runtime.GOARCH)
	}

	return fmt.Sprintf("musher_%s_%s_%s.tar.gz", version, osName, archName)
}

func newMockGitHub(t *testing.T, version string, archive []byte, checksums string) *httptest.Server {
	t.Helper()

	tag := "v" + version
	server := &httptest.Server{
		Config: &http.Server{
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/releases/latest":
					http.Redirect(w, r, "/releases/tag/"+tag, http.StatusFound)
				case r.URL.Path == "/releases/tag/"+tag:
					w.WriteHeader(http.StatusOK)
				case strings.HasSuffix(r.URL.Path, ".tar.gz"):
					w.Header().Set("Content-Type", "application/octet-stream")
					_, _ = w.Write(archive)
				case strings.HasSuffix(r.URL.Path, "checksums.txt"):
					w.Header().Set("Content-Type", "text/plain")
					_, _ = w.Write([]byte(checksums))
				default:
					http.NotFound(w, r)
				}
			}),
		},
	}

	lc := net.ListenConfig{}

	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("local listener not available in this environment: %v", err)
	}

	server.Listener = ln
	server.StartTLS()

	return server
}

func TestArgumentParsing(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		result := runScript(t, []string{"--help"}, nil)
		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr: %s", result.exitCode, result.stderr)
		}

		if !strings.Contains(result.stdout, "Install Musher CLI") {
			t.Fatalf("stdout missing help text: %s", result.stdout)
		}
	})

	t.Run("missing version value", func(t *testing.T) {
		result := runScript(t, []string{"--version"}, nil)
		if result.exitCode == 0 {
			t.Fatal("expected non-zero exit code")
		}

		if !strings.Contains(result.stderr, "--version requires a value") {
			t.Fatalf("stderr missing validation message: %s", result.stderr)
		}
	})

	t.Run("unknown option", func(t *testing.T) {
		result := runScript(t, []string{"--bogus"}, nil)
		if result.exitCode == 0 {
			t.Fatal("expected non-zero exit code")
		}

		if !strings.Contains(result.stderr, "Unknown option") {
			t.Fatalf("stderr missing unknown-option message: %s", result.stderr)
		}
	})
}

func TestHelperFunctions(t *testing.T) {
	t.Run("check_path warns when directory missing from PATH", func(t *testing.T) {
		result := runFunction(t, "check_path /tmp/musher-not-in-path", nil)
		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", result.exitCode)
		}

		if !strings.Contains(result.stdout, "not in your PATH") {
			t.Fatalf("stdout missing PATH warning: %s", result.stdout)
		}
	})

	t.Run("maybe_sudo returns empty for writable directory", func(t *testing.T) {
		result := runFunction(t, fmt.Sprintf("maybe_sudo %q", t.TempDir()), nil)
		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", result.exitCode)
		}

		if strings.TrimSpace(result.stdout) != "" {
			t.Fatalf("stdout = %q, want empty", result.stdout)
		}
	})
}

func TestEndToEnd(t *testing.T) {
	t.Run("happy path with explicit version", func(t *testing.T) {
		archive, checksum := makeFakeArchive(t)
		archiveName := hostArchiveName(t, "0.1.0")

		server := newMockGitHub(t, "0.1.0", archive, fmt.Sprintf("%s  %s\n", checksum, archiveName))
		defer server.Close()

		prefix := filepath.Join(t.TempDir(), "install-root")
		result := runScript(t, []string{"--version", "0.1.0", "--prefix", prefix, "--yes"}, []string{
			"MUSHER_INSTALL_BASE_URL=" + server.URL,
			"MUSHER_INSTALL_INSECURE=1",
		})

		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
		}

		if !strings.Contains(result.stdout, "Successfully installed musher v0.1.0") {
			t.Fatalf("stdout missing success text: %s", result.stdout)
		}

		binPath := filepath.Join(prefix, "bin", "musher")

		info, err := os.Stat(binPath)
		if err != nil {
			t.Fatalf("stat installed binary: %v", err)
		}

		if info.Mode()&0o111 == 0 {
			t.Fatalf("binary is not executable: mode %o", info.Mode())
		}
	})

	t.Run("resolves latest version via redirect", func(t *testing.T) {
		archive, checksum := makeFakeArchive(t)
		archiveName := hostArchiveName(t, "0.3.2")

		server := newMockGitHub(t, "0.3.2", archive, fmt.Sprintf("%s  %s\n", checksum, archiveName))
		defer server.Close()

		result := runScript(t, []string{"--prefix", t.TempDir(), "--yes"}, []string{
			"MUSHER_INSTALL_BASE_URL=" + server.URL,
			"MUSHER_INSTALL_INSECURE=1",
		})

		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
		}

		if !strings.Contains(result.stdout, "Version:  v0.3.2") {
			t.Fatalf("stdout missing resolved version: %s", result.stdout)
		}
	})

	t.Run("checksum mismatch fails", func(t *testing.T) {
		archive, _ := makeFakeArchive(t)
		archiveName := hostArchiveName(t, "0.1.0")

		server := newMockGitHub(t, "0.1.0", archive, strings.Repeat("0", 64)+"  "+archiveName+"\n")
		defer server.Close()

		result := runScript(t, []string{"--version", "0.1.0", "--prefix", t.TempDir(), "--yes"}, []string{
			"MUSHER_INSTALL_BASE_URL=" + server.URL,
			"MUSHER_INSTALL_INSECURE=1",
		})

		if result.exitCode == 0 {
			t.Fatal("expected non-zero exit code")
		}

		if !strings.Contains(result.stderr, "Checksum mismatch") {
			t.Fatalf("stderr missing checksum message: %s", result.stderr)
		}
	})
}
