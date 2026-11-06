package elementary

import (
	"errors"
	"fmt"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
)

/*
PressKey is a method which allows you to press a single key or key combination on the active page, sending it to
whichever element currently has focus. key uses Playwright's key names, for example "Enter", "Tab", "Escape",
"ArrowDown", "a", or a combination joined with "+" such as "Control+A" or "Shift+Tab". Use PressKeyOnElement to
target a specific element instead of the focused one.

Example:

	err := agent.PressKey("Enter")
*/
func (shared *Instance) PressKey(key string) error {
	if key == "" {
		return errors.New("key must not be empty")
	}
	shared.captureScreenshot("press_key", "before", key)
	page, err := shared.getCurrentPage()
	if err != nil {
		shared.captureScreenshot("press_key", "failure", key)
		return err
	}
	if err := page.Keyboard().Press(key); err != nil {
		shared.captureScreenshot("press_key", "failure", key)
		return logger.Error(err, "failed to press key %s", key)
	}
	shared.captureScreenshot("press_key", "after", key)
	return nil
}

/*
PressKeyOnElement is a method which allows you to focus the element matched by selector and press a key or key
combination on it, using the same key names as PressKey. If no element matches the selector within the timeout, an
error wrapping ErrElementNotFound is returned and no key is pressed.

Example:

	err := agent.PressKeyOnElement("Enter", elementary.Selector{Value: "css=#search", Description: "search box"}, nil)
*/
func (shared *Instance) PressKeyOnElement(key string, selector Selector, parentLocator playwright.Locator, options ...ActionOptions) error {
	if key == "" {
		return errors.New("key must not be empty")
	}
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	details := fmt.Sprintf("%s_%s", key, selector)
	shared.captureScreenshot("press_key_on_element", "before", details)
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("press_key_on_element", "failure", details)
		return err
	}
	if err := element.Press(key, playwright.LocatorPressOptions{Timeout: &timeoutInMilliseconds}); err != nil {
		shared.captureScreenshot("press_key_on_element", "failure", details)
		return logger.Error(err, "failed to press key %s on %s", key, selector)
	}
	shared.captureScreenshot("press_key_on_element", "after", details)
	return nil
}

/*
SelectOption is a method which allows you to choose one or more options in the <select> dropdown matched by selector,
returning the values of the options that ended up selected. Each entry in values matches an option by either its
value attribute or its visible label, so callers can pass whichever is more convenient. In addition, the following
should be noted:

  - A single-choice dropdown selects only the first option matching any entry. Selecting several options requires the
    dropdown to carry the "multiple" attribute.

  - An empty values slice returns an error rather than clearing the selection. If no element matches the selector
    within the timeout, an error wrapping ErrElementNotFound is returned. If an entry matches no option, Playwright
    keeps waiting for it to appear until the timeout and then returns an error.

Example:

	selected, err := agent.SelectOption(elementary.Selector{Value: "css=#country", Description: "country"}, []string{"Canada"}, nil)
*/
func (shared *Instance) SelectOption(selector Selector, values []string, parentLocator playwright.Locator, options ...ActionOptions) ([]string, error) {
	if len(values) == 0 {
		return nil, errors.New("at least one option value or label is required to select")
	}
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("select_option", "before", selector.String())
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("select_option", "failure", selector.String())
		return nil, err
	}
	valuesOrLabels := append([]string(nil), values...)
	selected, err := element.SelectOption(
		playwright.SelectOptionValues{ValuesOrLabels: &valuesOrLabels},
		playwright.LocatorSelectOptionOptions{Timeout: &timeoutInMilliseconds},
	)
	if err != nil {
		shared.captureScreenshot("select_option", "failure", selector.String())
		return nil, logger.Error(err, "failed to select %v in %s", values, selector)
	}
	shared.captureScreenshot("select_option", "after", selector.String())
	return selected, nil
}

