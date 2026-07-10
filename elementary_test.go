package elementary_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/supercom32/elementary"
)

const fingerprintScanBaseURL = "https://fingerprint-scan.com"

// Selectors reused across multiple tests below, declared once so a markup change only needs one edit.
var (
	fingerprintHashSelector   = elementary.Selector{Value: "css=#fingerprintHash", Description: "fingerprint hash"}
	navLinksContainerSelector = elementary.Selector{Value: "css=.nav-links", Description: "nav links container"}
	navLinkItemsSelector      = elementary.Selector{Value: "css=.nav-links a", Description: "nav link items"}
	nonExistentSelector       = elementary.Selector{Value: "css=#this-selector-does-not-exist-on-the-page", Description: "guaranteed non-existent element"}
)

/*
newStealthAgent is a test helper which initializes an elementary.Instance with StealthPatch
enabled, using the normal cached driver/browser install location (no PLAYWRIGHT_DRIVER_PATH /
PLAYWRIGHT_BROWSERS_PATH redirection). Stealth mode is used as the default for this entire test
suite rather than as a special case: it is a strict superset of plain Chromium automation, so
testing under stealth exercises everything plain mode does plus the driver-patching path. A clean,
from-scratch driver/browser install is deliberately NOT forced here — that behavior is reserved for
tests that specifically verify installation itself (TestStealthPatchLifecycle,
TestRepairIsolatesPatchedAndUnpatchedDrivers in stealthpatch_test.go), so every test in this file
launches quickly against whatever is already cached on disk.

Every test in this file calls this helper independently with its own Instance and its own
defer Close() — none of them share a browser session, so each test's outcome depends only on
itself, not on execution order or another test's cleanup.
*/
func newStealthAgent(t *testing.T, defaultContextAlias string, defaultPageAlias string, width int, height int) *elementary.Instance {
	t.Helper()
	var agent elementary.Instance
	err := agent.Initialize(defaultContextAlias, defaultPageAlias, "chromium", width, height, false, &elementary.BrowserOptions{StealthPatch: true})
	if err != nil {
		t.Fatalf("Failed to initialize stealth-patched agent: %v", err)
	}
	return &agent
}

/*
fingerprintTableValue is a test helper which reads the property-value cell corresponding to a
named row in fingerprint-scan.com's results table. The site renders results as a two-column
table (`td.property-name`, `td.property-value` pairs within a shared `tr`), so this uses
Playwright's `:has()`/`:text-is()` selector extensions to look up a value by its label rather
than depending on row ordering or index.
*/
func fingerprintTableValue(t *testing.T, agent *elementary.Instance, propertyName string) string {
	t.Helper()
	selector := elementary.Selector{
		Value:       fmt.Sprintf(`css=tr:has(td.property-name:text-is("%s")) td.property-value`, propertyName),
		Description: fmt.Sprintf("fingerprint table row for %q", propertyName),
	}
	text, err := agent.GetElementText(selector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Failed to read fingerprint table value for %q: %v", propertyName, err)
	}
	return text
}

/*
TestFingerprintScanStealthEvadesAutomationDetection is a test which verifies, against the real
https://fingerprint-scan.com site, that the CDP/webdriver-level anti-automation signals
StealthPatch exists to defeat all read as undetected. This deliberately does not assert anything
about the User-Agent string — plain headless Chromium self-identifies as "HeadlessChrome" in its
UA regardless of CDP-level patching, which is a separate concern StealthPatch does not claim to
address.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Is Playwright, WebDriver, CDP Check, Is Selenium Chrome, Has Default Chrome Extension,
	    Canvas Modified, and Iframe Overridden all read "false".
*/
func TestFingerprintScanStealthEvadesAutomationDetection(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to %s: %v", fingerprintScanBaseURL, err)
	}

	checks := map[string]string{
		"Is Playwright":                "false",
		"WebDriver":                    "false",
		"CDP Check":                    "false",
		"Is Selenium Chrome":           "false",
		"Has Default Chrome Extension": "false",
		"Canvas Modified":              "false",
		"Iframe Overridden":            "false",
	}
	for property, want := range checks {
		got := fingerprintTableValue(t, agent, property)
		if got != want {
			t.Errorf("Expected fingerprint table row %q to read %q under stealth mode, got %q", property, want, got)
		}
	}
}

