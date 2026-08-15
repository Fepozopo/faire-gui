//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// escapedBatchPath returns a path safe for use inside a quoted Windows batch-file argument.
func escapedBatchPath(path string) string {
	// Percent signs are expanded by cmd.exe even in quotes, so double them before writing a batch file.
	return strings.ReplaceAll(path, "%", "%%")
}

// scheduleRestart launches a detached helper because Windows locks a running executable until the application exits.
func scheduleRestart(executablePath, downloadedPath string) error {
	script, err := os.CreateTemp("", "faire-gui-update-*.cmd")
	if err != nil {
		return fmt.Errorf("create update helper: %w", err)
	}
	scriptPath := script.Name()
	removeScript := true
	defer func() {
		if removeScript {
			_ = os.Remove(scriptPath)
		}
	}()

	contents := strings.Join([]string{
		"@echo off",
		"setlocal DisableDelayedExpansion",
		":retry",
		"move /Y \"" + escapedBatchPath(downloadedPath) + "\" \"" + escapedBatchPath(executablePath) + "\" >NUL 2>&1",
		"if errorlevel 1 (",
		"  timeout /T 1 /NOBREAK >NUL",
		"  goto retry",
		")",
		"start \"\" \"" + escapedBatchPath(executablePath) + "\"",
		"del \"%~f0\"",
		"",
	}, "\r\n")
	if _, err := script.WriteString(contents); err != nil {
		_ = script.Close()
		return fmt.Errorf("write update helper: %w", err)
	}
	if err := script.Close(); err != nil {
		return fmt.Errorf("close update helper: %w", err)
	}

	command := exec.Command("cmd.exe", "/D", "/C", scriptPath)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	removeScript = false
	return nil
}
