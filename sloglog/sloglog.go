// Copyright © Sébastien Gross
//
// Created: 2026-04-28
// Last changed: 2026-04-28
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

// Package sloglog provides a [log/slog.Handler] that forwards log
// records to any [logger.Logger] implementation.
//
// Use it to capture logs from libraries that emit via slog and route
// them to the cwlog backend of your choice (zerolog, fclog, …).
//
// Usage:
//
//	// Create your cwlog backend.
//	l, _ := fclog.New(fclog.Options{ConsoleLevel: logger.InfoLevel})
//
//	// Route slog output to it.
//	slog.SetDefault(slog.New(sloglog.NewHandler(l)))
//
//	// Or inject into a third-party library that accepts a *slog.Logger:
//	somelib.Init(slog.New(sloglog.NewHandler(l)))
//
// When the underlying logger implements [logger.StructuredLogger],
// attributes added via [slog.Logger.With] or [slog.Logger.WithGroup]
// are forwarded as key-value fields using [logger.StructuredLogger.WithField].
// Otherwise they are serialised as " key=value" suffixes appended to the
// message.
package sloglog

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/renard/cwlog/logger"
)

// toCwlogLevel translates a slog.Level to the closest logger.Level.
//
// slog uses an ordered integer scale (Debug=-4, Info=0, Warn=4, Error=8)
// and explicitly allows custom intermediate values. The >= comparisons
// handle those by rounding up to the nearest standard tier.
// Values below slog.LevelDebug are mapped to TraceLevel.
func toCwlogLevel(lvl slog.Level) logger.Level {
	switch {
	case lvl >= slog.LevelError:
		return logger.ErrorLevel
	case lvl >= slog.LevelWarn:
		return logger.WarnLevel
	case lvl >= slog.LevelInfo:
		return logger.InfoLevel
	case lvl >= slog.LevelDebug:
		return logger.DebugLevel
	default:
		return logger.TraceLevel
	}
}

// Handler is a [log/slog.Handler] that delegates to a [logger.Logger].
//
// Structured attributes are handled in two ways depending on whether the
// underlying logger implements [logger.StructuredLogger]:
//
//   - Structured path: each attribute is attached via WithField; the
//     derived logger is stored so that all subsequent records carry the
//     field natively (e.g. as a JSON key in zerolog output).
//
//   - Text path: attributes are serialised as " key=value" pairs and
//     appended to the message string at Handle time.
//
// Handler values are safe to copy; all mutating operations (WithAttrs,
// WithGroup) return a new Handler without modifying the receiver.
type Handler struct {
	log    logger.Logger
	suffix string // pre-formatted " k=v" pairs; non-empty only on the text path
	groups string // current group prefix, e.g. "http.request."
}

// NewHandler returns a [log/slog.Handler] that forwards records to l.
// Passing nil is safe: [logger.Safe] promotes it to a null logger.
func NewHandler(l logger.Logger) *Handler {
	return &Handler{log: logger.Safe(l)}
}

// Enabled reports whether log records at the given slog level will be
// processed. It delegates to the underlying logger after translating
// the level.
func (h *Handler) Enabled(_ context.Context, lvl slog.Level) bool {
	return h.log.Enabled(toCwlogLevel(lvl))
}

// WithAttrs returns a new Handler with the given attributes added.
//
// If the underlying logger implements [logger.StructuredLogger], each
// attribute is applied as a field via WithField (structured path).
// Otherwise, attributes are formatted as " key=value" suffixes (text
// path).
//
// Nested slog groups (KindGroup) are flattened with a dot-separated
// prefix. [slog.LogValuer] values are resolved before processing.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}

	nh := &Handler{log: h.log, suffix: h.suffix, groups: h.groups}

	if sl, ok := h.log.(logger.StructuredLogger); ok {
		// Structured path: attach each attr as a field. The starting point
		// is h.log which may already carry fields from prior WithAttrs calls.
		log := logger.Logger(sl)
		for _, a := range attrs {
			applyAttrStructured(&log, h.groups, a)
		}
		nh.log = log
		// suffix stays "" for the structured path.
	} else {
		// Text path: serialize into the suffix string.
		var b strings.Builder
		b.WriteString(h.suffix)
		for _, a := range attrs {
			appendAttr(&b, h.groups, a)
		}
		nh.suffix = b.String()
	}

	return nh
}

