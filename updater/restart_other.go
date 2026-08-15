//go:build !darwin && !windows

package updater

import "fmt"

// scheduleRestart rejects runtimes that do not have a supported self-replacement strategy.
func scheduleRestart(_, _ string) error {
	return fmt.Errorf("%w", ErrUnsupportedPlatform)
}
