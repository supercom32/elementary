package elementary

import (
	"errors"
	"fmt"
	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
	"github.com/supercom32/filesystem"
	neturl "net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

/*
captureScreenshot is a method which handles writing a diagnostic page state screenshot to disk if the corresponding setting is active.
*/
func (shared *Instance) captureScreenshot(actionName string, stage string, details string) {
	if shared.screenshotOptions.Directory == "" {
		return
	}
	if stage == "before" && !shared.screenshotOptions.BeforeAction {
		return
	}
	if stage == "after" && !shared.screenshotOptions.AfterAction {
		return
	}
	if stage == "failure" && !shared.screenshotOptions.OnFailure {
		return
	}
	screenshotIndex := shared.screenshotIndex.Add(1)
	screenshotName := fmt.Sprintf("%04d_%s_%s_%s.png", screenshotIndex, actionName, stage, details)
	err := shared.SaveScreenshot(shared.screenshotOptions.Directory, screenshotName, ActionOptions{InfiniteTimeout: true})
	if err != nil {
		logger.Log(logger.TYPE_WARN, "automated screenshot failed to save: %v", err)
	}
}

/*
GetElementAttribute is a method which extracts the value of a designated attribute from a target DOM element. This
retrieves runtime properties such as hyperlinks, resource paths, or style classes from the rendered markup.

Example:

	attribute, err := agent.GetElementAttribute("href", elementary.Selector{Value: "css=a.link"}, nil, ActionOptions{Timeout: 2 * time.Second})
*/
func (shared *Instance) GetElementAttribute(attribute string, selector Selector, locator playwright.Locator, options ...ActionOptions) (string, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	element, err := shared.GetElement(selector, locator, options...)
	if err != nil {
		return "", err
	}
	count, err := element.Count()
	if err != nil {
		return "", logger.Error(err, "failed to count matches for %s", selector)
	}
	if count == 0 {
		return "", nil
	}
	returnedAttribute, err := element.GetAttribute(attribute, playwright.LocatorGetAttributeOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return "", logger.Error(err, "failed to read attribute %s on selector %s", attribute, selector)
	}
	return returnedAttribute, nil
}

/*
ClickElement is a method which dispatches a mouse click event to the DOM node matched by the given selector. This
simulates natural user click sequences to trigger navigation links, button submits, or option selections. In addition,
the following should be noted:

  - This method is a thin wrapper around GetElement and ClickLocator. Selector resolution happens here; the click
    itself, along with all before/after/failure screenshot capture, is owned exclusively by ClickLocator. Any caller
    that already holds a resolved playwright.Locator (e.g. from GetElement, GetVisibleElement, or its own querying)
    should call ClickLocator directly instead of re-resolving through a selector.

Example:

	err := agent.ClickElement(elementary.Selector{Value: "css=#submit-button"}, nil, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) ClickElement(selector Selector, locator playwright.Locator, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	element, err := shared.GetElement(selector, locator, options...)
	if err != nil {
		return err
	}
	return shared.ClickLocator(element, selector.String(), options...)
}

/*
ClickLocator is a method which dispatches a mouse click event directly against an already-resolved playwright.Locator.
This is the single source of truth for click behavior in Elementary: it owns timeout resolution, the actual Click
call, and all before/after/failure screenshot capture. ClickElement, and any other method that needs to click a
resolved element, delegate to this method so that changes to click behavior or screenshot handling only need to be
made in one place. In addition, the following should be noted:

  - details is a free-form descriptive string (typically the originating selector, but any identifying label works)
    used only for naming the captured screenshot files and for error messages. It has no effect on the click itself.

  - A nil element is treated as a caller error rather than allowed to panic against the playwright.Locator interface.
    A "failure" screenshot is still captured in this case, since a page snapshot at the moment of an invalid call is
    valuable diagnostic evidence.

Example:

	selector := elementary.Selector{Value: "css=#submit-button"}
	element, err := agent.GetElement(selector, nil, ActionOptions{Timeout: 3 * time.Second})
	if err != nil {
	    return err
	}
	err = agent.ClickLocator(element, selector.String(), ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) ClickLocator(element playwright.Locator, details string, options ...ActionOptions) error {
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("click_locator", "before", details)
	if element == nil {
		shared.captureScreenshot("click_locator", "failure", details)
		return errors.New("cannot click a nil element for " + details)
	}
	err := element.Click(playwright.LocatorClickOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		shared.captureScreenshot("click_locator", "failure", details)
		return logger.Error(err, "failed to click element %s", details)
	}
	shared.captureScreenshot("click_locator", "after", details)
	return nil
}

/*
ClickElementManually is a method which executes a hardware-style mouse click at the exact coordinates of an element.
This bypasses high-level click interceptors, addressing complex layouts where standard click emulation is restricted.

Example:

	err := agent.ClickElementManually("left", locator, nil, ActionOptions{Timeout: time.Second})
*/
func (shared *Instance) ClickElementManually(mouseButton string, element playwright.Locator, parentLocator playwright.Locator, options ...ActionOptions) error {
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("click_element_manually", "before", mouseButton)
	locator := element
	var clickOptions playwright.MouseClickOptions
	if mouseButton == MOUSE_BUTTON_LEFT {
		clickOptions.Button = playwright.MouseButtonLeft
	} else if mouseButton == MOUSE_BUTTON_MIDDLE {
		clickOptions.Button = playwright.MouseButtonMiddle
	} else if mouseButton == MOUSE_BUTTON_RIGHT {
		clickOptions.Button = playwright.MouseButtonRight
	}
	page, err := shared.getCurrentPage()
	if err != nil {
		shared.captureScreenshot("click_element_manually", "failure", mouseButton)
		return err
	}
	_ = locator.Focus(playwright.LocatorFocusOptions{Timeout: &timeoutInMilliseconds})
	locatorInformation, err := locator.BoundingBox(playwright.LocatorBoundingBoxOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		shared.captureScreenshot("click_element_manually", "failure", mouseButton)
		return logger.Error(err, "failed to obtain bounding box of element")
	}
	err = page.Mouse().Click(float64(locatorInformation.X), float64(locatorInformation.Y), clickOptions)
	if err != nil {
		shared.captureScreenshot("click_element_manually", "failure", mouseButton)
		return logger.Error(err, "failed manual mouse click action")
	}
	shared.captureScreenshot("click_element_manually", "after", mouseButton)
	return nil
}

/*
FillElement is a method which focuses a designated input element and types the specified text content. This simulates
form input behaviors on fields, textual areas, or content-editable nodes.

Example:

	err := agent.FillElement("myPassword123", elementary.Selector{Value: "css=input[type='password']"}, nil, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) FillElement(textToFill string, selector Selector, parentLocator playwright.Locator, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("fill_element", "before", selector.String())
	locator, err := shared.GetElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("fill_element", "failure", selector.String())
		return err
	}
	err = locator.Fill(textToFill, playwright.LocatorFillOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		shared.captureScreenshot("fill_element", "failure", selector.String())
		return logger.Error(err, "failed to fill element %s", selector)
	}
	shared.captureScreenshot("fill_element", "after", selector.String())
	return nil
}

/*
MoveMouse is a method which executes a mouse down, linear pointer sweep, and mouse release sequence. This approximates
human mouse gestures across the canvas coordinates to satisfy basic anti-bot drag interactions.

Example:

	agent.MoveMouse()
*/
func (shared *Instance) MoveMouse() {
	page, err := shared.getCurrentPage()
	if err != nil {
		return
	}
	var options playwright.MouseMoveOptions
	steps := 100
	options.Steps = &steps
	_ = page.Mouse().Down()
	_ = page.Mouse().Move(100, 20, options)
	_ = page.Mouse().Up()
}

/*
MouseClick is a method which triggers a single mouse click at specified horizontal and vertical canvas offsets. This is
suited for targeting coordinate markers on static graphical surfaces that lack underlying HTML tag nodes.

Example:

	agent.MouseClick(500, 400)
*/
func (shared *Instance) MouseClick(xLocation int, yLocation int) {
	details := fmt.Sprintf("%d_%d", xLocation, yLocation)
	shared.captureScreenshot("mouse_click", "before", details)
	page, err := shared.getCurrentPage()
	if err != nil {
		return
	}
	_ = page.Mouse().Click(float64(xLocation), float64(yLocation))
	shared.captureScreenshot("mouse_click", "after", details)
}

/*
GetLocatorCount is a method which returns the total number of DOM nodes matched by the locator. This evaluates list sizes
and structural density within the parsed document.

Example:

	count := agent.GetLocatorCount(someListLocator)
*/
func (shared *Instance) GetLocatorCount(locator playwright.Locator) int {
	locatorCount, _ := locator.Count()
	return locatorCount
}

/*
IsLocatorExists is a method which confirms whether a locator is bound to at least one valid node in the DOM. This
pre-empts out-of-bounds exceptions when accessing collection elements.

Example:

	exists := agent.IsLocatorExists(listLocator)
*/
func (shared *Instance) IsLocatorExists(locator playwright.Locator) bool {
	if locator == nil {
		return false
	}
	locatorCount, _ := locator.Count()
	return locatorCount > 0
}

/*
IsElementVisible is a method which returns whether a designated element is rendered and occupies layout space. This
avoids attempting interactions on elements that are hidden by dialog frames or display properties.

Example:

	visible := agent.IsElementVisible(elementary.Selector{Value: "css=div.alert-box"}, someParentLocator)
*/
func (shared *Instance) IsElementVisible(selector Selector, locator playwright.Locator) bool {
	var page playwright.Page
	if locator == nil {
		var err error
		page, err = shared.getCurrentPage()
		if err != nil {
			return false
		}
	}
	elementFound := shared.resolveBaseLocator(page, locator, selector.Value)
	count, _ := elementFound.Count()
	if count > 0 {
		isVisible, _ := elementFound.IsVisible()
		return isVisible
	}
	return false
}

/*
IsElementExist is a method which checks if an element is mapped within the DOM. This validates the presence of an element
regardless of its current opacity, visibility, or rendering state.

Example:

	exists := agent.IsElementExist(elementary.Selector{Value: "css=#sidebar"}, nil)
*/
func (shared *Instance) IsElementExist(selector Selector, locator playwright.Locator) bool {
	var page playwright.Page
	if locator == nil {
		var err error
		page, err = shared.getCurrentPage()
		if err != nil {
			return false
		}
	}
	elementFound := shared.resolveBaseLocator(page, locator, selector.Value)
	count, _ := elementFound.Count()
	return count > 0
}

/*
GetElementCountWithTimeout is a method which retrieves the count of matching elements within a specified polling timeout.
This accommodates slow asynchronous layouts that render lists gradually.

Example:

	count, err := agent.GetElementCountWithTimeout(cardListLocator, ActionOptions{Timeout: 1500 * time.Millisecond})
*/
func (shared *Instance) GetElementCountWithTimeout(locator playwright.Locator, options ...ActionOptions) (int, error) {
	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	resultChan := make(chan int, 1)
	errorChan := make(chan error, 1)

	go func() {
		elementCount, err := locator.Count()
		if err != nil {
			errorChan <- err
			return
		}
		resultChan <- elementCount
	}()

	var timeoutChannel <-chan time.Time
	if !isInfinite {
		timeoutChannel = time.After(time.Duration(timeoutInMilliseconds) * time.Millisecond)
	}

	select {
	case elementCount := <-resultChan:
		return elementCount, nil
	case err := <-errorChan:
		return 0, logger.Error(err, "failed during element count operation")
	case <-timeoutChannel:
		return 0, errors.New("timeout occurred while counting elements")
	}
}

/*
createEmptyLocator is a method which returns a non-existent locator reference to represent missing state. This avoids Go panic
crashes when operations are executed on missing nodes.

Example:

	loc := shared.createEmptyLocator(page)
*/
func (shared *Instance) createEmptyLocator(page playwright.Page) playwright.Locator {
	return page.Locator("#non-existent-element-12345")
}

/*
GetElement is a method which locates the first matching element after verifying it has attached to the DOM tree. This
synchronizes element retrieval with asynchronous loading processes, securing reliable node references.

Example:

	loc, err := agent.GetElement(elementary.Selector{Value: "css=.btn-login"}, nil, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) GetElement(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) (playwright.Locator, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Getting element '%s' with timeout '%.0f'.", selector, timeoutInMilliseconds)
	page, err := shared.getCurrentPage()
	if err != nil {
		return nil, err
	}

	baseLocator := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	elementFound := baseLocator.First()
	waitOptions := *playwright.WaitForSelectorStateAttached
	err = shared.waitForLocatorState(elementFound, waitOptions, options...)
	if err != nil {
		if isTimeoutError(err) {
			return shared.createEmptyLocator(page), nil
		}
		return shared.createEmptyLocator(page), logger.Error(err, "failed to get element %s", selector)
	}

	return elementFound, nil
}

/*
UploadFile is a method which binds local file payloads directly to a target file input DOM selector. This automates multi-part
file uploads by mapping files from local storage streams.

Example:

	err := agent.UploadFile(elementary.Selector{Value: "css=input[type='file']"}, "test-image.jpg", ActionOptions{Timeout: 5 * time.Second})
*/
func (shared *Instance) UploadFile(selector Selector, fileName string, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}
	fileContentsInBytes, err := filesystem.GetFileContentsAsBytes(fileName)
	if err != nil {
		return logger.Error(err, "failed to read file content for upload")
	}
	fileEntry := playwright.InputFile{
		Name:     fileName,
		MimeType: "image/jpeg",
		Buffer:   fileContentsInBytes,
	}
	err = page.SetInputFiles(selector.Value, []playwright.InputFile{fileEntry}, playwright.PageSetInputFilesOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return logger.Error(err, "failed to upload file through playwright")
	}
	return nil
}

