package elementary

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
)

const (
	patchrightCoreRegistryURL = "https://registry.npmjs.org/patchright-core"

	// stealthDriverDirectorySuffix names the sibling directory a patched driver copy is built
	// into, next to the original, unpatched driver directory (e.g. ".../1.61.1-stealth" next
	// to ".../1.61.1"). Keeping the two fully separate is what lets StealthPatch-enabled and
	// StealthPatch-disabled Instances share the same cache without ever touching each other.
	stealthDriverDirectorySuffix = "-stealth"

	// stealthDriverVersionMarkerName is a small file written into a completed patched driver
	// copy recording which Playwright driver version it was built from. This is how a rebuild
	// notices a stale copy (built against a Playwright driver version this binary no longer
	// uses) without needing to re-hash or re-diff the whole copy.
	stealthDriverVersionMarkerName = ".stealth-patch-version"

	// maxPatchrightTarballBytes bounds how much of the patchright-core tarball response this
	// will read into memory. This is a defensive cap against a corrupted or unexpectedly huge
	// upstream response, not an expectation the real package ever gets close to it.
	maxPatchrightTarballBytes = 200 * 1024 * 1024

	// maxCoreBundleBytes bounds the extracted lib/coreBundle.js file size for the same reason.
	maxCoreBundleBytes = 50 * 1024 * 1024
)

// patchrightHTTPClient is used for every network call this file makes, so a slow or hanging
// npm registry / tarball host can never block Initialize indefinitely.
var patchrightHTTPClient = &http.Client{Timeout: 30 * time.Second}

/*
npmPackageMetadata is a structure which captures the small subset of npm registry
per-version metadata this file needs: the confirmed version string and the tarball
download URL.
*/
type npmPackageMetadata struct {
	Version string `json:"version"`
	Dist    struct {
		Tarball string `json:"tarball"`
	} `json:"dist"`
}

/*
resolvePlaywrightDriverVersion is a function which returns the exact Playwright driver
version bundled with the currently vendored playwright-go, read from the driver itself
via its exported NewDriver constructor. This stays correct across whichever playwright-go
version is in go.mod, rather than assuming a hardcoded value.

Example:

	version, err := resolvePlaywrightDriverVersion()
*/
func resolvePlaywrightDriverVersion() (string, error) {
	driver, err := playwright.NewDriver()
	if err != nil {
		return "", fmt.Errorf("could not resolve playwright driver version: %w", err)
	}
	return driver.Version, nil
}

/*
fetchPatchrightCoreMetadata is a function which queries the public npm registry for
patchright-core's metadata at an exact version. This is the fail-loud safety gate: if no
matching release exists, this returns an error rather than allowing a mismatched patch to
be applied.

Example:

	meta, err := fetchPatchrightCoreMetadata("1.61.1")
*/
func fetchPatchrightCoreMetadata(version string) (*npmPackageMetadata, error) {
	url := fmt.Sprintf("%s/%s", patchrightCoreRegistryURL, version)
	resp, err := patchrightHTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("could not reach npm registry for patchright-core: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no patchright-core release exists matching playwright version %s", version)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching patchright-core metadata", resp.StatusCode)
	}

	var meta npmPackageMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("could not parse patchright-core metadata: %w", err)
	}
	return &meta, nil
}

/*
downloadPatchrightCoreBundle is a function which downloads the patchright-core tarball at
the given URL and extracts just lib/coreBundle.js. This is the one file confirmed (by
direct byte comparison against stock playwright-core) to actually differ between the two
packages; everything else in the tarball is left untouched.

Example:

	bundle, err := downloadPatchrightCoreBundle(meta.Dist.Tarball)
*/
func downloadPatchrightCoreBundle(tarballURL string) ([]byte, error) {
	resp, err := patchrightHTTPClient.Get(tarballURL)
	if err != nil {
		return nil, fmt.Errorf("could not download patchright-core tarball: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d downloading patchright-core tarball", resp.StatusCode)
	}

	gzipReader, err := gzip.NewReader(io.LimitReader(resp.Body, maxPatchrightTarballBytes))
	if err != nil {
		return nil, fmt.Errorf("could not decompress patchright-core tarball: %w", err)
	}
	defer gzipReader.Close()

	const targetPath = "package/lib/coreBundle.js"
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("could not read patchright-core tarball: %w", err)
		}
		if header.Name == targetPath {
			data, err := io.ReadAll(io.LimitReader(tarReader, maxCoreBundleBytes))
			if err != nil {
				return nil, fmt.Errorf("could not read coreBundle.js from tarball: %w", err)
			}
			return data, nil
		}
	}
	return nil, errors.New("coreBundle.js not found in patchright-core tarball")
}

