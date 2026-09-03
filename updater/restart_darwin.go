//go:build darwin

package updater

import (
	"fmt"
	"os"
	"syscall"
)

// scheduleRestart atomically replaces executablePath with downloadedPath and execs the updated Darwin application.
// executablePath is the running program, downloadedPath is its verified replacement, and parentProcessID is unused because syscall.Exec replaces this process directly. It returns an error when replacement or exec cannot proceed.
func scheduleRestart(executablePath, downloadedPath string, _ int) error {
	if err := os.Rename(downloadedPath, executablePath); err != nil {
		return fmt.Errorf("replace application executable: %w", err)
	}
	if err := syscall.Exec(executablePath, os.Args, os.Environ()); err != nil {
		return fmt.Errorf("restart updated application: %w", err)
	}
	return nil
}
