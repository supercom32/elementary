package logger

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"sync"
	"sync/atomic"
)

/*
LogType is a type which represents the severity or type of log entry. This is useful for categorized routing and visual
formatting of log messages, enabling easier filtering and analysis of application events.
*/
type LogType int

const (
	TYPE_INFO LogType = iota
	TYPE_WARN
	TYPE_ERROR
	TYPE_DEBUG
	TYPE_OK
	TYPE_FAIL
	TYPE_PLAIN
	SCREENSHOT_DIRECTORY = "./debug"
)

var (
	isDebugModeEnabled int32
	mu                 sync.RWMutex
	standardLogger     = log.New(os.Stderr, "", log.LstdFlags)
)

/*
SetDebugMode is a function which enables or disables debug logging mode in a thread-safe manner. This modifies active
diagnostic output filters, enabling verbose tracing on demand. In addition, the following should be noted:

  - This function uses atomic stores to write the status value to the package-level variable. This ensures that
    concurrent read operations by other goroutines are synchronized safely and prevents any data race conditions or
    inconsistent states during execution.

Example:

	SetDebugMode(true)
*/
func SetDebugMode(isEnabled bool) {
	var value int32
	if isEnabled {
		value = 1
	}
	atomic.StoreInt32(&isDebugModeEnabled, value)
}

/*
IsDebugModeEnabled is a function which returns whether the debug logging mode is currently active. This avoids
unnecessary execution of complex format operations when low-level tracing is disabled. In addition, the following should be noted:

  - This function uses atomic load operations to retrieve the debug status value. This allows multiple concurrent
    goroutines to safely read the state without locking overhead, ensuring high performance while maintaining complete
    safety from race conditions.

Example:

	enabled := IsDebugModeEnabled()
*/
func IsDebugModeEnabled() bool {
	return atomic.LoadInt32(&isDebugModeEnabled) == 1
}

