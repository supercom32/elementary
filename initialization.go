package elementary

import (
	"errors"
	"fmt"
	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/archiver"
	"github.com/supercom32/elementary/logger"
	"github.com/supercom32/filesystem"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/*
IsInitialized is a method which returns whether the underlying Playwright controller is active. This enables safe,
preemptive checks before dispatching web driver operations, ensuring the automation engine is ready.

Example:

	initialized := agent.IsInitialized()
*/
func (shared *Instance) IsInitialized() bool {
	return shared.playwrightInstance != nil
}

/*
Initialize is a method which spins up the browser simulation engines. This bootstraps a clean, sandboxed web browser
environment tailored for headless scraping or automated testing, while managing driver setups and failures. In addition, the following should be noted:

- This method stores the initialization parameters such as width, height, browser type, and default aliases directly onto the instance to ensure that the engine can be cleanly restarted using identical initial conditions later if needed.

- This method attempts to automatically recover from driver startup failures by calling Repair, which resets only the driver, browser, and (if applicable) stealth-patch directories this Instance actually resolves to, then installing fresh browser binaries.

- This method returns the sentinel error ErrWindowsRestartRequired on Windows when a driver repair is performed, rather than abruptly exiting. This alerts the host application that a restart is required because Windows locks active binaries during the driver repair process.

Example:

	err := agent.Initialize("main", "home", "chromium", 1280, 720, false, nil)
*/
func (shared *Instance) Initialize(defaultContextAlias string, defaultPageAlias string, browserType string, width int, height int, isVisible bool, browserOptions *BrowserOptions) error {
	// Store initialization parameters for potential restart
	shared.defaultContextAlias = defaultContextAlias
	shared.defaultPageAlias = defaultPageAlias
	shared.browserType = browserType
	shared.width = width
	shared.height = height
	shared.isVisible = isVisible

	if browserOptions != nil && browserOptions.StealthPatch && browserType != BROWSER_CHROME {
		return logger.Error(errors.New("stealth patch is only supported for chromium"), "cannot apply stealth patch")
	}
	if browserOptions != nil && browserOptions.Channel != "" && browserType != BROWSER_CHROME {
		return logger.Error(errors.New("channel is only supported for chromium"), "cannot apply channel")
	}

	if IS_DEBUG_ENABLED {
		logger.SetDebugMode(true)
	}
	err := shared.InitializeDriver(defaultContextAlias, defaultPageAlias, browserType, width, height, isVisible, browserOptions)
	if err != nil {
		repairErr := shared.Repair()
		if repairErr != nil {
			return logger.Error(err, "could not repair Playwright drivers: %s", repairErr)
		}

		if runtime.GOOS == "windows" {
			return logger.Error(ErrWindowsRestartRequired, "Windows Playwright driver repair completed, restart required")
		}

		err = shared.InitializeDriver(defaultContextAlias, defaultPageAlias, browserType, width, height, isVisible, browserOptions)
		if err != nil {
			return logger.Error(err, "failed to start browser instance after Playwright driver repair")
		}
	}
	return nil
}

/*
Repair is a method which resets the specific driver, stealth-patch copy, and browser binary
directories this Instance actually resolves to, then reinstalls them fresh. This performs the
recovery step silently and registers the driver packages, returning any error encountered
during cleanup. In addition, the following should be noted:

  - This method honors the PLAYWRIGHT_DRIVER_PATH and PLAYWRIGHT_BROWSERS_PATH environment
    variable overrides the same way InitializeDriver does, rather than assuming the default
    OS cache location. Repairing an Instance whose driver was installed to a custom location
    would otherwise silently miss the corrupted directory and reinstall to the wrong place.

  - This method never deletes the shared, unpatched driver directory when this Instance was
    configured with StealthPatch — only the stealth-patched sibling copy, via
    RestoreStealthPatch (see stealthpatch.go), is removed and rebuilt. A StealthPatch Instance
    never runs against the unpatched driver directory directly (it is only ever read from to
    build the sibling copy), so deleting it would serve no purpose for this Instance while
    forcing an unrelated plain Instance sharing the same cache to redo its driver install, or
    breaking it outright if that plain Instance's driver is still running. A plain Instance
    repairing its driver never touches a patched sibling's copy either. The browser binary
    directory remains shared either way — StealthPatch does not build a separate copy of it —
    so repairing it is a step every Instance sharing the cache genuinely depends on.

  - This method rebuilds the stealth-patched copy immediately when this Instance uses
    StealthPatch, so Repair alone leaves the Instance ready to relaunch without depending on
    Initialize's retry path to notice the copy is missing.

  - This method returns an error if the Instance's driver is still running rather than
    deleting directories out from under it; callers should call this after Close, not before,
    the same as RestoreStealthPatch.

Example:

	err := agent.Repair()
*/
func (shared *Instance) Repair() error {
	if shared.playwrightInstance != nil {
		return errors.New("cannot repair driver directories while the browser driver is still running")
	}

	cacheDirectory, err := shared.getDefaultCacheDirectory()
	if err != nil {
		return logger.Error(err, "could not get default cache directory during driver repair")
	}
	baseDriverDirectory, err := shared.resolveBaseDriverDirectory()
	if err != nil {
		return logger.Error(err, "could not resolve driver directory during driver repair")
	}
	browsersDirectory, err := shared.resolveBrowsersDirectory()
	if err != nil {
		return logger.Error(err, "could not resolve browsers directory during driver repair")
	}

	_ = filesystem.DeleteDirectory(cacheDirectory + "/mozilla")
	if shared.browserOptions.StealthPatch {
		if err := shared.RestoreStealthPatch(); err != nil {
			return logger.Error(err, "could not remove corrupted stealth driver copy")
		}
	} else {
		if err := filesystem.DeleteDirectory(baseDriverDirectory); err != nil {
			return logger.Error(err, "could not remove corrupted driver directory: %s", baseDriverDirectory)
		}
	}
	if err := filesystem.DeleteDirectory(browsersDirectory); err != nil {
		return logger.Error(err, "could not remove corrupted browser binaries: %s", browsersDirectory)
	}

	var options playwright.RunOptions
	options.Browsers = append(options.Browsers, BROWSER_FIREFOX)
	options.Browsers = append(options.Browsers, BROWSER_CHROME)
	options.DriverDirectory = baseDriverDirectory

	// Install is safe to call unconditionally here even for a StealthPatch Instance whose
	// baseDriverDirectory was left untouched above: it only downloads the driver if missing or
	// mismatched, so on an intact directory this is a no-op that still reinstalls the deleted
	// browsersDirectory.
	if err := playwright.Install(&options); err != nil {
		return logger.Error(err, "could not install playwright drivers during repair")
	}
	if shared.browserOptions.StealthPatch {
		if _, err := shared.prepareStealthDriverDirectory(baseDriverDirectory); err != nil {
			return logger.Error(err, "could not rebuild stealth driver copy during repair")
		}
	}
	return nil
}

/*
resolveBrowsersDirectory is a method which locates the browser binary cache directory,
honoring the PLAYWRIGHT_BROWSERS_PATH environment variable the same way the underlying
Playwright driver does when it downloads Chromium and Firefox, otherwise falling back to the
default cache-directory convention. Honoring the override here is required for the same
reason resolveBaseDriverDirectory honors PLAYWRIGHT_DRIVER_PATH: repairing a directory other
than the one actually in use would leave the real corruption untouched.

Example:

	dir, err := shared.resolveBrowsersDirectory()
*/
func (shared *Instance) resolveBrowsersDirectory() (string, error) {
	if browsersPath := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); browsersPath != "" {
		return browsersPath, nil
	}
	cacheDirectory, err := shared.getDefaultCacheDirectory()
	if err != nil {
		return "", logger.Error(err, "could not get default cache directory")
	}
	return filepath.Join(cacheDirectory, "ms-playwright"), nil
}

