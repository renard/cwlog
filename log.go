// Copyright © Sébastien Gross
//
// Created: 2021-12-19
// Last changed: 2026-02-25 02:51:13
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

// Package log provides a zerolog-backed implementation of the
// logger.Logger interface. It writes to stderr with RFC3339 timestamps
// and enables ANSI colour output when stderr is a terminal.

// Usage:
//
//	l := log.New()
//	l.SetLevel(2)          // 0=warn(default) 1=info 2=debug 3+=trace
//	l.Info("server ready")
//
//	// Structured fields via the StructuredLogger extension:
//	if sl, ok := any(l).(logger.StructuredLogger); ok {
//	    sl.WithField("port", 8080).Info("listening")
//	}
package log

import (
	"os"
	"time"

	"github.com/renard/cwlog/logger"

	"github.com/mattn/go-isatty"
	"github.com/rs/zerolog"
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

// Log wraps a zerolog.Logger and satisfies both logger.Logger and
// logger.StructuredLogger. The struct is intentionally small: zerolog
// itself is value-typed and cheap to copy when WithField creates a
// derived logger.
type Log struct {
	log zerolog.Logger
}

// New constructs a Log writing to stderr at WarnLevel.
// Colour output is enabled automatically when stderr is a TTY.
func New() *Log {
	logWr := os.Stderr
	isTerm := isatty.IsTerminal(logWr.Fd())

	consoleWriter := zerolog.ConsoleWriter{
		TimeFormat: time.RFC3339,
		Out:        logWr,
		// NoColor disables ANSI escape codes when output is redirected
		// to a file or pipe, preventing garbage in log files.
		NoColor: !isTerm,
	}

	return &Log{
		log: zerolog.New(consoleWriter).
			With().Timestamp().Logger().
			Level(zerolog.WarnLevel),
	}
}

// SetLevel adjusts the minimum output level at runtime.
// The integer scale mirrors common CLI verbosity flags (-v, -vv, -vvv):
//
//	<= 0  no-op (keep current level, typically Warn)
//	   1  Info
//	   2  Debug
//	>= 3  Trace
//
// Passing a value <= 0 is intentionally a no-op so that a flag that
// was never set leaves the default level intact.
func (l *Log) SetLevel(lvl int) {
	switch {
	case lvl <= 0:
		// Caller passed no verbosity flag; leave the default level alone.
		return
	case lvl == 1:
		l.log = l.log.Level(zerolog.InfoLevel)
	case lvl == 2:
		l.log = l.log.Level(zerolog.DebugLevel)
	default:
		l.log = l.log.Level(zerolog.TraceLevel)
	}
}

// Enabled reports whether messages at the given abstract level will be
// processed by the underlying zerolog instance.
// It uses the pre-built levelMap array for a branchless O(1) translation.
func (l *Log) Enabled(lvl logger.Level) bool {
	if int(lvl) >= len(levelMap) {
		return false
	}
	// zerolog's GetLevel returns the minimum active level; a message is
	// processed when its level is >= that threshold, which maps to
	// "abstract level <= configured level" in our convention.
	return levelMap[lvl] >= l.log.GetLevel()
}

// WithField returns a new Logger with the given key-value pair attached
// to every subsequent log entry. The receiver is not modified.
// This satisfies the logger.StructuredLogger interface.
func (l *Log) WithField(key string, value any) logger.Logger {
	return &Log{log: l.log.With().Interface(key, value).Logger()}
}

func (l *Log) Trace(format string, v ...any) {
	l.log.Trace().Msgf(format, v...)
}

func (l *Log) Debug(format string, v ...any) {
	l.log.Debug().Msgf(format, v...)
}

func (l *Log) Info(format string, v ...any) {
	l.log.Info().Msgf(format, v...)
}

func (l *Log) Warn(format string, v ...any) {
	l.log.Warn().Msgf(format, v...)
}

func (l *Log) Error(format string, v ...any) {
	l.log.Error().Msgf(format, v...)
}