/*
WaitForPageToBeReady is a method which blocks execution until the page reaches the DOMContentLoaded state. This
secures a parsed page model before executing actions, without waiting on NetworkIdle, which can stall well past
DOM readiness on pages with persistent connections (ads, analytics beacons, websockets).

Example:

	err := agent.WaitForPageToBeReady()
*/
func (shared *Instance) WaitForPageToBeReady() error {
	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}
	timeoutInMilliseconds := float64(shared.defaultElementTimeout.Milliseconds())
	err = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State:   playwright.LoadStateDomcontentloaded,
		Timeout: &timeoutInMilliseconds,
	})
	if err != nil {
		return logger.Error(err, "failed to wait for DOMContentLoaded")
	}
	return nil
}

/*
GetElements is a method that extracts all elements matching the specified selector. This is suited for iterating over lists,
data grids, or matching child patterns inside a container element.

Example:

	elements, err := agent.GetElements(elementary.Selector{Value: "css=ul.nav > li"}, nil, ActionOptions{Timeout: 2500 * time.Millisecond})
*/
func (shared *Instance) GetElements(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) ([]playwright.Locator, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Getting element(s) '%s' with timeout '%.0f'.", selector, timeoutInMilliseconds)
	page, err := shared.getCurrentPage()
	if err != nil {
		return nil, err
	}

	baseLocator := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	waitOptions := *playwright.WaitForSelectorStateAttached
	err = shared.waitForLocatorState(baseLocator.First(), waitOptions, options...)
	if err != nil {
		if isTimeoutError(err) {
			return []playwright.Locator{}, nil
		}
		return nil, logger.Error(err, "failed to fetch element list for %s", selector)
	}

	elements := []playwright.Locator{}
	count, err := baseLocator.Count()
	if err != nil {
		return nil, logger.Error(err, "failed count during GetElements")
	}
	for i := 0; i < count; i++ {
		elements = append(elements, baseLocator.Nth(i))
	}
	return elements, nil
}

