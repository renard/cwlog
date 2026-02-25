// Copyright © Sébastien Gross
//
// Created: 2026-02-25
// Last changed: 2026-02-25 02:51:40
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

// Package fclog (file & console logger) provides a zerolog-backed
// Logger that can write to stderr, a file, or both simultaneously.
//
// Configuration is entirely driven by a single Options struct passed to
// New. Each output target is activated by setting its Level to a value
// other than logger.Disabled. Rotation and format are also declared in
// Options, keeping the API surface fixed regardless of which targets
// are active.
//
// Typical usage:
//
//	// Console only (stderr), coloured when TTY:
//	l, err := fclog.New(fclog.Options{
//	    ConsoleLevel: logger.WarnLevel,
//	})
//
//	// File only, plain text, daily rotation, keep 7 files:
//	l, err := fclog.New(fclog.Options{
//	    FileLevel:   logger.DebugLevel,
//	    FilePath:    "/var/log/app.log",
//	    Daily:       true,
//	    MaxBackups:  7,
//	})
//
//	// Both targets with independent levels and JSON file output:
//	l, err := fclog.New(fclog.Options{
//	    ConsoleLevel: logger.WarnLevel,
//	    FileLevel:    logger.DebugLevel,
//	    FilePath:     "/var/log/app.log",
//	    FileJSON:     true,
//	    MaxSizeMB:    100,
//	    MaxBackups:   5,
//	})
//
//	// Error handling: always fall back via logger.Safe:
//	if err != nil {
//	    l = logger.Null() // or logger.Std()
//	}
package fclog

import (
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/renard/cwlog/logger"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
)

// levelMap translates from the abstract logger.Level to zerolog.Level.
// Stored as a fixed-size array so the conversion is a single indexed
// read with no heap allocation or switch overhead in the hot path.
// Index order must match the Level constants in the logger package
// (Error=0, Warn=1, Info=2, Debug=3, Trace=4).
var levelMap = [...]zerolog.Level{
	logger.ErrorLevel: zerolog.ErrorLevel,
	logger.WarnLevel:  zerolog.WarnLevel,
	logger.InfoLevel:  zerolog.InfoLevel,
	logger.DebugLevel: zerolog.DebugLevel,
	logger.TraceLevel: zerolog.TraceLevel,
}

// Options holds the full configuration for a Log instance.
// Set a Level field to logger.Disabled to turn off that target.
// Both targets can be active simultaneously with independent levels.
type Options struct {
	// ConsoleLevel is the minimum level written to stderr.
	// Set to logger.Disabled to suppress console output entirely.
	ConsoleLevel logger.Level

	// FileLevel is the minimum level written to the log file.
	// Set to logger.Disabled to suppress file output entirely.
	// FilePath must be non-empty when FileLevel != Disabled.
	FileLevel logger.Level

	// FilePath is the path of the log file.
	// Required when FileLevel != logger.Disabled.
	FilePath string

	// FileJSON selects the file output format.
	// false (default): human-readable text via zerolog ConsoleWriter.
	// true:            newline-delimited JSON, suitable for log
	//                  aggregators (Loki, Datadog, Splunk…).
	FileJSON bool

	// MaxSizeMB is the maximum size of the log file in megabytes before
	// it is rotated. Zero disables size-based rotation.
	MaxSizeMB int

	// Daily triggers a file rotation at midnight local time when true.
	Daily bool

	// MaxBackups is the number of rotated (old) log files to keep on
	// disk. Files beyond this count are deleted after each rotation.
	// Zero keeps all old files. Ignored when rotation is disabled.
	MaxBackups int
}

// Log wraps up to two zerolog.Logger instances (console and file) and
// satisfies both logger.Logger and logger.StructuredLogger.
//
// The hasConsole and hasFile booleans gate dispatch in the hot path,
// avoiding nil-interface checks and keeping emit() branch-predictable.
type Log struct {
	consoleLog zerolog.Logger
	fileLog    zerolog.Logger
	hasConsole bool
	hasFile    bool
}

