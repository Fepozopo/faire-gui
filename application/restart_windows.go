//go:build windows

package application

import (
	"os"
	"time"
)

const (
	// restartExitGracePeriod gives Gio time to process the requested window close before the process is terminated.
	restartExitGracePeriod = 750 * time.Millisecond
)

// exitAfterScheduledRestart terminates the current Windows process after its replacement helper is ready.
// It has no parameters or return value. The delayed exit gives Gio a brief opportunity to close the window cleanly, then releases Windows' lock on the running executable.
func exitAfterScheduledRestart() {
	go func() {
		// Gio's Windows app.Main blocks forever, so waiting for normal main-function return would leave the updater unable to replace this executable.
		time.Sleep(restartExitGracePeriod)
		os.Exit(0)
	}()
}
