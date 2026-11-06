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
	"time"

	"github.com/supercom32/elementary"
)

/*
newDownloadServer is a test helper which starts a local server whose "/" page links to "/file", which is served as an
attachment named "../../report.txt" with the body "report body". The traversal in the name lets tests confirm it is
stripped. The server is closed when the test ends.
*/
func newDownloadServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/file" {
			writer.Header().Set("Content-Type", "text/plain")
			writer.Header().Set("Content-Disposition", `attachment; filename="../../report.txt"`)
			fmt.Fprint(writer, "report body")
			return
		}
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(writer, "<html><body><a id='get' href='/file'>download</a></body></html>")
	}))
	t.Cleanup(server.Close)
	return server
}

/*
TestDownloadByClickingSavesSafelyWithoutOverwriting is a test which allows you to verify that two downloads of a file
the site names "../../report.txt" both land directly inside the chosen (newly created) folder, never above it, that
the second is saved alongside the first with " (1)" added before ".txt" rather than overwriting it, and that each
contains "report body". A missing link returns ErrElementNotFound.
*/
func TestDownloadByClickingSavesSafelyWithoutOverwriting(t *testing.T) {
	server := newDownloadServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	downloadDirectory := filepath.Join(t.TempDir(), "nested", "downloads")
	link := elementary.Selector{Value: "css=#get", Description: "download link"}

	var savedPaths []string
	for range 2 {
		savedPath, err := agent.DownloadByClicking(link, downloadDirectory, nil)
		if err != nil {
			t.Fatalf("DownloadByClicking failed: %v", err)
		}
		if filepath.Dir(savedPath) != downloadDirectory {
			t.Errorf("Expected the download directly inside %s, got %s", downloadDirectory, savedPath)
		}
		contents, err := os.ReadFile(savedPath)
		if err != nil || string(contents) != "report body" {
			t.Errorf("Expected %s to contain \"report body\", got %q (err: %v)", savedPath, contents, err)
		}
		savedPaths = append(savedPaths, savedPath)
	}
	expectedSecond := strings.TrimSuffix(savedPaths[0], ".txt") + " (1).txt"
	if savedPaths[1] != expectedSecond {
		t.Errorf("Expected the second download at %s, got %s", expectedSecond, savedPaths[1])
	}

	if _, err := agent.DownloadByClicking(missingElementSelector, downloadDirectory, nil, shortActionOptions); !errors.Is(err, elementary.ErrElementNotFound) {
		t.Errorf("Expected ErrElementNotFound for a missing link, got %v", err)
	}
}

/*
TestWaitForDownloadFailsWhenNoDownloadStarts is a test which allows you to verify that WaitForDownload with an action
that starts no download returns an error within about its 500ms timeout, and that a nil action or empty folder is
rejected up front.
*/
func TestWaitForDownloadFailsWhenNoDownloadStarts(t *testing.T) {
	server := newDownloadServer(t)
	agent := newPlainAgent(t)
	navigateOrFail(t, agent, server.URL)
	started := time.Now()

	_, err := agent.WaitForDownload(func() error { return nil }, t.TempDir(), elementary.ActionOptions{Timeout: 500 * time.Millisecond})

	if err == nil {
		t.Error("Expected an error when no download starts")
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Errorf("Expected WaitForDownload to give up near its timeout, took %s", elapsed)
	}
	if _, err := agent.WaitForDownload(nil, t.TempDir()); err == nil {
		t.Error("Expected an error for a nil action")
	}
	if _, err := agent.WaitForDownload(func() error { return nil }, " "); err == nil {
		t.Error("Expected an error for an empty destination directory")
	}
}
