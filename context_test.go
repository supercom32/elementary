package elementary_test

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary"
)

/*
TestNewContextUsesSuppliedUserAgent is a test which allows you to verify that NewContext honors the UserAgent of a
non-nil BrowserOptions. The input is BrowserOptions{UserAgent: "ElementaryParity/1.0"}, and navigator.userAgent on a
page in that context is expected to equal "ElementaryParity/1.0".
*/
func TestNewContextUsesSuppliedUserAgent(t *testing.T) {
	agent := newPlainAgent(t)
	const customUserAgent = "ElementaryParity/1.0"

	if err := agent.NewContext("custom", 0, 0, &elementary.BrowserOptions{UserAgent: customUserAgent}); err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := agent.NewPage("custom-tab"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}

	if userAgent := evaluateString(t, agent, "navigator.userAgent"); userAgent != customUserAgent {
		t.Errorf("Expected user agent %q, got %q", customUserAgent, userAgent)
	}
}

/*
TestResetContextReturnsCreationError is a test which allows you to verify that ResetContext("", 0, 0, nil) returns
NewContext's empty-alias error rather than silently succeeding.
*/
func TestResetContextReturnsCreationError(t *testing.T) {
	agent := newPlainAgent(t)

	err := agent.ResetContext("", 0, 0, nil)
	if err == nil || !strings.Contains(err.Error(), "alias name must not be empty") {
		t.Errorf("Expected an empty-alias error, got %v", err)
	}
}

/*
TestEvaluateJavaScriptReturnsResults is a test which allows you to verify that EvaluateJavaScript returns 4 for
"2 + 2", "elementary" for a string expression, and 5 for the function "([a, b]) => a + b" called with [2, 3].
*/
func TestEvaluateJavaScriptReturnsResults(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body>js</body></html>")

	if result := evaluateOrFail(t, agent, "2 + 2"); result != 4 {
		t.Errorf("Expected 4, got %v (%T)", result, result)
	}
	if result := evaluateOrFail(t, agent, "'elementary'"); result != "elementary" {
		t.Errorf("Expected \"elementary\", got %v", result)
	}
	result, err := agent.EvaluateJavaScript("([a, b]) => a + b", []int{2, 3})
	if err != nil || result != 5 {
		t.Errorf("Expected 5 from a function with arguments, got %v (err: %v)", result, err)
	}
}

/*
newWindowAgent is a test helper which initializes a plain, non-stealth headless elementary.Instance
under the supplied context and page alias. The window-management tests that use it exercise raw
Chrome DevTools Protocol window state directly, which has nothing to do with fingerprint evasion, so
StealthPatch is deliberately left off to keep these tests launching as quickly as possible.
*/
func newWindowAgent(t *testing.T, contextAlias string, pageAlias string) *elementary.Instance {
	t.Helper()
	var agent elementary.Instance
	err := agent.Initialize(contextAlias, pageAlias, "chromium", 1024, 768, false, nil)
	if err != nil {
		t.Fatalf("Failed to initialize window agent: %v", err)
	}
	return &agent
}

/*
openRawWindowSession is a test helper which opens a Chrome DevTools Protocol session against page
independently of anything MinimizeWindow or RestoreWindow does, and resolves the identifier of the
window that page belongs to. Tests use this to manipulate and observe window state directly, so that
assertions are checking the real Chrome window state rather than only elementary's own bookkeeping.
*/
func openRawWindowSession(t *testing.T, page playwright.Page) (playwright.CDPSession, any) {
	t.Helper()
	session, err := page.Context().NewCDPSession(page)
	if err != nil {
		t.Fatalf("Failed to open raw CDP session: %v", err)
	}
	result, err := session.Send("Browser.getWindowForTarget", nil)
	if err != nil {
		t.Fatalf("Failed to resolve window id: %v", err)
	}
	return session, result.(map[string]any)["windowId"]
}

/*
setRawWindowState is a test helper which sends a Chrome DevTools Protocol Browser.setWindowBounds
command directly, bypassing MinimizeWindow and RestoreWindow entirely. This is how tests simulate a
user manually maximizing or restoring a window out from under an active watcher.
*/
func setRawWindowState(t *testing.T, session playwright.CDPSession, windowID any, state string) {
	t.Helper()
	_, err := session.Send("Browser.setWindowBounds", map[string]any{
		"windowId": windowID,
		"bounds": map[string]any{
			"windowState": state,
		},
	})
	if err != nil {
		t.Fatalf("Failed to set window state to %q: %v", state, err)
	}
}

