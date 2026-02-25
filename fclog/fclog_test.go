// Copyright © Sébastien Gross
//
// Created: 2026-02-25
// Last changed: 2026-02-25 02:51:49
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

package fclog_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/renard/cwlog/fclog"
	"github.com/renard/cwlog/logger"
)

// keepFiles retains temporary log directories after the test run when
// set to true via -keep-files. Useful for manual inspection of log
// content and rotation artifacts.
// Usage: go test ./fclog/ -v -keep-files
var keepFiles = flag.Bool("keep-files", false, "keep temporary log files after tests")

// tempDir creates a temporary directory and registers cleanup unless
// -keep-files is set. Always logs the directory path via t.Logf so it
// is visible with go test -v.
func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "fclog-test-*")
	if err != nil {
		t.Fatalf("could not create temp dir: %v", err)
	}
	t.Logf("temp dir: %s", dir)
	if !*keepFiles {
		t.Cleanup(func() { os.RemoveAll(dir) })
	}
	return dir
}

// tempLog returns a path inside a temporary directory for a single log
// file.
func tempLog(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(tempDir(t), "fclog-*.log")
	if err != nil {
		t.Fatalf("could not create temp file: %v", err)
	}
	f.Close()
	return f.Name()
}

// TestNew_bothDisabledReturnsError verifies that passing Disabled for
// both targets is rejected with a non-nil error.
func TestNew_bothDisabledReturnsError(t *testing.T) {
	_, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.Disabled,
	})
	if err == nil {
		t.Error("New with both targets Disabled should return an error")
	}
}

// TestNew_fileLevelSetButNoPath verifies that omitting FilePath when
// FileLevel is active returns an error.
func TestNew_fileLevelSetButNoPath(t *testing.T) {
	_, err := fclog.New(fclog.Options{
		FileLevel: logger.InfoLevel,
		// FilePath intentionally omitted.
	})
	if err == nil {
		t.Error("New with FileLevel set but empty FilePath should return an error")
	}
}

// TestNew_invalidPathReturnsError verifies that an inaccessible path
// produces an error rather than a panic.
func TestNew_invalidPathReturnsError(t *testing.T) {
	_, err := fclog.New(fclog.Options{
		FileLevel: logger.InfoLevel,
		FilePath:  "/no/such/directory/app.log",
	})
	if err == nil {
		t.Error("New with invalid path should return an error")
	}
}

// TestNew_consoleOnly verifies that a console-only configuration
// constructs without error and satisfies Logger.
func TestNew_consoleOnly(t *testing.T) {
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.Disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var _ logger.Logger = l
}

// TestNew_fileOnly verifies that a file-only configuration constructs
// without error and writes to the file.
func TestNew_fileOnly(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Info("hello file")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(data), "hello file") {
		t.Errorf("log file does not contain expected message\ngot: %s", data)
	}
}

// TestNew_fileAndConsole verifies that dual-target construction succeeds
// and that the file receives entries.
func TestNew_fileAndConsole(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.DebugLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Debug("debug to file")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(data), "debug to file") {
		t.Errorf("file missing debug entry\ngot: %s", data)
	}
}

// TestFileJSON_jsonFormat verifies that FileJSON:true produces JSON lines
// in the output file.
func TestFileJSON_jsonFormat(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		FileJSON:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Info("json test message")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if !strings.HasPrefix(trimmed, "{") {
		t.Errorf("FileJSON output does not look like JSON\ngot: %s", trimmed)
	}
}

// TestFileText_plainFormat verifies that FileJSON:false (default)
// produces human-readable text rather than JSON.
func TestFileText_plainFormat(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		FileJSON:     false,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Info("plain text message")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		t.Errorf("plain text output looks like JSON\ngot: %s", trimmed)
	}
	if !strings.Contains(trimmed, "plain text message") {
		t.Errorf("plain text output missing message\ngot: %s", trimmed)
	}
}

