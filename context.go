package elementary

import (
	"fmt"
	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
	"math/rand"
)

/*
getRandomViewportVariation is a function which yields a pseudo-random offset within a range of 0 to 200 pixels. This
introduces dynamic size variations across browser contexts, preventing static fingerprinting by heuristic bot blockers.
In addition, the following should be noted:

- This function relies on Go's auto-seeded global random generator to ensure high-entropy, unique sequence generation across calls.

Example:

	variation := getRandomViewportVariation()
*/
func getRandomViewportVariation() int {
	return rand.Intn(201)
}

/*
hasActiveContextUnlocked is a method which reports whether currentContextIndex currently points at a valid entry in
contextInstances. This is the single place the "is there an active browser context" bounds check lives, so every
context- and cookie-management method that depends on it shares identical behavior instead of each re-deriving it —
a change to how contexts are tracked only has to be reflected here once. Callers must already hold stateMutex.

Example:

	if !shared.hasActiveContextUnlocked() { return errors.New("no active context") }
*/
func (shared *Instance) hasActiveContextUnlocked() bool {
	return shared.currentContextIndex != NULL_CONTEXT && shared.currentContextIndex < len(shared.contextInstances)
}

/*
ResetContext is a method which closes and regenerates a specified browser context. This discards all active cookies,
cached storage pools, and session tracking states, returning the environment to a pristine state.

Example:

	agent.ResetContext("main-session", 1920, 1080, nil)
*/
func (shared *Instance) ResetContext(contextAlias string, viewportWidth int, viewportHeight int, browserOptions *BrowserOptions) {
	shared.CloseContext(contextAlias)
	_ = shared.NewContext(contextAlias, viewportWidth, viewportHeight, browserOptions)
}

/*
NewContext is a method which instantiates an isolated browser context mapping. This facilitates multi-session setups,
allowing concurrent page pipelines to execute with customized dimensions and distinct request headers. In addition,
the following should be noted:

  - This method automatically adds random viewport variations to the requested size. This prevents automated web servers from detecting
    identical static resolution fingerprints across repeatedly spawned browser automation sessions.

Example:

	err := agent.NewContext("guest-session", 1280, 720, nil)
*/
func (shared *Instance) NewContext(aliasName string, viewportWidth int, viewportHeight int, browserOptions *BrowserOptions) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	var newContextEntry contextType
	var err error
	if aliasName == "" {
		return fmt.Errorf("your specified alias name must not be empty")
	}
	var options playwright.BrowserNewContextOptions

	var viewport playwright.Size
	if viewportWidth != 0 && viewportHeight != 0 {
		viewport.Width = viewportWidth + getRandomViewportVariation()
		viewport.Height = viewportHeight + getRandomViewportVariation()
	} else {
		viewport.Width = 1280 + getRandomViewportVariation()
		viewport.Height = 720 + getRandomViewportVariation()
	}
	options.Viewport = &viewport

	if browserOptions == nil {
		if shared.browserOptions.UserAgent != "" {
			options.UserAgent = &shared.browserOptions.UserAgent
		}
	}
	if shared.browserInstance == nil {
		return logger.Error(fmt.Errorf("browser instance is not initialized"), "failed to create new browser context for alias %s", aliasName)
	}
	newContextEntry.instance, err = shared.browserInstance.NewContext(options)
	if err != nil {
		return logger.Error(fmt.Errorf("playwright context creation failed: %w", err), "failed to create new browser context for alias %s", aliasName)
	}
	newContextEntry.alias = aliasName
	newContextEntry.instance.SetDefaultNavigationTimeout(float64(shared.defaultNavigationTimeout.Milliseconds()))
	shared.contextInstances = append(shared.contextInstances, newContextEntry)
	shared.currentContextIndex = len(shared.contextInstances) - 1
	shared.currentPage = nil
	shared.currentPageIndex = NULL_PAGE
	return nil
}

/*
NewPage is a method which allocates and configures a brand-new page inside the active context. This registers a logical tab
where DOM traversals and automation scripts can execute independently of other tabs.

Example:

	err := agent.NewPage("dashboard-tab")
*/
func (shared *Instance) NewPage(aliasName string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	var newPageEntry pageType
	if aliasName == "" {
		return fmt.Errorf("your specified alias name must not be empty")
	}

	if !shared.hasActiveContextUnlocked() {
		return logger.Error(fmt.Errorf("no active browser context exists to create page"), "NewPage failed for page alias %s", aliasName)
	}

	page, err := shared.contextInstances[shared.currentContextIndex].instance.NewPage()
	if err != nil {
		return logger.Error(fmt.Errorf("playwright failed to allocate page: %w", err), "NewPage failed for page alias %s", aliasName)
	}

	newPageEntry.instance = page
	newPageEntry.alias = aliasName
	shared.contextInstances[shared.currentContextIndex].pageInstances =
		append(shared.contextInstances[shared.currentContextIndex].pageInstances, newPageEntry)
	shared.currentPageIndex = len(shared.contextInstances[shared.currentContextIndex].pageInstances) - 1
	shared.currentPage = shared.contextInstances[shared.currentContextIndex].pageInstances[shared.currentPageIndex].instance
	return nil
}