/*
readRawWindowState is a test helper which reads a window's current Chrome DevTools Protocol
windowState directly via Browser.getWindowBounds, bypassing elementary's own bookkeeping entirely.
*/
func readRawWindowState(t *testing.T, session playwright.CDPSession, windowID any) string {
	t.Helper()
	result, err := session.Send("Browser.getWindowBounds", map[string]any{"windowId": windowID})
	if err != nil {
		t.Fatalf("Failed to read window bounds: %v", err)
	}
	bounds, _ := result.(map[string]any)["bounds"].(map[string]any)
	state, _ := bounds["windowState"].(string)
	return state
}

/*
waitForRawWindowState is a test helper which polls a window's Chrome DevTools Protocol windowState
until it matches expected or timeout elapses, failing the test on timeout. Tests use this instead of
a fixed sleep so they run as fast as the watcher actually reacts, typically well under
minimizeWatcherPollInterval's 100 millisecond cadence, while still tolerating scheduling jitter.
*/
func waitForRawWindowState(t *testing.T, session playwright.CDPSession, windowID any, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastObserved string
	for {
		lastObserved = readRawWindowState(t, session, windowID)
		if lastObserved == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Timed out waiting for window state %q, last observed %q", expected, lastObserved)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

/*
waitForGoroutineCountAtMost is a test helper which polls runtime.NumGoroutine until it drops to at
most target or timeout elapses, failing the test on timeout. Tests use this to observe watcher
goroutines actually exiting, since a stopped watcher has no other externally visible signal.
*/
func waitForGoroutineCountAtMost(t *testing.T, target int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastObserved int
	for {
		lastObserved = runtime.NumGoroutine()
		if lastObserved <= target {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("Timed out waiting for goroutine count to reach %d, still at %d", target, lastObserved)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

/*
TestMinimizeWindowEnforceCorrectsManualRestore is a test which verifies that MinimizeWindow(true)
keeps a window minimized: after manually restoring the window via a raw Chrome DevTools Protocol
call, simulating a user un-minimizing it, the watcher corrects it back to minimized on its own,
without any further call into elementary.

Example:

	Expected Inputs:
	    A single headless Chromium window enforced via MinimizeWindow(true).

	Expected Outputs:
	    The window reads Chrome DevTools Protocol windowState "minimized" again within the poll
	    timeout, despite being manually set to "normal" in between.
*/
func TestMinimizeWindowEnforceCorrectsManualRestore(t *testing.T) {
	agent := newWindowAgent(t, "context", "page")
	defer agent.Close()

	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) failed: %v", err)
	}

	page := agent.GetCurrentPage()
	session, windowID := openRawWindowSession(t, page)
	defer session.Detach()

	waitForRawWindowState(t, session, windowID, "minimized", 2*time.Second)

	setRawWindowState(t, session, windowID, "normal")
	waitForRawWindowState(t, session, windowID, "minimized", 2*time.Second)
}

/*
TestMinimizeWindowEnforcesWindowsIndependently is a test which verifies that enforcing two separate
windows runs two independent watchers: manually restoring one enforced window does not disturb the
other, and each window's watcher corrects only its own window.

Example:

	Expected Inputs:
	    Two contexts, each with one page, each enforced via MinimizeWindow(true).

	Expected Outputs:
	    The two pages resolve to distinct Chrome DevTools Protocol window ids. Manually restoring
	    window B is corrected back to minimized while window A remains minimized throughout,
	    undisturbed.
*/
func TestMinimizeWindowEnforcesWindowsIndependently(t *testing.T) {
	agent := newWindowAgent(t, "context-a", "page-a")
	defer agent.Close()

	if err := agent.NewContext("context-b", 1024, 768, nil); err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := agent.NewPage("page-b"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}

	if !agent.SwitchPage("context-a", "page-a") {
		t.Fatalf("SwitchPage to context-a/page-a failed")
	}
	pageA := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window A failed: %v", err)
	}
	sessionA, windowIDA := openRawWindowSession(t, pageA)
	defer sessionA.Detach()

	if !agent.SwitchPage("context-b", "page-b") {
		t.Fatalf("SwitchPage to context-b/page-b failed")
	}
	pageB := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window B failed: %v", err)
	}
	sessionB, windowIDB := openRawWindowSession(t, pageB)
	defer sessionB.Detach()

	if windowIDA == windowIDB {
		t.Fatalf("Expected window A and window B to have distinct window ids, both got %v", windowIDA)
	}

	waitForRawWindowState(t, sessionA, windowIDA, "minimized", 2*time.Second)
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)

	setRawWindowState(t, sessionB, windowIDB, "normal")
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)

	if state := readRawWindowState(t, sessionA, windowIDA); state != "minimized" {
		t.Fatalf("Expected window A to remain undisturbed at 'minimized', got %q", state)
	}
}

