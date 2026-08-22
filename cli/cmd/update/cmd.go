package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"ocm.software/open-component-model/cli/cmd/version"
)

const (
	FlagCheck      = "check"
	FlagVersion    = "version"
	FlagPreRelease = "pre-release"
	FlagDryRun     = "dry-run"
	FlagYes        = "yes"

	tagPrefix = "cli/v"
)

var (
	githubOwnerRepo = "open-component-model/open-component-model"

	// httpDoFunc is the HTTP transport used for all GitHub API calls.
	// Tests override this to point at an httptest.Server.
	httpDoFunc = func(req *http.Request) (*http.Response, error) {
		client := &http.Client{Timeout: 30 * time.Second}
		return client.Do(req)
	}
)

type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	Assets     []githubAsset `json:"assets"`
}

type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update the OCM CLI to a newer version",
		Long: `Update the OCM CLI binary by downloading the latest release from GitHub.

The command checks for a newer version, prompts for confirmation, downloads
the release binary for the current platform, and atomically replaces the
running binary.

Use --check to only print whether an update is available without downloading.
Use --dry-run to see what would happen without making any changes.`,
		Example: `  # Check if an update is available
  ocm update --check

  # Update to the latest version (with confirmation prompt)
  ocm update

  # Update without prompting
  ocm update --yes

  # Update to a specific version
  ocm update --version v0.2.0

  # Include pre-release (RC) versions when checking for latest
  ocm update --pre-release --check`,
		RunE:              runUpdate,
		DisableAutoGenTag: true,
		SilenceUsage:      true,
	}

	cmd.Flags().Bool(FlagCheck, false, "check for a new version without downloading; exits 0 if update is available, 1 if already up to date")
	cmd.Flags().String(FlagVersion, "", "update to a specific version (e.g. v0.2.0 or 0.2.0)")
	cmd.Flags().Bool(FlagPreRelease, false, "include pre-release (RC) versions when determining the latest version")
	cmd.Flags().Bool(FlagDryRun, false, "print what would happen without downloading or replacing the binary")
	cmd.Flags().BoolP(FlagYes, "y", false, "skip the confirmation prompt")

	return cmd
}

func runUpdate(cmd *cobra.Command, _ []string) error {
	check, _ := cmd.Flags().GetBool(FlagCheck)
	flagVersion, _ := cmd.Flags().GetString(FlagVersion)
	preRelease, _ := cmd.Flags().GetBool(FlagPreRelease)
	dryRun, _ := cmd.Flags().GetBool(FlagDryRun)
	yes, _ := cmd.Flags().GetBool(FlagYes)

	ctx := cmd.Context()

	currentVersionStr, isRelease := getCurrentVersion()
	if !isRelease {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: running a dev build (version %q); version comparison may be inaccurate\n", currentVersionStr)
	}

	var release *githubRelease
	var err error
	if flagVersion != "" {
		tag := normalizeVersionTag(flagVersion)
		release, err = fetchReleaseByTag(ctx, tag)
		if err != nil {
			return fmt.Errorf("fetching release %q: %w", tag, err)
		}
	} else {
		release, err = fetchLatestRelease(ctx, preRelease)
		if err != nil {
			return fmt.Errorf("fetching latest release: %w", err)
		}
	}

	candidateVersionStr, err := semverFromTag(release.TagName)
	if err != nil {
		return err
	}

	var updateAvailable bool
	if !isRelease {
		// Dev builds always allow updating (no meaningful version to compare).
		updateAvailable = true
	} else {
		updateAvailable, err = isUpdateAvailable(currentVersionStr, candidateVersionStr)
		if err != nil {
			return err
		}
	}

	if !updateAvailable {
		fmt.Fprintf(cmd.OutOrStdout(), "OCM CLI is already up to date (version %s).\n", currentVersionStr)
		if check {
			return errors.New("no update available")
		}
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Update available: %s → %s\n", currentVersionStr, candidateVersionStr)

	if check {
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'ocm update' to install.")
		return nil
	}

	asset, err := findAsset(release)
	if err != nil {
		return err
	}

	binaryPath, err := getCurrentBinaryPath()
	if err != nil {
		return err
	}

	if !yes {
		confirmed, err := confirmUpdate(cmd, currentVersionStr, candidateVersionStr)
		if err != nil {
			return err
		}
		if !confirmed {
			fmt.Fprintln(cmd.OutOrStdout(), "Update cancelled.")
			return nil
		}
	}

	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] Would download %s (%d bytes) and replace %s\n",
			asset.BrowserDownloadURL, asset.Size, binaryPath)
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Downloading OCM CLI %s...\n", candidateVersionStr)
	tmpPath, err := downloadToTemp(ctx, asset, filepath.Dir(binaryPath), cmd.ErrOrStderr())
	if err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("downloading update: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	if err := replaceBinary(tmpPath, binaryPath); err != nil {
		return fmt.Errorf("replacing binary: %w", err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "OCM CLI updated to %s.\n", candidateVersionStr)
	return nil
}