/*
GetCurrentPage is a method which returns the active Playwright Page instance. This permits access to granular, low-level Playwright
methods that are not exposed via standard framework wrappers.

Example:

	page := agent.GetCurrentPage()
*/
func (shared *Instance) GetCurrentPage() playwright.Page {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	return shared.currentPage
}

/*
InjectJavaScript is a method which evaluates arbitrary JavaScript expressions on the active page. This is suited for querying
ephemeral DOM attributes, firing non-standard browser events, or mutating client-side variables dynamically.

Example:

	err := agent.InjectJavaScript("console.log('injected');")
*/
func (shared *Instance) InjectJavaScript(script string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.currentPage == nil {
		return fmt.Errorf("no page is currently active")
	}
	_, err := shared.currentPage.Evaluate(script)
	if err != nil {
		return logger.Error(err, "failed to inject javascript")
	}
	return nil
}

/*
SwitchContext is a method which transfers structural focus to a distinct browser context by alias. This redirects the target
context pool, maintaining isolated cookiestores and state profiles across browser pipelines. In addition, the following should be noted:

- This method will return an error if the requested context alias is not found in the instances list, preventing silent state failures.

- This method clears the active page reference on success, ensuring the developer explicitly switches to a page under the new context.

Example:

	err := agent.SwitchContext("admin-session")
*/
func (shared *Instance) SwitchContext(contextAlias string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	contextIndex, _ := shared.getContextInstanceUnlocked(contextAlias)
	if contextIndex == NULL_CONTEXT {
		return logger.Error(fmt.Errorf("context '%s' does not exist", contextAlias), "could not switch context")
	}
	shared.currentContextIndex = contextIndex
	shared.currentPage = nil
	shared.currentPageIndex = NULL_PAGE
	return nil
}

/*
SwitchPage is a method which coordinates page targeting by shifting execution focus to a specified logical tab. This allows navigating
between parent windows and spawned popup views seamlessly within the targeted context structure.

Example:

	switched := agent.SwitchPage("guest-session", "landing-tab")
*/
func (shared *Instance) SwitchPage(contextAlias string, pageAlias string) bool {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	contextIndex, _ := shared.getContextInstanceUnlocked(contextAlias)
	pageIndex, pageInstance := shared.getPageInstanceUnlocked(contextAlias, pageAlias)
	if pageIndex == NULL_PAGE {
		return false
	}
	shared.currentPage = pageInstance.instance
	shared.currentPageIndex = pageIndex
	shared.currentContextIndex = contextIndex
	return true
}

/*
GetPageInstance is a method which returns tracking data and reference points for a specific context page. This retrieves internal
slice pointers and logical descriptors using context and page descriptors.

Example:

	idx, page := agent.GetPageInstance("user-session", "checkout-tab")
*/
func (shared *Instance) GetPageInstance(contextAlias string, pageAlias string) (int, *pageType) {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	return shared.getPageInstanceUnlocked(contextAlias, pageAlias)
}

func (shared *Instance) getPageInstanceUnlocked(contextAlias string, pageAlias string) (int, *pageType) {
	_, contextInstance := shared.getContextInstanceUnlocked(contextAlias)
	for currentPageIndex, currentPageEntry := range contextInstance.pageInstances {
		if currentPageEntry.alias == pageAlias {
			return currentPageIndex, &currentPageEntry
		}
	}
	return NULL_PAGE, nil
}

/*
GetArrayOfUntrackedPageIndexes is a method which scans the active context for browser pages loaded outside of framework tracking.
This detects auxiliary popup windows, dynamic redirects, and nested tabs that bypassed the NewPage registry.

Example:

	indexes := agent.GetArrayOfUntrackedPageIndexes()
*/
func (shared *Instance) GetArrayOfUntrackedPageIndexes() []int {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	return shared.getArrayOfUntrackedPageIndexesUnlocked()
}