/*
TestMinimizeWindowFalseStopsOnlyCurrentWindowsWatcher is a test which verifies that
MinimizeWindow(false) stops enforcement for whichever window is current at the time of the call,
without affecting a watcher already running against a different window.

Example:

	Expected Inputs:
	    Two enforced windows, A and B. MinimizeWindow(false) is called while A is current.

	Expected Outputs:
	    Manually restoring window A afterward leaves it at "normal", uncorrected. Manually restoring
	    window B is still corrected back to "minimized".
*/
func TestMinimizeWindowFalseStopsOnlyCurrentWindowsWatcher(t *testing.T) {
	agent := newWindowAgent(t, "context-a", "page-a")
	defer agent.Close()

	if err := agent.NewContext("context-b", 1024, 768, nil); err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := agent.NewPage("page-b"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}

	agent.SwitchPage("context-a", "page-a")
	pageA := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window A failed: %v", err)
	}
	sessionA, windowIDA := openRawWindowSession(t, pageA)
	defer sessionA.Detach()

	agent.SwitchPage("context-b", "page-b")
	pageB := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window B failed: %v", err)
	}
	sessionB, windowIDB := openRawWindowSession(t, pageB)
	defer sessionB.Detach()

	waitForRawWindowState(t, sessionA, windowIDA, "minimized", 2*time.Second)
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)

	agent.SwitchPage("context-a", "page-a")
	if err := agent.MinimizeWindow(false); err != nil {
		t.Fatalf("MinimizeWindow(false) on window A failed: %v", err)
	}

	setRawWindowState(t, sessionA, windowIDA, "normal")
	time.Sleep(400 * time.Millisecond)
	if state := readRawWindowState(t, sessionA, windowIDA); state != "normal" {
		t.Fatalf("Expected window A to stay 'normal' after its watcher was stopped, got %q", state)
	}

	setRawWindowState(t, sessionB, windowIDB, "normal")
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)
}

/*
TestRestoreWindowOnlyAffectsCurrentWindow is a test which verifies that RestoreWindow restores and
stops enforcement for whichever window is current at the time of the call, leaving a watcher running
against a different window untouched.

Example:

	Expected Inputs:
	    Two enforced windows, A and B. RestoreWindow is called while A is current.

	Expected Outputs:
	    Window A reads "normal" immediately and stays "normal" afterward, uncorrected. Manually
	    restoring window B is still corrected back to "minimized".
*/
func TestRestoreWindowOnlyAffectsCurrentWindow(t *testing.T) {
	agent := newWindowAgent(t, "context-a", "page-a")
	defer agent.Close()

	if err := agent.NewContext("context-b", 1024, 768, nil); err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := agent.NewPage("page-b"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}

	agent.SwitchPage("context-a", "page-a")
	pageA := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window A failed: %v", err)
	}
	sessionA, windowIDA := openRawWindowSession(t, pageA)
	defer sessionA.Detach()

	agent.SwitchPage("context-b", "page-b")
	pageB := agent.GetCurrentPage()
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window B failed: %v", err)
	}
	sessionB, windowIDB := openRawWindowSession(t, pageB)
	defer sessionB.Detach()

	waitForRawWindowState(t, sessionA, windowIDA, "minimized", 2*time.Second)
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)

	agent.SwitchPage("context-a", "page-a")
	if err := agent.RestoreWindow(); err != nil {
		t.Fatalf("RestoreWindow on window A failed: %v", err)
	}
	if state := readRawWindowState(t, sessionA, windowIDA); state != "normal" {
		t.Fatalf("Expected window A to be restored to 'normal' immediately, got %q", state)
	}
	time.Sleep(400 * time.Millisecond)
	if state := readRawWindowState(t, sessionA, windowIDA); state != "normal" {
		t.Fatalf("Expected window A to stay 'normal' after RestoreWindow, got %q", state)
	}

	setRawWindowState(t, sessionB, windowIDB, "normal")
	waitForRawWindowState(t, sessionB, windowIDB, "minimized", 2*time.Second)
}

