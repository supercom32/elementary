package elementary

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
)

/*
SwitchToFrame is a method which allows you to move into the iframe matched by selector, so that every element method
(finding, clicking, filling, reading, waiting, hovering, uploading, form controls, and downloads) works inside that
frame from then on. Calling it again while inside a frame moves into a frame nested within the current one. In
addition, the following should be noted:

  - The selector is resolved within the current frame, and the element it matches must be an iframe or frame;
    anything else returns an error and leaves the current frame unchanged. If no element matches within the timeout,
    an error wrapping ErrElementNotFound is returned.

  - Any change of the active page, including switching away and back, opening or closing a page, or switching
    context, returns lookups to the main frame, as does NavigateTo or NavigateBack. A navigation the page starts on
    its own, such as a link click or a redirect, does not, since Elementary cannot tell it apart from one that only
    reloads part of the page.

  - JavaScript evaluation, ScrollPageBy, MouseClick, and MoveMouse still act on the page as a whole, not the frame.
    Use SwitchToParentFrame or SwitchToMainFrame to step back out.

  - The frame is looked up afresh on every action, so it keeps working if the frame reloads, as long as its selector
    still matches.

Example:

	err := agent.SwitchToFrame(elementary.Selector{Value: "css=iframe#payment", Description: "payment frame"})
	err = agent.FillElement("4242 4242 4242 4242", cardNumberSelector, nil)
	agent.SwitchToMainFrame()
*/
func (shared *Instance) SwitchToFrame(selector Selector, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}
	frameElement, err := shared.getRequiredElement(selector, nil, options...)
	if err != nil {
		return err
	}
	tagName, err := frameElement.Evaluate("el => el.tagName", nil, playwright.LocatorEvaluateOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return logger.Error(err, "failed to inspect frame element %s", selector)
	}
	if tag, _ := tagName.(string); !strings.EqualFold(tag, "iframe") && !strings.EqualFold(tag, "frame") {
		return fmt.Errorf("%s is a <%v> element, not an iframe or frame", selector, tagName)
	}

	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.currentPage != page {
		return errors.New("the active page changed while switching frames")
	}
	if shared.framePage != page {
		shared.framePath = nil
	}
	shared.framePage = page
	shared.framePath = append(append([]string(nil), shared.framePath...), shared.resolveSelector(selector.Value))
	return nil
}

/*
SwitchToParentFrame is a method which allows you to step out of the current frame into the frame that contains it,
or into the main page when the current frame is a top-level iframe. It does nothing when already on the main page.

Example:

	agent.SwitchToParentFrame()
*/
func (shared *Instance) SwitchToParentFrame() {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.framePage != shared.currentPage || len(shared.framePath) == 0 {
		shared.framePath = nil
		return
	}
	shared.framePath = append([]string(nil), shared.framePath[:len(shared.framePath)-1]...)
}

/*
SwitchToMainFrame is a method which allows you to leave every frame and return element lookups to the current page as
a whole. It does nothing when already on the main page.

Example:

	agent.SwitchToMainFrame()
*/
func (shared *Instance) SwitchToMainFrame() {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	shared.framePage = nil
	shared.framePath = nil
}

/*
GetFrameDepth is a method which allows you to report how many frames deep element lookups currently are on the active
page: 0 on the main page, 1 inside a top-level iframe, 2 inside an iframe nested in that one, and so on.

Example:

	depth := agent.GetFrameDepth()
*/
func (shared *Instance) GetFrameDepth() int {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	if shared.framePage != shared.currentPage {
		return 0
	}
	return len(shared.framePath)
}

/*
framePathFor is a method which allows you to get a copy of the frame path chosen with SwitchToFrame, but only when it
was chosen on page; for any other page it returns nil, meaning the main frame.

Example:

	framePath := shared.framePathFor(page)
*/
func (shared *Instance) framePathFor(page playwright.Page) []string {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	if page == nil || shared.framePage != page {
		return nil
	}
	return append([]string(nil), shared.framePath...)
}

/*
resetFrameForPage is a method which allows you to return element lookups to the main frame when page is the page the
current frame was chosen on. NavigateTo and NavigateBack call this before navigating, because the frame chosen with
SwitchToFrame belongs to the document being left.

Example:

	shared.resetFrameForPage(page)
*/
func (shared *Instance) resetFrameForPage(page playwright.Page) {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.framePage == page {
		shared.framePage = nil
		shared.framePath = nil
	}
}

/*
setCurrentPageUnlocked is a method which allows you to make page the active page and return element lookups to its
main frame. Every place that changes the active page goes through this, so a frame chosen with SwitchToFrame never
carries over to another page, nor survives leaving and returning to the page it was chosen on. Callers must already
hold stateMutex.

Example:

	shared.setCurrentPageUnlocked(pageInstance.instance)
*/
func (shared *Instance) setCurrentPageUnlocked(page playwright.Page) {
	shared.currentPage = page
	shared.framePage = nil
	shared.framePath = nil
}
