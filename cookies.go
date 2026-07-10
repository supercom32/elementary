package elementary

import (
	"fmt"
	"github.com/mxschmitt/playwright-go"
	"github.com/supercom32/elementary/logger"
	"github.com/supercom32/filesystem"
	"strconv"
	"strings"
	"time"
)

/*
SaveNetscapeCookies is a method which serializes active context cookies to an output file in the Netscape text format. This
persists authentications, session parameters, and identity tokens for reuse across future automation sessions. In addition, the following should be noted:

  - This method will automatically attempt to delete any pre-existing cookie file matching the specified filename. This prevents file-write collisions
    and ensures that obsolete or expired credentials are not retained in the output destination file.

Example:

	err := agent.SaveNetscapeCookies("my_cookies.txt")
*/
func (shared *Instance) SaveNetscapeCookies(fileName string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if filesystem.IsFileExists(fileName) {
		err := filesystem.DeleteFile(fileName)
		if err != nil {
			return logger.Error(fmt.Errorf("failed to delete existing cookie file %s: %w", fileName, err), "failed to delete existing cookie file %s", fileName)
		}
	}
	cookies, err := shared.getCookiesUnlocked()
	if err != nil {
		return err
	}
	for _, currentCookie := range cookies {
		expireTime := fmt.Sprintf("%f", currentCookie.Expires)
		cookieLine := currentCookie.Domain + "\tTRUE\t/\t" + strings.ToUpper(strconv.FormatBool(currentCookie.Secure)) + "\t" + expireTime + "\t" + currentCookie.Name + "\t" + currentCookie.Value + "\n"
		if err := filesystem.AppendLineToFile(fileName, cookieLine, 0); err != nil {
			return logger.Error(fmt.Errorf("failed to write cookie to file %s: %w", fileName, err), "failed to write cookie to file %s", fileName)
		}
	}
	return nil
}

/*
AddCookie is a method which inserts a single designated cookie into the current browser context. This allows seeding targeted
identification tags, tracking configurations, or authentication tokens prior to launching navigation pipelines. In addition, the following should be noted:

- This method assigns a default expiry time set to exactly 10,000 seconds in the future, ensuring the cookie remains active throughout the session.

- This method will return an error if the underlying Playwright instance fails to add the cookie to the active browser context.

Example:

	err := agent.AddCookie(".example.com", "session_token", "secretXYZ")
*/
func (shared *Instance) AddCookie(domain string, name string, value string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if !shared.hasActiveContextUnlocked() {
		return logger.Error(fmt.Errorf("no active browser context exists to add cookie"), "failed to add cookie to browser context")
	}
	var cookie playwright.OptionalCookie
	cookie.Domain = &domain
	cookie.Name = name
	cookie.Value = value
	cookie.Path = &[]string{"/"}[0]
	cookie.Secure = &[]bool{false}[0]
	expireTime := float64([]int{int(time.Now().Unix() + 10000)}[0])
	cookie.Expires = &expireTime
	err := shared.contextInstances[shared.currentContextIndex].instance.AddCookies([]playwright.OptionalCookie{cookie})
	if err != nil {
		return logger.Error(fmt.Errorf("failed to add cookie via playwright: %w", err), "failed to add cookie to browser context")
	}
	return nil
}

/*
ClearCookies is a method which purges all cookie definitions from the active browser context. This resets session states
and forces anonymous interactions on subsequent HTTP operations. In addition, the following should be noted:

- This method will return an error if the underlying Playwright instance fails to clear the context's cookie jar.

Example:

	err := agent.ClearCookies()
*/
func (shared *Instance) ClearCookies() error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if !shared.hasActiveContextUnlocked() {
		return logger.Error(fmt.Errorf("no active browser context exists to clear cookies"), "failed to clear cookies from browser context")
	}
	err := shared.contextInstances[shared.currentContextIndex].instance.ClearCookies()
	if err != nil {
		return logger.Error(fmt.Errorf("failed to clear cookies via playwright: %w", err), "failed to clear cookies from browser context")
	}
	return nil
}