/*
getDefaultCacheDirectory is a method which resolves the standard OS-specific cache directory path. This locates
where the driver binaries and browser installations are stored or should be cleaned on the current host platform.

Example:

	path, err := shared.getDefaultCacheDirectory()
*/
func (shared *Instance) getDefaultCacheDirectory() (string, error) {
	userHomeDir, err := os.UserHomeDir()
	if err != nil {
		return "", logger.Error(err, "could not get user home directory")
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(userHomeDir, "AppData", "Local"), nil
	case "darwin":
		return filepath.Join(userHomeDir, "Library", "Caches"), nil
	case "linux":
		return filepath.Join(userHomeDir, ".cache"), nil
	}
	return "", errors.New("could not determine cache directory")
}

/*
InitializeDriver is a method which downloads drivers, starts the browser backend, and provisions default state. This
handles the low-level bootstrapping of the Playwright framework and configures initial session structures. In addition,
the following should be noted:

  - This method launches a separate OS background process to run the Chromium or Firefox simulation engine. This means
    your host system must have sufficient permissions and memory to execute these external browser binaries. These
    background processes will persist in your operating system until the browser instance is explicitly closed.

  - This method modifies the shared playwrightInstance field, storing the newly initialized Playwright runner. This
    represents a stateful change that makes subsequent calls to this function act as a no-op if the instance is already
    running.

  - This method re-scaffolds default context and page structures by invoking the internal NewContext and NewPage methods.
    This populates the default browser tabs and contexts required for immediate automation.

Example:

	err := agent.InitializeDriver("guest", "tab1", "chromium", 1024, 768, true, nil)
*/
func (shared *Instance) InitializeDriver(defaultContextAlias string, defaultPageAlias string, browserType string, width int, height int, isVisible bool, browserOptions *BrowserOptions) error {
	var err error
	if shared.playwrightInstance != nil {
		return nil
	}
	shared.defaultElementTimeout = 3 * time.Second
	shared.defaultNavigationTimeout = 30 * time.Second
	shared.defaultPlaywrightTimeout = 5 * time.Second
	shared.defaultSelectorEngine = SELECTOR_ENGINE_XPATH
	if browserOptions != nil {
		shared.browserOptions = *browserOptions
	} else {
		shared.browserOptions = BrowserOptions{}
	}
	isHeadless := !isVisible
	var runOptions playwright.RunOptions
	runOptions.Browsers = []string{browserType}
	if browserOptions != nil && browserOptions.Channel != "" && browserType != BROWSER_CHROME {
		return logger.Error(errors.New("channel is only supported for chromium"), "cannot apply channel")
	}
	if browserOptions != nil && browserOptions.StealthPatch {
		if browserType != BROWSER_CHROME {
			return logger.Error(errors.New("stealth patch is only supported for chromium"), "cannot apply stealth patch")
		}
		baseDriverDirectory, err := shared.resolveBaseDriverDirectory()
		if err != nil {
			return logger.Error(err, "could not resolve driver directory for stealth patch")
		}
		runOptions.DriverDirectory = baseDriverDirectory
	}
	if err = playwright.Install(&runOptions); err != nil {
		return logger.Error(err, "could not install underlying browser driver")
	}
	if browserOptions != nil && browserOptions.StealthPatch {
		stealthDriverDirectory, err := shared.prepareStealthDriverDirectory(runOptions.DriverDirectory)
		if err != nil {
			return logger.Error(err, "failed to prepare stealth driver copy")
		}
		runOptions.DriverDirectory = stealthDriverDirectory
	}
	shared.playwrightInstance, err = playwright.Run(&runOptions)
	if err != nil {
		shared.playwrightInstance = nil
		return logger.Error(err, "could not start automation controller")
	}
	if browserType == BROWSER_CHROME {
		chromiumArgs := shared.browserOptions.Args
		if chromiumArgs == nil {
			chromiumArgs = []string{"--mute-audio", "--remote-debugging-port=9222"}
		}
		chromiumLaunchOptions := playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(isHeadless),
			Args:     chromiumArgs,
		}
		if shared.browserOptions.Channel != "" {
			chromiumLaunchOptions.Channel = playwright.String(shared.browserOptions.Channel)
		}
		shared.browserInstance, err = shared.playwrightInstance.Chromium.Launch(chromiumLaunchOptions)
	}
	if browserType == BROWSER_FIREFOX {
		shared.browserInstance, err = shared.playwrightInstance.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
			Headless: playwright.Bool(isHeadless),
			Args:     shared.browserOptions.Args,
		})
	}
	if err != nil {
		_ = shared.Terminate()
		return logger.Error(err, "could not start browser instance")
	}
	err = shared.NewContext(defaultContextAlias, width, height, browserOptions)
	if err != nil {
		_ = shared.Terminate()
		return logger.Error(err, "failed to create default browser context")
	}
	err = shared.NewPage(defaultPageAlias)
	if err != nil {
		_ = shared.Terminate()
		return logger.Error(err, "failed to create default page")
	}
	return nil
}