/*
TestFingerprintScanNavigateAcrossSubpages is a test which verifies NavigateTo, GetCurrentUrlAddress,
and GetElementText work correctly across all 5 real subpages of fingerprint-scan.com.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Each subpage's URL and <h1> heading match what is actually published there.
*/
func TestFingerprintScanNavigateAcrossSubpages(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	pages := map[string]string{
		"/":                   "Browser Fingerprint Scanner",
		"/ip_address":         "IP Address Information",
		"/canvas":             "Your Canvas Fingerprint",
		"/http_headers":       "Your HTTP Headers",
		"/browser_extensions": "Your Browser Extensions",
	}
	for path, expectedHeading := range pages {
		url := fingerprintScanBaseURL + path
		if err := agent.NavigateTo(url, elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
			t.Fatalf("Failed navigating to %s: %v", url, err)
		}
		currentURL := agent.GetCurrentUrlAddress()
		if !strings.Contains(currentURL, path) {
			t.Errorf("Expected current URL to contain %q, got %q", path, currentURL)
		}
		heading, err := agent.GetElementText(elementary.Selector{Value: "css=h1", Description: "page heading"}, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
		if err != nil {
			t.Fatalf("Failed reading h1 on %s: %v", url, err)
		}
		if heading != expectedHeading {
			t.Errorf("Expected h1 %q on %s, got %q", expectedHeading, url, heading)
		}
	}
}

/*
TestFingerprintScanNavigateBack is a test which verifies NavigateBack returns to the previous page
in a real browsing session.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    After navigating root -> /ip_address -> back, the current URL is no longer /ip_address.
*/
func TestFingerprintScanNavigateBack(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	if err := agent.NavigateTo(fingerprintScanBaseURL+"/ip_address", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to /ip_address: %v", err)
	}
	agent.NavigateBack()
	if err := agent.WaitForPageToBeReady(); err != nil {
		t.Fatalf("Failed waiting for page ready after NavigateBack: %v", err)
	}
	currentURL := agent.GetCurrentUrlAddress()
	if strings.Contains(currentURL, "/ip_address") {
		t.Errorf("Expected NavigateBack to leave /ip_address, but URL is still %q", currentURL)
	}
}

/*
TestFingerprintScanClickAndWaitForPageToChange is a test which verifies ClickElement followed by
WaitForPageToChange correctly observes a real client-driven navigation. This is the direct
regression test for WaitForPageToChange previously ignoring its own continueIfUrlContains
parameter entirely.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Clicking the Canvas Fingerprint nav link settles the URL on /canvas.
*/
func TestFingerprintScanClickAndWaitForPageToChange(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	canvasNavLinkSelector := elementary.Selector{Value: `css=.nav-links a[href="/canvas"]`, Description: "Canvas Fingerprint nav link"}
	if err := agent.ClickElement(canvasNavLinkSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatalf("Failed clicking Canvas Fingerprint nav link: %v", err)
	}
	agent.WaitForPageToChange("/canvas", elementary.ActionOptions{Timeout: 10 * time.Second})
	currentURL := agent.GetCurrentUrlAddress()
	if !strings.Contains(currentURL, "/canvas") {
		t.Errorf("Expected WaitForPageToChange to observe the URL settle on /canvas, got %q", currentURL)
	}
}

/*
TestFingerprintScanGetElementTextAndAttribute is a test which verifies GetElementText and
GetElementAttribute against real page content.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    The fingerprint hash text starts with "Fingerprint ID: " and the IP Address nav link's
	    href reads "/ip_address".
*/
func TestFingerprintScanGetElementTextAndAttribute(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}

	hashText, err := agent.GetElementText(fingerprintHashSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Failed reading fingerprint hash: %v", err)
	}
	if !strings.HasPrefix(hashText, "Fingerprint ID: ") {
		t.Errorf("Expected fingerprint hash text to start with 'Fingerprint ID: ', got %q", hashText)
	}

	ipAddressNavLinkSelector := elementary.Selector{Value: `css=.nav-links a[href="/ip_address"]`, Description: "IP Address nav link"}
	href, err := agent.GetElementAttribute("href", ipAddressNavLinkSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Failed reading nav link href: %v", err)
	}
	if href != "/ip_address" {
		t.Errorf("Expected nav link href %q, got %q", "/ip_address", href)
	}
}