// New constructs a Log according to opts.
//
// Returns an error when:
//   - Both ConsoleLevel and FileLevel are logger.Disabled (no active target).
//   - FileLevel != logger.Disabled but FilePath is empty.
//   - The log file cannot be opened or created.
//
// On error the caller should fall back via logger.Safe:
//
//	l, err := fclog.New(opts)
//	if err != nil {
//	    l = logger.Null()
//	}
func New(opts Options) (*Log, error) {
	if opts.ConsoleLevel == logger.Disabled && opts.FileLevel == logger.Disabled {
		return nil, errors.New("fclog.New: at least one target must be enabled")
	}

	out := &Log{}

	if opts.ConsoleLevel != logger.Disabled {
		out.consoleLog = buildConsoleLogger(os.Stderr, levelMap[opts.ConsoleLevel])
		out.hasConsole = true
	}

	if opts.FileLevel != logger.Disabled {
		if opts.FilePath == "" {
			return nil, errors.New("fclog.New: FilePath is required when FileLevel is set")
		}
		w, err := buildFileWriter(opts)
		if err != nil {
			return nil, fmt.Errorf("fclog.New: %w", err)
		}
		out.fileLog = buildFileLogger(w, levelMap[opts.FileLevel], opts.FileJSON)
		out.hasFile = true
	}

	return out, nil
}

// SetLevel sets the minimum output level on all active targets at once.
// To control targets independently use SetConsoleLevel / SetFileLevel.
//
// Integer scale mirrors common CLI verbosity flags:
//
//	<= 0  no-op (keep current level)
//	   1  Info
//	   2  Debug
//	>= 3  Trace
func (l *Log) SetLevel(lvl int) {
	zl := verbosityToZerolog(lvl)
	if zl == zerolog.NoLevel {
		return
	}
	if l.hasConsole {
		l.consoleLog = l.consoleLog.Level(zl)
	}
	if l.hasFile {
		l.fileLog = l.fileLog.Level(zl)
	}
}

// SetConsoleLevel sets the minimum output level for stderr only.
// No-op if the console target is inactive or lvl <= 0.
func (l *Log) SetConsoleLevel(lvl int) {
	if !l.hasConsole {
		return
	}
	zl := verbosityToZerolog(lvl)
	if zl == zerolog.NoLevel {
		return
	}
	l.consoleLog = l.consoleLog.Level(zl)
}

// SetFileLevel sets the minimum output level for the file target only.
// No-op if the file target is inactive or lvl <= 0.
func (l *Log) SetFileLevel(lvl int) {
	if !l.hasFile {
		return
	}
	zl := verbosityToZerolog(lvl)
	if zl == zerolog.NoLevel {
		return
	}
	l.fileLog = l.fileLog.Level(zl)
}

// Enabled reports whether messages at the given abstract level will be
// processed by at least one active target.
// Returns true as soon as one target accepts the level, without
// evaluating the other, to keep the common case fast.
func (l *Log) Enabled(lvl logger.Level) bool {
	if lvl == logger.Disabled || int(lvl) >= len(levelMap) {
		return false
	}
	zl := levelMap[lvl]
	if l.hasConsole && zl >= l.consoleLog.GetLevel() {
		return true
	}
	if l.hasFile && zl >= l.fileLog.GetLevel() {
		return true
	}
	return false
}

// WithField returns a new Logger with the given key-value pair attached
// to every subsequent log entry on all active targets.
// The receiver is not modified (zerolog loggers are value types).
// Satisfies logger.StructuredLogger.
func (l *Log) WithField(key string, value any) logger.Logger {
	derived := &Log{hasConsole: l.hasConsole, hasFile: l.hasFile}
	if l.hasConsole {
		derived.consoleLog = l.consoleLog.With().Interface(key, value).Logger()
	}
	if l.hasFile {
		derived.fileLog = l.fileLog.With().Interface(key, value).Logger()
	}
	return derived
}

// emit is the single hot-path dispatcher for all five log methods.
// Each active target applies its own level filter independently inside
// zerolog, so a Debug entry may appear in the file but be silenced on
// the console when their configured levels differ.
func (l *Log) emit(lvl zerolog.Level, format string, v []any) {
	if l.hasConsole {
		l.consoleLog.WithLevel(lvl).Msgf(format, v...)
	}
	if l.hasFile {
		l.fileLog.WithLevel(lvl).Msgf(format, v...)
	}
}

