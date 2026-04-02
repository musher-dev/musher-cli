//go:build windows

package install_test

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type psRunResult struct {
	stdout   string
	stderr   string
	exitCode int
}

func powershellBinary(t *testing.T) string {
	t.Helper()

	if path, err := exec.LookPath("pwsh"); err == nil {
		return path
	}

	if path, err := exec.LookPath("powershell"); err == nil {
		return path
	}

	t.Skip("PowerShell is not available")
	return ""
}

func scriptPath(t *testing.T) string {
	t.Helper()

	path, err := filepath.Abs("../../install.ps1")
	if err != nil {
		t.Fatalf("resolve install.ps1 path: %v", err)
	}

	return path
}

func runPowerShellScript(t *testing.T, args, env []string) psRunResult {
	t.Helper()

	ctx, cancel := time.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	cmdArgs := append([]string{
		"-NoProfile",
		"-ExecutionPolicy", "Bypass",
		"-File", scriptPath(t),
	}, args...)

	cmd := exec.CommandContext(ctx, powershellBinary(t), cmdArgs...)
	cmd.Env = append(os.Environ(), env...)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("run PowerShell script: %v", err)
		}
	}

	return psRunResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode,
	}
}

func makeFakeZip(t *testing.T) ([]byte, string) {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	file, err := zw.Create("musher.exe")
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}

	if _, err := file.Write([]byte("fake-windows-binary")); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}

	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}

	data := buf.Bytes()
	sum := sha256.Sum256(data)

	return data, fmt.Sprintf("%x", sum)
}

func newMockGitHub(t *testing.T, version string, archive []byte, checksums string) *httptest.Server {
	t.Helper()

	tag := "v" + version
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/releases/latest":
			http.Redirect(w, r, fmt.Sprintf("/releases/tag/%s", tag), http.StatusFound)
		case strings.HasSuffix(r.URL.Path, ".zip"):
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write(archive)
		case strings.HasSuffix(r.URL.Path, "checksums.txt"):
			_, _ = w.Write([]byte(checksums))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestInstallPS1(t *testing.T) {
	t.Run("help", func(t *testing.T) {
		result := runPowerShellScript(t, []string{"-Help"}, nil)
		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr: %s", result.exitCode, result.stderr)
		}

		if !strings.Contains(result.stdout, "Install Musher CLI") {
			t.Fatalf("stdout missing help text: %s", result.stdout)
		}
	})

	t.Run("happy path", func(t *testing.T) {
		archive, checksum := makeFakeZip(t)
		version := "0.3.2"
		archiveName := fmt.Sprintf("musher_%s_windows_amd64.zip", version)
		server := newMockGitHub(t, version, archive, fmt.Sprintf("%s  %s\n", checksum, archiveName))
		defer server.Close()

		installDir := t.TempDir()
		result := runPowerShellScript(t, []string{"-Version", version, "-InstallDir", installDir, "-Yes"}, []string{
			"MUSHER_INSTALL_BASE_URL=" + server.URL,
			"MUSHER_INSTALL_INSECURE=1",
			"PROCESSOR_ARCHITECTURE=AMD64",
		})

		if result.exitCode != 0 {
			t.Fatalf("exit code = %d, want 0\nstdout: %s\nstderr: %s", result.exitCode, result.stdout, result.stderr)
		}

		if !strings.Contains(result.stdout, "Successfully installed musher v0.3.2") {
			t.Fatalf("stdout missing success text: %s", result.stdout)
		}

		if _, err := os.Stat(filepath.Join(installDir, "musher.exe")); err != nil {
			t.Fatalf("stat installed binary: %v", err)
		}
	})

	t.Run("checksum mismatch fails", func(t *testing.T) {
		archive, _ := makeFakeZip(t)
		version := "0.3.2"
		archiveName := fmt.Sprintf("musher_%s_windows_amd64.zip", version)
		server := newMockGitHub(t, version, archive, strings.Repeat("0", 64)+"  "+archiveName+"\n")
		defer server.Close()

		result := runPowerShellScript(t, []string{"-Version", version, "-InstallDir", t.TempDir(), "-Yes"}, []string{
			"MUSHER_INSTALL_BASE_URL=" + server.URL,
			"MUSHER_INSTALL_INSECURE=1",
			"PROCESSOR_ARCHITECTURE=AMD64",
		})

		if result.exitCode == 0 {
			t.Fatal("expected non-zero exit code")
		}

		if !strings.Contains(result.stdout+result.stderr, "Checksum mismatch") {
			t.Fatalf("missing checksum failure message\nstdout: %s\nstderr: %s", result.stdout, result.stderr)
		}
	})
}
