package elementary_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/supercom32/elementary"
)

/*
TestActionsReturnErrElementNotFoundForMissingElements is a test which allows you to verify that ClickElement,
FillElement, HoverOverElement, and HoverAndClickElement each return an error satisfying
errors.Is(err, ErrElementNotFound) for a selector matching nothing, within a 300ms timeout.
*/
func TestActionsReturnErrElementNotFoundForMissingElements(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body><p>empty</p></body></html>")

	actions := map[string]func() error{
		"ClickElement":         func() error { return agent.ClickElement(missingElementSelector, nil, shortActionOptions) },
		"FillElement":          func() error { return agent.FillElement("text", missingElementSelector, nil, shortActionOptions) },
		"HoverOverElement":     func() error { return agent.HoverOverElement(missingElementSelector, nil, 0, 0, shortActionOptions) },
		"HoverAndClickElement": func() error { return agent.HoverAndClickElement(missingElementSelector, nil, 0, 0, shortActionOptions) },
	}
	for name, action := range actions {
		if err := action(); !errors.Is(err, elementary.ErrElementNotFound) {
			t.Errorf("Expected %s to return ErrElementNotFound, got %v", name, err)
		}
	}
}

/*
TestGetElementAttributeValueReadsLiveValue is a test which allows you to verify the "value" special case. An input
whose markup value is "initial" and which is filled with "typed" is expected to return "typed" (requested as "VALUE"
to cover letter case), while <div value='markup'>, which has no value property, is expected to return "markup".
*/
func TestGetElementAttributeValueReadsLiveValue(t *testing.T) {
	agent := newPlainAgent(t)
	page := "data:text/html,<html><body><div id='plain' value='markup'>x</div>" +
		"<input id='inp' type='text' value='initial' /></body></html>"
	if err := agent.NavigateTo(page); err != nil {
		t.Fatalf("NavigateTo failed: %v", err)
	}
	inputSelector := elementary.Selector{Value: "css=#inp", Description: "text input"}

	if err := agent.FillElement("typed", inputSelector, nil); err != nil {
		t.Fatalf("FillElement failed: %v", err)
	}

	liveValue, err := agent.GetElementAttribute("VALUE", inputSelector, nil)
	if err != nil || liveValue != "typed" {
		t.Errorf("Expected live value \"typed\", got %q (err: %v)", liveValue, err)
	}
	markupValue, err := agent.GetElementAttribute("value", elementary.Selector{Value: "css=#plain"}, nil)
	if err != nil || markupValue != "markup" {
		t.Errorf("Expected markup value \"markup\", got %q (err: %v)", markupValue, err)
	}
}

/*
TestClickElementManuallyClicksTheCenterOfARoundButton is a test which allows you to verify the click lands on the
element's center. The button is a 100x100 circle, so its bounding box's top-left corner lies outside the button and
a corner click would miss it.
*/
func TestClickElementManuallyClicksTheCenterOfARoundButton(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body style='margin:40px'>"+
		"<button id='b' style='width:100px;height:100px;border-radius:50%;border:0;padding:0'"+
		" onclick=\"this.textContent='clicked'\">x</button></body></html>")
	button := elementary.Selector{Value: "css=#b", Description: "round button"}
	element, err := agent.GetElement(button, nil)
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if err := agent.ClickElementManually(elementary.MOUSE_BUTTON_LEFT, element, nil); err != nil {
		t.Fatalf("ClickElementManually failed: %v", err)
	}

	if text, err := agent.GetElementText(button, nil); err != nil || text != "clicked" {
		t.Errorf("Expected the round button to be clicked, got %q (err: %v)", text, err)
	}
}

/*
TestClickElementManuallyRejectsBadInput is a test which allows you to verify that mouseButton "sideways" and a nil
element each return an error instead of clicking.
*/
func TestClickElementManuallyRejectsBadInput(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body><button id='b'>x</button></body></html>")
	element, err := agent.GetElement(elementary.Selector{Value: "css=#b"}, nil)
	if err != nil {
		t.Fatalf("GetElement failed: %v", err)
	}

	if err := agent.ClickElementManually("sideways", element, nil); err == nil || !strings.Contains(err.Error(), "unsupported mouse button") {
		t.Errorf("Expected an unsupported mouse button error, got %v", err)
	}
	if err := agent.ClickElementManually(elementary.MOUSE_BUTTON_LEFT, nil, nil); err == nil {
		t.Error("Expected an error for a nil element")
	}
}

/*
TestMouseClickMoveMouseAndNavigateBackReturnNil is a test which allows you to verify that MouseClick(60, 30) clicks a
full-width button at the top of the page, and that MoveMouse and NavigateBack complete and return nil.
*/
func TestMouseClickMoveMouseAndNavigateBackReturnNil(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body>first</body></html>")
	navigateOrFail(t, agent, "data:text/html,<html><body style='margin:0'><button id='b' style='width:100%;height:60px'"+
		" onclick=\"this.textContent='clicked'\">x</button></body></html>")

	if err := agent.MouseClick(60, 30); err != nil {
		t.Fatalf("MouseClick failed: %v", err)
	}
	if text, _ := agent.GetElementText(elementary.Selector{Value: "css=#b"}, nil); text != "clicked" {
		t.Errorf("Expected MouseClick to click the button, got %q", text)
	}
	if err := agent.MoveMouse(); err != nil {
		t.Errorf("MoveMouse failed: %v", err)
	}
	if err := agent.NavigateBack(); err != nil {
		t.Errorf("NavigateBack failed: %v", err)
	}
}