/*
GetVisibleElement is a method that filters and returns the first visible element matching a selector. This ignores secondary,
off-screen, or display-none duplicates of target selector fields.

Example:

	visibleElement, err := agent.GetVisibleElement(elementary.Selector{Value: "css=button.active"}, nil, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) GetVisibleElement(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) (playwright.Locator, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Getting first visible element '%s' with timeout '%.0f'.", selector, timeoutInMilliseconds)
	page, err := shared.getCurrentPage()
	if err != nil {
		return nil, err
	}

	baseLocator := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	waitOptions := *playwright.WaitForSelectorStateVisible
	err = shared.waitForLocatorState(baseLocator.First(), waitOptions, options...)
	if err == nil {
		return baseLocator.First(), nil
	}

	if !isTimeoutError(err) {
		return shared.createEmptyLocator(page), logger.Error(err, "failed to get visible element %s", selector)
	}

	elementHandles, err := baseLocator.ElementHandles()
	if err != nil {
		return shared.createEmptyLocator(page), logger.Error(err, "failed to get element handles for visible check")
	}

	for i, handle := range elementHandles {
		isVisible, err := handle.IsVisible()
		if err != nil {
			continue
		}
		if isVisible {
			return baseLocator.Nth(i), nil
		}
	}

	return shared.createEmptyLocator(page), nil
}

/*
GetElementText is a method that extracts the raw text content from an element. This is useful when scraping textual information,
validating field values, or inspecting dynamic alerts.

Example:

	text, err := agent.GetElementText(elementary.Selector{Value: "css=h1.title"}, nil, ActionOptions{Timeout: 1500 * time.Millisecond})
*/
func (shared *Instance) GetElementText(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) (string, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Getting element text for '%s' with timeout '%.0f'.", selector, timeoutInMilliseconds)
	if _, err := shared.getCurrentPage(); err != nil {
		return "", err
	}

	locator, err := shared.GetElement(selector, parentLocator, options...)
	if err != nil {
		return "", err
	}

	count, err := locator.Count()
	if err != nil {
		return "", logger.Error(err, "failed to count matches for %s", selector)
	}
	if count == 0 {
		return "", nil
	}

	textFound, err := locator.TextContent(playwright.LocatorTextContentOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return "", logger.Error(err, "failed text retrieval on %s", selector)
	}

	return textFound, nil
}

