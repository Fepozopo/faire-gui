//go:build !darwin && !windows

package application

import (
	"context"
	"fmt"
	"runtime"
)

// chooseCSVFile reports that no native picker is available on unsupported platforms.
// ctx is the caller's lifetime; users on these platforms can still enter the path manually.
func chooseCSVFile(ctx context.Context) (string, error) {
	return "", fmt.Errorf("no native file picker for %s", runtime.GOOS)
}