// WithGroup returns a new Handler with the given group name pushed onto
// the attribute key prefix. Subsequent WithAttrs calls on the new handler
// will prefix their keys with "<name>.".
//
// An empty name is a no-op as required by the slog contract.
func (h *Handler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	return &Handler{
		log:    h.log,
		suffix: h.suffix,
		groups: h.groups + name + ".",
	}
}

// Handle forwards r to the underlying logger at the mapped cwlog level.
//
// Per-record attributes (those passed inline to slog.Info, slog.Debug,
// etc.) are handled the same way as WithAttrs attributes: fields for
// structured loggers, message suffix for plain loggers.
//
// Handle always returns nil; errors in the underlying logger are
// silently discarded in keeping with slog's contract.
func (h *Handler) Handle(_ context.Context, r slog.Record) error {
	cwlvl := toCwlogLevel(r.Level)
	if !h.log.Enabled(cwlvl) {
		return nil
	}

	log := h.log

	// Determine message: start with the record's message, then append
	// per-record attributes. The approach mirrors WithAttrs: structured
	// loggers use a temporary derived logger; plain loggers get a suffix.
	msg := r.Message
	if r.NumAttrs() > 0 {
		if _, ok := log.(logger.StructuredLogger); ok {
			// Structured path: derive a one-shot logger for this record.
			r.Attrs(func(a slog.Attr) bool {
				applyAttrStructured(&log, h.groups, a)
				return true
			})
		} else {
			// Text path: append per-record attrs after h.suffix.
			var b strings.Builder
			b.WriteString(r.Message)
			b.WriteString(h.suffix)
			r.Attrs(func(a slog.Attr) bool {
				appendAttr(&b, h.groups, a)
				return true
			})
			msg = b.String()
		}
	} else if h.suffix != "" {
		// No per-record attrs but there is a pre-built suffix (text path).
		msg = r.Message + h.suffix
	}

	dispatch(log, cwlvl, msg)
	return nil
}

// dispatch calls the appropriate method on l for the given cwlog level.
// Using "%s" avoids any format-string interpretation of the message.
func dispatch(l logger.Logger, lvl logger.Level, msg string) {
	switch lvl {
	case logger.TraceLevel:
		l.Trace("%s", msg)
	case logger.DebugLevel:
		l.Debug("%s", msg)
	case logger.InfoLevel:
		l.Info("%s", msg)
	case logger.WarnLevel:
		l.Warn("%s", msg)
	default:
		l.Error("%s", msg)
	}
}

// applyAttrStructured attaches a single slog.Attr to *log via WithField.
// KindGroup attrs are flattened: each sub-attr is applied with a
// "<group>." prefix. LogValuer values are resolved before use.
func applyAttrStructured(log *logger.Logger, prefix string, a slog.Attr) {
	a = slog.Attr{Key: a.Key, Value: a.Value.Resolve()}
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		sub := prefix + a.Key + "."
		for _, child := range a.Value.Group() {
			applyAttrStructured(log, sub, child)
		}
		return
	}
	if sl, ok := (*log).(logger.StructuredLogger); ok {
		*log = sl.WithField(prefix+a.Key, a.Value.Any())
	}
}

// appendAttr serialises a slog.Attr as " key=value" and writes it to b.
// KindGroup attrs are flattened recursively. LogValuer values are resolved.
func appendAttr(b *strings.Builder, prefix string, a slog.Attr) {
	a = slog.Attr{Key: a.Key, Value: a.Value.Resolve()}
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		sub := prefix + a.Key + "."
		for _, child := range a.Value.Group() {
			appendAttr(b, sub, child)
		}
		return
	}
	fmt.Fprintf(b, " %s%s=%v", prefix, a.Key, a.Value.Any())
}