/*
GetNumberOfElements is a method which returns the total count of element nodes matching a selector. This checks list sizes, data grid rows, or pagination counts.

Example:

	count, err := agent.GetNumberOfElements(elementary.Selector{Value: "css=div.product-card"}, nil, ActionOptions{Timeout: 2 * time.Second})
*/
func (shared *Instance) GetNumberOfElements(selector Selector, parentLocator playwright.Locator, options ...ActionOptions) (int, error) {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Getting number of elements for '%s' with timeout '%.0f'.", selector, timeoutInMilliseconds)
	page, err := shared.getCurrentPage()
	if err != nil {
		return 0, err
	}

	baseLocator := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	waitOptions := *playwright.WaitForSelectorStateAttached
	err = shared.waitForLocatorState(baseLocator.First(), waitOptions, options...)
	if err != nil {
		if isTimeoutError(err) {
			return 0, nil
		}
		return 0, logger.Error(err, "failed getting element count")
	}

	count, err := baseLocator.Count()
	if err != nil {
		return 0, logger.Error(err, "failed element count read")
	}
	return count, nil
}

/*
NavigateBack is a method which triggers backwards navigation in the active browser tab's history. This emulates clicking
the standard browser back button to handle multi-step layouts.

Example:

	agent.NavigateBack()
*/
func (shared *Instance) NavigateBack() {
	page, err := shared.getCurrentPage()
	if err != nil {
		return
	}
	shared.captureScreenshot("navigate_back", "before", page.URL())
	_, _ = page.GoBack()
	shared.trackAllowedNavigationHost(page.URL())
	shared.captureScreenshot("navigate_back", "after", page.URL())
}

/*
WaitForElementCount is a method which blocks execution until the matched elements reach a specific count. This is useful for waiting for dynamic additions or AJAX lists to finish rendering.
In addition, the following should be noted:

  - This method counts against the full, un-narrowed locator for selector rather than a
    .First()-scoped one, so it can correctly wait for counts greater than 1. Reusing GetElement
    here would silently cap every wait at a maximum observable count of 1, since GetElement
    narrows its returned locator to the first match.

Example:

	err := agent.WaitForElementCount(elementary.Selector{Value: "css=tr.table-row"}, nil, 10, ActionOptions{Timeout: 4 * time.Second})
*/
func (shared *Instance) WaitForElementCount(selector Selector, parentLocator playwright.Locator, elementInstanceCount int, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}

	baseLocator := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	startTime := time.Now()
	timeoutDuration := time.Duration(timeoutInMilliseconds) * time.Millisecond
	for {
		count, err := baseLocator.Count()
		if err == nil && count == elementInstanceCount {
			return nil
		}

		if !isInfinite && time.Since(startTime) >= timeoutDuration {
			return errors.New("Timeout waiting for element count")
		}

		time.Sleep(100 * time.Millisecond)
	}
}