// getCurrentVersion returns the current CLI version string and whether it is
// a real release version (true) or a dev build (false).
func getCurrentVersion() (string, bool) {
	if version.BuildVersion != "n/a" {
		return version.BuildVersion, true
	}
	return version.BuildVersion, false
}

// normalizeVersionTag converts a user-supplied version string to the
// GitHub release tag format used by this project.
//
//	"v0.2.0"     → "cli/v0.2.0"
//	"0.2.0"      → "cli/v0.2.0"
//	"cli/v0.2.0" → "cli/v0.2.0"  (idempotent)
func normalizeVersionTag(s string) string {
	if strings.HasPrefix(s, tagPrefix) {
		return s
	}
	if strings.HasPrefix(s, "v") {
		return tagPrefix + s[1:]
	}
	return tagPrefix + s
}

// semverFromTag strips the "cli/" prefix from a GitHub release tag and returns
// a plain semver string, e.g. "cli/v0.2.0" → "0.2.0".
func semverFromTag(tag string) (string, error) {
	if !strings.HasPrefix(tag, tagPrefix) {
		return "", fmt.Errorf("release tag %q does not have expected prefix %q", tag, tagPrefix)
	}
	return strings.TrimPrefix(tag, tagPrefix), nil
}

// isUpdateAvailable returns true if candidate is a higher semver than current.
func isUpdateAvailable(current, candidate string) (bool, error) {
	currentV, err := semver.NewVersion(current)
	if err != nil {
		return false, fmt.Errorf("parsing current version %q: %w", current, err)
	}
	candidateV, err := semver.NewVersion(candidate)
	if err != nil {
		return false, fmt.Errorf("parsing candidate version %q: %w", candidate, err)
	}
	return candidateV.GreaterThan(currentV), nil
}

// fetchLatestRelease fetches the latest CLI release from GitHub.
// This repo publishes releases for multiple components (cli, kubernetes/controller, etc.),
// so /releases/latest may not point to a CLI release. We always use the list endpoint
// and pick the highest semver tag with the expected "cli/v" prefix.
// When includePreRelease is false, pre-release (RC) tags are excluded.
func fetchLatestRelease(ctx context.Context, includePreRelease bool) (*githubRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=100", githubOwnerRepo)
	req, err := newGitHubRequest(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	resp, err := httpDoFunc(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API responded with status %d for %s", resp.StatusCode, apiURL)
	}

	var releases []githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, fmt.Errorf("decoding GitHub releases response: %w", err)
	}

	var best *githubRelease
	var bestSemver *semver.Version
	for i := range releases {
		r := &releases[i]
		if r.Draft {
			continue
		}
		if !includePreRelease && r.Prerelease {
			continue
		}
		if !strings.HasPrefix(r.TagName, tagPrefix) {
			continue
		}
		vStr := strings.TrimPrefix(r.TagName, tagPrefix)
		v, err := semver.NewVersion(vStr)
		if err != nil {
			continue
		}
		if best == nil || v.GreaterThan(bestSemver) {
			best = r
			bestSemver = v
		}
	}

	if best == nil {
		return nil, fmt.Errorf("no CLI releases found in repository %s", githubOwnerRepo)
	}
	return best, nil
}

// fetchReleaseByTag fetches a specific release by its full GitHub tag (e.g. "cli/v0.2.0").
func fetchReleaseByTag(ctx context.Context, tag string) (*githubRelease, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/tags/%s",
		githubOwnerRepo, url.PathEscape(tag))
	r, err := fetchRelease(ctx, apiURL)
	if err != nil {
		return nil, fmt.Errorf("release %q not found: %w", tag, err)
	}
	return r, nil
}

func fetchRelease(ctx context.Context, apiURL string) (*githubRelease, error) {
	req, err := newGitHubRequest(ctx, apiURL)
	if err != nil {
		return nil, err
	}
	resp, err := httpDoFunc(req)
	if err != nil {
		return nil, fmt.Errorf("GitHub API request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (status 404)")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API responded with status %d", resp.StatusCode)
	}

	var r githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("decoding GitHub release response: %w", err)
	}
	return &r, nil
}

func newGitHubRequest(ctx context.Context, apiURL string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", apiURL, err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "ocm-cli/"+version.BuildVersion)
	return req, nil
}