/*
SetTimeout is a method which overrides the default timeout threshold for element interactions. This
adjusts how long driver operations wait for selectors to resolve or become interactable before failing.

Example:

	agent.SetTimeout(5 * time.Second)
*/
func (shared *Instance) SetTimeout(timeout time.Duration) {
	shared.defaultElementTimeout = timeout
}

/*
GetTimeout is a method which returns the active timeout limit for element interactions. This allows
inspecting current wait thresholds to dynamically adjust timing configurations for slow network targets.

Example:

	timeout := agent.GetTimeout()
*/
func (shared *Instance) GetTimeout() time.Duration {
	return shared.defaultElementTimeout
}

/*
SetDefaultSelectorEngine is a method which overrides which Playwright locator engine unprefixed
selector strings are parsed as (SELECTOR_ENGINE_XPATH or SELECTOR_ENGINE_CSS). Selectors that
already declare their own engine (e.g. "css=.btn", "role=button", "text=Submit") are unaffected by
this setting — it only applies when a selector has no engine prefix at all. Defaults to
SELECTOR_ENGINE_XPATH, so calling this is only necessary to opt into a different default.

Example:

	agent.SetDefaultSelectorEngine(elementary.SELECTOR_ENGINE_CSS)
*/
func (shared *Instance) SetDefaultSelectorEngine(engine string) {
	shared.defaultSelectorEngine = engine
}

