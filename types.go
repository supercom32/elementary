package elementary

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/archiver"
)

const (
	IS_DEBUG_ENABLED      = false
	BROWSER_FIREFOX       = "firefox"
	BROWSER_CHROME        = "chromium"
	MOUSE_BUTTON_LEFT     = "left"
	MOUSE_BUTTON_MIDDLE   = "middle"
	MOUSE_BUTTON_RIGHT    = "right"
	UNTRACKED_PAGE        = -1
	NULL_PAGE             = -2
	NULL_CONTEXT          = -1
	SELECTOR_ENGINE_XPATH = "xpath"
	SELECTOR_ENGINE_CSS   = "css"
)

/*
ActionOptions is a structure which encapsulates the optional configuration values accepted by
action methods. This replaces raw millisecond integers and magic sentinel values with a single,
explicit options object that mirrors the plain options structs used by Playwright itself.
*/
type ActionOptions struct {
	// Timeout bounds the action to an explicit duration. The zero value applies the
	// instance default for the action being performed.
	Timeout time.Duration

	// InfiniteTimeout removes the timeout bound entirely, allowing the action to wait
	// indefinitely. When set, this field takes precedence over Timeout.
	InfiniteTimeout bool
}

/*
resolveTimeoutInMilliseconds is a function which reduces the supplied options to the effective
timeout in milliseconds and an unlimited-wait flag. This maps the resolved configuration to
Playwright's float64 expectation, where 0 represents an infinite timeout.

Example:

	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(3*time.Second, options)
*/
func resolveTimeoutInMilliseconds(defaultTimeout time.Duration, options []ActionOptions) (float64, bool) {
	if len(options) == 0 {
		return float64(defaultTimeout.Milliseconds()), false
	}
	config := options[0]
	if config.InfiniteTimeout {
		return 0, true
	}
	if config.Timeout > 0 {
		return float64(config.Timeout.Milliseconds()), false
	}
	return float64(defaultTimeout.Milliseconds()), false
}

/*
applySelectorTimeout is a function which folds a Selector's own Timeout override into the
caller-supplied ActionOptions whenever the caller did not already request an explicit Timeout or
InfiniteTimeout. Every selector-accepting action method calls this first, before doing anything
else (including passing options along to nested calls), so that the effective timeout is settled
once and stays consistent across every downstream resolveTimeoutInMilliseconds call — including
ones several layers deep that no longer have access to the original Selector.

Precedence, most specific first: an explicit per-call ActionOptions.Timeout/InfiniteTimeout, then
selector.Timeout, then the instance default.

Example:

	options = applySelectorTimeout(selector, options)
*/
func applySelectorTimeout(selector Selector, options []ActionOptions) []ActionOptions {
	var config ActionOptions
	if len(options) > 0 {
		config = options[0]
	}
	if !config.InfiniteTimeout && config.Timeout <= 0 && selector.Timeout > 0 {
		config.Timeout = selector.Timeout
	}
	return []ActionOptions{config}
}

/*
Selector is a structure which pairs a Playwright-style selector string with a human-readable description of the
element it targets. Every action method that locates an element by selector accepts one of these instead of a bare
string, so that reusable, named selectors (declared once, referenced everywhere) automatically carry a meaningful
label into logs, screenshots, and error messages instead of a raw CSS/XPath expression. Named Selector rather than
Locator since several methods already take a resolved playwright.Locator alongside it, and reusing "Locator" for
both would make those signatures ambiguous.

Example:

	var OkButtonSelector = elementary.Selector{Value: "//button[1]", Description: "OK button"}
	var SlowWidgetSelector = elementary.Selector{Value: "#slow-widget", Timeout: 15 * time.Second}
*/
type Selector struct {
	Value       string
	Description string

	// Timeout overrides the instance's default element timeout for every action performed
	// against this selector. The zero value means "not specified," in which case the caller's
	// per-call ActionOptions.Timeout (if any) or the instance default applies instead — the same
	// zero-means-unset convention already used by ActionOptions.Timeout.
	Timeout time.Duration
}

/*
String is a method that returns the Selector's Description when set, falling back to Value otherwise. Because
Selector implements fmt.Stringer, every existing %s-formatted log and error message resolves to this automatically.
*/
func (selector Selector) String() string {
	if selector.Description != "" {
		return selector.Description
	}
	return selector.Value
}

