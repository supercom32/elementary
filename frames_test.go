package elementary_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/supercom32/elementary"
)

var (
	whereSelector       = elementary.Selector{Value: "css=#where", Description: "location label"}
	outerFrameSelector  = elementary.Selector{Value: "css=iframe#outer", Description: "outer frame"}
	innerFrameSelector  = elementary.Selector{Value: "css=iframe#inner", Description: "inner frame"}
	frameInputSelector  = elementary.Selector{Value: "css=#name", Description: "frame text input"}
	frameUploadSelector = elementary.Selector{Value: "css=#up", Description: "frame file input"}
)

/*
newFrameServer is a test helper which starts a local server with three pages: "/" holds a label reading "main" and
an iframe#outer showing "/outer"; "/outer" holds a label reading "outer", a text input, a file input, and an
iframe#inner showing "/inner"; "/inner" holds a label reading "inner". The server is closed when the test ends.
*/
func newFrameServer(t *testing.T) *httptest.Server {
	t.Helper()
	pages := map[string]string{
		"/":      "<p id='where'>main</p><iframe id='outer' src='/outer'></iframe>",
		"/outer": "<p id='where'>outer</p><input id='name'/><input id='up' type='file'/><iframe id='inner' src='/inner'></iframe>",
		"/inner": "<p id='where'>inner</p>",
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		body, found := pages[request.URL.Path]
		if !found {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(writer, "<html><body>%s</body></html>", body)
	}))
	t.Cleanup(server.Close)
	return server
}

/*
expectWhere is a test helper which asserts that the location label visible in the current frame scope reads
expected and that GetFrameDepth reports expectedDepth.
*/
func expectWhere(t *testing.T, agent *elementary.Instance, expected string, expectedDepth int) {
	t.Helper()
	if text, err := agent.GetElementText(whereSelector, nil); err != nil || text != expected {
		t.Errorf("Expected the label to read %q, got %q (err: %v)", expected, text, err)
	}
	if depth := agent.GetFrameDepth(); depth != expectedDepth {
		t.Errorf("Expected frame depth %d, got %d", expectedDepth, depth)
	}
}

/*
TestSwitchToFrameNavigatesNestedFrames is a test which allows you to verify frame switching through two levels. The
label reads "main" at depth 0, "outer" after SwitchToFrame(outer) at depth 1, and "inner" after SwitchToFrame(inner)
at depth 2. SwitchToParentFrame returns to "outer" at depth 1 and SwitchToMainFrame to "main" at depth 0. Filling and
reading the outer frame's input with "Ada" works while inside it.
*/
func TestSwitchToFrameNavigatesNestedFrames(t *testing.T) {
	server := newFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	expectWhere(t, agent, "main", 0)

	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame(outer) failed: %v", err)
	}
	expectWhere(t, agent, "outer", 1)
	if err := agent.FillElement("Ada", frameInputSelector, nil); err != nil {
		t.Fatalf("FillElement inside the frame failed: %v", err)
	}
	if value, err := agent.GetElementAttribute("value", frameInputSelector, nil); err != nil || value != "Ada" {
		t.Errorf("Expected the frame input to hold \"Ada\", got %q (err: %v)", value, err)
	}
	if err := agent.WaitForElementVisibleStatus(whereSelector, nil, true); err != nil {
		t.Errorf("Expected WaitForElementVisibleStatus to find the label inside the frame, got %v", err)
	}

	if err := agent.SwitchToFrame(innerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame(inner) failed: %v", err)
	}
	expectWhere(t, agent, "inner", 2)

	agent.SwitchToParentFrame()
	expectWhere(t, agent, "outer", 1)
	agent.SwitchToMainFrame()
	expectWhere(t, agent, "main", 0)
	agent.SwitchToParentFrame()
	expectWhere(t, agent, "main", 0)
}

/*
TestSwitchToFrameRejectsNonFramesAndMissingElements is a test which allows you to verify that switching to the
<p id='where'> label returns an error naming it as not an iframe, that a missing selector returns ErrElementNotFound,
and that neither changes the frame depth from 0.
*/
func TestSwitchToFrameRejectsNonFramesAndMissingElements(t *testing.T) {
	server := newFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)

	if err := agent.SwitchToFrame(whereSelector); err == nil || !strings.Contains(err.Error(), "not an iframe") {
		t.Errorf("Expected a not-an-iframe error, got %v", err)
	}
	if err := agent.SwitchToFrame(missingElementSelector, shortActionOptions); !errors.Is(err, elementary.ErrElementNotFound) {
		t.Errorf("Expected ErrElementNotFound, got %v", err)
	}
	expectWhere(t, agent, "main", 0)
}