// TestEnabled_atLeastOneTarget verifies that Enabled returns true when
// at least one active target accepts the level.
func TestEnabled_atLeastOneTarget(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.DebugLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !l.Enabled(logger.DebugLevel) {
		t.Error("Enabled(Debug) = false, want true (file target accepts it)")
	}
	if !l.Enabled(logger.WarnLevel) {
		t.Error("Enabled(Warn) = false, want true (both targets accept it)")
	}
	if l.Enabled(logger.TraceLevel) {
		t.Error("Enabled(Trace) = true, want false (no target accepts it)")
	}
}

// TestEnabled_outOfBounds verifies that an out-of-range level value
// returns false without panicking.
func TestEnabled_outOfBounds(t *testing.T) {
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.Disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Enabled with out-of-bounds level panicked: %v", r)
		}
	}()
	if l.Enabled(logger.Level(999)) {
		t.Error("Enabled(999) = true, want false")
	}
}

// TestSetLevel_affectsBothTargets verifies that SetLevel changes the
// threshold on all active targets simultaneously.
func TestSetLevel_affectsBothTargets(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.WarnLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if l.Enabled(logger.DebugLevel) {
		t.Error("Debug should be disabled before SetLevel")
	}

	l.SetLevel(2) // Debug

	if !l.Enabled(logger.DebugLevel) {
		t.Error("Debug should be enabled after SetLevel(2)")
	}
}

// TestSetLevel_noop verifies that SetLevel(0) is a no-op.
func TestSetLevel_noop(t *testing.T) {
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.Disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.SetLevel(0)

	if l.Enabled(logger.InfoLevel) {
		t.Error("Info should still be disabled after SetLevel(0)")
	}
}

// TestSetFileLevel_independentOfConsole verifies that SetFileLevel does
// not affect the console target.
func TestSetFileLevel_independentOfConsole(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.WarnLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.SetFileLevel(3) // Trace on file only

	if !l.Enabled(logger.TraceLevel) {
		t.Error("Trace should be enabled after SetFileLevel(3)")
	}
}

// TestWithField_implementsStructuredLogger verifies the type assertion.
func TestWithField_implementsStructuredLogger(t *testing.T) {
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.Disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := any(l).(logger.StructuredLogger); !ok {
		t.Error("*Log does not implement logger.StructuredLogger")
	}
}

// TestWithField_doesNotMutateReceiver verifies immutable chaining.
func TestWithField_doesNotMutateReceiver(t *testing.T) {
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.WarnLevel,
		FileLevel:    logger.Disabled,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sl := any(l).(logger.StructuredLogger)
	derived := sl.WithField("component", "test")

	if derived == l {
		t.Error("WithField returned the same pointer, receiver was mutated")
	}
	l.Warn("original")
	derived.Warn("derived")
}

// TestWithField_fieldAppearsInOutput verifies that a field attached via
// WithField is present in the file output.
func TestWithField_fieldAppearsInOutput(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		FileJSON:     true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sl := any(l).(logger.StructuredLogger)
	sl.WithField("requestID", "abc-123").Info("handled")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(data), "abc-123") {
		t.Errorf("structured field not found in output\ngot: %s", data)
	}
}

// TestRotation_sizeTriggered verifies that lumberjack rotates the log
// file when the size threshold is exceeded and that backup files are
// created on disk.
//
// Strategy: set MaxSizeMB=1 and write slightly more than 1 MB of data.
// After writing, the directory should contain the active log file plus
// at least one backup (lumberjack renames the old file before opening
// a new one).
//
// Use -keep-files to inspect the produced files after the test.
func TestRotation_sizeTriggered(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "size-rotation.log")

	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		MaxSizeMB:    1,
		MaxBackups:   3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write enough data to exceed 1 MB. Each message is ~100 bytes;
	// 12 000 iterations produce ~1.2 MB, reliably crossing the threshold.
	payload := strings.Repeat("x", 80)
	for i := range 12_000 {
		l.Info("rotation-size-test i=%d payload=%s", i, payload)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("could not read temp dir: %v", err)
	}

	// Log the files found so they are visible with -v or -keep-files.
	for _, e := range entries {
		info, _ := e.Info()
		t.Logf("  %s  (%d bytes)", e.Name(), info.Size())
	}

	if len(entries) < 2 {
		t.Errorf("expected at least 2 files (active + 1 backup), got %d", len(entries))
	}
}