/*
TestFingerprintScanGetElementsAndNumberOfElements is a test which verifies GetElements and
GetNumberOfElements agree with each other against the real 5-link sidebar navigation.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Both report 5 matching nav links.
*/
func TestFingerprintScanGetElementsAndNumberOfElements(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}

	elements, err := agent.GetElements(navLinkItemsSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GetElements failed: %v", err)
	}
	count, err := agent.GetNumberOfElements(navLinkItemsSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GetNumberOfElements failed: %v", err)
	}
	if len(elements) != count {
		t.Errorf("Expected GetElements length (%d) to match GetNumberOfElements (%d)", len(elements), count)
	}
	if count != 5 {
		t.Errorf("Expected 5 sidebar nav links, got %d", count)
	}
}

/*
TestFingerprintScanWaitForElementCountAboveOne is a test which verifies WaitForElementCount can
observe a count greater than 1. This is the direct regression test: WaitForElementCount previously
resolved through GetElement's .First()-scoped locator, capping every observable count at 1
regardless of how many elements actually matched.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    WaitForElementCount observes exactly 5 nav links without timing out.
*/
func TestFingerprintScanWaitForElementCountAboveOne(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	if err := agent.WaitForElementCount(navLinkItemsSelector, nil, 5, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Errorf("Expected WaitForElementCount to observe 5 nav links, got error: %v", err)
	}
}

/*
TestFingerprintScanGetElementCountWithTimeout is a test which verifies GetElementCountWithTimeout
against a raw, un-narrowed Playwright locator obtained directly via GetCurrentPage.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Reports 5 matching nav links.
*/
func TestFingerprintScanGetElementCountWithTimeout(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	rawLocator := agent.GetCurrentPage().Locator(".nav-links a")
	count, err := agent.GetElementCountWithTimeout(rawLocator, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GetElementCountWithTimeout failed: %v", err)
	}
	if count != 5 {
		t.Errorf("Expected 5 nav links via GetElementCountWithTimeout, got %d", count)
	}
}

/*
TestFingerprintScanIsElementVisibleAndExist is a test which verifies IsElementExist and
IsElementVisible correctly report both true for a real element and false for a selector
guaranteed not to exist on the page. IsElementExist/IsElementVisible are deliberately instant,
non-waiting snapshot checks (unlike GetElement/WaitForElementExistenceStatus, which actively poll
until a timeout), so WaitForElementExistenceStatus is used first to guarantee the fingerprint hash
has actually been client-rendered before taking the snapshot — fingerprint-scan.com computes and
paints it via JS after the initial page load settles, so checking instantly after NavigateTo
returns is a real race against that rendering, not a library bug.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    #fingerprintHash reports exist=true, visible=true; a bogus selector reports both false.
*/
func TestFingerprintScanIsElementVisibleAndExist(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	if err := agent.WaitForElementExistenceStatus(fingerprintHashSelector, nil, true, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatalf("Expected #fingerprintHash to eventually render: %v", err)
	}
	if !agent.IsElementExist(fingerprintHashSelector, nil) {
		t.Error("Expected #fingerprintHash to exist")
	}
	if !agent.IsElementVisible(fingerprintHashSelector, nil) {
		t.Error("Expected #fingerprintHash to be visible")
	}
	if agent.IsElementExist(nonExistentSelector, nil) {
		t.Error("Expected a bogus selector to not exist")
	}
	if agent.IsElementVisible(nonExistentSelector, nil) {
		t.Error("Expected a bogus selector to not be visible")
	}
}