/*
TestFrameChoiceBelongsToThePage is a test which allows you to verify that after switching into the outer frame,
opening a new tab reports depth 0, and switching back to the original tab also reports depth 0 with the label reading
"main", because the frame choice is not restored.
*/
func TestFrameChoiceBelongsToThePage(t *testing.T) {
	server := newFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame failed: %v", err)
	}

	if err := agent.NewPage("tab2"); err != nil {
		t.Fatalf("NewPage failed: %v", err)
	}
	if depth := agent.GetFrameDepth(); depth != 0 {
		t.Errorf("Expected a new tab to start at depth 0, got %d", depth)
	}
	if !agent.SwitchPage("main", "tab1") {
		t.Fatal("SwitchPage back to tab1 failed")
	}
	expectWhere(t, agent, "main", 0)
}

/*
TestUploadFileInsideAFrame is a test which allows you to verify that UploadFile reaches a file input inside the outer
frame, leaving that input's value ending with "frame-upload.txt".
*/
func TestUploadFileInsideAFrame(t *testing.T) {
	server := newFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	uploadPath := filepath.Join(t.TempDir(), "frame-upload.txt")
	if err := os.WriteFile(uploadPath, []byte("data"), 0o644); err != nil {
		t.Fatalf("Failed to write upload file: %v", err)
	}
	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame failed: %v", err)
	}

	if err := agent.UploadFile(frameUploadSelector, uploadPath); err != nil {
		t.Fatalf("UploadFile inside the frame failed: %v", err)
	}

	if value, err := agent.GetElementAttribute("value", frameUploadSelector, nil); err != nil || !strings.HasSuffix(value, "frame-upload.txt") {
		t.Errorf("Expected the frame file input to hold the upload, got %q (err: %v)", value, err)
	}
}

/*
newCrossSiteFrameServer is a test helper which starts a local server where every frame comes from a different site
than its parent. "/" (served as 127.0.0.1) holds a "main" label and an iframe#outer loading http://localhost:<port>/outer;
"/outer" holds an "outer" label and an iframe#inner loading http://127.0.0.1:<port>/inner; "/inner" holds an "inner"
label. The browser treats 127.0.0.1 and localhost as separate sites, so Chromium runs each frame in its own process.
*/
func newCrossSiteFrameServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		port := request.Host[strings.LastIndex(request.Host, ":")+1:]
		var body string
		switch request.URL.Path {
		case "/":
			body = fmt.Sprintf("<p id='where'>main</p><iframe id='outer' src='http://localhost:%s/outer'></iframe>", port)
		case "/outer":
			body = fmt.Sprintf("<p id='where'>outer</p><input id='name'/><iframe id='inner' src='http://127.0.0.1:%s/inner'></iframe>", port)
		case "/inner":
			body = "<p id='where'>inner</p>"
		default:
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(writer, "<html><body>%s</body></html>", body)
	}))
	t.Cleanup(server.Close)
	return server
}

/*
TestSwitchToFrameReachesCrossSiteFrames is a test which allows you to verify frame switching when every frame comes
from a different site than its parent: the page frames list confirms the outer frame loaded from localhost, the label
reads "outer" at depth 1 and "inner" at depth 2, and filling the cross-site outer frame's input with "Ada" reads back
"Ada".
*/
func TestSwitchToFrameReachesCrossSiteFrames(t *testing.T) {
	server := newCrossSiteFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)

	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame(outer) failed: %v", err)
	}
	expectWhere(t, agent, "outer", 1)
	crossSiteFrameLoaded := false
	for _, frame := range agent.GetCurrentPage().Frames() {
		if strings.HasPrefix(frame.URL(), "http://localhost:") {
			crossSiteFrameLoaded = true
		}
	}
	if !crossSiteFrameLoaded {
		t.Fatal("Expected the outer frame to load from localhost, a different site than the page")
	}
	if err := agent.FillElement("Ada", frameInputSelector, nil); err != nil {
		t.Fatalf("FillElement in the cross-site frame failed: %v", err)
	}
	if value, err := agent.GetElementAttribute("value", frameInputSelector, nil); err != nil || value != "Ada" {
		t.Errorf("Expected the cross-site frame input to hold \"Ada\", got %q (err: %v)", value, err)
	}

	if err := agent.SwitchToFrame(innerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame(inner) failed: %v", err)
	}
	expectWhere(t, agent, "inner", 2)
}

/*
TestNavigationReturnsToTheMainFrame is a test which allows you to verify that NavigateTo and NavigateBack each leave
the frame chosen on the previous document. From inside the outer frame, NavigateTo("/inner") reports depth 0 with the
label reading "inner". Then, after going back to "/" and into the outer frame again, NavigateBack returns to "/inner"
and also reports depth 0 with the label reading "inner".
*/
func TestNavigationReturnsToTheMainFrame(t *testing.T) {
	server := newFrameServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame failed: %v", err)
	}

	navigateOrFail(t, agent, server.URL+"/inner")
	expectWhere(t, agent, "inner", 0)

	navigateOrFail(t, agent, server.URL)
	if err := agent.SwitchToFrame(outerFrameSelector); err != nil {
		t.Fatalf("SwitchToFrame after returning failed: %v", err)
	}
	expectWhere(t, agent, "outer", 1)
	if err := agent.NavigateBack(); err != nil {
		t.Fatalf("NavigateBack failed: %v", err)
	}
	expectWhere(t, agent, "inner", 0)
}
