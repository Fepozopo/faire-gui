//go:build darwin

package updater

import (
	"fmt"
	"os"
	"syscall"
)

// scheduleRestart atomically replaces the Darwin executable and replaces this process with the updated application.
func scheduleRestart(executablePath, downloadedPath string) error {
	if err := os.Rename(downloadedPath, executablePath); err != nil {
		return fmt.Errorf("replace application executable: %w", err)
	}
	if err := syscall.Exec(executablePath, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("restart updated application: %w", err)
	}
	return nil
}