/*
TestMinimizeWindowEnforceTwiceOnSameWindowStartsOnlyOneWatcher is a test which verifies that calling
MinimizeWindow(true) a second time against a window that is already enforced does not start a second
watcher goroutine for it.

Example:

	Expected Inputs:
	    A single window, MinimizeWindow(true) called twice in a row against it.

	Expected Outputs:
	    The live goroutine count grows by at least one after the first call, then does not grow
	    further after the second call.
*/
func TestMinimizeWindowEnforceTwiceOnSameWindowStartsOnlyOneWatcher(t *testing.T) {
	agent := newWindowAgent(t, "context", "page")
	defer agent.Close()

	baseline := runtime.NumGoroutine()

	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("First MinimizeWindow(true) failed: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	afterFirst := runtime.NumGoroutine()
	if afterFirst < baseline+1 {
		t.Fatalf("Expected goroutine count to grow by at least 1 after first enforce, went from %d to %d", baseline, afterFirst)
	}

	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("Second MinimizeWindow(true) failed: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	afterSecond := runtime.NumGoroutine()
	if afterSecond > afterFirst {
		t.Fatalf("Expected no additional watcher goroutine from a second enforce call on the same window, went from %d to %d", afterFirst, afterSecond)
	}
}

/*
TestMinimizeWindowWatcherStopsWhenItsWindowCloses is a test which verifies that a window's watcher
exits on its own once that window's only page closes, rather than continuing to poll a window that no
longer exists. In this environment, every page opens as its own distinct Chrome DevTools Protocol
window (verified separately, including for window.open() popups), so closing a window's only page
leaves the watcher with no surviving page to recover to.

Example:

	Expected Inputs:
	    A single enforced window whose only page is then closed via ClosePage.

	Expected Outputs:
	    The live goroutine count, elevated while the watcher runs, drops back to its pre-enforcement
	    baseline within the timeout after the page closes.
*/
func TestMinimizeWindowWatcherStopsWhenItsWindowCloses(t *testing.T) {
	agent := newWindowAgent(t, "context", "page")
	defer agent.Close()

	baseline := runtime.NumGoroutine()

	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) failed: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	withWatcher := runtime.NumGoroutine()
	if withWatcher < baseline+1 {
		t.Fatalf("Expected goroutine count to grow by at least 1 with the watcher running, went from %d to %d", baseline, withWatcher)
	}

	agent.ClosePage()

	waitForGoroutineCountAtMost(t, baseline, 2*time.Second)
}

/*
TestCloseStopsAllWindowWatchers is a test which verifies that closing the instance stops every
window's watcher together, not just one, regardless of how many windows were being enforced.

Example:

	Expected Inputs:
	    Two enforced windows, A and B, followed by Close.

	Expected Outputs:
	    The live goroutine count, elevated while both watchers run, drops back to its
	    pre-enforcement baseline within the timeout after Close returns.
*/
func TestCloseStopsAllWindowWatchers(t *testing.T) {
	agent := newWindowAgent(t, "context-a", "page-a")

	if err := agent.NewContext("context-b", 1024, 768, nil); err != nil {
		t.Fatalf("NewContext failed: %v", err)
	}
	if err := agent.NewPage("page-b"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}

	baseline := runtime.NumGoroutine()

	agent.SwitchPage("context-a", "page-a")
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window A failed: %v", err)
	}
	agent.SwitchPage("context-b", "page-b")
	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("MinimizeWindow(true) on window B failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)
	withWatchers := runtime.NumGoroutine()
	if withWatchers < baseline+2 {
		t.Fatalf("Expected goroutine count to grow by at least 2 with two watchers running, went from %d to %d", baseline, withWatchers)
	}

	if err := agent.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	waitForGoroutineCountAtMost(t, baseline, 2*time.Second)
}

/*
TestMinimizeWindowNoActivePageIsNoOp is a test which verifies that MinimizeWindow and RestoreWindow
both return cleanly, without error, when no page is currently active, rather than panicking or
blocking on a window that does not exist.

Example:

	Expected Inputs:
	    An instance whose only page has been closed, leaving no active page.

	Expected Outputs:
	    Both MinimizeWindow(true) and RestoreWindow return nil.
*/
func TestMinimizeWindowNoActivePageIsNoOp(t *testing.T) {
	agent := newWindowAgent(t, "context", "page")
	defer agent.Close()

	agent.ClosePage()

	if err := agent.MinimizeWindow(true); err != nil {
		t.Fatalf("Expected MinimizeWindow to no-op cleanly with no active page, got error: %v", err)
	}
	if err := agent.RestoreWindow(); err != nil {
		t.Fatalf("Expected RestoreWindow to no-op cleanly with no active page, got error: %v", err)
	}
}
