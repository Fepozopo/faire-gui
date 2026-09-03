//go:build !darwin && !windows

package updater

import "fmt"

// scheduleRestart rejects runtimes without a supported self-replacement strategy.
// executablePath, downloadedPath, and parentProcessID are unused because no replacement helper exists for these platforms; it returns ErrUnsupportedPlatform.
func scheduleRestart(_, _ string, _ int) error {
	return fmt.Errorf("%w", ErrUnsupportedPlatform)
}
