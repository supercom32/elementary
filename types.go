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
CookieOptions is a structure which encapsulates the optional configuration values accepted by AddCookie, letting
callers control a cookie's path, security flags, and lifetime instead of always getting a fixed set of defaults.
The zero value creates a session cookie scoped to path "/" with no security flags set.
*/
type CookieOptions struct {
	// Path is the cookie's path scope. Empty defaults to "/".
	Path string

	// Secure marks the cookie Secure, restricting it to HTTPS connections. Many sites require
	// this for session cookies served over HTTPS.
	Secure bool

	// HTTPOnly marks the cookie HttpOnly, making it inaccessible to page JavaScript.
	HTTPOnly bool

	// SameSite is the cookie's SameSite attribute: "Strict", "Lax", or "None". Empty leaves the
	// attribute unset, letting the browser apply its own default.
	SameSite string

	// ExpiresIn is how long from now the cookie should expire. The zero value creates a session
	// cookie that expires when the browser context closes.
	ExpiresIn time.Duration
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

	// ErrElementNotFound is wrapped by every action that must act on an element (ClickElement, FillElement,
	// HoverOverElement, HoverAndClickElement) when no element matches its selector within the timeout. Test for it
	// with errors.Is.
	ErrElementNotFound = errors.New("element not found")
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

	// Proxy routes all of the browser's traffic through a proxy server, per Playwright's proxy launch option. Nil
	// connects directly. It applies to every context the Instance creates.
	Proxy *ProxyOptions

	// StorageStatePath names a file previously written by SaveStorageState. When set, each context created with
	// these options starts with that file's cookies and local storage, restoring a saved login session. Empty
	// starts every context with a clean session.
	StorageStatePath string

	// Args overrides the command-line arguments passed to the browser at launch, per
	// Playwright's Args launch option. When nil, Chromium launches with Elementary's default
	// ("--mute-audio") and Firefox launches with none. Setting Args replaces the list entirely
	// rather than appending to it, so callers who want to keep the Chromium default alongside
	// their own flags must include it explicitly.
	Args []string
}

/*
ProxyOptions is a structure which describes the proxy server BrowserOptions.Proxy routes browser traffic through.
*/
type ProxyOptions struct {
	// Server is the proxy address, for example "http://myproxy.com:3128" or "socks5://myproxy.com:3128". A form
	// without a scheme such as "myproxy.com:3128" is treated as an HTTP proxy. Required.
	Server string

	// Bypass is an optional comma-separated list of domains that connect directly instead of through the proxy, for
	// example ".example.com, internal.local".
	Bypass string

	// Username and Password are optional credentials for an HTTP proxy that requires authentication.
	Username string
	Password string
}

/*
DialogOptions is a structure which decides how Elementary answers JavaScript dialogs (alert, confirm, prompt, and
beforeunload) on every page the Instance controls. The zero value dismisses every dialog, matching Playwright's own
default.
*/
type DialogOptions struct {
	// Accept answers dialogs with OK when true and with Cancel when false. Accepting a beforeunload dialog lets the
	// page be left; dismissing it keeps the page open.
	Accept bool

	// PromptText is the answer entered into prompt() dialogs when Accept is true. It is ignored for other dialog
	// types and when dismissing.
	PromptText string
}

/*
DialogInfo is a structure which records one JavaScript dialog Elementary answered, as returned by GetDialogs.
*/
type DialogInfo struct {
	// ContextAlias is the alias of the browser context whose page showed the dialog.
	ContextAlias string

	// Type is the dialog kind: "alert", "confirm", "prompt", or "beforeunload".
	Type string

	// Message is the text the dialog displayed.
	Message string

	// DefaultValue is the pre-filled answer of a prompt() dialog, and empty for other types.
	DefaultValue string

	// PageURL is the address of the page that showed the dialog, when known.
	PageURL string

	// Accepted reports whether the dialog was answered with OK rather than Cancel.
	Accepted bool
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

	// dialogOptions and dialogHistory back SetDialogOptions and GetDialogs. They are guarded by their own
	// dialogMutex rather than stateMutex because dialog handlers run while a page call is blocked waiting for the
	// dialog to be answered, and that call may already hold stateMutex (InjectJavaScript does).
	dialogMutex   sync.Mutex
	dialogOptions DialogOptions
	dialogHistory []DialogInfo

	// minimizeWatchers holds one entry per window currently enforced by MinimizeWindow(true), keyed by that
	// window's Chrome DevTools Protocol windowId. Each value is closed by stopMinimizeWatcherUnlocked or
	// stopAllMinimizeWatchersUnlocked to signal that window's watcher goroutine to exit. Guarded by stateMutex.
	minimizeWatchers map[any]chan struct{}

	// framePage and framePath record the frame chosen with SwitchToFrame: framePath holds one resolved frame
	// selector per nesting level, and applies only while framePage is still the current page, so switching to a
	// different page or context automatically returns lookups to that page's main frame. Guarded by stateMutex.
	framePage playwright.Page
	framePath []string

	// Parameters for reinitialization
	defaultContextAlias string
	defaultPageAlias    string
	browserType         string
	width               int
	height              int
	isVisible           bool
}