/*
pageType is a structure which binds an active Playwright Page instance with an identifier alias. This facilitates
the association of high-level logical names to discrete browser tabs or windows, simplifying execution dispatching.
*/
type pageType struct {
	instance playwright.Page
	alias    string
}

/*
contextType is a structure which orchestrates a browser context, its associated page tree, and popup/redirect policies. This
maintains runtime isolation across cookie store environments, storage scopes, and session contexts.
*/
type contextType struct {
	instance                playwright.BrowserContext
	pageInstances           []pageType
	alias                   string
	popupBlockingEnabled    bool
	redirectBlockingEnabled bool
	allowedNavigationHost   string
}

/*
BrowserOptions is a structure which encapsulates customization parameters for launching browser contexts. This enables
runtime override of client metadata such as user agent headers to approximate diverse device families.
*/
var (
	// ErrWindowsRestartRequired is returned when an automatic driver repair occurs on Windows, requiring the application to be restarted.
	ErrWindowsRestartRequired = errors.New("the simulation engine has been reset, but requires a program restart on Windows to unlock binaries")
)

type BrowserOptions struct {
	UserAgent string

	// StealthPatch opts into launching against a separate, patched copy of the Chromium
	// driver built with Patchright's stealth patches. The original driver installation is
	// never modified, so Instances with and without StealthPatch can run side by side against
	// the same cache. Experimental: Chromium only, requires network access during Initialize
	// to fetch the patch and build the copy on first use. See stealthpatch.go.
	StealthPatch bool

	Channel string

	// Args overrides the command-line arguments passed to the browser at launch, per
	// Playwright's Args launch option. When nil, Chromium launches with Elementary's defaults
	// ("--mute-audio", "--remote-debugging-port=9222") and Firefox launches with none. Setting
	// Args replaces the list entirely rather than appending to it, so callers who want to keep
	// the Chromium defaults alongside their own flags must include them explicitly.
	Args []string
}

/*
ScreenshotOptions is a structure which bundles all automated screenshot behavior into a single configuration unit,
covering where captures are saved, which action stages trigger a capture, and whether captures are collected into a
TAR archive rather than written as loose files.
*/
type ScreenshotOptions struct {
	// Directory is the target directory where automated task screenshots are saved. Automated
	// capture (BeforeAction, AfterAction, OnFailure) is disabled entirely while this is empty,
	// regardless of those flags. When ArchiveFilename is also set, this is where the archive
	// itself is created rather than where loose screenshot files are written.
	Directory string

	// BeforeAction enables automatic screenshot capture right before any state-changing action.
	BeforeAction bool

	// AfterAction enables automatic screenshot capture right after any state-changing action.
	AfterAction bool

	// OnFailure enables automatic screenshot capture whenever an action fails.
	OnFailure bool

	// ArchiveFilename, when set, routes every saved screenshot into a TAR archive of this name
	// created inside Directory, instead of writing loose PNG files. Empty disables archiving.
	//
	// Each screenshot is appended directly to the working ".tar" file as it is captured, so it
	// survives an ungraceful shutdown (crash, panic, killed process) intact. Unlike a ZIP archive,
	// TAR has no central index that must be finalized for prior entries to remain readable. On a
	// graceful shutdown (SetScreenshotOptions called again, or the instance closed), the archive is
	// gzip-compressed into "<ArchiveFilename>.gz" and the intermediate plain TAR file is removed.
	ArchiveFilename string
}

/*
Instance is a structure which defines the primary orchestrator of the web automation engine. This maintains global
state parameters including process pointers, active context registries, navigation limits, and current page scopes.
*/
type Instance struct {
	stateMutex               sync.RWMutex
	playwrightInstance       *playwright.Playwright
	browserInstance          playwright.Browser
	contextInstances         []contextType
	currentContextIndex      int
	currentPageIndex         int
	currentPage              playwright.Page
	defaultElementTimeout    time.Duration
	defaultNavigationTimeout time.Duration
	defaultPlaywrightTimeout time.Duration
	defaultSelectorEngine    string
	browserOptions           BrowserOptions
	screenshotOptions        ScreenshotOptions
	screenshotIndex          atomic.Int32
	screenshotArchiver       archiver.Archiver
	archiverMutex            sync.Mutex

	// Parameters for reinitialization
	defaultContextAlias string
	defaultPageAlias    string
	browserType         string
	width               int
	height              int
	isVisible           bool
}