/*
SetOutput is a function which redirects standard logging destinations to a custom writer. This maps diagnostic channels
to customized file descriptors, system outputs, or in-memory byte streams for assertions. In addition, the following should be noted:

  - This function uses a mutual exclusion lock to guarantee safe writer replacement. This blocks concurrent write
    operations briefly during the replacement to prevent memory corruption or nil pointer dereferences if logging
    occurs during the output destination switch.

Example:

	logger.SetOutput(os.Stdout)
*/
func SetOutput(w io.Writer) {
	if w == nil {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	standardLogger.SetOutput(w)
}

/*
getLogPrefixTitle is a function which maps log types to their corresponding bracketed header strings. This generates
visually distinct categories for terminal output pipelines.

Example:

	prefix := getLogPrefixTitle(logger.TYPE_INFO)
*/
func getLogPrefixTitle(logType LogType) string {
	switch logType {
	case TYPE_INFO:
		return "[INFO]"
	case TYPE_WARN:
		return "[WARN]"
	case TYPE_ERROR:
		return "[FAIL]"
	case TYPE_DEBUG:
		return "[DBUG]"
	case TYPE_OK:
		return "[ OK ]"
	case TYPE_FAIL:
		return "[FAIL]"
	case TYPE_PLAIN:
		return ""
	default:
		return "[INFO]"
	}
}

/*
Log is a function which prints structured log statements evaluated against the active verbosity constraints. This
abstracts message forwarding, ensuring uniform formatting across informational, diagnostic, and error channels. In addition, the following should be noted:

  - This function checks if the debug flag is enabled before delegating to the underlying red debug logger. This
    prevents debug logs from cluttering standard output when debug mode is disabled, improving console readability
    and performance.

Example:

	logger.Log(logger.TYPE_INFO, "User connected from IP: %s", "127.0.0.1")
*/
func Log(logType LogType, stringTemplate string, parameters ...any) {
	if logType == TYPE_INFO || logType == TYPE_WARN || logType == TYPE_OK || logType == TYPE_FAIL || logType == TYPE_ERROR {
		logStandardMessage(logType, stringTemplate, parameters...)
	} else if logType == TYPE_DEBUG && IsDebugModeEnabled() {
		logDebugMessage(logType, stringTemplate, parameters...)
	} else if logType == TYPE_PLAIN {
		logPlainMessage(stringTemplate, parameters...)
	}
}

/*
logStandardMessage is a function which outputs standard format logs directly to stderr under synchronization constraints.
This guarantees clean sequence order when multiple execution routines post alerts concurrently. In addition, the following should be noted:

  - This function uses a read lock on the shared logger mutex before writing to the output stream. This prevents
    interleaving of log messages or stream corruption when multiple concurrent goroutines attempt to write log entries
    simultaneously.

Example:

	logStandardMessage(logger.TYPE_INFO, "Standard message: %s", "hello")
*/
func logStandardMessage(logType LogType, stringTemplate string, parameters ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logTypePrefix := getLogPrefixTitle(logType)
	if len(parameters) > 0 {
		standardLogger.Printf(logTypePrefix+" "+stringTemplate, parameters...)
	} else {
		standardLogger.Printf(logTypePrefix + " " + stringTemplate)
	}
}

/*
logDebugMessage is a function which emits ANSI-escaped red log output to stderr for immediate terminal visualization.
This isolates system diagnostic lines from normal application info sequences. In addition, the following should be noted:

  - This function wraps the prefix and log message in standard ANSI escape sequences for the color red. This visual
    coloring will only render properly in terminal environments that support ANSI color codes, and may display as raw
    characters in plain text log files.

  - This function uses a read lock on the shared logger mutex before writing to the output stream. This prevents
    interleaving of log messages or stream corruption when multiple concurrent goroutines attempt to write log entries
    simultaneously.

Example:

	logDebugMessage(logger.TYPE_DEBUG, "Debug message: %d", 123)
*/
func logDebugMessage(logType LogType, stringTemplate string, parameters ...any) {
	mu.RLock()
	defer mu.RUnlock()
	logTypePrefix := getLogPrefixTitle(logType)
	// Red color escape codes
	red := "\033[31m"
	reset := "\033[0m"
	if len(parameters) > 0 {
		standardLogger.Printf(red+logTypePrefix+" "+stringTemplate+reset, parameters...)
	} else {
		standardLogger.Printf(red + logTypePrefix + " " + stringTemplate + reset)
	}
}

/*
logPlainMessage is a function which writes raw, unformatted string lines directly to stdout. This is suited for simple CLI outputs
or clean textual reports that must skip timestamp prefixing.

Example:

	logPlainMessage("Hello world: %s", "user")
*/
func logPlainMessage(stringTemplate string, parameters ...any) {
	if len(parameters) > 0 {
		fmt.Printf(stringTemplate+"\n", parameters...)
	} else {
		fmt.Printf(stringTemplate + "\n")
	}
}

/*
Sprint is a function which outputs a formatted string and returns an empty error struct. This preserves legacy signature mappings
for compatibility with earlier interface specifications. In addition, the following should be noted:

  - This function uses a read lock on the shared logger mutex before writing to the output stream. This prevents interleaving of log messages or
    stream corruption when multiple concurrent goroutines attempt to write log entries simultaneously.

Example:

	logger.Sprint(logger.TYPE_INFO, "Simple format string")
*/
func Sprint(logType LogType, stringTemplate string, parameters ...any) error {
	mu.RLock()
	defer mu.RUnlock()
	if len(parameters) > 0 {
		standardLogger.Printf(stringTemplate, parameters...)
	} else {
		standardLogger.Printf(stringTemplate)
	}
	return errors.New("")
}

/*
Error is a function which constructs an augmented error instance by appending context templates. This supports error chaining,
wrapping nested execution errors while preserving the root causal reference. In addition, the following should be noted:

  - This function propagates input errors while augmenting them with format context. This utilizes standard Go error wrapping syntax, allowing
    subsequent calls to errors.Is or errors.As to successfully unpack and inspect the underlying root-cause error.

Example:

	err := logger.Error(errors.New("connection reset"), "Failed to parse chunk")
*/
func Error(err error, stringTemplate string, parameters ...any) error {
	if stringTemplate == "" && err == nil {
		return nil
	}
	formattedStr := fmt.Sprintf(stringTemplate, parameters...)
	if err != nil {
		if formattedStr != "" {
			return fmt.Errorf("%s: %w", formattedStr, err)
		}
		return err
	}
	return errors.New(formattedStr)
}