/*
TestFingerprintScanGetVisibleElement is a test which verifies GetVisibleElement resolves a real,
visible locator.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    The resolved locator reports IsVisible() == true.
*/
func TestFingerprintScanGetVisibleElement(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	locator, err := agent.GetVisibleElement(fingerprintHashSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GetVisibleElement failed: %v", err)
	}
	isVisible, err := locator.IsVisible()
	if err != nil {
		t.Fatalf("Failed checking visibility of resolved locator: %v", err)
	}
	if !isVisible {
		t.Error("Expected GetVisibleElement to return a visible locator")
	}
}

/*
TestFingerprintScanWaitForElementExistenceAndVisibleStatus is a test which verifies
WaitForElementExistenceStatus and WaitForElementVisibleStatus, including
WaitForElementVisibleStatus's parent-scoping behavior: a non-nil element argument scopes the
lookup to search *within* that element, using xPath as the child selector, rather than describing
the element itself.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    #fingerprintHash reaches exist=true and visible=true; #fingerprintTable is found nested
	    inside its parent container and also reaches visible=true.
*/
func TestFingerprintScanWaitForElementExistenceAndVisibleStatus(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	if err := agent.WaitForElementExistenceStatus(fingerprintHashSelector, nil, true, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Errorf("Expected #fingerprintHash to reach exist=true: %v", err)
	}

	// element == nil searches the whole page directly by selector.
	if err := agent.WaitForElementVisibleStatus(fingerprintHashSelector, nil, true, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Errorf("Expected #fingerprintHash to reach visible=true: %v", err)
	}

	// A non-nil element scopes the lookup to search within it.
	parentContainerSelector := elementary.Selector{Value: "css=#fingerprint-info-container", Description: "fingerprint info container"}
	parentLocator, err := agent.GetElement(parentContainerSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("GetElement for parent container failed: %v", err)
	}
	parentHandle, err := parentLocator.ElementHandle()
	if err != nil {
		t.Fatalf("Failed resolving parent element handle: %v", err)
	}
	fingerprintTableSelector := elementary.Selector{Value: "css=#fingerprintTable", Description: "fingerprint table"}
	if err := agent.WaitForElementVisibleStatus(fingerprintTableSelector, parentHandle, true, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Errorf("Expected #fingerprintTable nested inside the parent container to reach visible=true: %v", err)
	}
}

/*
TestFingerprintScanHamburgerMenuTogglesNavLinksClass is a test which verifies ClickElement and
GetElementAttribute together by clicking the real mobile hamburger menu and confirming the
nav-links class list actually changes as a result. The default context is launched narrow (with a
small base width to stay under any reasonable mobile breakpoint even with NewContext's viewport
jitter applied) since the hamburger menu only does anything at narrow viewport widths.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    The .nav-links class attribute differs before and after clicking .hamburger.
*/
func TestFingerprintScanHamburgerMenuTogglesNavLinksClass(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 320, 700)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}

	classBefore, err := agent.GetElementAttribute("class", navLinksContainerSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Failed reading nav-links class before click: %v", err)
	}
	hamburgerSelector := elementary.Selector{Value: "css=.hamburger", Description: "mobile hamburger menu"}
	if err := agent.ClickElement(hamburgerSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatalf("Failed clicking hamburger menu: %v", err)
	}
	classAfter, err := agent.GetElementAttribute("class", navLinksContainerSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("Failed reading nav-links class after click: %v", err)
	}
	if classBefore == classAfter {
		t.Errorf("Expected clicking the hamburger menu to change the nav-links class list, stayed %q", classBefore)
	}
}