/*
resolveBaseDriverDirectory is a method which locates the unpatched Playwright driver
directory, reusing the same resolution order playwright-go itself uses internally: the
PLAYWRIGHT_DRIVER_PATH environment variable takes precedence if set, otherwise the default
cache-directory and version convention is used. Honoring the same override here is required
for correctness — if playwright-go installed the driver into a location controlled by that
env var, patching a copy of the default location instead would silently miss the driver
actually in use. This directory is never written to by the stealth patch feature; it is only
ever read from when building the separate, patched copy in prepareStealthDriverDirectory.

Example:

	dir, err := shared.resolveBaseDriverDirectory()
*/
func (shared *Instance) resolveBaseDriverDirectory() (string, error) {
	if driverPath := os.Getenv("PLAYWRIGHT_DRIVER_PATH"); driverPath != "" {
		return driverPath, nil
	}

	cacheDirectory, err := shared.getDefaultCacheDirectory()
	if err != nil {
		return "", logger.Error(err, "could not get default cache directory")
	}
	playwrightVersion, err := resolvePlaywrightDriverVersion()
	if err != nil {
		return "", err
	}
	return filepath.Join(cacheDirectory, "ms-playwright-go", playwrightVersion), nil
}

/*
stealthDriverUpToDate is a function which checks whether a patched driver copy at
stealthDriverDirectory was already built for playwrightVersion, via the small version marker
file written alongside it. This is the idempotency check that lets repeated Initialize calls,
and concurrent builders racing on the same copy, avoid redundant downloads and rebuilds.

Example:

	upToDate, err := stealthDriverUpToDate("/home/user/.cache/ms-playwright-go/1.61.1-stealth", "1.61.1")
*/
func stealthDriverUpToDate(stealthDriverDirectory string, playwrightVersion string) (bool, error) {
	markerData, err := os.ReadFile(filepath.Join(stealthDriverDirectory, "package", stealthDriverVersionMarkerName))
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not read stealth driver version marker: %w", err)
	}
	return string(markerData) == playwrightVersion, nil
}

/*
copyDriverDirectory is a function which recursively copies every file, directory, and symlink
under sourceDirectory into destinationDirectory, preserving file modes. Preserving modes is
required for correctness, not just fidelity: the copied Node.js executable must keep its
executable bit on Linux and macOS or the patched driver copy will fail to launch.

Example:

	err := copyDriverDirectory(baseDriverDirectory, tempDirectory)
*/
func copyDriverDirectory(sourceDirectory string, destinationDirectory string) error {
	return filepath.WalkDir(sourceDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relativePath, err := filepath.Rel(sourceDirectory, path)
		if err != nil {
			return err
		}
		destinationPath := filepath.Join(destinationDirectory, relativePath)

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return os.MkdirAll(destinationPath, info.Mode().Perm())
		}

		if entry.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			return os.Symlink(linkTarget, destinationPath)
		}

		sourceFile, err := os.Open(path)
		if err != nil {
			return err
		}
		defer sourceFile.Close()

		destinationFile, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		defer destinationFile.Close()

		_, err = io.Copy(destinationFile, sourceFile)
		return err
	})
}

