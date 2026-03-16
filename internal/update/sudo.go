//go:build !windows

package update

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// NeedsElevation returns true if the binary's parent directory is not writable.
func NeedsElevation(binaryPath string) bool {
	dir := filepath.Dir(binaryPath)

	// Check writable by attempting to create a temporary file.
	f, err := os.CreateTemp(dir, ".musher-update-check-*")
	if err != nil {
		return true
	}

	_ = f.Close()
	_ = os.Remove(f.Name())

	return false
}

// ReExecWithSudo re-launches the current command under sudo.
// This replaces the current process via syscall.Exec.
func ReExecWithSudo() error {
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return fmt.Errorf("sudo not found in PATH; run this command with elevated permissions manually")
	}

	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	fmt.Fprintln(os.Stderr, "Elevated permissions required. Requesting sudo...")

	argv := append([]string{"sudo", execPath}, os.Args[1:]...)

	if err := syscall.Exec(sudoPath, argv, os.Environ()); err != nil { //nolint:gosec // G204: intentional sudo re-exec
		return fmt.Errorf("exec sudo process: %w", err)
	}

	return nil
}