/*
TestHoverOverElementDoesNotClick is a test which allows you to verify that hovering records "hovered" via onmouseover
but never fires onclick, while HoverAndClickElement fires both, leaving the text "hovered-clicked".
*/
func TestHoverOverElementDoesNotClick(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body><div id='h' style='width:200px;height:50px'"+
		" onmouseover=\"if(!this.dataset.h){this.dataset.h=1;this.textContent='hovered';}\""+
		" onclick=\"this.textContent=this.textContent+'-clicked'\">start</div></body></html>")
	target := elementary.Selector{Value: "css=#h", Description: "hover target"}

	if err := agent.HoverOverElement(target, nil, 0, 0); err != nil {
		t.Fatalf("HoverOverElement failed: %v", err)
	}
	if text, _ := agent.GetElementText(target, nil); text != "hovered" {
		t.Errorf("Expected hover alone to leave %q, got %q", "hovered", text)
	}

	if err := agent.HoverAndClickElement(target, nil, 0, 0); err != nil {
		t.Fatalf("HoverAndClickElement failed: %v", err)
	}
	if text, _ := agent.GetElementText(target, nil); text != "hovered-clicked" {
		t.Errorf("Expected hover and click to leave %q, got %q", "hovered-clicked", text)
	}
}

/*
TestUploadFileDetectsMimeType is a test which allows you to verify that UploadFile labels uploads by extension: a
".png" file is expected to arrive with type "image/png", and a ".unknownext" file with "application/octet-stream".
*/
func TestUploadFileDetectsMimeType(t *testing.T) {
	agent := newPlainAgent(t)
	if err := agent.NavigateTo("data:text/html,<html><body><input id='up' type='file'/></body></html>"); err != nil {
		t.Fatalf("NavigateTo failed: %v", err)
	}
	uploadSelector := elementary.Selector{Value: "css=#up", Description: "file input"}
	tempDirectory := t.TempDir()

	for fileName, expectedType := range map[string]string{
		"photo.png":       "image/png",
		"data.unknownext": "application/octet-stream",
	} {
		filePath := filepath.Join(tempDirectory, fileName)
		if err := os.WriteFile(filePath, []byte("opaque bytes"), 0o644); err != nil {
			t.Fatalf("Failed to write %s: %v", filePath, err)
		}
		if err := agent.UploadFile(uploadSelector, filePath); err != nil {
			t.Fatalf("UploadFile(%s) failed: %v", fileName, err)
		}
		if uploadedType := evaluateString(t, agent, "document.querySelector('#up').files[0].type"); uploadedType != expectedType {
			t.Errorf("Expected %s to upload as %q, got %q", fileName, expectedType, uploadedType)
		}
	}
}

/*
TestUploadFilesSendsBaseNamesAndHonorsTheDefaultEngine is a test which allows you to verify that with the default
selector engine set to CSS, a bare "#up" selector works and two files reach a multiple input as
[["a.png", "image/png"], ["b.txt", "text/plain"]], and that an empty file list returns an error.
*/
func TestUploadFilesSendsBaseNamesAndHonorsTheDefaultEngine(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body><input id='up' type='file' multiple/></body></html>")
	agent.SetDefaultSelectorEngine(elementary.SELECTOR_ENGINE_CSS)
	tempDirectory := t.TempDir()
	var filePaths []string
	for _, fileName := range []string{"a.png", "b.txt"} {
		filePath := filepath.Join(tempDirectory, fileName)
		if err := os.WriteFile(filePath, []byte("data"), 0o644); err != nil {
			t.Fatalf("Failed to write %s: %v", filePath, err)
		}
		filePaths = append(filePaths, filePath)
	}
	uploadSelector := elementary.Selector{Value: "#up"}

	if err := agent.UploadFiles(uploadSelector, filePaths); err != nil {
		t.Fatalf("UploadFiles failed: %v", err)
	}

	result, err := agent.GetCurrentPage().Evaluate("[...document.querySelector('#up').files].map(f => [f.name, f.type])")
	if err != nil {
		t.Fatalf("Failed to read uploaded files: %v", err)
	}
	expected := []any{[]any{"a.png", "image/png"}, []any{"b.txt", "text/plain"}}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected uploaded files %v, got %v", expected, result)
	}
	if err := agent.UploadFiles(uploadSelector, nil); err == nil {
		t.Error("Expected an error for an empty file list")
	}
}

