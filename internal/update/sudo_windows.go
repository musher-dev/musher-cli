//go:build windows

package update

import "fmt"

// NeedsElevation always returns false on Windows (no auto-elevation).
func NeedsElevation(binaryPath string) bool {
	return false
}

// ReExecWithSudo is not supported on Windows.
func ReExecWithSudo() error {
	return fmt.Errorf("automatic elevation is not supported on Windows; please run this command as Administrator")
}