/*
LoadNetscapeCookies is a method which parses a Netscape cookie file and populates the active context jar. This restores
identity structures and authorization states directly into the browser session. In addition, the following should be noted:

- This method parses individual fields separated by horizontal tabs and dynamically adds them to the context's cookie jar. This handles comment lines and blank rows gracefully, throwing detailed errors if structural parsing or expiration timestamp casting fails during execution.

- This method will return an error if the underlying Playwright instance fails to add any of the parsed cookies to the active browser context.

Example:

	err := agent.LoadNetscapeCookies("saved_cookies.txt")
*/
func (shared *Instance) LoadNetscapeCookies(fileName string) error {
	shared.stateMutex.Lock()
	defer shared.stateMutex.Unlock()
	if !shared.hasActiveContextUnlocked() {
		return logger.Error(fmt.Errorf("no active browser context exists to load cookies"), "failed to load cookies from file %s", fileName)
	}
	file := filesystem.GetFileInstance()
	err := file.Open(fileName, 0)
	if err != nil {
		return logger.Error(fmt.Errorf("failed to open cookie file %s: %w", fileName, err), "failed to open cookie file %s", fileName)
	}
	defer file.Close()
	cookies, err := file.GetFileContents()
	if err != nil {
		return logger.Error(fmt.Errorf("failed to read cookie file contents %s: %w", fileName, err), "failed to read cookie file contents %s", fileName)
	}
	cookiesAsString := strings.Replace(string(cookies), "\r", "", -1)
	arrayOfLines := strings.Split(cookiesAsString, "\n")
	for _, currentLine := range arrayOfLines {
		trimmedLine := strings.TrimSpace(currentLine)
		if trimmedLine == "" || strings.HasPrefix(trimmedLine, "#") {
			continue
		}
		cookieValues := strings.Split(currentLine, "\t")
		// Netscape spec has up to 7 horizontal tab-separated values (columns 0 through 6).
		// Standard columns: 0: domain, 1: flag (secure/all machine), 2: path, 3: secure, 4: expiration, 5: name, 6: value.
		if len(cookieValues) < 6 {
			continue
		}
		var cookie playwright.OptionalCookie
		cookie.Domain = &cookieValues[0]
		if len(cookieValues) > 3 && cookieValues[3] == "TRUE" {
			cookie.Secure = &[]bool{true}[0]
		} else {
			cookie.Secure = &[]bool{false}[0]
		}
		if len(cookieValues) > 2 {
			cookie.Path = &cookieValues[2]
		} else {
			cookie.Path = &[]string{"/"}[0]
		}
		cookie.Name = cookieValues[5]
		if len(cookieValues) > 6 {
			cookie.Value = cookieValues[6]
		} else {
			cookie.Value = ""
		}
		var expireTime float64
		if len(cookieValues) > 4 && cookieValues[4] != "" {
			var parseError error
			expireTime, parseError = strconv.ParseFloat(cookieValues[4], 64)
			if parseError != nil {
				return logger.Error(fmt.Errorf("failed to parse cookie expiration timestamp '%s': %w", cookieValues[4], parseError), "failed to parse cookie expiration timestamp")
			}
		}
		formattedExpireTime := float64([]int{int(expireTime)}[0])
		cookie.Expires = &formattedExpireTime
		err = shared.contextInstances[shared.currentContextIndex].instance.AddCookies([]playwright.OptionalCookie{cookie})
		if err != nil {
			return logger.Error(fmt.Errorf("failed to add parsed cookie via playwright: %w", err), "failed to add parsed cookie to context")
		}
	}
	return nil
}

/*
GetCookies is a method which extracts all cookie parameters stored within the current browser context. This allows auditing active
security cookies or extracting session state strings for verification.

Example:

	cookies, err := agent.GetCookies()
*/
func (shared *Instance) GetCookies() ([]playwright.Cookie, error) {
	shared.stateMutex.RLock()
	defer shared.stateMutex.RUnlock()
	return shared.getCookiesUnlocked()
}

func (shared *Instance) getCookiesUnlocked() ([]playwright.Cookie, error) {
	if !shared.hasActiveContextUnlocked() {
		return nil, logger.Error(fmt.Errorf("no active browser context exists to retrieve cookies"), "failed to retrieve cookies from context")
	}
	cookies, err := shared.contextInstances[shared.currentContextIndex].instance.Cookies()
	if err != nil {
		return nil, logger.Error(fmt.Errorf("failed to retrieve cookies via playwright: %w", err), "failed to retrieve cookies from context")
	}
	return cookies, nil
}
