// Copyright © Sébastien Gross
//
// Created: 2026-02-25
// Last changed: 2026-02-25 03:12:50
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

// Package logger defines a minimal, reusable logging interface for Go
// projects. It is independent of any logging backend (zerolog, slog,
// logrus, etc.) and can be safely used in libraries or applications.
//
// Usage:
//
//	import "github.com/renard/cwlog/logger"
//
//	// 1. Safe default (null logger) if none is provided
//	log := logger.Safe(nil)
//
//	// 2. Stdlib fallback
//	log = logger.Std()
//
//	// 3. Using your own logger (zerolog, slog, etc.)
//	log = NewZeroLogger(myZerolog)
//
//	// 4. Using levels safely
//	if log.Enabled(logger.TraceLevel) {
//	    log.Trace("heavy payload: %+v", bigStruct)
//	}
//
//	// 5. Typical log calls
//	log.Info("starting service")
//	log.Warn("something minor happened")
//	log.Error("something bad happened")
//
//	// 6. Structured logging (optional, backend must implement StructuredLogger)
//	if sl, ok := log.(logger.StructuredLogger); ok {
//	    sl.WithField("requestID", id).Info("handled request")
//	}
package logger

import (
	stdlog "log"
)

// Level represents a logging verbosity level.
// Higher values mean more verbose output.
// The special value Disabled (-1) can be used by backends to indicate
// that a particular output target should be turned off entirely.
type Level int

const (
	// Disabled turns off a log target when used as a level in backend
	// option structs. It is not a valid argument to Logger.Enabled or
	// to any of the five log methods.
	Disabled Level = -1

	ErrorLevel Level = iota
	WarnLevel
	InfoLevel
	DebugLevel
	TraceLevel
)

// Logger is the interface libraries and applications should depend on.
// It covers standard log levels and an optional level-enabled check
// to skip expensive argument construction before hot log calls.
type Logger interface {
	Trace(format string, v ...any)
	Debug(format string, v ...any)
	Info(format string, v ...any)
	Warn(format string, v ...any)
	Error(format string, v ...any)

	// Enabled reports whether messages at the given level will be
	// processed. Callers can use this to skip expensive serialisation
	// when the level is inactive:
	//
	//   if log.Enabled(logger.TraceLevel) {
	//       log.Trace("dump: %+v", expensiveValue())
	//   }
	Enabled(Level) bool
}

// StructuredLogger is an optional extension of Logger for backends that
// support key-value structured fields (e.g. zerolog, slog). Libraries
// should never require this interface; use a runtime type assertion
// instead so code still compiles against plain Logger.
//
// Example:
//
//	if sl, ok := log.(logger.StructuredLogger); ok {
//	    log = sl.WithField("component", "parser")
//	}
type StructuredLogger interface {
	Logger

	// WithField returns a new Logger with the given key-value pair
	// attached to every subsequent log entry. The original logger is
	// left unchanged (immutable chaining).
	WithField(key string, value any) Logger
}

// nullLogger is a no-op implementation of Logger.
// All methods are empty so the compiler can inline and eliminate them.
type nullLogger struct{}

func (nullLogger) Trace(string, ...any) {}
func (nullLogger) Debug(string, ...any) {}
func (nullLogger) Info(string, ...any)  {}
func (nullLogger) Warn(string, ...any)  {}
func (nullLogger) Error(string, ...any) {}

// Enabled always returns false: a null logger never processes messages,
// which lets callers short-circuit any argument construction.
func (nullLogger) Enabled(Level) bool { return false }

// Null returns a Logger that silently discards all messages.
// Use it as a safe default in libraries to avoid nil-guard boilerplate.
func Null() Logger {
	return nullLogger{}
}

// prefixes holds the pre-built format prefix for each Level value.
// Indexed by Level (0=Error … 4=Trace) so lookup is a single array
// access with no allocation, avoiding a per-call string concatenation
// in the stdLogger hot path.
var prefixes = [...]string{
	ErrorLevel: "[ERROR] ",
	WarnLevel:  "[WARN]  ",
	InfoLevel:  "[INFO]  ",
	DebugLevel: "[DEBUG] ",
	TraceLevel: "[TRACE] ",
}

// stdLogger wraps Go's standard log package.
// It is intentionally simple: no goroutine, no channel, direct Printf.
type stdLogger struct {
	l   *stdlog.Logger
	lvl Level
}

// Std returns a Logger backed by the default stdlib logger at WarnLevel.
// Suitable as a fallback during development or in simple CLIs.
func Std() Logger {
	return stdLogger{l: stdlog.Default(), lvl: WarnLevel}
}

// StdWithLevel returns a stdlib Logger at the given level.
// Returns the Logger interface so callers stay decoupled from the
// concrete type.
func StdWithLevel(lvl Level) Logger {
	return stdLogger{l: stdlog.Default(), lvl: lvl}
}

// logf is the single hot-path helper for all stdLogger methods.
// The level guard happens here once, and the prefix is read from the
// pre-allocated prefixes array to avoid string concatenation.
func (s stdLogger) logf(lvl Level, format string, v []any) {
	if lvl > s.lvl {
		// Level inactive: return immediately with zero allocations.
		return
	}
	// prefixes[lvl] is a compile-time constant string; the concatenation
	// with format produces one allocation per actual log call, which is
	// acceptable since we already know output will be produced.
	s.l.Printf(prefixes[lvl]+format, v...)
}

func (s stdLogger) Trace(f string, v ...any) { s.logf(TraceLevel, f, v) }
func (s stdLogger) Debug(f string, v ...any) { s.logf(DebugLevel, f, v) }
func (s stdLogger) Info(f string, v ...any)  { s.logf(InfoLevel, f, v) }
func (s stdLogger) Warn(f string, v ...any)  { s.logf(WarnLevel, f, v) }
func (s stdLogger) Error(f string, v ...any) { s.logf(ErrorLevel, f, v) }

// Enabled reports whether the given level is at or below the configured
// threshold. Stdlib has no native level concept so we emulate it here.
func (s stdLogger) Enabled(l Level) bool {
	return l <= s.lvl
}

// Safe returns l if it is non-nil, or Null() otherwise.
// Use it in library constructors to guarantee a usable logger without
// forcing callers to always provide one:
//
//	func New(log logger.Logger) *MyService {
//	    return &MyService{log: logger.Safe(log)}
//	}
func Safe(l Logger) Logger {
	if l == nil {
		return Null()
	}
	return l
}

// Optional: adapters for real loggers (example: zerolog)
// Users can implement this interface in their own modules.

// Example adapter pattern:
// type ZeroAdapter struct {
//     log zerolog.Logger
// }
// func (z ZeroAdapter) Trace(f string, v ...any) { z.log.Trace().Msgf(f, v...) }
// func (z ZeroAdapter) Debug(f string, v ...any) { z.log.Debug().Msgf(f, v...) }
// func (z ZeroAdapter) Info(f string, v ...any)  { z.log.Info().Msgf(f, v...) }
// func (z ZeroAdapter) Warn(f string, v ...any)  { z.log.Warn().Msgf(f, v...) }
// func (z ZeroAdapter) Error(f string, v ...any) { z.log.Error().Msgf(f, v...) }
// func (z ZeroAdapter) Enabled(level Level) bool { ... }
