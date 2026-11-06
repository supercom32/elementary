package elementary_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/supercom32/elementary"
)

/*
TestProxyRoutesTrafficThroughTheProxyServer is a test which allows you to verify that with
BrowserOptions{Proxy: &ProxyOptions{Server: <local proxy>}}, navigating to the made-up http://elementary.test/ is
answered by the local proxy, which receives the request for host "elementary.test", and that an empty proxy Server
is rejected before the browser launches.
*/
func TestProxyRoutesTrafficThroughTheProxyServer(t *testing.T) {
	var proxiedRequests atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Host == "elementary.test" {
			proxiedRequests.Add(1)
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(writer, "<html><body><h1 id='via'>proxied %s</h1></body></html>", request.URL.Host)
	}))
	defer proxy.Close()

	var agent elementary.Instance
	err := agent.Initialize("main", "tab1", elementary.BROWSER_CHROME, 1024, 768, false, &elementary.BrowserOptions{
		Proxy: &elementary.ProxyOptions{Server: proxy.URL},
	})
	if err != nil {
		t.Fatalf("Initialize with proxy failed: %v", err)
	}
	defer agent.Close()

	navigateOrFail(t, &agent, "http://elementary.test/")

	heading, err := agent.GetElementText(elementary.Selector{Value: "css=#via"}, nil)
	if err != nil || heading != "proxied elementary.test" {
		t.Errorf("Expected the page to come from the proxy, got %q (err: %v)", heading, err)
	}
	if proxiedRequests.Load() == 0 {
		t.Error("Expected the proxy to receive the request for elementary.test")
	}

	var invalid elementary.Instance
	err = invalid.Initialize("main", "tab1", elementary.BROWSER_CHROME, 1024, 768, false, &elementary.BrowserOptions{
		Proxy: &elementary.ProxyOptions{Server: " "},
	})
	if err == nil || !strings.Contains(err.Error(), "proxy server must not be empty") {
		_ = invalid.Close()
		t.Errorf("Expected an empty proxy server to be rejected, got %v", err)
	}
}
