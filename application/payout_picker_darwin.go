//go:build darwin

package application

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// chooseCSVFile uses macOS's native Open dialog to select a local CSV file.
// ctx cancels the AppleScript process on shutdown; it returns the POSIX path or a selection error.
func chooseCSVFile(ctx context.Context) (string, error) {
	// osascript requests a native file picker through macOS rather than opening a Terminal window.
	command := exec.CommandContext(ctx, "osascript", "-e", "POSIX path of (choose file with prompt \"Select a CSV file\")")
	output, err := command.Output()
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(output))
	if path == "" {
		return "", fmt.Errorf("no file selected")
	}
	return path, nil
}
