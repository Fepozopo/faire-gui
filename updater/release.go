// Package updater checks GitHub releases and installs supported desktop release assets.
package updater

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"
)

const (
	// GitHubRepository identifies the public GitHub repository that publishes application releases.
	GitHubRepository = "Fepozopo/faire-gui"

	// latestReleaseURL is GitHub's stable-release endpoint for the application repository.
	latestReleaseURL = "https://api.github.com/repos/" + GitHubRepository + "/releases/latest"

	// releaseAssetPrefix is the filename prefix shared by all supported binary assets.
	releaseAssetPrefix = "faire-gui_"

	// maxReleaseResponseBytes bounds memory consumed while reading a GitHub release document.
	maxReleaseResponseBytes = 1 << 20
)

var (
	// ErrUnsupportedPlatform reports that no self-updating binary is published for a runtime platform.
	ErrUnsupportedPlatform = errors.New("self-update is not supported on this platform")

	// ErrNoReleaseAsset reports that a newer release does not contain the required platform asset.
	ErrNoReleaseAsset = errors.New("release does not contain an asset for this platform")
)

// Platform identifies the operating system and architecture of a release binary.
type Platform struct {
	// OS is the Go operating-system identifier, such as "darwin" or "windows".
	OS string
	// Architecture is the Go architecture identifier, such as "arm64" or "amd64".
	Architecture string
}

// CurrentPlatform returns the runtime platform of the currently running application.
func CurrentPlatform() Platform {
	return Platform{OS: runtime.GOOS, Architecture: runtime.GOARCH}
}

// AssetName returns the exact GitHub release asset expected for platform.
// Only Darwin ARM64, Windows ARM64, and Windows AMD64 are supported.
func (platform Platform) AssetName() (string, error) {
	baseName := releaseAssetPrefix + platform.OS + "_" + platform.Architecture
	switch platform {
	case Platform{OS: "darwin", Architecture: "arm64"}:
		return baseName, nil
	case Platform{OS: "windows", Architecture: "arm64"}, Platform{OS: "windows", Architecture: "amd64"}:
		return baseName + ".exe", nil
	default:
		return "", fmt.Errorf("%w: %s/%s", ErrUnsupportedPlatform, platform.OS, platform.Architecture)
	}
}

// Version is a parsed Semantic Version 2.0.0 value.
// Its fields are private so callers compare versions through Compare instead of relying on lexical ordering.
type Version struct {
	major      string
	minor      string
	patch      string
	preRelease []versionIdentifier
	build      []string
}

// versionIdentifier is one dot-delimited pre-release component and preserves whether SemVer treats it numerically.
type versionIdentifier struct {
	value   string
	numeric bool
}

// ParseVersion parses a Semantic Version 2.0.0 string.
// A single leading "v" is accepted because GitHub release tags commonly use that presentation.
func ParseVersion(value string) (Version, error) {
	original := strings.TrimPrefix(strings.TrimSpace(value), "v")
	if original == "" {
		return Version{}, errors.New("version is empty")
	}

	coreAndPreRelease, build, hasBuild := strings.Cut(original, "+")
	if hasBuild {
		if err := validateIdentifierList(build, false); err != nil {
			return Version{}, fmt.Errorf("invalid build metadata: %w", err)
		}
	}

	core, preRelease, hasPreRelease := strings.Cut(coreAndPreRelease, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return Version{}, fmt.Errorf("version %q must contain major, minor, and patch numbers", value)
	}
	major, err := parseCoreNumber(parts[0])
	if err != nil {
		return Version{}, fmt.Errorf("invalid major version: %w", err)
	}
	minor, err := parseCoreNumber(parts[1])
	if err != nil {
		return Version{}, fmt.Errorf("invalid minor version: %w", err)
	}
	patch, err := parseCoreNumber(parts[2])
	if err != nil {
		return Version{}, fmt.Errorf("invalid patch version: %w", err)
	}

	parsed := Version{major: major, minor: minor, patch: patch}
	if hasPreRelease {
		identifiers, err := parsePreRelease(preRelease)
		if err != nil {
			return Version{}, fmt.Errorf("invalid pre-release: %w", err)
		}
		parsed.preRelease = identifiers
	}
	if hasBuild {
		parsed.build = strings.Split(build, ".")
	}
	return parsed, nil
}

