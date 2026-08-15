package updater

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestParseVersionOrdersSemanticVersions verifies core, pre-release, and build-metadata precedence rules.
func TestParseVersionOrdersSemanticVersions(t *testing.T) {
	t.Parallel()

	ordered := []string{
		"1.0.0-alpha",
		"1.0.0-alpha.1",
		"1.0.0-alpha.beta",
		"1.0.0-beta",
		"1.0.0-beta.2",
		"1.0.0-beta.11",
		"1.0.0-rc.1",
		"1.0.0",
		"1.0.1",
	}
	for index := 0; index < len(ordered)-1; index++ {
		left := mustParseVersion(t, ordered[index])
		right := mustParseVersion(t, ordered[index+1])
		if left.Compare(right) >= 0 {
			t.Fatalf("Compare(%q, %q) = %d, want an older result", left, right, left.Compare(right))
		}
	}
	if got := mustParseVersion(t, "v1.2.3+build.7").String(); got != "1.2.3+build.7" {
		t.Fatalf("parsed version = %q, want canonical version with build metadata", got)
	}
	if comparison := mustParseVersion(t, "1.2.3+build.1").Compare(mustParseVersion(t, "1.2.3+build.2")); comparison != 0 {
		t.Fatalf("build metadata comparison = %d, want equal precedence", comparison)
	}
	if comparison := mustParseVersion(t, "18446744073709551616.0.0").Compare(mustParseVersion(t, "18446744073709551617.0.0")); comparison >= 0 {
		t.Fatalf("large core-number comparison = %d, want an older result", comparison)
	}
	if comparison := mustParseVersion(t, "1.0.0-18446744073709551616").Compare(mustParseVersion(t, "1.0.0-18446744073709551617")); comparison >= 0 {
		t.Fatalf("large pre-release-number comparison = %d, want an older result", comparison)
	}
}

// TestParseVersionRejectsInvalidSemanticVersions verifies malformed release tags do not become update candidates.
func TestParseVersionRejectsInvalidSemanticVersions(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "1.2", "01.2.3", "1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3-alpha_1"} {
		if _, err := ParseVersion(value); err == nil {
			t.Errorf("ParseVersion(%q) error = nil, want validation error", value)
		}
	}
}

// TestAssetNameUsesOnlySupportedPlatforms verifies the release filename contract for every supported target.
func TestAssetNameUsesOnlySupportedPlatforms(t *testing.T) {
	t.Parallel()

	tests := []struct {
		platform Platform
		want     string
	}{
		{platform: Platform{OS: "darwin", Architecture: "arm64"}, want: "faire-gui_darwin_arm64"},
		{platform: Platform{OS: "windows", Architecture: "arm64"}, want: "faire-gui_windows_arm64.exe"},
		{platform: Platform{OS: "windows", Architecture: "amd64"}, want: "faire-gui_windows_amd64.exe"},
	}
	for _, test := range tests {
		got, err := test.platform.AssetName()
		if err != nil || got != test.want {
			t.Errorf("AssetName(%+v) = (%q, %v), want (%q, nil)", test.platform, got, err, test.want)
		}
	}
	if _, err := (Platform{OS: "linux", Architecture: "amd64"}).AssetName(); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("unsupported platform error = %v, want ErrUnsupportedPlatform", err)
	}
}

// TestCheckerFindsNewerCompatibleRelease verifies the checker compares release tags and selects only the active platform asset.
func TestCheckerFindsNewerCompatibleRelease(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/release" {
			t.Fatalf("request path = %q, want /release", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v0.2.0","assets":[{"name":"faire-gui_windows_amd64.exe","browser_download_url":"https://example.invalid/windows","size":4},{"name":"faire-gui_darwin_arm64","browser_download_url":"https://example.invalid/darwin","size":3}]}`))
	}))
	defer server.Close()

	checker := NewChecker("0.1.0")
	checker.ReleasesURL = server.URL + "/release"
	checker.HTTPClient = server.Client()
	checker.Platform = Platform{OS: "darwin", Architecture: "arm64"}
	update, available, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("Check() error = %v", err)
	}
	if !available || update.Version.String() != "0.2.0" || update.Asset.Name != "faire-gui_darwin_arm64" {
		t.Fatalf("Check() = (%+v, %t), want newer Darwin update", update, available)
	}
}

// TestCheckerRejectsNewerReleaseWithoutCompatibleAsset verifies incomplete releases never prompt an unusable update.
func TestCheckerRejectsNewerReleaseWithoutCompatibleAsset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v0.2.0","assets":[]}`))
	}))
	defer server.Close()

	checker := NewChecker("0.1.0")
	checker.ReleasesURL = server.URL
	checker.HTTPClient = server.Client()
	checker.Platform = Platform{OS: "darwin", Architecture: "arm64"}
	_, available, err := checker.Check(context.Background())
	if available || !errors.Is(err, ErrNoReleaseAsset) {
		t.Fatalf("Check() = (available:%t, error:%v), want no available update and ErrNoReleaseAsset", available, err)
	}
}

// TestDownloadAssetVerifiesReleaseSize verifies downloaded assets are written only after their expected byte count is received.
func TestDownloadAssetVerifiesReleaseSize(t *testing.T) {
	t.Parallel()

	const contents = "new executable"
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/asset" {
			t.Fatalf("request path = %q, want /asset", request.URL.Path)
		}
		_, _ = writer.Write([]byte(contents))
	}))
	defer server.Close()

	installer := Installer{HTTPClient: server.Client()}
	path, err := installer.downloadAsset(context.Background(), Asset{Name: "faire-gui_darwin_arm64", URL: server.URL + "/asset", Size: int64(len(contents))}, t.TempDir())
	if err != nil {
		t.Fatalf("downloadAsset() error = %v", err)
	}
	defer func() {
		_ = os.Remove(path)
	}()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(got) != contents {
		t.Fatalf("downloaded contents = %q, want %q", got, contents)
	}
	if info, err := os.Stat(path); err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("downloaded mode = (%v, %v), want executable file", info, err)
	}
	_, err = installer.downloadAsset(context.Background(), Asset{Name: "faire-gui_darwin_arm64", URL: server.URL + "/asset", Size: int64(len(contents) - 1)}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "expected") {
		t.Fatalf("short size download error = %v, want expected-size validation error", err)
	}
}

// mustParseVersion parses value for a test and fails the calling test when it is invalid.
func mustParseVersion(t *testing.T, value string) Version {
	t.Helper()
	parsed, err := ParseVersion(value)
	if err != nil {
		t.Fatalf("ParseVersion(%q) error = %v", value, err)
	}
	return parsed
}