/*
waitForLocatorState is a method which blocks until a Playwright locator transitions to a target state. This synchronizes internal steps
before executing operations on elements.

Example:

	err := shared.waitForLocatorState(locator, state, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) waitForLocatorState(locator playwright.Locator, state playwright.WaitForSelectorState, options ...ActionOptions) error {
	timeout, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	err := locator.WaitFor(playwright.LocatorWaitForOptions{
		State:   &state,
		Timeout: &timeout,
	})
	if err != nil {
		return logger.Error(err, "locator failed to reach state within timeout")
	}
	return nil
}

/*
WaitForElementExistenceStatus is a method which blocks until an element's existence status matches the target value. This is useful for waiting
for loading overlays to detach or target panels to attach to the tree.

Example:

	err := agent.WaitForElementExistenceStatus(elementary.Selector{Value: "css=div#spinner"}, nil, false, ActionOptions{Timeout: 5 * time.Second})
*/
func (shared *Instance) WaitForElementExistenceStatus(selector Selector, parentLocator playwright.Locator, isExistStatus bool, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)

	logger.Log(logger.TYPE_DEBUG, "Waiting for element '%s' to %s with timeout '%.0f'.",
		selector,
		map[bool]string{true: "exist", false: "not exist"}[isExistStatus],
		timeoutInMilliseconds)

	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}

	locatorFound := shared.resolveBaseLocator(page, parentLocator, selector.Value)

	var waitOptions playwright.WaitForSelectorState
	if isExistStatus {
		waitOptions = *playwright.WaitForSelectorStateAttached
	} else {
		waitOptions = *playwright.WaitForSelectorStateDetached
	}

	return shared.waitForLocatorState(locatorFound.First(), waitOptions, options...)
}

/*
WaitForElementVisibleStatus is a method which blocks execution until the element matches the target visibility status. This is useful for waiting
for visual fades or transition stages to resolve before interaction.

Example:

	err := agent.WaitForElementVisibleStatus(elementary.Selector{Value: "//div[@id='modal']"}, nil, true, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) WaitForElementVisibleStatus(selector Selector, element playwright.ElementHandle, isVisibleStatus bool, options ...ActionOptions) error {
	options = applySelectorTimeout(selector, options)
	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)

	logger.Log(logger.TYPE_DEBUG, "Waiting for element with xpath '%s' to %s with timeout '%.0f'.",
		selector,
		map[bool]string{true: "be visible", false: "be hidden"}[isVisibleStatus],
		timeoutInMilliseconds)

	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}

	var elementFound playwright.ElementHandle
	resolved := shared.resolveSelector(selector.Value)
	if element != nil {
		elementFound, err = element.QuerySelector(resolved)
	} else {
		elementFound, err = page.QuerySelector(resolved)
	}

	if err != nil {
		return logger.Error(err, "error querying element visbility via xpath")
	}

	if elementFound == nil {
		if !isVisibleStatus {
			return nil
		}
		return errors.New("element not found with xpath: " + selector.String())
	}

	start := time.Now()
	deadline := start.Add(time.Duration(timeoutInMilliseconds) * time.Millisecond)
	backoff := 10 * time.Millisecond
	maxBackoff := 100 * time.Millisecond

	for isInfinite || time.Now().Before(deadline) {
		isElementVisible, err := elementFound.IsVisible()
		if err != nil {
			if strings.Contains(err.Error(), "detached") && !isVisibleStatus {
				return nil
			}
			return logger.Error(err, "failed visibility check polling")
		}

		if isElementVisible == isVisibleStatus {
			return nil
		}

		time.Sleep(backoff)
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}

	return errors.New("timeout occurred waiting for element visible status '" + strconv.FormatBool(isVisibleStatus) + "'.")
}

/*
trackAllowedNavigationHost is a method which records the hostname of url as the legitimate navigation target for the current
context, for BlockRedirects to compare subsequent main-frame navigations against. Every method that performs a deliberate,
user-requested top-level navigation (NavigateTo, NavigateBack, ...) must call this so BlockRedirects does not mistake its own
legitimate navigation for a cross-site redirect attempt.
*/
func (shared *Instance) trackAllowedNavigationHost(url string) {
	if parsedURL, parseErr := neturl.Parse(url); parseErr == nil && parsedURL.Hostname() != "" {
		shared.setAllowedNavigationHostForCurrentContext(parsedURL.Hostname())
	}
}