// String returns version in canonical Semantic Version form without a leading "v".
func (version Version) String() string {
	value := version.major + "." + version.minor + "." + version.patch
	if len(version.preRelease) > 0 {
		parts := make([]string, len(version.preRelease))
		for index, identifier := range version.preRelease {
			parts[index] = identifier.value
		}
		value += "-" + strings.Join(parts, ".")
	}
	if len(version.build) > 0 {
		value += "+" + strings.Join(version.build, ".")
	}
	return value
}

// Compare returns -1 when version is older than other, 0 when their precedence is equal, and 1 when version is newer.
// Build metadata is deliberately ignored because Semantic Version does not include it in precedence.
func (version Version) Compare(other Version) int {
	for _, pair := range [][2]string{{version.major, other.major}, {version.minor, other.minor}, {version.patch, other.patch}} {
		comparison := compareNumberStrings(pair[0], pair[1])
		if comparison != 0 {
			return comparison
		}
	}
	if len(version.preRelease) == 0 && len(other.preRelease) > 0 {
		return 1
	}
	if len(version.preRelease) > 0 && len(other.preRelease) == 0 {
		return -1
	}
	for index := 0; index < len(version.preRelease) && index < len(other.preRelease); index++ {
		comparison := compareIdentifier(version.preRelease[index], other.preRelease[index])
		if comparison != 0 {
			return comparison
		}
	}
	if len(version.preRelease) < len(other.preRelease) {
		return -1
	}
	if len(version.preRelease) > len(other.preRelease) {
		return 1
	}
	return 0
}

// Asset identifies one downloadable GitHub release asset.
type Asset struct {
	// Name is the filename displayed by GitHub Releases.
	Name string
	// URL is the browser download URL returned by GitHub.
	URL string
	// Size is the exact asset size in bytes reported by GitHub.
	Size int64
}

// Update describes a newer supported release and its platform-specific binary asset.
type Update struct {
	// Version is the newer release version.
	Version Version
	// Asset is the binary that replaces the current executable.
	Asset Asset
}

// Checker retrieves and evaluates the latest GitHub release for one application build and platform.
type Checker struct {
	// CurrentVersion is the semantic version embedded in the running application.
	CurrentVersion string
	// ReleasesURL is GitHub's latest-release endpoint and may be overridden by tests.
	ReleasesURL string
	// HTTPClient performs release requests. A nil value uses a bounded default client.
	HTTPClient *http.Client
	// Platform determines which exact asset name is acceptable.
	Platform Platform
}

// NewChecker creates a Checker for the running platform and the repository's latest stable GitHub release.
func NewChecker(currentVersion string) Checker {
	return Checker{
		CurrentVersion: currentVersion,
		ReleasesURL:    latestReleaseURL,
		HTTPClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		Platform: CurrentPlatform(),
	}
}

