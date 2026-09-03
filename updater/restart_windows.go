//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

const (
	// restartRetryLimit bounds helper waiting so an abandoned update never leaves a permanent background cmd.exe process.
	restartRetryLimit = 120
)

// escapedBatchPath returns a path safe for use inside a quoted Windows batch-file argument.
// path is a native filesystem path, and the returned value escapes percent signs that cmd.exe expands even inside quoted arguments.
func escapedBatchPath(path string) string {
	// Percent signs are expanded by cmd.exe even in quotes, so double them before writing a batch file.
	return strings.ReplaceAll(path, "%", "%%")
}

// scheduleRestart starts a detached Windows helper that replaces executablePath with downloadedPath after parentProcessID exits.
// executablePath is the locked running application, downloadedPath is its verified replacement, and parentProcessID identifies the process that must release the lock. It returns an error only when creating or starting the helper fails.
func scheduleRestart(executablePath, downloadedPath string, parentProcessID int) error {
	if parentProcessID <= 0 {
		return fmt.Errorf("schedule update restart: invalid parent process ID %d", parentProcessID)
	}

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

	parentID := fmt.Sprintf("%d", parentProcessID)
	contents := strings.Join([]string{
		"@echo off",
		"setlocal DisableDelayedExpansion",
		"set /a parentWaits=0",
		"set /a replaceAttempts=0",
		":wait_for_parent",
		"tasklist /FI \"PID eq " + parentID + "\" /NH | findstr /R /C:\" [ ]*" + parentID + "[ ]\" >NUL",
		"if not errorlevel 1 goto parent_running",
		"goto replace",
		":parent_running",
		"set /a parentWaits=parentWaits+1",
		"if %parentWaits% GEQ " + fmt.Sprintf("%d", restartRetryLimit) + " goto failed",
		"timeout /T 1 /NOBREAK >NUL",
		"goto wait_for_parent",
		":replace",
		"move /Y \"" + escapedBatchPath(downloadedPath) + "\" \"" + escapedBatchPath(executablePath) + "\" >NUL 2>&1",
		"if not errorlevel 1 goto launch",
		"set /a replaceAttempts=replaceAttempts+1",
		"if %replaceAttempts% GEQ " + fmt.Sprintf("%d", restartRetryLimit) + " goto failed",
		"timeout /T 1 /NOBREAK >NUL",
		"goto replace",
		":launch",
		"start \"\" \"" + escapedBatchPath(executablePath) + "\"",
		"del \"%~f0\"",
		"goto :EOF",
		":failed",
		"del /F /Q \"" + escapedBatchPath(downloadedPath) + "\" >NUL 2>&1",
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

	// cmd.exe executes the temporary helper independently so the application can close and release its executable lock.
	command := exec.Command("cmd.exe", "/D", "/C", scriptPath)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start update helper: %w", err)
	}
	removeScript = false
	return nil
}