/*
GetDefaultSelectorEngine is a method which returns the Playwright locator engine currently applied
to unprefixed selector strings.

Example:

	engine := agent.GetDefaultSelectorEngine()
*/
func (shared *Instance) GetDefaultSelectorEngine() string {
	return shared.defaultSelectorEngine
}

/*
SetNavigationTimeout is a method which overrides the default timeout threshold for page navigations. This
adjusts how long driver operations wait for page loads, redirections, or URL changes to complete before failing.

Example:

	agent.SetNavigationTimeout(45 * time.Second)
*/
func (shared *Instance) SetNavigationTimeout(timeout time.Duration) {
	shared.defaultNavigationTimeout = timeout
}

/*
GetNavigationTimeout is a method which returns the active timeout limit for page navigations. This allows
inspecting current navigation wait thresholds to dynamically adjust load configurations for slower networks.

Example:

	timeout := agent.GetNavigationTimeout()
*/
func (shared *Instance) GetNavigationTimeout() time.Duration {
	return shared.defaultNavigationTimeout
}

/*
SetPlaywrightTimeout is a method which overrides the default timeout threshold for internal Playwright operations.
This adjusts how long low-level Playwright execution loops wait before throwing system errors.

Example:

	agent.SetPlaywrightTimeout(10 * time.Second)
*/
func (shared *Instance) SetPlaywrightTimeout(timeout time.Duration) {
	shared.defaultPlaywrightTimeout = timeout
}

/*
GetPlaywrightTimeout is a method which returns the active timeout limit for internal Playwright operations.
This allows inspecting current low-level driver execution thresholds.

Example:

	timeout := agent.GetPlaywrightTimeout()
*/
func (shared *Instance) GetPlaywrightTimeout() time.Duration {
	return shared.defaultPlaywrightTimeout
}

/*
SetScreenshotOptions is a method which configures every aspect of automated screenshot capture in a single call:
the target directory, which action stages trigger a capture, and optional TAR archiving. In addition, the following
should be noted:

  - This method replaces the entire prior configuration rather than merging with it, so callers that only want to
    change one field must pass the others through unchanged (e.g. built from a previously stored ScreenshotOptions).

  - This method tears down and replaces any existing archiver whenever it is called, even if
    options.ArchiveFilename names the same file as before. Passing options.ArchiveFilename == "" disables archiving
    and closes (i.e. gzip-finalizes, see ArchiveFilename) any archiver currently open.

  - This method resolves the archive path as options.Directory joined with options.ArchiveFilename, so archiving
    requires both fields to be set.

Example:

	err := agent.SetScreenshotOptions(elementary.ScreenshotOptions{
	    Directory:       "./screenshots",
	    BeforeAction:    true,
	    AfterAction:     true,
	    OnFailure:       true,
	    ArchiveFilename: "archive.tar",
	})
*/
func (shared *Instance) SetScreenshotOptions(options ScreenshotOptions) error {
	shared.archiverMutex.Lock()
	defer shared.archiverMutex.Unlock()
	if shared.screenshotArchiver != nil {
		_ = shared.screenshotArchiver.Close()
		shared.screenshotArchiver = nil
	}
	if options.ArchiveFilename != "" {
		tarEngine, err := archiver.NewTarArchiver(filepath.Join(options.Directory, options.ArchiveFilename))
		if err != nil {
			return logger.Error(err, "failed to initialize automatic tar archiving")
		}
		shared.screenshotArchiver = tarEngine
	}
	shared.screenshotOptions = options
	return nil
}

