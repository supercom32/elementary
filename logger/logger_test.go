package logger_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/supercom32/elementary/logger"
)

/*
TestLogger is a test which verifies that the production-grade logger handles different log levels and formats correctly.

Example:

	Expected Inputs:
	    A call to SetDebugMode(true) and logging output to a custom bytes.Buffer.

	Expected Outputs:
	    Verifies that [INFO] and [DBUG] output matches expected patterns in the buffer.
*/
func TestLogger(t *testing.T) {
	var buf bytes.Buffer
	logger.SetOutput(&buf)

	logger.SetDebugMode(true)
	if !logger.IsDebugModeEnabled() {
		t.Error("Expected debug mode to be enabled")
	}

	logger.Log(logger.TYPE_INFO, "Testing info logging: %s", "success")
	out := buf.String()
	if !strings.Contains(out, "[INFO]") || !strings.Contains(out, "Testing info logging: success") {
		t.Errorf("Unexpected standard log output: %q", out)
	}

	buf.Reset()
	logger.Log(logger.TYPE_DEBUG, "Testing debug logging: %d", 42)
	out = buf.String()
	if !strings.Contains(out, "[DBUG]") || !strings.Contains(out, "Testing debug logging: 42") {
		t.Errorf("Unexpected debug log output: %q", out)
	}

	buf.Reset()
	logger.SetDebugMode(false)
	logger.Log(logger.TYPE_DEBUG, "Should not be printed")
	out = buf.String()
	if out != "" {
		t.Errorf("Expected empty output when debug mode is disabled, got: %q", out)
	}
}

/*
TestLoggerError is a test which verifies that the Error formatting function operates correctly.

Example:

	Expected Inputs:
	    Calling logger.Error with an error object and a formatting template string.

	Expected Outputs:
	    A combined error containing both formatting and wrapped error context.
*/
func TestLoggerError(t *testing.T) {
	origErr := errors.New("underlying failure")
	wrapped := logger.Error(origErr, "high-level operation failed %s", "gracefully")
	if wrapped == nil {
		t.Fatal("Expected non-nil error")
	}

	expected := "high-level operation failed gracefully: underlying failure"
	if wrapped.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, wrapped.Error())
	}

	if !errors.Is(wrapped, origErr) {
		t.Error("Expected error to wrap underlying failure")
	}
}