/*
TestFingerprintScanMultiContextIsolation is a test which verifies two browser contexts within a
single Instance stay independently navigable, and that closing one does not disturb the other.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Each context's URL reflects only its own navigation; the main context survives closing
	    the second one.
*/
func TestFingerprintScanMultiContextIsolation(t *testing.T) {
	agent := newStealthAgent(t, "main-context", "main-page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating main context to root: %v", err)
	}

	if err := agent.NewContext("second-context", 1280, 900, nil); err != nil {
		t.Fatalf("Failed creating second context: %v", err)
	}
	if err := agent.NewPage("second-page"); err != nil {
		t.Fatalf("Failed creating page in second context: %v", err)
	}
	if err := agent.NavigateTo(fingerprintScanBaseURL+"/canvas", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating second context: %v", err)
	}
	secondContextURL := agent.GetCurrentUrlAddress()

	if err := agent.SwitchContext("main-context"); err != nil {
		t.Fatalf("Failed switching to main context: %v", err)
	}
	if !agent.SwitchPage("main-context", "main-page") {
		t.Fatalf("Failed switching to main page")
	}
	mainContextURL := agent.GetCurrentUrlAddress()

	if mainContextURL == secondContextURL {
		t.Errorf("Expected independent context URLs, both read %q", mainContextURL)
	}
	if !strings.Contains(secondContextURL, "/canvas") {
		t.Errorf("Expected second context to still be on /canvas, got %q", secondContextURL)
	}

	// Closing the second context should not disturb the main one.
	agent.CloseContext("second-context")
	if err := agent.SwitchContext("main-context"); err != nil {
		t.Fatalf("Expected main context to remain usable after closing second-context: %v", err)
	}
}

