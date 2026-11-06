package elementary

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
)

// maxDownloadNameAttempts bounds how many numbered variants uniqueFilePath tries before giving up.
const maxDownloadNameAttempts = 10000

/*
WaitForDownload is a method which allows you to run action, wait for the download it starts on the active page, and
save the finished file into destinationDirectory, returning the saved file's path. action is whatever triggers the
download, such as a click. The file keeps the name the site suggests. In addition, the following should be noted:

  - destinationDirectory is created when it does not exist.

  - The suggested name is reduced to a plain file name with illegal characters replaced, so a site cannot write
    outside destinationDirectory with a name such as "../../evil". An existing file is never overwritten; a number is
    added instead, as in "report (1).pdf".

  - The timeout bounds only how long to wait for the download to start, defaulting to the navigation timeout. Once
    started, the method waits for the download to finish however long it takes.

  - An error is returned if action fails, no download starts in time, or the download fails part way.

Example:

	path, err := agent.WaitForDownload(func() error {
	    return agent.ClickElement(elementary.Selector{Value: "css=#export", Description: "export button"}, nil)
	}, "./downloads")
*/
func (shared *Instance) WaitForDownload(action func() error, destinationDirectory string, options ...ActionOptions) (string, error) {
	if action == nil {
		return "", errors.New("a download action is required")
	}
	if strings.TrimSpace(destinationDirectory) == "" {
		return "", errors.New("download destination directory must not be empty")
	}
	if err := os.MkdirAll(destinationDirectory, 0o755); err != nil {
		return "", logger.Error(err, "failed to create download directory %s", destinationDirectory)
	}
	page, err := shared.getCurrentPage()
	if err != nil {
		return "", err
	}
	timeoutInMilliseconds, _ := resolveTimeoutInMilliseconds(shared.defaultNavigationTimeout, options)
	download, err := page.ExpectDownload(action, playwright.PageExpectDownloadOptions{Timeout: &timeoutInMilliseconds})
	if err != nil {
		return "", logger.Error(err, "no download completed")
	}
	savePath, err := uniqueFilePath(destinationDirectory, safeDownloadName(download.SuggestedFilename()))
	if err != nil {
		_ = download.Cancel()
		return "", err
	}
	if err := download.SaveAs(savePath); err != nil {
		return "", logger.Error(err, "failed to save download to %s", savePath)
	}
	if err := download.Failure(); err != nil {
		_ = os.Remove(savePath)
		return "", logger.Error(err, "download failed")
	}
	return savePath, nil
}

/*
DownloadByClicking is a method which allows you to click the element matched by selector and save the file the click
downloads into destinationDirectory, returning the saved file's path. It is the common case of WaitForDownload and
follows the same naming, overwrite, and directory rules. If no element matches the selector within the timeout, an
error wrapping ErrElementNotFound is returned and nothing is clicked. In addition, the following should be noted:

  - Without an explicit timeout, from ActionOptions or the Selector itself, finding and clicking the element uses the
    element timeout and waiting for the download to start uses the navigation timeout. An explicit timeout applies
    to each of those steps.

Example:

	path, err := agent.DownloadByClicking(elementary.Selector{Value: "css=a.invoice", Description: "invoice link"}, "./downloads", nil)
*/
func (shared *Instance) DownloadByClicking(selector Selector, destinationDirectory string, parentLocator playwright.Locator, options ...ActionOptions) (string, error) {
	options = applySelectorTimeout(selector, options)
	elementTimeout, _ := resolveTimeoutInMilliseconds(shared.defaultElementTimeout, options)
	shared.captureScreenshot("download_by_clicking", "before", selector.String())
	element, err := shared.getRequiredElement(selector, parentLocator, options...)
	if err != nil {
		shared.captureScreenshot("download_by_clicking", "failure", selector.String())
		return "", err
	}
	var downloadOptions []ActionOptions
	if options[0].Timeout > 0 || options[0].InfiniteTimeout {
		downloadOptions = options
	}
	savedPath, err := shared.WaitForDownload(func() error {
		return element.Click(playwright.LocatorClickOptions{Timeout: &elementTimeout})
	}, destinationDirectory, downloadOptions...)
	if err != nil {
		shared.captureScreenshot("download_by_clicking", "failure", selector.String())
		return "", err
	}
	shared.captureScreenshot("download_by_clicking", "after", selector.String())
	return savedPath, nil
}

/*
safeDownloadName is a function which allows you to reduce a site-suggested download name to a plain file name that
is safe to write: any directory part is dropped, illegal characters are replaced with underscores, and a name that
ends up empty or reserved ("." or "..") becomes "download".

Example:

	name := safeDownloadName("../../report.pdf") // "report.pdf"
*/
func safeDownloadName(suggestedName string) string {
	baseName := filepath.Base(strings.ReplaceAll(suggestedName, "\\", "/"))
	baseName = strings.TrimSpace(illegalFilenameCharacterReplacer.Replace(baseName))
	if baseName == "" || baseName == "." || baseName == ".." {
		return "download"
	}
	return baseName
}

/*
uniqueFilePath is a function which allows you to pick a path for fileName inside directory that does not already
exist, adding " (1)", " (2)", and so on before the extension when needed, as in "report (1).pdf". It returns an error
if every numbered variant up to maxDownloadNameAttempts is taken.

Example:

	path, err := uniqueFilePath("./downloads", "report.pdf")
*/
func uniqueFilePath(directory string, fileName string) (string, error) {
	candidate := filepath.Join(directory, fileName)
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate, nil
	}
	extension := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, extension)
	for attempt := 1; attempt <= maxDownloadNameAttempts; attempt++ {
		candidate = filepath.Join(directory, fmt.Sprintf("%s (%d)%s", stem, attempt, extension))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not find a free file name for %s in %s", fileName, directory)
}
