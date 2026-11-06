[![Go Reference](https://pkg.go.dev/badge/github.com/supercom32/elementary@main.svg)](https://pkg.go.dev/github.com/supercom32/elementary@main)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](https://opensource.org/licenses/MIT)

# Elementary

Elementary is a clean, reliable, and zero-boilerplate browser automation wrapper built natively for Go on top of Playwright. It simplifies browser lifecycle management, session switching, and element interaction into a highly readable, developer-friendly API.

By automating low-level setup, state coordination, and browser-driver recoveries, Elementary lets you focus strictly on writing your automation flows rather than fighting framework boilerplate.

# Features

* Zero-boilerplate browser, context, and page setup with a single function call.
* Logical session and tab aliasing to easily open and jump between multiple isolated contexts and pages.
* Automatic driver repair to programmatically intercept and self-heals corrupted browser binaries or execution timeouts during startup.
* Smart selector auto-resolution supporting both XPath (the default) for advanced DOM traversal and CSS selectors for piercing web component shadow roots, with a one-line override if you'd rather default to CSS.
* Optional stealth patching (Chromium only) that swaps in Patchright's driver patches to evade automated webdriver detectors.

# Why Elementary?

While standard raw Playwright is incredibly capable, it requires significant code overhead to handle basic tasks. Here is a direct comparison of how Elementary simplifies your workflow:

### Descriptive, Reusable Locators

Instead of a bare selector string, methods like `ClickElement` and `GetElementText` accept a `Selector`:

```go
addToCartButton := Elementary.Selector{
    Value:       "css=shop-product-card .add-to-cart-button",
    Description: "add to cart button",
}
browser.ClickElement(addToCartButton, nil)
```

A `Description` makes screenshots, debug logs, and error messages read like "add to cart button" instead of a raw CSS/XPath expression. Declaring the selector once also means you can reuse it everywhere, instead of retyping the same string across your suite. `Description` is optional, so `Elementary.Selector{Value: "css=#submit-button"}` works fine on its own. `Selector` also takes an optional `Timeout`, so a flaky element can declare its own wait threshold a single time instead of at every call site:

```go
slowWidget := Elementary.Selector{
    Value:       "css=#slow-widget",
    Description: "slow widget",
    Timeout:     15 * time.Second,
}
browser.ClickElement(slowWidget, nil)
```

Timeouts are resolved from most to least specific. A timeout passed at the call site always wins. If none is given, the selector's own `Timeout` is used. If that's also unset, the instance's default timeout applies.

A `Value` with no engine prefix (`css=`, `xpath=`, `role=`, `text=`, ...) is resolved as XPath by default, so `Value: "//button[1]"` and `Value: "button"` both work without a prefix. A bare CSS-style selector like `.add-to-cart-button` needs an explicit `css=` prefix, as shown above, since it isn't valid XPath on its own. If most of your selectors are CSS, call `browser.SetDefaultSelectorEngine(Elementary.SELECTOR_ENGINE_CSS)` once to flip the instance-wide default instead of prefixing every selector.

### Setting up a Browser & Page

In standard raw Playwright, you must write ~25 lines of code, handle 4 separate pointer variables, and catch up to 4 potential errors just to safely open a single page:

```go
// raw Playwright
pw, err := playwright.Run()
if err != nil {
    log.Fatalf("could not start playwright: %v", err)
}
defer pw.Stop()

browser, err := pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
    Headless: playwright.Bool(false),
})
if err != nil {
    log.Fatalf("could not launch browser: %v", err)
}
defer browser.Close()

context, err := browser.NewContext()
if err != nil {
    log.Fatalf("could not create context: %v", err)
}
defer context.Close()

page, err := context.NewPage()
if err != nil {
    log.Fatalf("could not create page: %v", err)
}

_, err = page.Goto("https://example.com")
if err != nil {
    log.Fatalf("could not navigate: %v", err)
}
```

In Elementary, this entire pipeline is initialized automatically:

```go
// Elementary
browser := Elementary.Instance{}
err := browser.Initialize("default_session", "main_tab", Elementary.BROWSER_CHROME, 1280, 800, true, nil)
if err != nil {
    log.Fatalf("Failed to initialize: %v", err)
}
defer browser.Close()

browser.NavigateTo("https://example.com")
```

### Multi-User / Multi-Session Interactions (Single Browser)

If you want to automate two users interacting on the same site at the same time (e.g., testing that when User A locks a product in their cart, User B sees it as "unavailable"), standard raw Playwright forces you to pass and manage multiple parallel context and page objects across your functions. 

With Elementary, you simply use aliases to switch back and forth on a single instance:

```go
// Create a separate, isolated session for User B (separate cookies/storage)
browser.NewContext("user_b_session", 1280, 800, nil)
browser.SwitchContext("user_b_session")
browser.NewPage("product_details")
browser.NavigateTo("https://example.com/product/123")

// User A adds the limited-stock product to their cart
browser.SwitchPage("user_a_session", "shopping_cart")
browser.NavigateTo("https://example.com/product/123")
addToCartButton := Elementary.Selector{Value: "css=.add-to-cart-btn", Description: "add to cart button"}
browser.ClickElement(addToCartButton, nil)

// User B checks the same product page and verifies it's now locked/unavailable
browser.SwitchPage("user_b_session", "product_details")
stockStatus := Elementary.Selector{Value: "css=.stock-status", Description: "stock status label"}
status, _ := browser.GetElementText(stockStatus, nil)
// status == "In someone else's cart"
```

### Running Multiple Browsers Simultaneously (e.g., Chrome & Firefox)

To test cross-browser workflows or real-time multi-browser interactions, standard raw Playwright forces you to manage parallel driver processes, launch variables, and separate context and page pointers manually. 

With Elementary, because state is fully encapsulated within each instance, you can spin up Chrome and Firefox side-by-side with almost no boilerplate:

```go
// Chrome Instance
chrome := Elementary.Instance{}
chrome.Initialize("chrome_session", "tab_1", Elementary.BROWSER_CHROME, 1280, 800, true, nil)
chrome.NavigateTo("https://example.com")

// Firefox Instance (runs simultaneously in a separate process)
firefox := Elementary.Instance{}
firefox.Initialize("firefox_session", "tab_1", Elementary.BROWSER_FIREFOX, 1280, 800, true, nil)
firefox.NavigateTo("https://example.com")

// Both instances automate independently and simultaneously
checkoutButton := Elementary.Selector{Value: "css=.checkout-btn", Description: "checkout button"}
chrome.ClickElement(checkoutButton, nil)
firefox.ClickElement(checkoutButton, nil)
```

### Selective Error Handling (Optional Steps)

In many real-world flows, some steps are strictly critical (like navigating to a product or clicking purchase), while other steps are completely optional (like attempting to dismiss a promotional pop-up newsletter modal on page entry). 

With Elementary, you can cleanly run optional steps directly without assigning or catching their errors. If the optional element isn't present, the error is quietly ignored, and execution continues quietly:

```go
// 1. Critical Step: We must load the main shop page
err := browser.NavigateTo("https://example.com/shop")
if err != nil {
    log.Fatalf("Shop page failed to load: %v", err)
}

// 2. Optional Step: Attempt to close the promotional newsletter banner. 
// If the banner does not appear during this session, the click fails, but 
// we completely ignore the error and proceed without any assignment boilerplate.
newsletterCloseButton := Elementary.Selector{Value: "css=#close-newsletter-popup-btn", Description: "newsletter popup close button"}
browser.ClickElement(newsletterCloseButton, nil)

// 3. Critical Step: Click the target product card. This must succeed.
addToCartButton := Elementary.Selector{Value: "css=shop-product-card .add-to-cart-button", Description: "add to cart button"}
err = browser.ClickElement(addToCartButton, nil)
if err != nil {
    log.Fatalf("Fatal: Failed to add item to cart: %v", err)
}
```

# Installation

```bash
go get github.com/supercom32/elementary
```

# Stealth Patching (Chromium only)

Elementary can patch the Playwright driver with [Patchright](https://github.com/Kaliiiiiiiiii-Vinyzu/patchright)'s stealth patches before launch, for stronger anti-detection than injecting JS into the page. Enable it via `BrowserOptions.StealthPatch`:

```go
browser := Elementary.Instance{}
err := browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, true, &Elementary.BrowserOptions{
    StealthPatch: true,
})
```

This is experimental and Chromium-only; passing it with `Elementary.BROWSER_FIREFOX` returns an error. It requires network access during `Initialize` to fetch a matching `patchright-core` release for your vendored Playwright driver version, and downloads real third-party content, so only enable it where that's acceptable.

The patch is never applied in place. On first use, Elementary builds a separate, patched copy of the driver in a sibling directory (e.g. `~/.cache/ms-playwright-go/1.61.1-stealth` next to `1.61.1`) and leaves the original driver install untouched. This means instances with and without `StealthPatch` can run side by side against the same cache without either affecting the other, and there's nothing to "corrupt": the original is only ever read, never written. Call `browser.RestoreStealthPatch()` (after `Close()`) to delete the patched copy and force it to be rebuilt on the next stealth-enabled `Initialize`.

## Driving a Branded Browser Channel

`BrowserOptions.Channel` drives an already-installed branded browser (e.g. `"chrome"`, `"msedge"`) instead of the bundled Chromium build, matching Playwright's own `channel` launch option:

```go
browser := Elementary.Instance{}
err := browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, true, &Elementary.BrowserOptions{
    Channel: "chrome",
})
```

This lets you target your local, real browser instead of Elementary's managed one. The requested channel must already be installed on the host. It's useful for evading browser fingerprinting, since a real Chrome install looks different (TLS fingerprint, version stamp, etc.) than the bundled Chromium build. `StealthPatch` is unaffected either way, since it patches the driver rather than the browser binary, so it applies regardless of which channel you launch.

Valid values are `"chrome"`, `"chrome-beta"`, `"chrome-dev"`, `"chrome-canary"`, `"msedge"`, `"msedge-beta"`, `"msedge-dev"`, and `"msedge-canary"`. The default, an empty string, uses Elementary's bundled/managed Chromium build rather than any locally-installed browser.

## Overriding Launch Arguments

`BrowserOptions.Args` overrides the command-line arguments passed to the browser at launch, matching Playwright's own `Args` launch option. When left `nil`, Chromium launches with Elementary's default (`--mute-audio`) and Firefox launches with none:

```go
browser := Elementary.Instance{}
err := browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, true, &Elementary.BrowserOptions{
    Args: []string{"--mute-audio", "--lang=fr-FR"},
})
```

Setting `Args` replaces the list entirely rather than appending to it. Elementary's Chromium default is `--mute-audio`. If you want to keep it alongside your own flags, include it explicitly as shown above.

## Routing Traffic Through a Proxy

`BrowserOptions.Proxy` sends all of the browser's traffic through a proxy server, so sites see the proxy's address instead of yours. HTTP and SOCKS proxies are supported, and `Bypass`, `Username`, and `Password` are optional:

```go
err := browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, false, &Elementary.BrowserOptions{
    Proxy: &Elementary.ProxyOptions{Server: "http://myproxy.com:3128", Username: "user", Password: "secret"},
})
```

## Saving and Restoring a Login Session

`SaveStorageState` writes the current context's cookies and local storage to a JSON file. Pass that file as `BrowserOptions.StorageStatePath` to start a later session already logged in, including on sites that keep login tokens in local storage rather than cookies. Treat the file like a password, since it holds live session credentials:

```go
err := browser.SaveStorageState("session.json")

// later, in a new run
err = browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, false, &Elementary.BrowserOptions{
    StorageStatePath: "session.json",
})
```

## Handling Pop-up Dialogs

JavaScript `alert`, `confirm`, and `prompt` boxes never block a script: every dialog is answered automatically, by default with Cancel. `SetDialogOptions` changes the answer for every page from then on, and `GetDialogs` lists what was shown:

```go
browser.SetDialogOptions(Elementary.DialogOptions{Accept: true, PromptText: "yes"})
browser.ClickElement(deleteButton, nil) // the "Are you sure?" box is answered with OK

for _, dialog := range browser.GetDialogs() {
    fmt.Println(dialog.Type, dialog.Message)
}
```

## Downloading Files

`DownloadByClicking` clicks a link or button and saves the file it downloads, returning where it was saved. `WaitForDownload` does the same for any other trigger. The site's suggested file name is kept but made safe, and an existing file is never overwritten (a number is added instead, as in `report (1).pdf`):

```go
path, err := browser.DownloadByClicking(invoiceLink, "./downloads", nil)
```

## Working Inside Iframes

Payment forms, login widgets, and embedded editors often live inside an `<iframe>`. `SwitchToFrame` moves every element method (find, click, fill, read, wait, hover, upload, form controls, and downloads) inside that frame. Call it again to go into a frame nested inside it, `SwitchToParentFrame` to step out one level, and `SwitchToMainFrame` to return to the page. Changing to another tab or context, or calling `NavigateTo` or `NavigateBack`, always starts back on the main page:

```go
paymentFrame := Elementary.Selector{Value: "css=iframe#payment", Description: "payment frame"}
browser.SwitchToFrame(paymentFrame)
browser.FillElement("4242 4242 4242 4242", cardNumberField, nil)
browser.SwitchToMainFrame()
```

JavaScript evaluation, `ScrollPageBy`, and mouse coordinate methods still act on the page as a whole.

# Example

Here is a highly compact, single-browser example that demonstrates almost all of Elementary's core features (setup, timeouts, multiple tabs, selector auto-detection, screenshots, attributes, and visibility checks) in one short script.

```go
package main

import (
	"fmt"
	"log"
	"time"

	"github.com/supercom32/elementary"
)

func main() {
	// 1. Initialize our browser with our primary tab ("shop")
	browser := Elementary.Instance{}
	err := browser.Initialize("default_ctx", "shop", Elementary.BROWSER_CHROME, 1280, 800, true, nil)
	if err != nil {
		log.Fatalf("Initialization failed: %v", err)
	}
	defer browser.Close()

	// 2. Configure default timeouts globally once
	browser.SetTimeout(5 * time.Second)
	browser.SetNavigationTimeout(30 * time.Second)

	// --- SHOP TAB FLOW ---
	// Navigate to site and add a limited product to our cart (using CSS to pierce Shadow DOM)
	browser.NavigateTo("https://example.com/shop")
	addToCartButton := Elementary.Selector{Value: "css=shop-product-card .add-to-cart-button", Description: "add to cart button"}
	browser.ClickElement(addToCartButton, nil)

	// 3. Open a separate tab ("receipt") to verify real-time checkout updates
	browser.NewPage("receipt")
	browser.NavigateTo("https://example.com/checkout/status")

	// Read order attribute values and textual elements
	checkoutSummary := Elementary.Selector{Value: "css=checkout-summary", Description: "checkout summary"}
	statusMessage := Elementary.Selector{Value: "//div[@id='status-message']", Description: "status message"}
	price, _ := browser.GetElementAttribute("data-price", checkoutSummary, nil)
	status, _ := browser.GetElementText(statusMessage, nil)
	fmt.Printf("Order Status: %s | Price: %s\n", status, price)

	// Check if a modal is visible; if so, dismiss it using a custom inline timeout override
	cookieConsentModal := Elementary.Selector{Value: "//div[@class='cookie-consent']", Description: "cookie consent modal"}
	if browser.IsElementVisible(cookieConsentModal, nil) {
		opts := Elementary.ActionOptions{Timeout: 2 * time.Second}
		dismissButton := Elementary.Selector{Value: "//button[contains(text(), 'Dismiss')]", Description: "dismiss button"}
		browser.ClickElement(dismissButton, nil, opts)
	}

	// Switch back to the original "shop" tab and take a debug screenshot
	browser.SwitchPage("default_ctx", "shop")
	browser.SaveScreenshot("./debug", "final_shop_state.png")
}
```