/*
TestFingerprintScanSwitchToSpawnedPage is a test which verifies GetArrayOfUntrackedPageIndexes and
SwitchToSpawnedPage correctly detect and focus a real, newly-opened tab. This is the direct
regression test for the deadlock that previously made SwitchToSpawnedPage hang forever whenever it
actually found a spawned page to switch to (it took stateMutex.Lock() and then called
SwitchToUntrackedPage, which locked the same non-reentrant mutex again). A real Playwright
click on a target="_blank" link is used to open the tab, rather than an injected window.open()
call, since the latter is often popup-blocked as not originating from a genuine user gesture.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    SwitchToSpawnedPage returns true and focus lands on the spawned /ip_address tab.
*/
func TestFingerprintScanSwitchToSpawnedPage(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}

	injectScript := `
		var a = document.createElement('a');
		a.href = '/ip_address';
		a.target = '_blank';
		a.id = 'elementary-test-popup-link';
		a.textContent = 'popup link';
		document.body.appendChild(a);
	`
	if err := agent.InjectJavaScript(injectScript); err != nil {
		t.Fatalf("Failed injecting popup link: %v", err)
	}
	popupLinkSelector := elementary.Selector{Value: "css=#elementary-test-popup-link", Description: "injected popup link"}
	if err := agent.ClickElement(popupLinkSelector, nil, elementary.ActionOptions{Timeout: 10 * time.Second}); err != nil {
		t.Fatalf("Failed clicking popup link: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var spawnedIndexes []int
	for time.Now().Before(deadline) {
		spawnedIndexes = agent.GetArrayOfUntrackedPageIndexes()
		if len(spawnedIndexes) > 0 {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if len(spawnedIndexes) == 0 {
		t.Fatalf("Expected a spawned page to be detected after clicking the target=_blank link")
	}

	switched := agent.SwitchToSpawnedPage()
	if !switched {
		t.Fatalf("Expected SwitchToSpawnedPage to succeed")
	}
	currentURL := agent.GetCurrentUrlAddress()
	if !strings.Contains(currentURL, "/ip_address") {
		t.Errorf("Expected focus to switch to the spawned /ip_address tab, got %q", currentURL)
	}
}

/*
TestFingerprintScanCookiesRoundTrip is a test which verifies AddCookie, SaveNetscapeCookies,
ClearCookies, LoadNetscapeCookies, and GetCookies round-trip a real cookie correctly against a
live browser context.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    The cookie is present and correctly valued after being saved, cleared, and reloaded.
*/
func TestFingerprintScanCookiesRoundTrip(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	if err := agent.AddCookie(".fingerprint-scan.com", "elementary_test_cookie", "hello_world"); err != nil {
		t.Fatalf("AddCookie failed: %v", err)
	}

	tmpDir := t.TempDir()
	cookieFile := filepath.Join(tmpDir, "cookies.txt")
	if err := agent.SaveNetscapeCookies(cookieFile); err != nil {
		t.Fatalf("SaveNetscapeCookies failed: %v", err)
	}
	if err := agent.ClearCookies(); err != nil {
		t.Fatalf("ClearCookies failed: %v", err)
	}
	cookiesAfterClear, err := agent.GetCookies()
	if err != nil {
		t.Fatalf("GetCookies after clear failed: %v", err)
	}
	for _, c := range cookiesAfterClear {
		if c.Name == "elementary_test_cookie" {
			t.Fatalf("Expected the test cookie to be gone after ClearCookies")
		}
	}

	if err := agent.LoadNetscapeCookies(cookieFile); err != nil {
		t.Fatalf("LoadNetscapeCookies failed: %v", err)
	}
	cookiesAfterLoad, err := agent.GetCookies()
	if err != nil {
		t.Fatalf("GetCookies after load failed: %v", err)
	}
	found := false
	for _, c := range cookiesAfterLoad {
		if c.Name == "elementary_test_cookie" && c.Value == "hello_world" {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected the round-tripped cookie to be present with its original value")
	}
}

/*
TestFingerprintScanSaveScreenshotAndGetScreenshotAsBinary is a test which verifies both screenshot
capture paths against a real, rendered page, decoding the returned bytes as a real PNG rather than
just checking for a nil error. This is regression coverage for the SaveScreenshot/
GetScreenshotAsBinary merge: SaveScreenshot now obtains its bytes via GetScreenshotAsBinary instead
of duplicating capture logic.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    GetScreenshotAsBinary returns a decodable, non-zero-sized PNG; SaveScreenshot writes
	    exactly one file to disk.
*/
func TestFingerprintScanSaveScreenshotAndGetScreenshotAsBinary(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	screenshotData, err := agent.GetScreenshotAsBinary()
	if err != nil {
		t.Fatalf("GetScreenshotAsBinary failed: %v", err)
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(screenshotData))
	if err != nil {
		t.Fatalf("Expected GetScreenshotAsBinary to return a valid, decodable image: %v", err)
	}
	if config.Width == 0 || config.Height == 0 {
		t.Errorf("Expected a non-zero-sized screenshot, got %dx%d", config.Width, config.Height)
	}

	tmpDir := t.TempDir()
	if err := agent.SaveScreenshot(tmpDir, "live_screenshot.png"); err != nil {
		t.Fatalf("SaveScreenshot failed: %v", err)
	}
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("Failed reading screenshot directory: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected exactly one saved screenshot file, found %d", len(entries))
	}
}

/*
TestFingerprintScanScreenshotArchiving is a test which verifies automated screenshots of real,
rendered pages can be directed to a TAR archive cleanly, gzip-finalized on a graceful shutdown.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    The resulting tar.gz contains exactly the 2 archived screenshots.
*/
func TestFingerprintScanScreenshotArchiving(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	tmpDir := t.TempDir()
	tarPath := filepath.Join(tmpDir, "live_runs.tar")
	if err := agent.SetScreenshotOptions(elementary.ScreenshotOptions{
		Directory:       tmpDir,
		ArchiveFilename: "live_runs.tar",
	}); err != nil {
		t.Fatalf("SetScreenshotOptions failed: %v", err)
	}

	for _, path := range []string{"/", "/canvas"} {
		if err := agent.NavigateTo(fingerprintScanBaseURL+path, elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
			t.Fatalf("Failed navigating to %s: %v", path, err)
		}
		if err := agent.SaveScreenshot(tmpDir, "archived.png"); err != nil {
			t.Fatalf("SaveScreenshot into archive failed: %v", err)
		}
	}

	if err := agent.SetScreenshotOptions(elementary.ScreenshotOptions{}); err != nil {
		t.Fatalf("Failed to close and detach archiver: %v", err)
	}

	file, err := os.Open(tarPath + ".gz")
	if err != nil {
		t.Fatalf("Failed opening archived tar.gz: %v", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("Failed opening gzip stream: %v", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	count := 0
	for {
		_, err := tarReader.Next()
		if err != nil {
			break
		}
		count++
	}
	if count != 2 {
		t.Errorf("Expected 2 archived screenshots, found %d", count)
	}
}

/*
TestFingerprintScanRestartPreservesCustomTimeouts is a test which verifies Restart preserves
custom timeouts set via SetTimeout, SetNavigationTimeout, and SetPlaywrightTimeout. This is the
direct regression test for Restart previously silently discarding these back to hardcoded
defaults, despite its doc comment's "identical startup properties" contract.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    All three timeouts read back exactly as set after Restart, and the instance remains
	    navigable afterward.
*/
func TestFingerprintScanRestartPreservesCustomTimeouts(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	agent.SetTimeout(12345 * time.Millisecond)
	agent.SetNavigationTimeout(54321 * time.Millisecond)
	agent.SetPlaywrightTimeout(9999 * time.Millisecond)

	if err := agent.Restart(); err != nil {
		t.Fatalf("Restart failed: %v", err)
	}

	if got := agent.GetTimeout(); got != 12345*time.Millisecond {
		t.Errorf("Expected element timeout to survive Restart as 12345ms, got %v", got)
	}
	if got := agent.GetNavigationTimeout(); got != 54321*time.Millisecond {
		t.Errorf("Expected navigation timeout to survive Restart as 54321ms, got %v", got)
	}
	if got := agent.GetPlaywrightTimeout(); got != 9999*time.Millisecond {
		t.Errorf("Expected playwright timeout to survive Restart as 9999ms, got %v", got)
	}

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Expected the instance to remain navigable after Restart: %v", err)
	}
}

/*
TestFingerprintScanConcurrentReadOnlyCallsUnderRace is a test which stress-tests the stateMutex
locking fixes (the SwitchToSpawnedPage deadlock, the Terminate/Shutdown races) under real
concurrent load against a live session, rather than relying on single-threaded call ordering to
happen to avoid the race. Run with `go test -race` for this to actually catch anything.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com; run under `go test -race`.

	Expected Outputs:
	    No data races reported, and navigation completes normally while concurrent reads run.
*/
func TestFingerprintScanConcurrentReadOnlyCallsUnderRace(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = agent.GetCurrentUrlAddress()
					_, _ = agent.GetCookies()
					_ = agent.IsElementVisible(fingerprintHashSelector, nil)
				}
			}
		}()
	}
	for _, path := range []string{"/", "/canvas", "/"} {
		if err := agent.NavigateTo(fingerprintScanBaseURL+path, elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
			t.Fatalf("Failed navigating during concurrency test: %v", err)
		}
	}
	close(stop)
	wg.Wait()
}

/*
TestFingerprintScanNonExistentSelectorHandling is a test which verifies element-lookup methods
behave correctly (empty/zero/false, not a panic or hang) for a selector guaranteed not to exist on
the page.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    GetElementText returns "" with no error; GetNumberOfElements returns 0 with no error;
	    IsElementExist returns false.
*/
func TestFingerprintScanNonExistentSelectorHandling(t *testing.T) {
	agent := newStealthAgent(t, "context", "page", 1280, 900)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to root: %v", err)
	}
	bogusSelector := elementary.Selector{Value: "css=#definitely-does-not-exist-xyz123", Description: "guaranteed non-existent element"}

	text, err := agent.GetElementText(bogusSelector, nil, elementary.ActionOptions{Timeout: 2 * time.Second})
	if err != nil {
		t.Errorf("Expected GetElementText on a missing selector to return an empty result, not an error: %v", err)
	}
	if text != "" {
		t.Errorf("Expected empty text for a missing selector, got %q", text)
	}

	count, err := agent.GetNumberOfElements(bogusSelector, nil, elementary.ActionOptions{Timeout: 2 * time.Second})
	if err != nil {
		t.Errorf("Expected GetNumberOfElements on a missing selector to return 0, not an error: %v", err)
	}
	if count != 0 {
		t.Errorf("Expected 0 elements for a missing selector, got %d", count)
	}

	if agent.IsElementExist(bogusSelector, nil) {
		t.Error("Expected a bogus selector to not exist")
	}
}