/*
NavigateTo is a method which loads and opens the target URL in the active page tab. This initiates standard page browsing
sessions, loading the requested document context.

Example:

	err := agent.NavigateTo("https://example.com", ActionOptions{Timeout: 10 * time.Second})
*/
func (shared *Instance) NavigateTo(url string, options ...ActionOptions) error {
	shared.captureScreenshot("navigate_to", "before", url)
	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(shared.defaultNavigationTimeout, options)
	logger.Log(logger.TYPE_DEBUG, "Now navigating to '%s' with timeout '%.0f.", url, timeoutInMilliseconds)
	page, err := shared.getCurrentPage()
	if err != nil {
		return err
	}
	shared.trackAllowedNavigationHost(url)
	navigateOptions := playwright.PageGotoOptions{}
	navigateOptions.Timeout = &timeoutInMilliseconds
	startTime := time.Now()
	timeout := time.Duration(timeoutInMilliseconds) * time.Millisecond
	_, err = page.Goto(url, navigateOptions)
	for err != nil && (isInfinite || time.Since(startTime) < timeout) {
		_, err = page.Goto(url, navigateOptions)
	}
	if err != nil {
		shared.captureScreenshot("navigate_to", "failure", url)
		return logger.Error(err, "failed to navigate to URL %s", url)
	}
	_ = shared.WaitForPageToBeReady()
	logger.Log(logger.TYPE_DEBUG, "Page '%s' is now ready!", url)
	shared.captureScreenshot("navigate_to", "after", url)
	return err
}