func (shared *Instance) getArrayOfUntrackedPageIndexesUnlocked() []int {
	var arrayOfUntrackedPageIndexes []int
	if !shared.hasActiveContextUnlocked() {
		return nil
	}
	arrayOfLoadedPages := shared.contextInstances[shared.currentContextIndex].instance.Pages()
	for currentLoadedPageIndex, currentLoadedPage := range arrayOfLoadedPages {
		isPageFound := false
		for _, currentTrackedPageInstance := range shared.contextInstances[shared.currentContextIndex].pageInstances {
			if currentTrackedPageInstance.instance == currentLoadedPage {
				isPageFound = true
			}
		}
		if isPageFound == false {
			arrayOfUntrackedPageIndexes = append(arrayOfUntrackedPageIndexes, currentLoadedPageIndex)
		}
	}
	return arrayOfUntrackedPageIndexes
}

/*
SwitchToUntrackedPage is a method which re-targets page execution focus to an untracked index slot. This gains control over recently
discovered browser tabs or child windows spawned by user interactions or redirection blocks.

Example:

	success := agent.SwitchToUntrackedPage(1)
*/
func (shared *Instance) SwitchToUntrackedPage(pageIndex int) bool {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	return shared.switchToUntrackedPageUnlocked(pageIndex)
}

func (shared *Instance) switchToUntrackedPageUnlocked(pageIndex int) bool {
	if pageIndex <= 0 {
		return false
	}
	arrayOfUntrackedPageIndexes := shared.getArrayOfUntrackedPageIndexesUnlocked()
	if len(arrayOfUntrackedPageIndexes) <= pageIndex-1 {
		return false
	}
	if !shared.hasActiveContextUnlocked() {
		return false
	}
	shared.currentPageIndex = -1
	shared.currentPage = shared.contextInstances[shared.currentContextIndex].instance.Pages()[arrayOfUntrackedPageIndexes[pageIndex-1]]
	return true
}

/*
SwitchToSpawnedPage is a method which shifts active context focus to the most recently detected unmapped tab. This helps coordinate Page
manipulation during complex identity provider flows, SSO checks, or multi-window dialog runs.

Example:

	success := agent.SwitchToSpawnedPage()
*/
func (shared *Instance) SwitchToSpawnedPage() bool {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	arrayOfSpawnedPages := shared.getArrayOfUntrackedPageIndexesUnlocked()
	if len(arrayOfSpawnedPages) != 0 {
		return shared.switchToUntrackedPageUnlocked(len(arrayOfSpawnedPages))
	}
	return false
}

/*
removePage is a method which removes a tracked page entry from an internal tracking slice. This updates the underlying slice structures
to reflect closed or dismantled browser tabs accurately.

Example:

	slice = shared.removePage(slice, index)
*/
func (shared *Instance) removePage(slice []pageType, sliceIndex int) []pageType {
	return append(slice[:sliceIndex], slice[sliceIndex+1:]...)
}

/*
ClosePage is a method which shuts down the active page and redirects focus to the preceding tab in the active context stack. This is
useful for recycling transient browser windows and maintaining stable tab focusing patterns.

Example:

	agent.ClosePage()
*/
func (shared *Instance) ClosePage() {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.currentPage == nil || shared.currentPageIndex == NULL_PAGE {
		return
	}
	_ = shared.currentPage.Close()
	if !shared.hasActiveContextUnlocked() {
		shared.currentPage = nil
		shared.currentPageIndex = NULL_PAGE
		return
	}
	if shared.currentPageIndex != UNTRACKED_PAGE && shared.currentPageIndex < len(shared.contextInstances[shared.currentContextIndex].pageInstances) {
		shared.contextInstances[shared.currentContextIndex].pageInstances = shared.removePage(shared.contextInstances[shared.currentContextIndex].pageInstances, shared.currentPageIndex)
	}
	if len(shared.contextInstances[shared.currentContextIndex].pageInstances) != 0 {
		shared.currentPageIndex = len(shared.contextInstances[shared.currentContextIndex].pageInstances) - 1
		shared.currentPage = shared.contextInstances[shared.currentContextIndex].pageInstances[shared.currentPageIndex].instance
	} else {
		shared.currentPage = nil
		shared.currentPageIndex = NULL_PAGE
	}
}

/*
CloseContext is a method which tears down a target browser context and terminates its associated web pages. This releases allocated
session memory and closes subprocess threads tied to the designated virtual profile.

Example:

	agent.CloseContext("temporary-session")
*/
func (shared *Instance) CloseContext(contextAlias string) {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	contextIndexFound, contextFound := shared.getContextInstanceUnlocked(contextAlias)
	if contextIndexFound == NULL_PAGE || contextIndexFound == NULL_CONTEXT {
		return
	}
	for _, pageEntry := range contextFound.pageInstances {
		_ = pageEntry.instance.Close()
	}
	if len(shared.contextInstances) == 0 {
		return
	}
	if shared.currentContextIndex >= 0 && shared.currentContextIndex < len(shared.contextInstances) {
		currentContextInUse := shared.contextInstances[shared.currentContextIndex]
		if contextFound.alias == currentContextInUse.alias {
			shared.currentPage = nil
			shared.currentPageIndex = UNTRACKED_PAGE
			if len(shared.contextInstances) > 1 {
				shared.currentContextIndex = len(shared.contextInstances) - 2
			} else {
				shared.currentContextIndex = NULL_CONTEXT
			}
		} else {
			if contextIndexFound < shared.currentContextIndex {
				shared.currentContextIndex--
			}
		}
	}
	shared.removeContextInstanceUnlocked(contextIndexFound)
	if len(shared.contextInstances) == 0 {
		shared.currentContextIndex = NULL_CONTEXT
	}
}