/*
TestStealthPatchRequiresChromium is a test which verifies that requesting StealthPatch with a
non-Chromium browser fails fast with a clear error, before any network or driver work is
attempted. This needs no live site and no network access.

Example:

	Expected Inputs:
	    None.

	Expected Outputs:
	    Initialize returns a non-nil error immediately.
*/
func TestStealthPatchRequiresChromium(t *testing.T) {
	var agent elementary.Instance
	err := agent.Initialize("firefox-stealth-context", "firefox-stealth-page", "firefox", 1280, 720, false, &elementary.BrowserOptions{StealthPatch: true})
	if err == nil {
		t.Error("Expected requesting StealthPatch with Firefox to fail, but Initialize succeeded")
		_ = agent.Close()
	}
}

/*
TestProductionReadyFeatures is a test which verifies production-grade robustness features under
stealth mode against a real page: SwitchContext returning a clean error for an unknown alias, and
cookie addition/removal working against a live browser context.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    Switching to a non-existent context returns a non-nil error, and adding/clearing cookies
	    executes successfully.
*/
func TestProductionReadyFeatures(t *testing.T) {
	agent := newStealthAgent(t, "test-context", "test-page", 1280, 720)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to %s: %v", fingerprintScanBaseURL, err)
	}

	if err := agent.SwitchContext("non-existent-context-alias"); err == nil {
		t.Error("Expected SwitchContext to return an error for a non-existent context, but it returned nil")
	}

	if err := agent.AddCookie(".fingerprint-scan.com", "test_cookie", "cookie_value"); err != nil {
		t.Errorf("AddCookie failed: %v", err)
	}

	if err := agent.ClearCookies(); err != nil {
		t.Errorf("ClearCookies failed: %v", err)
	}
}

