package elementary

import (
	"os"
	"path/filepath"
	"testing"
)

/*
TestStealthPatchLifecycle is a test which verifies the stealth-patch build mechanism itself from
a genuine zero state: no Playwright driver, no browser binary, and no patched driver copy already
present anywhere. It redirects PLAYWRIGHT_DRIVER_PATH and PLAYWRIGHT_BROWSERS_PATH to fresh,
per-run temporary directories instead of touching the machine's real shared cache, so running this
does not disturb (or depend on) anything another tool might have installed at ~/.cache/ms-playwright*.
Because nothing is cached, every run performs the full download of the driver, the browser, and the
stealth patch from scratch. This intentionally does not exercise general browsing behavior (real-page
navigation, element interaction, etc.) — that is covered end-to-end, under stealth mode by default,
by elementary_test.go. This test verifies only what is unique to the patch mechanism: that Initialize
actually builds a patched copy, and that the original driver is left untouched.

Example:

	Expected Inputs:
	    Network access to registry.npmjs.org.

	Expected Outputs:
	    No errors are returned, and the original driver directory is left with a coreBundle.js that
	    differs from the patched copy's.
*/
func TestStealthPatchLifecycle(t *testing.T) {
	driverDir := t.TempDir()
	browsersDir := t.TempDir()
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", driverDir)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browsersDir)

	var browserAgent Instance
	err := browserAgent.Initialize(
		"stealth-test-context",
		"stealth-test-page",
		"chromium",
		1280,
		720,
		false,
		&BrowserOptions{StealthPatch: true},
	)
	if err != nil {
		t.Fatalf("Failed to initialize with stealth patch enabled: %v", err)
	}
	defer func() {
		if err := browserAgent.Close(); err != nil {
			t.Errorf("Failed to cleanly close stealth-patched browser agent: %v", err)
		}
		if err := browserAgent.RestoreStealthPatch(); err != nil {
			t.Errorf("Failed to remove the patched driver copy after stealth patch test: %v", err)
		}
	}()

	// Verify the original driver install was never touched: the stock coreBundle.js under
	// driverDir must still differ from the patched copy's, proving StealthPatch built a
	// separate side-by-side copy instead of patching driverDir in place.
	originalBundlePath := filepath.Join(driverDir, "package", "lib", "coreBundle.js")
	patchedBundlePath := filepath.Join(driverDir+"-stealth", "package", "lib", "coreBundle.js")
	originalBundle, err := os.ReadFile(originalBundlePath)
	if err != nil {
		t.Fatalf("Expected original coreBundle.js to exist at %s: %v", originalBundlePath, err)
	}
	patchedBundle, err := os.ReadFile(patchedBundlePath)
	if err != nil {
		t.Fatalf("Expected patched coreBundle.js to exist at %s: %v", patchedBundlePath, err)
	}
	if string(originalBundle) == string(patchedBundle) {
		t.Error("Expected the original and patched coreBundle.js to differ, but they were identical")
	}
}