/*
SetChecked is a method which allows you to tick or untick the checkbox or radio button matched by selector, so it
ends up checked when checked is true and unchecked when it is false. Calling it when the element is already in the
requested state does nothing. If no element matches the selector within the timeout, an error wrapping
ErrElementNotFound is returned. In addition, the following should be noted:

  - A radio button cannot be unchecked directly; passing false for a checked radio button returns an error. Check a
    different radio button in the same group instead.

Example:

	err := agent.SetChecked(elementary.Selector{Value: "css=#terms", Description: "terms checkbox"}, true, nil)
*/
func (shared *Instance) SetChecked(selector Selector, checked bool, parentLocator playwright.Locator, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	details := fmt.Sprintf("%s_%t", selector, checked)
	shared.captureScreenshot("set_checked", "before", details)
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("set_checked", "failure", details)
		return err
	}
	if err := element.SetChecked(checked, playwright.LocatorSetCheckedOptions{Timeout: &timeoutInMilliseconds}); err != nil {
		shared.captureScreenshot("set_checked", "failure", details)
		return logger.Error(err, "failed to set %s checked to %t", selector, checked)
	}
	shared.captureScreenshot("set_checked", "after", details)
	return nil
}

/*
IsChecked is a method which allows you to report whether the checkbox or radio button matched by selector is
currently checked. If no element matches the selector within the timeout, an error wrapping ErrElementNotFound is
returned, and an element that is not a checkbox or radio button also returns an error.

Example:

	checked, err := agent.IsChecked(elementary.Selector{Value: "css=#terms", Description: "terms checkbox"}, nil)
*/
func (shared *Instance) IsChecked(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) (bool, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		return false, err
	}
	checked, err := element.IsChecked(playwright.LocatorIsCheckedOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return false, logger.Error(err, "failed to read checked state of %s", selector)
	}
	return checked, nil
}

/*
ScrollToElement is a method which allows you to scroll the page, and any scrollable containers, just enough to bring
the element matched by selector into view. Nothing scrolls if the element is already fully visible. If no element
matches the selector within the timeout, an error wrapping ErrElementNotFound is returned.

Example:

	err := agent.ScrollToElement(elementary.Selector{Value: "css=footer", Description: "page footer"}, nil)
*/
func (shared *Instance) ScrollToElement(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("scroll_to_element", "before", selector.String())
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("scroll_to_element", "failure", selector.String())
		return err
	}
	if err := element.ScrollIntoViewIfNeeded(playwright.LocatorScrollIntoViewIfNeededOptions{Timeout: &timeoutInMilliseconds}); err != nil {
		shared.captureScreenshot("scroll_to_element", "failure", selector.String())
		return logger.Error(err, "failed to scroll to %s", selector)
	}
	shared.captureScreenshot("scroll_to_element", "after", selector.String())
	return nil
}

/*
ScrollPageBy is a method which allows you to scroll the active page by xPixels horizontally and yPixels vertically,
relative to its current position. Positive values scroll right and down, negative values scroll left and up. This
suits pages that load more results as you scroll. In addition, the following should be noted:

  - The scroll is instant even on pages that style scrolling as smooth, so the new position is in effect as soon as
    this returns. Scrolling past either end of the page simply stops there.

Example:

	err := agent.ScrollPageBy(0, 800)
*/
func (shared *Instance) ScrollPageBy(xPixels int, yPixels int) error {
	details := fmt.Sprintf("%d_%d", xPixels, yPixels)
	shared.captureScreenshot("scroll_page_by", "before", details)
	page, err := shared.getCurrentPage()
	if err != nil {
		shared.captureScreenshot("scroll_page_by", "failure", details)
		return err
	}
	if _, err := page.Evaluate(scrollPageByScript, []int{xPixels, yPixels}); err != nil {
		shared.captureScreenshot("scroll_page_by", "failure", details)
		return logger.Error(err, "failed to scroll the page by %s", details)
	}
	shared.captureScreenshot("scroll_page_by", "after", details)
	return nil
}

// scrollPageByScript scrolls the window instantly by the [x, y] pixel offsets it is given.
const scrollPageByScript = `([x, y]) => window.scrollBy({ left: x, top: y, behavior: "instant" })`