/*
TestScreenshotArchiving is a test which verifies that automated screenshots of a real, rendered
page can be directed to a TAR archive cleanly, gzip-finalized on a graceful shutdown.

Example:

	Expected Inputs:
	    Network access to fingerprint-scan.com.

	Expected Outputs:
	    No errors are returned and a valid screenshot file is added into the resulting tar.gz.
*/
func TestScreenshotArchiving(t *testing.T) {
	agent := newStealthAgent(t, "test-context", "test-page", 1280, 720)
	defer agent.Close()

	if err := agent.NavigateTo(fingerprintScanBaseURL+"/", elementary.ActionOptions{Timeout: 20 * time.Second}); err != nil {
		t.Fatalf("Failed navigating to %s: %v", fingerprintScanBaseURL, err)
	}

	tmpDir, err := os.MkdirTemp("", "archive_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	tarPath := filepath.Join(tmpDir, "runs.tar")
	err = agent.SetScreenshotOptions(elementary.ScreenshotOptions{
		Directory:       tmpDir,
		OnFailure:       true,
		ArchiveFilename: "runs.tar",
	})
	if err != nil {
		t.Fatalf("SetScreenshotOptions failed: %v", err)
	}

	err = agent.SaveScreenshot(tmpDir, "manual_archived_view.png")
	if err != nil {
		t.Fatalf("SaveScreenshot failed: %v", err)
	}

	err = agent.SetScreenshotOptions(elementary.ScreenshotOptions{})
	if err != nil {
		t.Fatalf("Failed to cleanly close and detach archiver: %v", err)
	}

	_, err = os.Stat(tarPath + ".gz")
	if err != nil {
		t.Fatalf("expected tar.gz archive to exist on filesystem, but stat failed: %v", err)
	}
}
