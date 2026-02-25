// Copyright © Sébastien Gross
//
// Created: 2026-02-25
// Last changed: 2026-02-25 02:51:27
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

package log_test

import (
	"testing"

	log "github.com/renard/cwlog"
	"github.com/renard/cwlog/logger"
)

// TestNew_implementsLogger verifies that New() returns a value that
// satisfies the logger.Logger interface at compile time.
func TestNew_implementsLogger(t *testing.T) {
	var _ logger.Logger = log.New()
}

// TestNew_implementsStructuredLogger verifies that the zerolog-backed
// Log satisfies the optional StructuredLogger extension.
func TestNew_implementsStructuredLogger(t *testing.T) {
	l := log.New()
	if _, ok := any(l).(logger.StructuredLogger); !ok {
		t.Error("*Log does not implement logger.StructuredLogger")
	}
}

// TestNew_defaultLevelIsWarn verifies that a freshly created Log only
// accepts Error and Warn messages by default.
func TestNew_defaultLevelIsWarn(t *testing.T) {
	l := log.New()

	if !l.Enabled(logger.ErrorLevel) {
		t.Error("Enabled(ErrorLevel) = false on a fresh Log, want true")
	}
	if !l.Enabled(logger.WarnLevel) {
		t.Error("Enabled(WarnLevel) = false on a fresh Log, want true")
	}
	if l.Enabled(logger.InfoLevel) {
		t.Error("Enabled(InfoLevel) = true on a fresh Log, want false")
	}
	if l.Enabled(logger.DebugLevel) {
		t.Error("Enabled(DebugLevel) = true on a fresh Log, want false")
	}
	if l.Enabled(logger.TraceLevel) {
		t.Error("Enabled(TraceLevel) = true on a fresh Log, want false")
	}
}

// TestSetLevel verifies the integer-to-zerolog mapping.
func TestSetLevel(t *testing.T) {
	cases := []struct {
		verbosity int
		wantInfo  bool
		wantDebug bool
		wantTrace bool
	}{
		// <= 0 is a no-op: default WarnLevel stays.
		{0, false, false, false},
		{-1, false, false, false},
		// 1 => InfoLevel
		{1, true, false, false},
		// 2 => DebugLevel
		{2, true, true, false},
		// >= 3 => TraceLevel
		{3, true, true, true},
		{99, true, true, true},
	}

	for _, c := range cases {
		l := log.New()
		l.SetLevel(c.verbosity)

		if got := l.Enabled(logger.InfoLevel); got != c.wantInfo {
			t.Errorf("SetLevel(%d): Enabled(Info) = %v, want %v",
				c.verbosity, got, c.wantInfo)
		}
		if got := l.Enabled(logger.DebugLevel); got != c.wantDebug {
			t.Errorf("SetLevel(%d): Enabled(Debug) = %v, want %v",
				c.verbosity, got, c.wantDebug)
		}
		if got := l.Enabled(logger.TraceLevel); got != c.wantTrace {
			t.Errorf("SetLevel(%d): Enabled(Trace) = %v, want %v",
				c.verbosity, got, c.wantTrace)
		}
	}
}

// TestEnabled_outOfBounds verifies that passing a level value beyond
// the known range (e.g. a future level or a corrupted value) returns
// false rather than panicking with an index out of range.
func TestEnabled_outOfBounds(t *testing.T) {
	l := log.New()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Enabled with out-of-bounds level panicked: %v", r)
		}
	}()
	got := l.Enabled(logger.Level(999))
	if got {
		t.Error("Enabled(999) = true, want false")
	}
}

// TestWithField_doesNotMutateReceiver verifies that WithField returns a
// new logger and leaves the original unchanged.
func TestWithField_doesNotMutateReceiver(t *testing.T) {
	l := log.New()
	l.SetLevel(3)

	sl, ok := any(l).(logger.StructuredLogger)
	if !ok {
		t.Fatal("*Log does not implement StructuredLogger")
	}

	derived := sl.WithField("key", "value")
	if derived == l {
		t.Error("WithField returned the same pointer, receiver was mutated")
	}

	// Both must still satisfy Logger without panicking.
	l.Info("original")
	derived.Info("derived")
}

// TestLog_noPanic verifies that all five log methods can be called at
// any configured level without panicking.
func TestLog_noPanic(t *testing.T) {
	l := log.New()
	l.SetLevel(3)

	l.Trace("trace %s", "ok")
	l.Debug("debug %s", "ok")
	l.Info("info %s", "ok")
	l.Warn("warn %s", "ok")
	l.Error("error %s", "ok")
}
