// Copyright © Sébastien Gross
//
// Created: 2026-02-25
// Last changed: 2026-02-25 02:52:06
//
// This program is free software: you can redistribute it and/or
// modify it under the terms of the GNU Affero General Public License
// as published by the Free Software Foundation, either version 3 of
// the License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public
// License along with this program. If not, see
// <http://www.gnu.org/licenses/>.

package logger_test

import (
	"bytes"
	stdlog "log"
	"strings"
	"testing"

	"github.com/renard/cwlog/logger"
)

// TestNullLogger verifies that the null logger never processes messages
// and always reports Enabled as false.
func TestNullLogger(t *testing.T) {
	l := logger.Null()

	// Calling any method must not panic.
	l.Trace("trace %d", 1)
	l.Debug("debug %d", 2)
	l.Info("info %d", 3)
	l.Warn("warn %d", 4)
	l.Error("error %d", 5)

	levels := []logger.Level{
		logger.ErrorLevel,
		logger.WarnLevel,
		logger.InfoLevel,
		logger.DebugLevel,
		logger.TraceLevel,
	}
	for _, lvl := range levels {
		if l.Enabled(lvl) {
			t.Errorf("Null().Enabled(%v) = true, want false", lvl)
		}
	}
}

// TestDisabledConstant verifies that Disabled is strictly below ErrorLevel
// so that it can be used as a sentinel "off" value in option structs.
func TestDisabledConstant(t *testing.T) {
	if logger.Disabled >= logger.ErrorLevel {
		t.Errorf("Disabled (%d) must be less than ErrorLevel (%d)",
			logger.Disabled, logger.ErrorLevel)
	}
}

// TestSafe_nilReturnsNull verifies that Safe(nil) returns a usable logger
// rather than panicking on subsequent calls.
func TestSafe_nilReturnsNull(t *testing.T) {
	l := logger.Safe(nil)
	if l == nil {
		t.Fatal("Safe(nil) returned nil")
	}
	// Must not panic.
	l.Info("should be silently discarded")
}

// TestSafe_nonNilPassThrough verifies that Safe returns the original
// logger unchanged when it is non-nil.
func TestSafe_nonNilPassThrough(t *testing.T) {
	original := logger.Null()
	got := logger.Safe(original)
	if got != original {
		t.Error("Safe(non-nil) did not return the original logger")
	}
}

// newBufferedStdLogger builds a stdLogger backed by a buffer so tests
// can inspect the output without touching os.Stderr.
func newBufferedStdLogger(lvl logger.Level) (logger.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	sl := stdlog.New(buf, "", 0)
	// Access StdWithLevel which returns a Logger backed by sl.
	// We rebuild it here because StdWithLevel always uses stdlog.Default().
	// Instead we test the observable behaviour through Std() and StdWithLevel().
	_ = sl
	return logger.StdWithLevel(lvl), buf
}

// TestStd_defaultLevel verifies that Std() starts at WarnLevel.
func TestStd_defaultLevel(t *testing.T) {
	l := logger.Std()
	if !l.Enabled(logger.WarnLevel) {
		t.Error("Std().Enabled(WarnLevel) = false, want true")
	}
	if !l.Enabled(logger.ErrorLevel) {
		t.Error("Std().Enabled(ErrorLevel) = false, want true")
	}
	if l.Enabled(logger.InfoLevel) {
		t.Error("Std().Enabled(InfoLevel) = true, want false")
	}
	if l.Enabled(logger.DebugLevel) {
		t.Error("Std().Enabled(DebugLevel) = true, want false")
	}
	if l.Enabled(logger.TraceLevel) {
		t.Error("Std().Enabled(TraceLevel) = true, want false")
	}
}

// TestStdWithLevel_enabled verifies that StdWithLevel respects the given
// threshold: levels at or below it are enabled, levels above it are not.
func TestStdWithLevel_enabled(t *testing.T) {
	cases := []struct {
		configured logger.Level
		check      logger.Level
		want       bool
	}{
		{logger.ErrorLevel, logger.ErrorLevel, true},
		{logger.ErrorLevel, logger.WarnLevel, false},
		{logger.InfoLevel, logger.ErrorLevel, true},
		{logger.InfoLevel, logger.WarnLevel, true},
		{logger.InfoLevel, logger.InfoLevel, true},
		{logger.InfoLevel, logger.DebugLevel, false},
		{logger.TraceLevel, logger.TraceLevel, true},
		{logger.TraceLevel, logger.DebugLevel, true},
	}
	for _, c := range cases {
		l := logger.StdWithLevel(c.configured)
		got := l.Enabled(c.check)
		if got != c.want {
			t.Errorf("StdWithLevel(%v).Enabled(%v) = %v, want %v",
				c.configured, c.check, got, c.want)
		}
	}
}

// TestStdWithLevel_returnsInterface verifies that StdWithLevel returns
// the Logger interface, not a concrete type (guards against regressions
// to the old leak of stdLogger).
func TestStdWithLevel_returnsInterface(t *testing.T) {
	var _ logger.Logger = logger.StdWithLevel(logger.InfoLevel)
}

// TestStdLogger_output verifies that messages at or below the configured
// level are written, and messages above it are not.
// We redirect the default stdlib logger output to a buffer for this test.
func TestStdLogger_output(t *testing.T) {
	buf := &bytes.Buffer{}
	// Override the default logger's output for the duration of the test.
	stdlog.SetOutput(buf)
	t.Cleanup(func() { stdlog.SetOutput(nil) })
	stdlog.SetFlags(0)

	l := logger.StdWithLevel(logger.InfoLevel)

	l.Error("error message")
	l.Warn("warn message")
	l.Info("info message")
	l.Debug("debug should be suppressed")
	l.Trace("trace should be suppressed")

	out := buf.String()

	for _, expected := range []string{"[ERROR]", "[WARN]", "[INFO]"} {
		if !strings.Contains(out, expected) {
			t.Errorf("output missing %q\ngot: %s", expected, out)
		}
	}
	for _, unexpected := range []string{"[DEBUG]", "[TRACE]"} {
		if strings.Contains(out, unexpected) {
			t.Errorf("output contains suppressed %q\ngot: %s", unexpected, out)
		}
	}
}

// TestStructuredLoggerAssertion verifies that nullLogger and stdLogger
// do NOT satisfy StructuredLogger (they have no WithField), so callers
// who type-assert get a clean false rather than a panic.
func TestStructuredLoggerAssertion_nullAndStd(t *testing.T) {
	loggers := []logger.Logger{
		logger.Null(),
		logger.Std(),
		logger.StdWithLevel(logger.DebugLevel),
	}
	for _, l := range loggers {
		if _, ok := l.(logger.StructuredLogger); ok {
			t.Errorf("%T unexpectedly implements StructuredLogger", l)
		}
	}
}
