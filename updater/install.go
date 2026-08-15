package updater

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	// maxAssetSize prevents a malformed GitHub response from consuming unbounded local storage.
	maxAssetSize int64 = 1 << 31
)

// Installer downloads a release asset next to the current executable and delegates platform-specific replacement and restart.
type Installer struct {
	// HTTPClient performs release-asset downloads. A nil value uses a bounded default client.
	HTTPClient *http.Client
}

// NewInstaller creates an Installer with a timeout suitable for desktop release downloads.
func NewInstaller() Installer {
	return Installer{HTTPClient: &http.Client{Timeout: 5 * time.Minute}}
}

// Apply downloads asset, replaces the running executable, and schedules its restart.
// On Darwin this call does not return after a successful replacement because it execs the new process; on Windows it returns after scheduling a helper that runs once this process exits.
func (installer Installer) Apply(ctx context.Context, asset Asset) error {
	executablePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find running executable: %w", err)
	}
	downloadedPath, err := installer.downloadAsset(ctx, asset, filepath.Dir(executablePath))
	if err != nil {
		return err
	}
	if err := scheduleRestart(executablePath, downloadedPath); err != nil {
		_ = os.Remove(downloadedPath)
		return err
	}
	return nil
}

// downloadAsset downloads asset to a private temporary file in directory and verifies its reported byte count before returning it.
func (installer Installer) downloadAsset(ctx context.Context, asset Asset, directory string) (string, error) {
	if asset.Name == "" || asset.URL == "" {
		return "", errors.New("release asset has incomplete download metadata")
	}
	if asset.Size <= 0 || asset.Size > maxAssetSize {
		return "", fmt.Errorf("release asset size %d is outside the permitted range", asset.Size)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", fmt.Errorf("create asset request: %w", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	request.Header.Set("User-Agent", "faire-gui-updater")

	response, err := installer.httpClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("download release asset: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download release asset: server returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength >= 0 && response.ContentLength != asset.Size {
		return "", fmt.Errorf("download release asset: expected %d bytes, server reported %d", asset.Size, response.ContentLength)
	}

	extension := filepath.Ext(asset.Name)
	temporaryFile, err := os.CreateTemp(directory, ".faire-gui-update-*"+extension)
	if err != nil {
		return "", fmt.Errorf("create update file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	removeTemporaryFile := true
	defer func() {
		if removeTemporaryFile {
			_ = os.Remove(temporaryPath)
		}
	}()

	// Limit the copy to one extra byte so a dishonest response cannot exhaust disk space before the size check fails.
	written, copyErr := io.Copy(temporaryFile, io.LimitReader(response.Body, asset.Size+1))
	closeErr := temporaryFile.Close()
	if copyErr != nil {
		return "", fmt.Errorf("write release asset: %w", copyErr)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close release asset: %w", closeErr)
	}
	if written != asset.Size {
		return "", fmt.Errorf("download release asset: received %d bytes, expected %d", written, asset.Size)
	}
	if err := os.Chmod(temporaryPath, 0o755); err != nil {
		return "", fmt.Errorf("mark release asset executable: %w", err)
	}
	removeTemporaryFile = false
	return temporaryPath, nil
}

// httpClient returns the configured HTTP client or a safe default when callers construct Installer directly.
func (installer Installer) httpClient() *http.Client {
	if installer.HTTPClient != nil {
		return installer.HTTPClient
	}
	return &http.Client{Timeout: 5 * time.Minute}
}