/*
GetScreenshotAsBinary is a method which captures a full-page snapshot and returns the binary payload. This retrieves the
page's current visual state in-memory, permitting direct stream forwarding, API transmission, or instant processing.

By default the capture is bounded by the instance's element interaction timeout, which may be too short for full-page
captures (e.g. while waiting on web fonts to load). Pass ActionOptions{InfiniteTimeout: true} or an explicit longer
ActionOptions.Timeout to override this on a per-call basis.

Example:

	data, err := agent.GetScreenshotAsBinary(ActionOptions{InfiniteTimeout: true})
*/
func (shared *Instance) GetScreenshotAsBinary(options ...ActionOptions) ([]byte, error) {
	page, err := shared.getCurrentPage()
	if err != nil {
		return nil, err
	}
	var screenshotOptions playwright.PageScreenshotOptions
	timeout, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	screenshotOptions.Timeout = &timeout
	isFullPage := true
	screenshotOptions.FullPage = &isFullPage
	screenshotData, err := page.Screenshot(screenshotOptions)
	if err != nil {
		return nil, logger.Error(err, "failed to get screenshot bytes")
	}
	return screenshotData, nil
}

/*
SaveScreenshot is a method which captures a full-page snapshot and writes the output directly to disk. This is useful
for archiving visual evidence, creating a clear execution audit trail, or inspecting rendering failures offline.
In addition, the following should be noted:

  - This method obtains the screenshot bytes via GetScreenshotAsBinary and then persists them itself, rather than
    duplicating the capture logic or relying on Playwright's own Path-based save option (which would otherwise result
    in the same bytes being written to disk twice: once by Playwright and once by this method).

  - This method writes a file directly to the host filesystem, which requires appropriate write permissions for the
    target directory. If the directory does not exist, this method will attempt to create it automatically.

  - This method normalizes the filename by removing illegal characters and prepending a timestamp in the format
    YYYY-MM-DD_HH-MM-SS. This prevents naming collisions and filesystem issues across different operating systems.

Example:

	err := agent.SaveScreenshot("./debug/", "failure_view.png", ActionOptions{InfiniteTimeout: true})
*/
func (shared *Instance) SaveScreenshot(path string, screenshotName string, options ...ActionOptions) error {
	screenshotData, err := shared.GetScreenshotAsBinary(options...)
	if err != nil {
		return err
	}

	timestamp := time.Now().Format("2006-01-02_15-04-05")
	normalizedFilename := fmt.Sprintf("%s_%s", timestamp, screenshotName)
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", "?", "_", "%", "_", "*", "_",
		":", "_", "|", "_", "\"", "_", "<", "_", ">", "_",
	)
	normalizedFilename = replacer.Replace(normalizedFilename)

	shared.archiverMutex.Lock()
	if shared.screenshotArchiver != nil {
		err := shared.screenshotArchiver.AddFile(normalizedFilename, screenshotData)
		shared.archiverMutex.Unlock()
		if err != nil {
			return logger.Error(err, "failed to append screenshot bytes to TAR archive")
		}
		return nil
	}
	shared.archiverMutex.Unlock()

	normalizedPath := filesystem.GetNormalizedDirectoryPath(path)
	if err := filesystem.CreateDirectory(normalizedPath, 0); err != nil {
		return logger.Error(err, "failed to create directory for screenshot: %s", normalizedPath)
	}
	normalizedPath = normalizedPath + normalizedFilename
	if err := filesystem.WriteBytesToFile(normalizedPath, screenshotData, 0); err != nil {
		return logger.Error(err, "failed to write screenshot to %s", normalizedPath)
	}
	return nil
}

/*
Close is a method which disposes of the active browser session and pages. This performs a graceful teardown to reclaim
allocated resources and stop background browser instances.

Example:

	err := agent.Close()
*/
func (shared *Instance) Close() error {
	return shared.Terminate()
}

