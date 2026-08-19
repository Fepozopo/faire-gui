//go:build !windows

package application

// exitAfterScheduledRestart preserves a shared update-completion path on platforms that do not need to force-release a Windows executable lock.
// It has no parameters or return value. Darwin replaces this process with syscall.Exec before an install result is published, and unsupported platforms cannot successfully schedule a restart.
func exitAfterScheduledRestart() {}