/*
CloseAllContextInstances is a method which closes all active browser contexts managed by the instance. This guarantees thorough resource
cleanup, terminating any lingering headless sandboxes upon agent shutdown.

Example:

	agent.CloseAllContextInstances()
*/
func (shared *Instance) CloseAllContextInstances() {
	shared.stateMutex.Lock()
	aliases := make([]string, len(shared.contextInstances))
	for i, contextInstance := range shared.contextInstances {
		aliases[i] = contextInstance.alias
	}
	shared.stateMutex.Unlock()

	for _, alias := range aliases {
		shared.CloseContext(alias)
	}
}

/*
removeContextInstance is a method which deletes a tracked context reference from the runtime context slice. This adjusts internal
slice maps as contexts are shutdown or recycled.

Example:

	shared.removeContextInstance(0)
*/
func (shared *Instance) removeContextInstance(contextIndex int) {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	shared.removeContextInstanceUnlocked(contextIndex)
}

func (shared *Instance) removeContextInstanceUnlocked(contextIndex int) {
	if contextIndex >= 0 && contextIndex < len(shared.contextInstances) {
		shared.contextInstances = append(shared.contextInstances[:contextIndex], shared.contextInstances[contextIndex+1:]...)
	}
}

/*
getContextInstance is a method which looks up a tracked browser context entry by its configured alias. This retrieves context pointers and
internal tracking dimensions.

Example:

	idx, ctx := shared.getContextInstance("session-alias")
*/
func (shared *Instance) getContextInstance(contextAlias string) (int, contextType) {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	return shared.getContextInstanceUnlocked(contextAlias)
}

func (shared *Instance) getContextInstanceUnlocked(contextAlias string) (int, contextType) {
	var contextIndexFound int = NULL_CONTEXT
	var contextInstanceFound contextType
	for contextEntryIndex, contextEntry := range shared.contextInstances {
		if contextEntry.alias == contextAlias {
			contextIndexFound = contextEntryIndex
			contextInstanceFound = contextEntry
		}
	}
	return contextIndexFound, contextInstanceFound
}

/*
setAllowedNavigationHostForCurrentContext is a method which records host as the legitimate navigation target for whichever
context is current at the moment the lock is acquired. Resolving "the current context" and writing its host must happen
under a single lock acquisition — splitting it into a separate "read current alias" call followed by a separate "write by
alias" call leaves a window where a concurrent SwitchContext/CloseContext could change which context is current in between,
attaching the host to the wrong (or a since-removed) context.
*/
func (shared *Instance) setAllowedNavigationHostForCurrentContext(host string) {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if shared.currentContextIndex < 0 || shared.currentContextIndex >= len(shared.contextInstances) {
		return
	}
	shared.contextInstances[shared.currentContextIndex].allowedNavigationHost = host
}

/*
getAllowedNavigationHost is a method which returns the hostname that BlockRedirects currently permits the main frame of the
target context to navigate to. This is updated automatically whenever NavigateTo is called.
*/
func (shared *Instance) getAllowedNavigationHost(contextAlias string) string {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	if contextIndex, contextInstance := shared.getContextInstanceUnlocked(contextAlias); contextIndex != NULL_CONTEXT {
		return contextInstance.allowedNavigationHost
	}
	return ""
}

/*
setAllowedNavigationHost is a method which records the hostname that BlockRedirects should treat as the legitimate navigation
target for the given context. Cross-host main frame navigations are aborted while this policy is active.
*/
func (shared *Instance) setAllowedNavigationHost(contextAlias string, host string) {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if contextIndex, _ := shared.getContextInstanceUnlocked(contextAlias); contextIndex != NULL_CONTEXT {
		shared.contextInstances[contextIndex].allowedNavigationHost = host
	}
}

/*
GetCurrentUrlAddress is a method which returns the active location address of the current page. This allows checking navigation progress
or evaluating target redirect chains against expected route values.

Example:

	url := agent.GetCurrentUrlAddress()
*/
func (shared *Instance) GetCurrentUrlAddress() string {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	if shared.currentPage == nil {
		return ""
	}
	return shared.currentPage.URL()
}