// Check returns the newer update available for the configured platform.
// It returns available=false for equal or older releases and errors for invalid release metadata or network failures.
func (checker Checker) Check(ctx context.Context) (update Update, available bool, err error) {
	current, err := ParseVersion(checker.CurrentVersion)
	if err != nil {
		return Update{}, false, fmt.Errorf("parse current version: %w", err)
	}
	assetName, err := checker.Platform.AssetName()
	if err != nil {
		return Update{}, false, err
	}
	if checker.ReleasesURL == "" {
		return Update{}, false, errors.New("release URL is empty")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, checker.ReleasesURL, nil)
	if err != nil {
		return Update{}, false, fmt.Errorf("create release request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "faire-gui-updater")

	response, err := checker.httpClient().Do(request)
	if err != nil {
		return Update{}, false, fmt.Errorf("request latest release: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	if response.StatusCode != http.StatusOK {
		return Update{}, false, fmt.Errorf("request latest release: GitHub returned HTTP %d", response.StatusCode)
	}

	var release githubRelease
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxReleaseResponseBytes))
	if err := decoder.Decode(&release); err != nil {
		return Update{}, false, fmt.Errorf("decode latest release: %w", err)
	}
	latest, err := ParseVersion(release.TagName)
	if err != nil {
		return Update{}, false, fmt.Errorf("parse latest release tag %q: %w", release.TagName, err)
	}
	if latest.Compare(current) <= 0 {
		return Update{}, false, nil
	}

	for _, asset := range release.Assets {
		if asset.Name == assetName {
			if asset.BrowserDownloadURL == "" || asset.Size <= 0 {
				return Update{}, false, fmt.Errorf("release asset %q has incomplete download metadata", assetName)
			}
			return Update{Version: latest, Asset: Asset{Name: asset.Name, URL: asset.BrowserDownloadURL, Size: asset.Size}}, true, nil
		}
	}
	return Update{}, false, fmt.Errorf("%w: %s", ErrNoReleaseAsset, assetName)
}

// httpClient returns the configured HTTP client or a safe default when callers construct Checker directly.
func (checker Checker) httpClient() *http.Client {
	if checker.HTTPClient != nil {
		return checker.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// githubRelease models only the GitHub API fields needed to discover the latest compatible release.
type githubRelease struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

// githubAsset models only the GitHub API fields needed to download and verify one release binary.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// parseCoreNumber validates a non-negative SemVer core number that does not contain a leading zero.
// It preserves the value as text because SemVer does not impose a fixed integer-width limit.
func parseCoreNumber(value string) (string, error) {
	if value == "" {
		return "", errors.New("number is empty")
	}
	if len(value) > 1 && value[0] == '0' {
		return "", errors.New("number has a leading zero")
	}
	if !isDigits(value) {
		return "", errors.New("number contains a non-digit")
	}
	return value, nil
}

// parsePreRelease parses dot-delimited identifiers and records numeric identifiers for precedence comparison.
func parsePreRelease(value string) ([]versionIdentifier, error) {
	if err := validateIdentifierList(value, true); err != nil {
		return nil, err
	}
	parts := strings.Split(value, ".")
	identifiers := make([]versionIdentifier, len(parts))
	for index, part := range parts {
		identifier := versionIdentifier{value: part}
		if isDigits(part) {
			identifier.numeric = true
		}
		identifiers[index] = identifier
	}
	return identifiers, nil
}

// validateIdentifierList validates SemVer pre-release or build metadata components.
func validateIdentifierList(value string, rejectNumericLeadingZero bool) error {
	if value == "" {
		return errors.New("identifier list is empty")
	}
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return errors.New("identifier is empty")
		}
		for _, character := range identifier {
			if !isIdentifierCharacter(character) {
				return fmt.Errorf("identifier %q contains an invalid character", identifier)
			}
		}
		if rejectNumericLeadingZero && len(identifier) > 1 && identifier[0] == '0' && isDigits(identifier) {
			return fmt.Errorf("numeric identifier %q has a leading zero", identifier)
		}
	}
	return nil
}

// isIdentifierCharacter reports whether character is permitted in a SemVer pre-release or build identifier.
func isIdentifierCharacter(character rune) bool {
	return (character >= '0' && character <= '9') ||
		(character >= 'A' && character <= 'Z') ||
		(character >= 'a' && character <= 'z') ||
		character == '-'
}

// isDigits reports whether value contains at least one character and every character is an ASCII digit.
func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

// compareNumberStrings compares validated non-negative integer strings without imposing a machine integer-width limit.
func compareNumberStrings(left, right string) int {
	if len(left) < len(right) {
		return -1
	}
	if len(left) > len(right) {
		return 1
	}
	return strings.Compare(left, right)
}

// compareIdentifier returns the SemVer precedence comparison for two pre-release identifiers.
func compareIdentifier(left, right versionIdentifier) int {
	if left.numeric && right.numeric {
		return compareNumberStrings(left.value, right.value)
	}
	if left.numeric {
		return -1
	}
	if right.numeric {
		return 1
	}
	return strings.Compare(left.value, right.value)
}