/*
prepareStealthDriverDirectory is a method which builds a fully independent copy of the
Playwright driver, patched with Patchright's stealth patches, in a sibling directory next to
baseDriverDirectory — leaving baseDriverDirectory itself completely untouched. This is what
lets a StealthPatch-enabled Instance and a plain Instance run side by side against the same
cached Playwright installation: whichever runs first installs the shared, unpatched driver
that both read from, and only the copy this method builds is ever mutated.

The copy is built into a temporary directory first and only made visible under its final
name via an atomic rename, so a crash, disk-full error, or another process racing to build
the same copy can never leave a half-copied or half-patched driver visible at that path. If
another builder wins the race, this discards its own redundant copy and reuses the winner's.
Repeated calls for a driver version that already has a complete, correctly-versioned copy are
a no-op.

Example:

	stealthDriverDir, err := shared.prepareStealthDriverDirectory(baseDriverDirectory)
*/
func (shared *Instance) prepareStealthDriverDirectory(baseDriverDirectory string) (string, error) {
	playwrightVersion, err := resolvePlaywrightDriverVersion()
	if err != nil {
		return "", err
	}

	stealthDriverDirectory := baseDriverDirectory + stealthDriverDirectorySuffix

	if upToDate, err := stealthDriverUpToDate(stealthDriverDirectory, playwrightVersion); err == nil && upToDate {
		logger.Log(logger.TYPE_DEBUG, "stealth driver copy already up to date for playwright %s, skipping", playwrightVersion)
		return stealthDriverDirectory, nil
	}

	meta, err := fetchPatchrightCoreMetadata(playwrightVersion)
	if err != nil {
		return "", fmt.Errorf("stealth patch version check failed: %w", err)
	}
	bundleData, err := downloadPatchrightCoreBundle(meta.Dist.Tarball)
	if err != nil {
		return "", fmt.Errorf("stealth patch download failed: %w", err)
	}

	parentDirectory := filepath.Dir(stealthDriverDirectory)
	if err := os.MkdirAll(parentDirectory, 0o755); err != nil {
		return "", fmt.Errorf("could not create parent directory for stealth driver copy: %w", err)
	}
	tempDirectory, err := os.MkdirTemp(parentDirectory, filepath.Base(stealthDriverDirectory)+".building-*")
	if err != nil {
		return "", fmt.Errorf("could not create temp directory for stealth driver copy: %w", err)
	}
	defer os.RemoveAll(tempDirectory)

	if err := copyDriverDirectory(baseDriverDirectory, tempDirectory); err != nil {
		return "", fmt.Errorf("could not copy driver directory for stealth patching: %w", err)
	}

	bundlePath := filepath.Join(tempDirectory, "package", "lib", "coreBundle.js")
	if err := os.WriteFile(bundlePath, bundleData, 0o644); err != nil {
		return "", fmt.Errorf("could not write patched coreBundle.js: %w", err)
	}
	markerPath := filepath.Join(tempDirectory, "package", stealthDriverVersionMarkerName)
	if err := os.WriteFile(markerPath, []byte(playwrightVersion), 0o644); err != nil {
		return "", fmt.Errorf("could not write stealth driver version marker: %w", err)
	}

	if err := activateStealthDriverDirectory(tempDirectory, stealthDriverDirectory, playwrightVersion); err != nil {
		return "", err
	}
	return stealthDriverDirectory, nil
}

/*
activateStealthDriverDirectory is a function which makes a fully-built patched driver copy at
tempDirectory visible at its final stealthDriverDirectory path via a single atomic rename.
This function contains the one place a racing concurrent builder is tolerated: if the rename
fails because stealthDriverDirectory now exists, that means another builder finished first,
so this checks whether the winner's copy already matches playwrightVersion and, if so, treats
that as success rather than an error. Only a stale copy left over from a previous Playwright
driver version is replaced, and only via one bounded retry.

Example:

	err := activateStealthDriverDirectory(tempDirectory, stealthDriverDirectory, "1.61.1")
*/
func activateStealthDriverDirectory(tempDirectory string, stealthDriverDirectory string, playwrightVersion string) error {
	renameErr := os.Rename(tempDirectory, stealthDriverDirectory)
	if renameErr == nil {
		return nil
	}

	if upToDate, err := stealthDriverUpToDate(stealthDriverDirectory, playwrightVersion); err == nil && upToDate {
		return nil
	}

	if err := os.RemoveAll(stealthDriverDirectory); err != nil {
		return fmt.Errorf("could not replace stale stealth driver copy: %w", err)
	}
	if err := os.Rename(tempDirectory, stealthDriverDirectory); err != nil {
		return fmt.Errorf("could not activate patched driver copy: %w", err)
	}
	return nil
}

/*
RestoreStealthPatch is a method which removes a previously built patched driver copy, freeing
the disk space it used and forcing the next StealthPatch-enabled Initialize to rebuild it from
scratch. Because the stealth patch feature never modifies the original driver directory —
StealthPatch-enabled and StealthPatch-disabled Instances always run against separate driver
copies — this never needs to restore anything to the original; it only ever deletes the
sibling copy. This is a no-op if no patched copy was ever built. Callers should call this
after Close, not before: on Windows, deleting a directory that a still-running driver process
has files open in fails with a sharing violation, so this returns an error if the Instance's
driver is still running rather than failing with a confusing platform-specific error later.

Example:

	err := agent.RestoreStealthPatch()
*/
func (shared *Instance) RestoreStealthPatch() error {
	if shared.playwrightInstance != nil {
		return errors.New("cannot remove the stealth driver copy while the browser driver is still running; call Close first")
	}

	baseDriverDirectory, err := shared.resolveBaseDriverDirectory()
	if err != nil {
		return err
	}
	stealthDriverDirectory := baseDriverDirectory + stealthDriverDirectorySuffix

	if _, statErr := os.Stat(stealthDriverDirectory); os.IsNotExist(statErr) {
		return nil
	} else if statErr != nil {
		return fmt.Errorf("could not check stealth driver copy: %w", statErr)
	}

	if err := os.RemoveAll(stealthDriverDirectory); err != nil {
		return fmt.Errorf("could not remove stealth driver copy: %w", err)
	}
	return nil
}