/*
TestRestoreStealthPatchNoOp is a test which verifies that RestoreStealthPatch is a safe
no-op when no patch was ever applied, so callers can call it unconditionally during cleanup
without checking whether stealth mode was ever used.

Example:

	Expected Inputs:
	    An initialized Instance that never enabled StealthPatch.

	Expected Outputs:
	    RestoreStealthPatch returns nil.
*/
func TestRestoreStealthPatchNoOp(t *testing.T) {
	var browserAgent Instance
	err := browserAgent.Initialize(
		"restore-noop-context",
		"restore-noop-page",
		"chromium",
		1280,
		720,
		false,
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	if err := browserAgent.Close(); err != nil {
		t.Fatalf("Failed to close: %v", err)
	}

	if err := browserAgent.RestoreStealthPatch(); err != nil {
		t.Errorf("Expected RestoreStealthPatch to be a safe no-op, got error: %v", err)
	}
}

/*
TestRepairIsolatesPatchedAndUnpatchedDrivers is a test which verifies that Repair, when called
on a plain (non-stealth) Instance, never touches a stealth-patched sibling driver copy that
another Instance built against the same shared driver/browsers cache — and vice versa. This is
the isolation guarantee stealth-patched and plain Instances are documented to have with each
other (see stealthpatch.go), and Repair must preserve it rather than wiping the whole shared
cache indiscriminately.

Example:

	Expected Inputs:
	    Network access to registry.npmjs.org.

	Expected Outputs:
	    Repairing the plain Instance leaves the stealth-patched sibling's coreBundle.js byte-for-byte
	    unchanged, and repairing the stealth-patched Instance leaves the plain driver directory intact.
*/
func TestRepairIsolatesPatchedAndUnpatchedDrivers(t *testing.T) {
	driverDir := t.TempDir()
	browsersDir := t.TempDir()
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", driverDir)
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", browsersDir)

	var patchedAgent Instance
	if err := patchedAgent.Initialize(
		"repair-isolation-patched-context", "repair-isolation-patched-page",
		"chromium", 1280, 720, false,
		&BrowserOptions{StealthPatch: true},
	); err != nil {
		t.Fatalf("Failed to initialize stealth-patched instance: %v", err)
	}
	if err := patchedAgent.Close(); err != nil {
		t.Fatalf("Failed to close stealth-patched instance: %v", err)
	}

	patchedBundlePath := filepath.Join(driverDir+"-stealth", "package", "lib", "coreBundle.js")
	patchedBundleBefore, err := os.ReadFile(patchedBundlePath)
	if err != nil {
		t.Fatalf("Expected patched coreBundle.js to exist at %s: %v", patchedBundlePath, err)
	}

	var plainAgent Instance
	if err := plainAgent.Initialize(
		"repair-isolation-plain-context", "repair-isolation-plain-page",
		"chromium", 1280, 720, false, nil,
	); err != nil {
		t.Fatalf("Failed to initialize plain instance: %v", err)
	}
	if err := plainAgent.Close(); err != nil {
		t.Fatalf("Failed to close plain instance: %v", err)
	}

	if err := plainAgent.Repair(); err != nil {
		t.Fatalf("Failed to repair plain instance: %v", err)
	}

	patchedBundleAfter, err := os.ReadFile(patchedBundlePath)
	if err != nil {
		t.Fatalf("Expected patched coreBundle.js to still exist at %s after repairing the plain instance: %v", patchedBundlePath, err)
	}
	if string(patchedBundleBefore) != string(patchedBundleAfter) {
		t.Error("Expected repairing the plain instance to leave the stealth-patched sibling copy untouched, but its coreBundle.js changed")
	}

	originalBundlePath := filepath.Join(driverDir, "package", "lib", "coreBundle.js")
	originalBundleBeforePatchedRepair, err := os.ReadFile(originalBundlePath)
	if err != nil {
		t.Fatalf("Expected repairing the plain instance to leave a usable driver at %s: %v", originalBundlePath, err)
	}

	if err := patchedAgent.Repair(); err != nil {
		t.Fatalf("Failed to repair stealth-patched instance: %v", err)
	}

	originalBundleAfterPatchedRepair, err := os.ReadFile(originalBundlePath)
	if err != nil {
		t.Fatalf("Expected repairing the stealth-patched instance to leave the plain driver usable at %s: %v", originalBundlePath, err)
	}
	if string(originalBundleBeforePatchedRepair) != string(originalBundleAfterPatchedRepair) {
		t.Error("Expected repairing the stealth-patched instance to leave the shared, unpatched driver directory untouched, but its coreBundle.js changed")
	}

	if err := patchedAgent.RestoreStealthPatch(); err != nil {
		t.Errorf("Failed to remove the patched driver copy during cleanup: %v", err)
	}
}

/*
TestResolveBaseDriverDirectoryHonorsOverride is a test which verifies that
resolveBaseDriverDirectory returns PLAYWRIGHT_DRIVER_PATH verbatim when it is set, matching
playwright-go's own resolution order.

Example:

	dir, err := agent.resolveBaseDriverDirectory()
*/
func TestResolveBaseDriverDirectoryHonorsOverride(t *testing.T) {
	var agent Instance
	customDirectory := t.TempDir()
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", customDirectory)

	got, err := agent.resolveBaseDriverDirectory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != customDirectory {
		t.Errorf("expected %q, got %q", customDirectory, got)
	}
}

/*
TestResolveBaseDriverDirectoryDefaultsToCacheConvention is a test which verifies that
resolveBaseDriverDirectory falls back to the default cache-directory convention when
PLAYWRIGHT_DRIVER_PATH is unset.

Example:

	dir, err := agent.resolveBaseDriverDirectory()
*/
func TestResolveBaseDriverDirectoryDefaultsToCacheConvention(t *testing.T) {
	var agent Instance
	t.Setenv("PLAYWRIGHT_DRIVER_PATH", "")

	cacheDirectory, err := agent.getDefaultCacheDirectory()
	if err != nil {
		t.Fatalf("unexpected error resolving cache directory: %v", err)
	}
	version, err := resolvePlaywrightDriverVersion()
	if err != nil {
		t.Fatalf("unexpected error resolving playwright version: %v", err)
	}
	want := filepath.Join(cacheDirectory, "ms-playwright-go", version)

	got, err := agent.resolveBaseDriverDirectory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

/*
TestResolveBrowsersDirectoryHonorsOverride is a test which verifies that
resolveBrowsersDirectory returns PLAYWRIGHT_BROWSERS_PATH verbatim when it is set.

Example:

	dir, err := agent.resolveBrowsersDirectory()
*/
func TestResolveBrowsersDirectoryHonorsOverride(t *testing.T) {
	var agent Instance
	customDirectory := t.TempDir()
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", customDirectory)

	got, err := agent.resolveBrowsersDirectory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != customDirectory {
		t.Errorf("expected %q, got %q", customDirectory, got)
	}
}

/*
TestResolveBrowsersDirectoryDefaultsToCacheConvention is a test which verifies that
resolveBrowsersDirectory falls back to the default cache-directory convention when
PLAYWRIGHT_BROWSERS_PATH is unset.

Example:

	dir, err := agent.resolveBrowsersDirectory()
*/
func TestResolveBrowsersDirectoryDefaultsToCacheConvention(t *testing.T) {
	var agent Instance
	t.Setenv("PLAYWRIGHT_BROWSERS_PATH", "")

	cacheDirectory, err := agent.getDefaultCacheDirectory()
	if err != nil {
		t.Fatalf("unexpected error resolving cache directory: %v", err)
	}
	want := filepath.Join(cacheDirectory, "ms-playwright")

	got, err := agent.resolveBrowsersDirectory()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

/*
TestStealthDriverUpToDateMissingDirectory is a test which verifies that stealthDriverUpToDate
reports false, without error, when the stealth driver directory does not exist yet.

Example:

	upToDate, err := stealthDriverUpToDate(dir, "1.61.1")
*/
func TestStealthDriverUpToDateMissingDirectory(t *testing.T) {
	upToDate, err := stealthDriverUpToDate(filepath.Join(t.TempDir(), "does-not-exist"), "1.61.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upToDate {
		t.Error("expected a missing stealth driver directory to report not up to date")
	}
}

/*
TestStealthDriverUpToDateMatchingVersion is a test which verifies that stealthDriverUpToDate
reports true when the version marker matches the requested Playwright driver version.

Example:

	upToDate, err := stealthDriverUpToDate(dir, "1.61.1")
*/
func TestStealthDriverUpToDateMatchingVersion(t *testing.T) {
	stealthDirectory := t.TempDir()
	writeStealthVersionMarker(t, stealthDirectory, "1.61.1")

	upToDate, err := stealthDriverUpToDate(stealthDirectory, "1.61.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !upToDate {
		t.Error("expected a matching version marker to report up to date")
	}
}

/*
TestStealthDriverUpToDateStaleVersion is a test which verifies that stealthDriverUpToDate
reports false when the version marker does not match the requested Playwright driver version.

Example:

	upToDate, err := stealthDriverUpToDate(dir, "1.61.1")
*/
func TestStealthDriverUpToDateStaleVersion(t *testing.T) {
	stealthDirectory := t.TempDir()
	writeStealthVersionMarker(t, stealthDirectory, "1.60.0")

	upToDate, err := stealthDriverUpToDate(stealthDirectory, "1.61.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if upToDate {
		t.Error("expected a mismatched version marker to report not up to date")
	}
}

/*
TestActivateStealthDriverDirectoryFreshRename is a test which verifies that
activateStealthDriverDirectory moves a freshly-built temp directory into place via a plain
rename when nothing already occupies the destination.

Example:

	err := activateStealthDriverDirectory(tempDir, destination, "1.61.1")
*/
func TestActivateStealthDriverDirectoryFreshRename(t *testing.T) {
	parentDirectory := t.TempDir()
	tempDirectory := filepath.Join(parentDirectory, "building")
	writeFile(t, filepath.Join(tempDirectory, "sentinel.txt"), "built")
	destination := filepath.Join(parentDirectory, "1.61.1-stealth")

	if err := activateStealthDriverDirectory(tempDirectory, destination, "1.61.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(tempDirectory); !os.IsNotExist(err) {
		t.Error("expected the temp directory to be moved away by the rename")
	}
	data, err := os.ReadFile(filepath.Join(destination, "sentinel.txt"))
	if err != nil || string(data) != "built" {
		t.Errorf("expected activated directory to contain the built sentinel file, got %q, err=%v", data, err)
	}
}

// Simulates a concurrent builder losing the race: the destination already holds a
// complete, correctly-versioned copy by the time this one tries to activate.
func TestActivateStealthDriverDirectoryKeepsExistingWinner(t *testing.T) {
	parentDirectory := t.TempDir()
	destination := filepath.Join(parentDirectory, "1.61.1-stealth")
	writeStealthVersionMarker(t, destination, "1.61.1")
	writeFile(t, filepath.Join(destination, "winner.txt"), "winner")

	tempDirectory := filepath.Join(parentDirectory, "building")
	writeFile(t, filepath.Join(tempDirectory, "loser.txt"), "loser")

	if err := activateStealthDriverDirectory(tempDirectory, destination, "1.61.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destination, "winner.txt")); err != nil {
		t.Error("expected the winning builder's content to be preserved")
	}
	if _, err := os.Stat(filepath.Join(destination, "loser.txt")); !os.IsNotExist(err) {
		t.Error("expected this builder's redundant copy to be discarded rather than merged in")
	}
}

// Simulates activating over a copy left behind by a previous Playwright driver version.
func TestActivateStealthDriverDirectoryReplacesStaleCopy(t *testing.T) {
	parentDirectory := t.TempDir()
	destination := filepath.Join(parentDirectory, "1.61.1-stealth")
	writeStealthVersionMarker(t, destination, "1.60.0")

	tempDirectory := filepath.Join(parentDirectory, "building")
	writeFile(t, filepath.Join(tempDirectory, "fresh.txt"), "fresh")

	if err := activateStealthDriverDirectory(tempDirectory, destination, "1.61.1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(destination, "fresh.txt")); err != nil {
		t.Error("expected the stale copy to be replaced with the freshly built one")
	}
}

/*
TestCopyDriverDirectoryPreservesModesAndSymlinks is a test which verifies that
copyDriverDirectory preserves file permissions (critical for the copied Node.js executable)
and recreates symlinks rather than following them.

Example:

	err := copyDriverDirectory(source, destination)
*/
func TestCopyDriverDirectoryPreservesModesAndSymlinks(t *testing.T) {
	source := t.TempDir()
	destination := t.TempDir()

	writeFileWithMode(t, filepath.Join(source, "node"), "#!/bin/sh\necho fake-node\n", 0o755)
	writeFileWithMode(t, filepath.Join(source, "package", "lib", "coreBundle.js"), "console.log('bundle')", 0o644)
	if err := os.Symlink("node", filepath.Join(source, "node-link")); err != nil {
		t.Skipf("symlinks not supported on this filesystem: %v", err)
	}

	if err := copyDriverDirectory(source, destination); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	copiedExecutableInfo, err := os.Stat(filepath.Join(destination, "node"))
	if err != nil {
		t.Fatalf("expected copied executable to exist: %v", err)
	}
	if copiedExecutableInfo.Mode().Perm() != 0o755 {
		t.Errorf("expected copied executable to keep mode 0755, got %v", copiedExecutableInfo.Mode().Perm())
	}

	copiedBundleData, err := os.ReadFile(filepath.Join(destination, "package", "lib", "coreBundle.js"))
	if err != nil || string(copiedBundleData) != "console.log('bundle')" {
		t.Errorf("expected copied file content to match, got %q, err=%v", copiedBundleData, err)
	}

	linkTarget, err := os.Readlink(filepath.Join(destination, "node-link"))
	if err != nil {
		t.Fatalf("expected copied symlink to exist: %v", err)
	}
	if linkTarget != "node" {
		t.Errorf("expected copied symlink to point at %q, got %q", "node", linkTarget)
	}
}

func writeStealthVersionMarker(t *testing.T, stealthDirectory string, version string) {
	t.Helper()
	writeFile(t, filepath.Join(stealthDirectory, "package", stealthDriverVersionMarkerName), version)
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()
	writeFileWithMode(t, path, content, 0o644)
}

func writeFileWithMode(t *testing.T, path string, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