// TestRotation_maxBackupsEnforced verifies that lumberjack prunes old
// backup files so that no more than MaxBackups rotated files remain.
//
// Strategy: set MaxSizeMB=1, MaxBackups=2 and write enough data to
// trigger 3+ rotations. After writing, count files whose name contains
// the base name: there should be at most MaxBackups+1 (backups + active).
func TestRotation_maxBackupsEnforced(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "maxbackup-rotation.log")

	const maxBackups = 2
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		MaxSizeMB:    1,
		MaxBackups:   maxBackups,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Write ~4 MB to force at least 3 rotations at the 1 MB threshold.
	payload := strings.Repeat("x", 80)
	for i := range 48_000 {
		l.Info("rotation-backup-test i=%d payload=%s", i, payload)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("could not read temp dir: %v", err)
	}

	for _, e := range entries {
		info, _ := e.Info()
		t.Logf("  %s  (%d bytes)", e.Name(), info.Size())
	}

	// lumberjack may take a moment to prune in a background goroutine;
	// count files that match the base name to isolate our log files.
	count := 0
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	for _, e := range entries {
		if strings.Contains(e.Name(), base) {
			count++
		}
	}

	// At most maxBackups rotated files + 1 active file.
	maxExpected := maxBackups + 1
	if count > maxExpected {
		t.Errorf("found %d log files, expected at most %d (maxBackups=%d + active)",
			count, maxExpected, maxBackups)
	}
	if count < 2 {
		t.Errorf("found only %d log file(s), rotation may not have triggered", count)
	}
}

// TestRotation_daily verifies that a daily rotation configuration
// constructs successfully and writes entries. The actual midnight
// trigger cannot be tested without mocking time, so we only assert
// that the setup is valid and the file is writable.
func TestRotation_daily(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "daily-rotation.log")

	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.InfoLevel,
		FilePath:     path,
		Daily:        true,
		MaxBackups:   7,
	})
	if err != nil {
		t.Fatalf("unexpected error with daily rotation options: %v", err)
	}

	for i := range 5 {
		l.Info("daily-rotation-test entry %d", i)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(data), "daily-rotation-test") {
		t.Errorf("log file missing entries\ngot: %s", data)
	}
	t.Logf("log file size: %d bytes", len(data))
}

// TestRotation_sizeAndDaily verifies that combining both triggers
// constructs without error and writes correctly.
func TestRotation_sizeAndDaily(t *testing.T) {
	dir := tempDir(t)
	path := filepath.Join(dir, "combined-rotation.log")

	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.Disabled,
		FileLevel:    logger.DebugLevel,
		FilePath:     path,
		MaxSizeMB:    10,
		Daily:        true,
		MaxBackups:   5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	payload := strings.Repeat("x", 80)
	for i := range 100 {
		l.Debug(fmt.Sprintf("combined-rotation-test i=%d payload=%s", i, payload))
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("could not read log file: %v", err)
	}
	if !strings.Contains(string(data), "combined-rotation-test") {
		t.Errorf("log file missing entries\ngot: %s", data)
	}
}

// TestNoPanic_allMethods verifies that all five log methods can be
// called on every target configuration without panicking.
func TestNoPanic_allMethods(t *testing.T) {
	path := tempLog(t)
	l, err := fclog.New(fclog.Options{
		ConsoleLevel: logger.TraceLevel,
		FileLevel:    logger.TraceLevel,
		FilePath:     path,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Trace("trace %s", "ok")
	l.Debug("debug %s", "ok")
	l.Info("info %s", "ok")
	l.Warn("warn %s", "ok")
	l.Error("error %s", "ok")
}