func (l *Log) Trace(format string, v ...any) { l.emit(zerolog.TraceLevel, format, v) }
func (l *Log) Debug(format string, v ...any) { l.emit(zerolog.DebugLevel, format, v) }
func (l *Log) Info(format string, v ...any)  { l.emit(zerolog.InfoLevel, format, v) }
func (l *Log) Warn(format string, v ...any)  { l.emit(zerolog.WarnLevel, format, v) }
func (l *Log) Error(format string, v ...any) { l.emit(zerolog.ErrorLevel, format, v) }

// buildConsoleLogger constructs a zerolog logger writing human-readable
// output to w. ANSI colour codes are enabled only when w is a TTY to
// prevent escape sequences from polluting redirected output.
func buildConsoleLogger(w *os.File, lvl zerolog.Level) zerolog.Logger {
	cw := zerolog.ConsoleWriter{
		TimeFormat: time.RFC3339,
		Out:        w,
		NoColor:    !isatty.IsTerminal(w.Fd()),
	}
	return zerolog.New(cw).With().Timestamp().Logger().Level(lvl)
}

// buildFileLogger constructs a zerolog logger writing to w.
// When json is true, output is newline-delimited JSON (structured,
// machine-readable). When false, output uses zerolog's ConsoleWriter
// in NoColor mode, producing human-readable text suitable for tailing
// with standard tools (tail, less, grep…).
func buildFileLogger(w io.Writer, lvl zerolog.Level, json bool) zerolog.Logger {
	if json {
		return zerolog.New(w).With().Timestamp().Logger().Level(lvl)
	}
	// NoColor is always true for file output: files are never a TTY,
	// and ANSI codes would corrupt the output when opened in editors or
	// processed by grep/awk.
	cw := zerolog.ConsoleWriter{
		TimeFormat: time.RFC3339,
		Out:        w,
		NoColor:    true,
	}
	return zerolog.New(cw).With().Timestamp().Logger().Level(lvl)
}

// buildFileWriter returns a thread-safe io.Writer for opts.FilePath.
//
// Without rotation (all rotation fields zero): opens the file in append
// mode. O_APPEND writes are atomic at the syscall level on POSIX for
// sizes below PIPE_BUF (~4 KB), which covers typical log lines.
//
// With rotation: delegates to lumberjack, which serialises all writes
// with an internal mutex and handles rename-and-reopen atomically.
// lumberjack is the de-facto standard rotation library in the zerolog
// and zap ecosystems.
func buildFileWriter(opts Options) (io.Writer, error) {
	rotationEnabled := opts.MaxSizeMB > 0 || opts.Daily || opts.MaxBackups > 0

	if !rotationEnabled {
		f, err := os.OpenFile(opts.FilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			return nil, err
		}
		return f, nil
	}

	lb := &lumberjack.Logger{
		Filename:   opts.FilePath,
		MaxBackups: opts.MaxBackups,
		// LocalTime appends local-timezone timestamps to rotated file
		// names, which is more readable in most operational contexts.
		LocalTime: true,
	}
	if opts.MaxSizeMB > 0 {
		lb.MaxSize = opts.MaxSizeMB
	}
	if opts.Daily {
		// MaxAge=1 with LocalTime=true causes lumberjack to rotate once
		// the file is older than 1 calendar day, effectively at midnight
		// local time.
		lb.MaxAge = 1
	}
	return lb, nil
}

// verbosityToZerolog maps a CLI verbosity integer to a zerolog.Level.
// Returns zerolog.NoLevel for values <= 0 so callers can treat the
// result as a no-op without a separate boolean return value.
func verbosityToZerolog(lvl int) zerolog.Level {
	switch {
	case lvl <= 0:
		return zerolog.NoLevel
	case lvl == 1:
		return zerolog.InfoLevel
	case lvl == 2:
		return zerolog.DebugLevel
	default:
		return zerolog.TraceLevel
	}
}