// findAsset returns the release asset matching the current OS and architecture.
// Asset names follow the pattern "ocm-{GOOS}-{GOARCH}".
func findAsset(release *githubRelease) (*githubAsset, error) {
	name := fmt.Sprintf("ocm-%s-%s", runtime.GOOS, runtime.GOARCH)
	for i := range release.Assets {
		if release.Assets[i].Name == name {
			return &release.Assets[i], nil
		}
	}

	available := make([]string, 0, len(release.Assets))
	for _, a := range release.Assets {
		available = append(available, a.Name)
	}
	return nil, fmt.Errorf("no binary asset found for %s/%s in release %s (available: %s)",
		runtime.GOOS, runtime.GOARCH, release.TagName, strings.Join(available, ", "))
}

// getCurrentBinaryPath returns the absolute path of the running binary,
// resolving symlinks so we replace the real file.
func getCurrentBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("determining current binary path: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// Fall back to unresolved path on error (e.g. dangling symlink).
		return exe, nil
	}
	return resolved, nil
}

// progressWriter wraps an io.Writer and prints download progress to out.
type progressWriter struct {
	dst   io.Writer
	out   io.Writer
	total int64
	n     int64
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.dst.Write(p)
	pw.n += int64(n)
	if pw.total > 0 {
		fmt.Fprintf(pw.out, "\r  %d / %d bytes (%.0f%%)", pw.n, pw.total, float64(pw.n)/float64(pw.total)*100)
	} else {
		fmt.Fprintf(pw.out, "\r  %d bytes", pw.n)
	}
	return n, err
}

// downloadToTemp downloads the asset to a temporary file in dir (same directory
// as the target binary, ensuring same-filesystem rename later).
// Progress is written to progressOut (typically cmd.ErrOrStderr()).
// The caller is responsible for removing the temp file on error.
func downloadToTemp(ctx context.Context, asset *githubAsset, dir string, progressOut io.Writer) (string, error) {
	f, err := os.CreateTemp(dir, "ocm-update-*")
	if err != nil {
		return "", fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := f.Name()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.BrowserDownloadURL, nil)
	if err != nil {
		f.Close()
		return tmpPath, fmt.Errorf("creating download request: %w", err)
	}
	req.Header.Set("User-Agent", "ocm-cli/"+version.BuildVersion)

	resp, err := httpDoFunc(req)
	if err != nil {
		f.Close()
		return tmpPath, fmt.Errorf("downloading asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		f.Close()
		return tmpPath, fmt.Errorf("download responded with status %d", resp.StatusCode)
	}

	pw := &progressWriter{dst: f, out: progressOut, total: asset.Size}
	if _, err := io.Copy(pw, resp.Body); err != nil {
		f.Close()
		return tmpPath, fmt.Errorf("writing download: %w", err)
	}
	fmt.Fprintln(progressOut) // newline after progress line

	if err := f.Chmod(0o755); err != nil {
		f.Close()
		return tmpPath, fmt.Errorf("setting permissions on temp file: %w", err)
	}

	return tmpPath, f.Close()
}

// replaceBinary atomically replaces binaryPath with the file at tmpPath.
// It backs up the current binary to binaryPath+".old" and rolls back on failure.
func replaceBinary(tmpPath, binaryPath string) error {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("stat current binary: %w", err)
	}
	mode := info.Mode()

	if err := os.Chmod(tmpPath, mode); err != nil {
		return fmt.Errorf("setting permissions on new binary: %w", err)
	}

	backupPath := binaryPath + ".old"
	// Remove any stale backup from a previous failed update.
	_ = os.Remove(backupPath)

	// Rename current binary to backup.
	if err := os.Rename(binaryPath, backupPath); err != nil {
		return fmt.Errorf("backing up current binary: %w", err)
	}

	// Move new binary into place.
	if err := os.Rename(tmpPath, binaryPath); err != nil {
		// Rollback: restore from backup.
		if rbErr := os.Rename(backupPath, binaryPath); rbErr != nil {
			return fmt.Errorf("replacing binary failed (%w) and rollback also failed: %w", err, rbErr)
		}
		return fmt.Errorf("replacing binary: %w", err)
	}

	// Best-effort cleanup of backup (may fail on Windows if still in use).
	_ = os.Remove(backupPath)
	return nil
}

// confirmUpdate asks the user to confirm the update via stdin.
// Returns true if the user confirms. Auto-confirms when stdin is not a terminal.
func confirmUpdate(cmd *cobra.Command, currentVersion, targetVersion string) (bool, error) {
	fmt.Fprintf(cmd.OutOrStdout(), "Update OCM CLI from %s to %s? [y/N]: ", currentVersion, targetVersion)
	var response string
	if _, err := fmt.Fscanln(cmd.InOrStdin(), &response); err != nil {
		// EOF or non-interactive stdin — treat as confirmed.
		fmt.Fprintln(cmd.OutOrStdout())
		return true, nil
	}
	return strings.EqualFold(strings.TrimSpace(response), "y"), nil
}