/*
Terminate is a method which stops all running contexts and terminates backend browser processes. This guarantees
clean termination of the automation lifecycle, preventing stray web driver processes from consuming system resources.
In addition, the following should be noted:

  - This method clears internal references by setting playwrightInstance, browserInstance, contextInstances, and
    currentPage to nil. This allows the garbage collector to reclaim memory allocated to the browser session.

Example:

	err := agent.Terminate()
*/
func (shared *Instance) Terminate() error {
	shared.archiverMutex.Lock()
	if shared.screenshotArchiver != nil {
		_ = shared.screenshotArchiver.Close()
		shared.screenshotArchiver = nil
	}
	shared.archiverMutex.Unlock()
	shared.CloseAllContextInstances()

	// Snapshot under the lock rather than holding it across the blocking Close/Stop calls below
	// (CloseAllContextInstances above also takes stateMutex internally, so holding it here too
	// would deadlock).
	shared.stateMutex.Lock()
	browserInstance := shared.browserInstance
	playwrightInstance := shared.playwrightInstance
	shared.stateMutex.Unlock()

	if browserInstance != nil {
		if err := browserInstance.Close(); err != nil {
			return logger.Error(err, "could not stop browser simulation")
		}
	}
	if playwrightInstance != nil {
		if err := playwrightInstance.Stop(); err != nil {
			return logger.Error(err, "could not stop simulation controller")
		}
	}

	shared.stateMutex.Lock()
	shared.resetTeardownStateUnlocked()
	shared.stateMutex.Unlock()
	return nil
}

/*
resetTeardownStateUnlocked is a method which nils out the driver/browser/context/page references
and resets the context and page indices to their sentinel "nothing active" values. This is the
single place Terminate and Shutdown agree on what a fully torn-down Instance looks like, so the
two teardown paths can't drift out of sync on which fields get reset. Callers must already hold
stateMutex.

Example:

	shared.resetTeardownStateUnlocked()
*/
func (shared *Instance) resetTeardownStateUnlocked() {
	shared.playwrightInstance = nil
	shared.browserInstance = nil
	shared.contextInstances = nil
	shared.currentContextIndex = NULL_CONTEXT
	shared.currentPageIndex = NULL_PAGE
	shared.currentPage = nil
}

/*
Shutdown is a method which executes an immediate reset of all active session fields. This halts the browser engine and
discards context mappings without raising errors, returning instance registers back to their clean zero states.
In addition, the following should be noted:

  - This method mutates internal state fields of the instance, nil-ing out playwrightInstance, browserInstance,
    contextInstances, and currentPage while setting context and page indices to 0. This prepares the instance for a
    clean re-initialization.

Example:

	agent.Shutdown()
*/
func (shared *Instance) Shutdown() {
	shared.stateMutex.Lock()
	playwrightInstance := shared.playwrightInstance
	currentPage := shared.currentPage
	shared.stateMutex.Unlock()

	if playwrightInstance == nil {
		return
	}
	if currentPage != nil {
		_ = currentPage.Close()
	}
	_ = playwrightInstance.Stop()

	shared.stateMutex.Lock()
	shared.resetTeardownStateUnlocked()
	shared.stateMutex.Unlock()
}

/*
Restart is a method which restarts the browser simulation engine using identical startup properties. This is valuable
for re-establishing a pristine test environment to clear memory fragmentation or bypass frozen browser states.
In addition, the following should be noted:

  - This method first calls Shutdown to terminate any active browser processes and clear state before invoking Initialize.
    This ensures that resources are fully released before a new browser engine is spun up.

  - This method restores any timeouts previously set via SetTimeout, SetNavigationTimeout, or
    SetPlaywrightTimeout, and any selector engine previously set via SetDefaultSelectorEngine,
    after Initialize completes. Initialize/InitializeDriver otherwise reset these to hardcoded
    defaults on every call, which would silently undo prior customization on every restart
    despite this method's "identical startup properties" contract.

Example:

	err := agent.Restart()
*/
func (shared *Instance) Restart() error {
	browserOptionsCopy := shared.browserOptions
	browserOptions := &browserOptionsCopy
	elementTimeout := shared.defaultElementTimeout
	navigationTimeout := shared.defaultNavigationTimeout
	playwrightTimeout := shared.defaultPlaywrightTimeout
	selectorEngine := shared.defaultSelectorEngine

	shared.Shutdown()

	if err := shared.Initialize(
		shared.defaultContextAlias,
		shared.defaultPageAlias,
		shared.browserType,
		shared.width,
		shared.height,
		shared.isVisible,
		browserOptions,
	); err != nil {
		return err
	}

	shared.defaultElementTimeout = elementTimeout
	shared.defaultNavigationTimeout = navigationTimeout
	shared.defaultPlaywrightTimeout = playwrightTimeout
	shared.defaultSelectorEngine = selectorEngine
	return nil
}