/*
WaitForPageToChange is a method which blocks execution until the active tab registers a DOM content loaded event, then
polls until the page URL contains continueIfUrlContains (when non-empty). This is useful for waiting for simple document
updates or page transition boundaries.

Example:

	agent.WaitForPageToChange("/dashboard", ActionOptions{Timeout: 5 * time.Second})
*/
func (shared *Instance) WaitForPageToChange(continueIfUrlContains string, options ...ActionOptions) {
	page, err := shared.getCurrentPage()
	if err != nil {
		return
	}
	timeoutInMilliseconds, isInfinite := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	waitOptions := playwright.LoadStateDomcontentloaded
	_ = page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{State: waitOptions, Timeout: &timeoutInMilliseconds})

	if continueIfUrlContains == "" {
		return
	}

	startTime := time.Now()
	timeout := time.Duration(timeoutInMilliseconds) * time.Millisecond
	for !strings.Contains(page.URL(), continueIfUrlContains) {
		if !isInfinite && time.Since(startTime) >= timeout {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

/*
HoverOverElement is a method which scrolls to, positions the mouse cursor over, and clicks the target element. This triggers hover-based
dropdowns or context menus.

Example:

	err := agent.HoverOverElement(elementary.Selector{Value: "css=a.hover-menu"}, nil, 15, 15, ActionOptions{Timeout: 3 * time.Second})
*/
func (shared *Instance) HoverOverElement(selector Selector, locator playwright.Locator, xLocation int, yLocation int, options ...ActionOptions) error {
	shared.captureScreenshot("hover_over_element", "before", selector.String())
	options = applySelectorTimeout(selector, options)
	var hoverOptions playwright.LocatorHoverOptions

	timeout, _ := resolveTimeoutInMilliseconds(5*time.Second, options)
	hoverOptions.Timeout = &timeout

	forced := true
	hoverOptions.Force = &forced

	x := float64(xLocation)
	y := float64(yLocation)
	hoverOptions.Position = new(playwright.Position)
	hoverOptions.Position.X = x
	hoverOptions.Position.Y = y

	element, err := shared.GetElement(selector, locator, options...)
	if err != nil {
		shared.captureScreenshot("hover_over_element", "failure", selector.String())
		return err
	}

	if shared.IsLocatorExists(element) {
		err = element.Hover(hoverOptions)
		if err != nil {
			shared.captureScreenshot("hover_over_element", "failure", selector.String())
			return logger.Error(err, "failed to hover over the element")
		}

		err = element.Click(playwright.LocatorClickOptions{Timeout: &timeout})
		if err != nil {
			shared.captureScreenshot("hover_over_element", "failure", selector.String())
			return logger.Error(err, "failed to click on the element")
		}
	}
	shared.captureScreenshot("hover_over_element", "after", selector.String())
	return nil
}

/*
getCurrentPage is a method which returns a race-free snapshot of the active page reference under a read lock. In addition,
the following should be noted:

  - This method returns an error immediately if either active context count is zero or the current page reference is nil.
    Callers should use the returned page value for the remainder of their operation instead of re-reading shared.currentPage,
    since the latter is not safe to read outside of stateMutex.

Example:

	page, err := shared.getCurrentPage()
*/
func (shared *Instance) getCurrentPage() (playwright.Page, error) {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	if len(shared.contextInstances) == 0 {
		return nil, errors.New("there is no active browser context to perform this operation")
	}
	if shared.currentPage == nil {
		return nil, errors.New("there is no open page to perform this operation")
	}
	return shared.currentPage, nil
}

/*
BlockPopups is a method which intercepts and closes dynamically opened popup pages on the current browser context. This prevents marketing
popups, blank targets, or ad redirections from interrupting active test procedures.

Example:

	err := agent.BlockPopups()
*/
func (shared *Instance) BlockPopups() error {
	shared.stateMutex.RLock()
	if len(shared.contextInstances) == 0 || shared.currentContextIndex < 0 {
		shared.stateMutex.RUnlock()
		return nil
	}
	currentContext := shared.contextInstances[shared.currentContextIndex]
	shared.stateMutex.RUnlock()

	if currentContext.popupBlockingEnabled {
		return nil
	}

	ctx := currentContext.instance

	initScript := `(function () {
		try {
			window.open = function() { return null; };
			document.addEventListener('click', function (e) {
				var a = e.target.closest && e.target.closest('a[target="_blank"]');
				if (a) {
					e.preventDefault();
				}
			}, true);
		} catch (e) {
		}
	})();`

	if err := ctx.AddInitScript(playwright.Script{Content: playwright.String(initScript)}); err != nil {
		logger.Log(logger.TYPE_WARN, "AddInitScript failed: %v", err)
	}

	attachToPage := func(p playwright.Page) {
		p.On("popup", func(pop playwright.Page) {
			_ = pop.Close()
		})
		_, _ = p.Evaluate(fmt.Sprintf(`() => { %s }`, initScript))
	}

	for _, p := range ctx.Pages() {
		attachToPage(p)
	}

	ctx.On("page", func(p playwright.Page) {
		attachToPage(p)
		go func(pp playwright.Page) {
			time.Sleep(150 * time.Millisecond)
			url := pp.URL()
			if url == "" || strings.Contains(url, "about:blank") {
				time.Sleep(250 * time.Millisecond)
			}
			_ = pp.Close()
		}(p)
	})

	shared.stateMutex.Lock()
	if contextIndex, _ := shared.getContextInstanceUnlocked(currentContext.alias); contextIndex != NULL_CONTEXT {
		shared.contextInstances[contextIndex].popupBlockingEnabled = true
	}
	shared.stateMutex.Unlock()
	return nil
}

/*
BlockRedirects is a method which neutralizes navigation attempts away from the intended site on the current browser context. This
prevents ad networks, tracking pixels, or hijacked pages from redirecting the active tab away from its loaded content mid-session.
In addition, the following should be noted:

  - Script-driven navigation (location assignment, meta-refresh tags) is neutralized directly in the page via an injected script.
  - Main frame navigation requests are also gated at the network layer: once NavigateTo establishes a target hostname for the
    context, any subsequent top-level navigation request to a different hostname is aborted outright, regardless of whether it
    originated from a script, a synthetic click, or a real click on a hijacked full-page link. This is what actually stops sites
    that redirect via native anchor navigation rather than JavaScript location APIs.
  - Navigating to a different hostname on purpose still works; calling NavigateTo again simply updates the allowed hostname
    before its own request is evaluated.
  - blockedHosts is an optional list of additional hostnames (ad networks, trackers, etc.) to reject outright, for every
    resource type, regardless of the navigation gate above. Matching is by exact hostname or subdomain (e.g. "ads.example.com"
    matches a blocked "example.com"), not a partial/substring match, so unrelated domains that merely share a suffix (e.g.
    "notexample.com") are not blocked. When no hostnames are passed, no such hard blocking occurs, and only the navigation
    gate and script neutralization apply.

Example:

	err := agent.BlockRedirects()
	err := agent.BlockRedirects("profitabledisplaynetwork.com", "kettledroopingcontinuation.com")
*/
func (shared *Instance) BlockRedirects(blockedHosts ...string) error {
	shared.stateMutex.Lock()
	if len(shared.contextInstances) == 0 || shared.currentContextIndex < 0 {
		shared.stateMutex.Unlock()
		return nil
	}
	if shared.contextInstances[shared.currentContextIndex].redirectBlockingEnabled {
		shared.stateMutex.Unlock()
		return nil
	}
	shared.contextInstances[shared.currentContextIndex].redirectBlockingEnabled = true
	currentContext := shared.contextInstances[shared.currentContextIndex]
	shared.stateMutex.Unlock()

	ctx := currentContext.instance

	initScript := `(function () {
		try {
			try {
				window.location.assign = function () {};
				window.location.replace = function () {};
			} catch (e) {}
			try {
				var descriptor = Object.getOwnPropertyDescriptor(Location.prototype, 'href');
				if (descriptor && descriptor.get && descriptor.configurable) {
					Object.defineProperty(window.location, 'href', {
						configurable: true,
						get: function () { return descriptor.get.call(window.location); },
						set: function () {}
					});
				}
			} catch (e) {}
			var stripMetaRefresh = function () {
				try {
					var tags = document.querySelectorAll('meta[http-equiv="refresh" i]');
					for (var i = 0; i < tags.length; i++) {
						tags[i].parentNode && tags[i].parentNode.removeChild(tags[i]);
					}
				} catch (e) {}
			};
			stripMetaRefresh();
			try {
				var observer = new MutationObserver(stripMetaRefresh);
				observer.observe(document.documentElement || document, { childList: true, subtree: true });
			} catch (e) {}
		} catch (e) {
		}
	})();`

	if err := ctx.AddInitScript(playwright.Script{Content: playwright.String(initScript)}); err != nil {
		logger.Log(logger.TYPE_WARN, "AddInitScript failed: %v", err)
	}

	attachToPage := func(p playwright.Page) {
		_, _ = p.Evaluate(fmt.Sprintf(`() => { %s }`, initScript))
	}

	for _, p := range ctx.Pages() {
		attachToPage(p)
	}

	ctx.On("page", func(p playwright.Page) {
		attachToPage(p)
	})

	contextAlias := currentContext.alias
	if err := ctx.Route("**/*", func(route playwright.Route) {
		request := route.Request()
		targetHost := ""
		if parsedURL, parseErr := neturl.Parse(request.URL()); parseErr == nil {
			targetHost = parsedURL.Hostname()
		}
		if targetHost != "" {
			for _, blockedHost := range blockedHosts {
				if hostIsOrIsSubdomainOf(targetHost, blockedHost) {
					logger.Log(logger.TYPE_DEBUG, "Blocked request to '%s' matching blocked host '%s'", request.URL(), blockedHost)
					_ = route.Abort()
					return
				}
			}
		}
		if !request.IsNavigationRequest() {
			_ = route.Continue()
			return
		}
		frame := request.Frame()
		if frame == nil || frame.ParentFrame() != nil {
			_ = route.Continue()
			return
		}
		if targetHost == "" {
			_ = route.Continue()
			return
		}
		allowedHost := shared.getAllowedNavigationHost(contextAlias)
		if allowedHost == "" {
			shared.setAllowedNavigationHost(contextAlias, targetHost)
			_ = route.Continue()
			return
		}
		if hostsAreSameSite(targetHost, allowedHost) {
			_ = route.Continue()
			return
		}
		logger.Log(logger.TYPE_DEBUG, "Blocked cross-site redirect attempt to '%s'", request.URL())
		_ = route.Abort()
	}); err != nil {
		logger.Log(logger.TYPE_WARN, "Route registration failed: %v", err)
	}

	return nil
}

/*
hostsAreSameSite is a function which compares two hostnames to determine if they belong to the same site. This treats a
hostname and any of its subdomains as equivalent in either direction (for example "www.xslist.org" and "xslist.org"), while
still distinguishing genuinely different domains, including ones that merely share a suffix (for example "evilxslist.org" is
not treated as the same site as "xslist.org"). This bidirectional equivalence is what the navigation-allowlist gate wants:
whichever host of the two you navigated to first, a subsequent navigation to the other should still be permitted.
*/
func hostsAreSameSite(hostA string, hostB string) bool {
	hostA = strings.ToLower(hostA)
	hostB = strings.ToLower(hostB)
	if hostA == hostB {
		return true
	}
	return strings.HasSuffix(hostA, "."+hostB) || strings.HasSuffix(hostB, "."+hostA)
}

/*
hostIsOrIsSubdomainOf is a function which reports whether host is exactly parent, or a subdomain of parent (for example
"ads.example.com" is a subdomain of "example.com"). Unlike hostsAreSameSite, this is intentionally one-directional: it is
used to decide whether a request host falls under a blocked host, and blocking "ads.example.com" must not also block its
parent "example.com" — only hosts at or below the blocked one should match.
*/
func hostIsOrIsSubdomainOf(host string, parent string) bool {
	host = strings.ToLower(host)
	parent = strings.ToLower(parent)
	return host == parent || strings.HasSuffix(host, "."+parent)
}

/*
selectorEnginePrefixPattern matches a leading "engine=" prefix on a selector string (e.g. "css=",
"xpath=", "role=", "text=", "nth="). Any selector matching this is assumed to already declare its
own engine and is passed through to Playwright untouched — Playwright's own driver validates the
engine name against its registry, so resolveSelector never needs to enumerate known engines itself.
*/
var selectorEnginePrefixPattern = regexp.MustCompile(`^[a-zA-Z_][\w-]*=`)

/*
resolveSelector is a method which formats a selector string for Playwright. Selectors that already
declare an engine prefix (anything matching "word=", e.g. "css=", "xpath=", "role=") are returned
unchanged and left for Playwright itself to validate. A bare selector with no such prefix is
qualified with the instance's default selector engine (see SetDefaultSelectorEngine), which is
SELECTOR_ENGINE_XPATH unless overridden.

Example:

	resolved := shared.resolveSelector("//div[@id='id']")
*/
func (shared *Instance) resolveSelector(selector string) string {
	if selectorEnginePrefixPattern.MatchString(selector) {
		return selector
	}
	engine := shared.defaultSelectorEngine
	if engine == "" {
		engine = SELECTOR_ENGINE_XPATH
	}
	return engine + "=" + selector
}

/*
resolveBaseLocator is a method which builds the starting locator for a selector lookup, scoped to parentLocator when
one is given and to the full page otherwise. This is the single place selector resolution and parent-scoping are wired
together, so every element-lookup method shares identical behavior instead of each re-deriving it.

Example:

	baseLocator := shared.resolveBaseLocator(page, parentLocator, "button.submit")
*/
func (shared *Instance) resolveBaseLocator(page playwright.Page, parentLocator playwright.Locator, selector string) playwright.Locator {
	if parentLocator != nil {
		return parentLocator.Locator(shared.resolveSelector(selector))
	}
	return page.Locator(shared.resolveSelector(selector))
}

/*
isTimeoutError is a function which reports whether err represents a Playwright wait/selector timeout, as opposed to some
other failure (a closed page, a detached context, a malformed selector, etc). This is the single place that string-based
classification lives, so every caller that needs to treat "nothing matched in time" as an empty result rather than a hard
failure agrees on what counts as a timeout.

Example:

	if isTimeoutError(err) { return emptyResult, nil }
*/
func isTimeoutError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "timeout")
}