/*
TestNavigateToRejectsAURLWithoutAScheme is a test which allows you to verify that NavigateTo("example.com") returns
an error within a second rather than retrying.
*/
func TestNavigateToRejectsAURLWithoutAScheme(t *testing.T) {
	agent := newPlainAgent(t)
	started := time.Now()

	err := agent.NavigateTo("example.com")

	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Errorf("Expected a missing-scheme error, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Errorf("Expected an immediate rejection, took %s", elapsed)
	}
}

/*
TestNavigateToGivesUpWithinItsTimeout is a test which allows you to verify that navigating to the unreachable
http://127.0.0.1:1/ with a 2 second timeout returns an error after retrying, and never runs much past those 2
seconds.
*/
func TestNavigateToGivesUpWithinItsTimeout(t *testing.T) {
	agent := newPlainAgent(t)
	started := time.Now()

	err := agent.NavigateTo("http://127.0.0.1:1/", elementary.ActionOptions{Timeout: 2 * time.Second})

	if err == nil || !strings.Contains(err.Error(), "failed to navigate") {
		t.Errorf("Expected a navigation failure, got %v", err)
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("Expected NavigateTo to stop near its 2s timeout, took %s", elapsed)
	}
}

/*
TestWaitForPageToChangeReturnsErrorWhenTheURLNeverMatches is a test which allows you to verify that waiting 300ms for
the URL to contain "/never" on a data: page returns a timeout error naming the fragment.
*/
func TestWaitForPageToChangeReturnsErrorWhenTheURLNeverMatches(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body>still</body></html>")

	err := agent.WaitForPageToChange("/never", shortActionOptions)

	if err == nil || !strings.Contains(err.Error(), "/never") {
		t.Errorf("Expected a timeout error naming \"/never\", got %v", err)
	}
}

/*
TestBlockPopupsKeepsTabsOpenedWithNewPage is a test which allows you to verify that a tab opened deliberately with
NewPage("tab2") after BlockPopups is still open one second later.
*/
func TestBlockPopupsKeepsTabsOpenedWithNewPage(t *testing.T) {
	agent := newPlainAgent(t)
	if err := agent.BlockPopups(); err != nil {
		t.Fatalf("BlockPopups failed: %v", err)
	}
	if err := agent.NewPage("tab2"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}
	page := agent.GetCurrentPage()

	time.Sleep(time.Second)

	if page.IsClosed() {
		t.Error("Expected the NewPage tab to stay open after BlockPopups")
	}
}

/*
TestBlockPopupsStillClosesRealPopups is a test which allows you to verify that a popup opened by the page itself, here
a form submitted with target="_blank", is closed, leaving exactly the one original page in the context.
*/
func TestBlockPopupsStillClosesRealPopups(t *testing.T) {
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, "data:text/html,<html><body><form id='f' action='about:blank' target='_blank'></form></body></html>")
	if err := agent.BlockPopups(); err != nil {
		t.Fatalf("BlockPopups failed: %v", err)
	}

	if err := agent.InjectJavaScript("document.getElementById('f').submit()"); err != nil {
		t.Fatalf("InjectJavaScript failed: %v", err)
	}
	time.Sleep(time.Second)

	if pageCount := len(agent.GetCurrentPage().Context().Pages()); pageCount != 1 {
		t.Errorf("Expected the popup to be closed leaving 1 page, got %d", pageCount)
	}
}

/*
TestBlockRedirectsKeepsCurrentPageOnCrossSiteLink is a test which allows you to verify that a blocked cross-site
navigation leaves the tab on its current page instead of Chromium's error page. A local server is reached as
http://127.0.0.1:<port>/start, which links to http://localhost:<port>/other; after BlockRedirects, clicking that link
is expected to leave the URL at /start and the heading text at "start".
*/
func TestBlockRedirectsKeepsCurrentPageOnCrossSiteLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		port := request.Host[strings.LastIndex(request.Host, ":")+1:]
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		if request.URL.Path == "/start" {
			fmt.Fprintf(writer, "<html><body><h1>start</h1><a id='away' href='http://localhost:%s/other'>away</a></body></html>", port)
			return
		}
		fmt.Fprint(writer, "<html><body><h1>other</h1></body></html>")
	}))
	defer server.Close()

	agent := newPlainAgent(t)
	startURL := server.URL + "/start"
	if err := agent.NavigateTo(startURL); err != nil {
		t.Fatalf("NavigateTo failed: %v", err)
	}
	if err := agent.BlockRedirects(); err != nil {
		t.Fatalf("BlockRedirects failed: %v", err)
	}

	if err := agent.ClickElement(elementary.Selector{Value: "css=#away", Description: "cross-site link"}, nil); err != nil {
		t.Fatalf("ClickElement failed: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	if currentURL := agent.GetCurrentUrlAddress(); currentURL != startURL {
		t.Errorf("Expected to stay on %s, got %s", startURL, currentURL)
	}
	heading, err := agent.GetElementText(elementary.Selector{Value: "css=h1"}, nil)
	if err != nil || heading != "start" {
		t.Errorf("Expected the original page content to remain, got heading %q (err: %v)", heading, err)
	}
}
