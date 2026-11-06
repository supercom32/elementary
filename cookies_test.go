package elementary_test

import (
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/supercom32/elementary"
)

/*
TestAddCookieDefaultsToSessionCookie is a test which allows you to verify that AddCookie(".example.com", "a", "b")
with no CookieOptions produces exactly one cookie with Path "/", Secure false, HttpOnly false, and Expires -1 (a
session cookie).
*/
func TestAddCookieDefaultsToSessionCookie(t *testing.T) {
	agent := newPlainAgent(t)

	if err := agent.AddCookie(".example.com", "a", "b"); err != nil {
		t.Fatalf("AddCookie failed: %v", err)
	}

	cookies, err := agent.GetCookies()
	if err != nil {
		t.Fatalf("GetCookies failed: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Path != "/" || cookie.Secure || cookie.HttpOnly || cookie.Expires != -1 {
		t.Errorf("Expected a session cookie on path \"/\" with no flags, got %+v", cookie)
	}
}

/*
TestAddCookieWithOptions is a test which allows you to verify that every CookieOptions field is applied. The input
is CookieOptions{Path: "/app", Secure: true, HTTPOnly: true, SameSite: "Strict", ExpiresIn: time.Hour}, and the
expected cookie has Path "/app", Secure true, HttpOnly true, SameSite "Strict", and an Expires timestamp within a
minute of one hour from now.
*/
func TestAddCookieWithOptions(t *testing.T) {
	agent := newPlainAgent(t)

	err := agent.AddCookie(".example.com", "session_token", "secret", elementary.CookieOptions{
		Path:      "/app",
		Secure:    true,
		HTTPOnly:  true,
		SameSite:  "Strict",
		ExpiresIn: time.Hour,
	})
	if err != nil {
		t.Fatalf("AddCookie failed: %v", err)
	}

	cookies, err := agent.GetCookies()
	if err != nil {
		t.Fatalf("GetCookies failed: %v", err)
	}
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Path != "/app" || !cookie.Secure || !cookie.HttpOnly {
		t.Errorf("Expected path \"/app\" with Secure and HttpOnly set, got %+v", cookie)
	}
	if cookie.SameSite == nil || string(*cookie.SameSite) != "Strict" {
		t.Errorf("Expected SameSite \"Strict\", got %v", cookie.SameSite)
	}
	expectedExpiry := float64(time.Now().Add(time.Hour).Unix())
	if math.Abs(cookie.Expires-expectedExpiry) > 60 {
		t.Errorf("Expected Expires near %.0f, got %.0f", expectedExpiry, cookie.Expires)
	}
}

/*
TestSaveNetscapeCookiesPreservesPath is a test which allows you to verify that a cookie added with Path "/app" is
written to the Netscape file with "/app" in its path column, rather than always being written as "/".
*/
func TestSaveNetscapeCookiesPreservesPath(t *testing.T) {
	agent := newPlainAgent(t)
	if err := agent.AddCookie(".example.com", "scoped", "value", elementary.CookieOptions{Path: "/app"}); err != nil {
		t.Fatalf("AddCookie failed: %v", err)
	}

	cookieFile := filepath.Join(t.TempDir(), "cookies.txt")
	if err := agent.SaveNetscapeCookies(cookieFile); err != nil {
		t.Fatalf("SaveNetscapeCookies failed: %v", err)
	}

	contents, err := os.ReadFile(cookieFile)
	if err != nil {
		t.Fatalf("Failed to read saved cookie file: %v", err)
	}
	if !strings.Contains(string(contents), ".example.com\tTRUE\t/app\tFALSE\t") {
		t.Errorf("Expected the saved line to carry path \"/app\", got %q", contents)
	}
}

/*
TestStorageStateRestoresLocalStorage is a test which allows you to verify session saving. A page on a local server
stores localStorage "token" = "abc123", SaveStorageState writes it to a file, and a new context created with
BrowserOptions{StorageStatePath: file} reads "abc123" back on the same origin. A fresh context without the option
reads null.
*/
func TestStorageStateRestoresLocalStorage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<html><body>origin</body></html>")
	}))
	defer server.Close()
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	evaluateOrFail(t, agent, "localStorage.setItem('token', 'abc123')")
	stateFile := filepath.Join(t.TempDir(), "session.json")

	if err := agent.SaveStorageState(stateFile); err != nil {
		t.Fatalf("SaveStorageState failed: %v", err)
	}

	for alias, expected := range map[string]any{"restored": "abc123", "fresh": nil} {
		var browserOptions *elementary.BrowserOptions
		if alias == "restored" {
			browserOptions = &elementary.BrowserOptions{StorageStatePath: stateFile}
		}
		if err := agent.NewContext(alias, 0, 0, browserOptions); err != nil {
			t.Fatalf("NewContext(%s) failed: %v", alias, err)
		}
		if err := agent.NewPage(alias + "-tab"); err != nil {
			t.Fatalf("NewPage(%s) failed: %v", alias, err)
		}
		navigateOrFail(t, agent, server.URL)
		if token := evaluateOrFail(t, agent, "localStorage.getItem('token')"); token != expected {
			t.Errorf("Context %s: expected token %v, got %v", alias, expected, token)
		}
	}
	if err := agent.SaveStorageState(" "); err == nil {
		t.Error("Expected an error for an empty file name")
	}
}
