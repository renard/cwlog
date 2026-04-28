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

package sloglog_test

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/renard/cwlog/logger"
	"github.com/renard/cwlog/sloglog"
)

// --- Test doubles ---------------------------------------------------------

// loggedCall records a single dispatch to one of the five log methods.
type loggedCall struct {
	level  logger.Level
	msg    string
	fields map[string]any // only populated for structCapture
}

// structCapture implements both logger.Logger and logger.StructuredLogger.
// All instances derived from the same root (via WithField) share a single
// calls slice so that tests can inspect all records regardless of which
// derived logger did the actual writing.
type structCapture struct {
	calls  *[]loggedCall
	fields map[string]any
	lvl    logger.Level
}

func newStructCapture(lvl logger.Level) *structCapture {
	calls := make([]loggedCall, 0, 8)
	return &structCapture{calls: &calls, fields: map[string]any{}, lvl: lvl}
}

func (c *structCapture) Enabled(lvl logger.Level) bool { return lvl <= c.lvl }

func (c *structCapture) emit(lvl logger.Level, format string, v []any) {
	if !c.Enabled(lvl) {
		return
	}
	msg := format
	if len(v) > 0 {
		msg = fmt.Sprintf(format, v...)
	}
	snap := make(map[string]any, len(c.fields))
	for k, val := range c.fields {
		snap[k] = val
	}
	*c.calls = append(*c.calls, loggedCall{level: lvl, msg: msg, fields: snap})
}

func (c *structCapture) Trace(f string, v ...any) { c.emit(logger.TraceLevel, f, v) }
func (c *structCapture) Debug(f string, v ...any) { c.emit(logger.DebugLevel, f, v) }
func (c *structCapture) Info(f string, v ...any)  { c.emit(logger.InfoLevel, f, v) }
func (c *structCapture) Warn(f string, v ...any)  { c.emit(logger.WarnLevel, f, v) }
func (c *structCapture) Error(f string, v ...any) { c.emit(logger.ErrorLevel, f, v) }

// WithField returns a new structCapture with the given field added,
// sharing the same calls slice.
func (c *structCapture) WithField(key string, value any) logger.Logger {
	newFields := make(map[string]any, len(c.fields)+1)
	for k, v := range c.fields {
		newFields[k] = v
	}
	newFields[key] = value
	return &structCapture{calls: c.calls, fields: newFields, lvl: c.lvl}
}

// plainCapture implements only logger.Logger (no WithField).
// It is used to exercise the text-path (suffix appended to message).
type plainCapture struct {
	calls []loggedCall
	lvl   logger.Level
}

func (c *plainCapture) Enabled(lvl logger.Level) bool { return lvl <= c.lvl }

func (c *plainCapture) emit(lvl logger.Level, format string, v []any) {
	if !c.Enabled(lvl) {
		return
	}
	msg := format
	if len(v) > 0 {
		msg = fmt.Sprintf(format, v...)
	}
	c.calls = append(c.calls, loggedCall{level: lvl, msg: msg})
}

func (c *plainCapture) Trace(f string, v ...any) { c.emit(logger.TraceLevel, f, v) }
func (c *plainCapture) Debug(f string, v ...any) { c.emit(logger.DebugLevel, f, v) }
func (c *plainCapture) Info(f string, v ...any)  { c.emit(logger.InfoLevel, f, v) }
func (c *plainCapture) Warn(f string, v ...any)  { c.emit(logger.WarnLevel, f, v) }
func (c *plainCapture) Error(f string, v ...any) { c.emit(logger.ErrorLevel, f, v) }

// newRecord is a helper that builds a slog.Record with no PC.
func newRecord(lvl slog.Level, msg string) slog.Record {
	return slog.NewRecord(time.Time{}, lvl, msg, 0)
}

// --- Compile-time interface check -----------------------------------------

// TestNewHandler_implementsSlogHandler verifies at compile time that
// *Handler satisfies the slog.Handler interface.
func TestNewHandler_implementsSlogHandler(t *testing.T) {
	var _ slog.Handler = sloglog.NewHandler(logger.Null())
}

// --- Enabled --------------------------------------------------------------

// TestEnabled_levelMapping verifies that Enabled correctly translates slog
// levels to cwlog levels and delegates to the underlying logger.
func TestEnabled_levelMapping(t *testing.T) {
	cases := []struct {
		name        string
		loggerLevel logger.Level // configured level of the underlying logger
		slogLevel   slog.Level  // queried via Enabled
		want        bool
	}{
		// Standard tiers: messages AT the configured level must be enabled.
		{"error/error", logger.ErrorLevel, slog.LevelError, true},
		{"warn/warn", logger.WarnLevel, slog.LevelWarn, true},
		{"info/info", logger.InfoLevel, slog.LevelInfo, true},
		{"debug/debug", logger.DebugLevel, slog.LevelDebug, true},
		{"trace/below-debug", logger.TraceLevel, slog.LevelDebug - 4, true},

		// More verbose than configured → disabled.
		{"error: info disabled", logger.ErrorLevel, slog.LevelInfo, false},
		{"warn: debug disabled", logger.WarnLevel, slog.LevelDebug, false},
		{"info: trace disabled", logger.InfoLevel, slog.LevelDebug - 1, false},

		// Less verbose than configured → enabled.
		{"debug: warn enabled", logger.DebugLevel, slog.LevelWarn, true},
		{"trace: error enabled", logger.TraceLevel, slog.LevelError, true},

		// Custom intermediate slog levels (mapped by >=).
		// WARN+1 is still >= LevelWarn → maps to WarnLevel → enabled.
		{"custom between warn/error", logger.WarnLevel, slog.LevelWarn + 1, true},
		// INFO+1 is still >= LevelInfo but < LevelWarn → maps to InfoLevel → enabled.
		{"custom between info/warn", logger.InfoLevel, slog.LevelInfo + 1, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sc := newStructCapture(c.loggerLevel)
			h := sloglog.NewHandler(sc)
			got := h.Enabled(context.Background(), c.slogLevel)
			if got != c.want {
				t.Errorf("Enabled(%v) with logger at %v = %v, want %v",
					c.slogLevel, c.loggerLevel, got, c.want)
			}
		})
	}
}

// --- Handle: level dispatch -----------------------------------------------

// TestHandle_dispatchLevels verifies that each slog level is forwarded to
// the correct cwlog method.
func TestHandle_dispatchLevels(t *testing.T) {
	cases := []struct {
		slogLevel slog.Level
		wantLevel logger.Level
	}{
		{slog.LevelError, logger.ErrorLevel},
		{slog.LevelWarn, logger.WarnLevel},
		{slog.LevelInfo, logger.InfoLevel},
		{slog.LevelDebug, logger.DebugLevel},
		{slog.LevelDebug - 4, logger.TraceLevel},
		// Custom levels rounded by the >= mapping.
		{slog.LevelError + 1, logger.ErrorLevel},
		{slog.LevelWarn + 1, logger.WarnLevel},
		{slog.LevelInfo + 1, logger.InfoLevel},
		{slog.LevelDebug + 1, logger.DebugLevel},
		{slog.LevelDebug - 1, logger.TraceLevel},
	}

	for _, c := range cases {
		sc := newStructCapture(logger.TraceLevel) // all levels active
		h := sloglog.NewHandler(sc)
		r := newRecord(c.slogLevel, "msg")
		if err := h.Handle(context.Background(), r); err != nil {
			t.Fatalf("Handle returned error: %v", err)
		}
		if len(*sc.calls) != 1 {
			t.Errorf("slog %v: expected 1 call, got %d", c.slogLevel, len(*sc.calls))
			continue
		}
		if (*sc.calls)[0].level != c.wantLevel {
			t.Errorf("slog %v: dispatched to cwlog %v, want %v",
				c.slogLevel, (*sc.calls)[0].level, c.wantLevel)
		}
	}
}

// TestHandle_messagePassthrough verifies that the record message is
// forwarded unchanged when there are no attributes.
func TestHandle_messagePassthrough(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc)

	r := newRecord(slog.LevelInfo, "hello world")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	if got := (*sc.calls)[0].msg; got != "hello world" {
		t.Errorf("msg = %q, want %q", got, "hello world")
	}
}

// TestHandle_disabledLevel verifies that a record whose level is below the
// configured threshold produces no call on the underlying logger.
func TestHandle_disabledLevel(t *testing.T) {
	sc := newStructCapture(logger.ErrorLevel) // only Error active
	h := sloglog.NewHandler(sc)

	r := newRecord(slog.LevelInfo, "should be dropped")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 0 {
		t.Errorf("expected 0 calls for disabled level, got %d", len(*sc.calls))
	}
}

// --- Handle: per-record attributes ----------------------------------------

// TestHandle_perRecordAttrs_textPath verifies that attrs added inline to a
// slog call (e.g. slog.Info("msg", "k", v)) appear in the message when the
// underlying logger is a plain logger.
func TestHandle_perRecordAttrs_textPath(t *testing.T) {
	pc := &plainCapture{lvl: logger.TraceLevel}
	h := sloglog.NewHandler(pc)

	r := newRecord(slog.LevelInfo, "request")
	r.AddAttrs(slog.String("method", "GET"), slog.Int("status", 200))
	_ = h.Handle(context.Background(), r)

	if len(pc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(pc.calls))
	}
	msg := pc.calls[0].msg
	for _, want := range []string{"request", "method=GET", "status=200"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

// TestHandle_perRecordAttrs_structuredPath verifies that inline attrs are
// forwarded as fields (via WithField) when the underlying logger implements
// StructuredLogger, and do NOT appear in the message.
func TestHandle_perRecordAttrs_structuredPath(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc)

	r := newRecord(slog.LevelInfo, "request")
	r.AddAttrs(slog.String("method", "GET"), slog.Int("status", 200))
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	call := (*sc.calls)[0]

	if call.msg != "request" {
		t.Errorf("msg = %q, want %q (fields must not pollute message)", call.msg, "request")
	}
	if call.fields["method"] != "GET" {
		t.Errorf("field method = %v, want GET", call.fields["method"])
	}
	// slog.Int stores values as int64 internally.
	if call.fields["status"] != int64(200) {
		t.Errorf("field status = %v (%T), want int64(200)", call.fields["status"], call.fields["status"])
	}
}

// --- WithAttrs ------------------------------------------------------------

// TestWithAttrs_textPath verifies that attributes added via WithAttrs are
// serialised as " key=value" and appended to the message for plain loggers.
func TestWithAttrs_textPath(t *testing.T) {
	pc := &plainCapture{lvl: logger.TraceLevel}
	h := sloglog.NewHandler(pc).WithAttrs([]slog.Attr{
		slog.String("component", "auth"),
		slog.Int("version", 3),
	})

	r := newRecord(slog.LevelInfo, "login")
	_ = h.Handle(context.Background(), r)

	if len(pc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(pc.calls))
	}
	msg := pc.calls[0].msg
	for _, want := range []string{"login", "component=auth", "version=3"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

// TestWithAttrs_structuredPath verifies that attributes added via WithAttrs
// are forwarded as fields for structured loggers and do not appear in the
// message.
func TestWithAttrs_structuredPath(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc).WithAttrs([]slog.Attr{
		slog.String("svc", "api"),
	})

	r := newRecord(slog.LevelInfo, "start")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	call := (*sc.calls)[0]

	if call.msg != "start" {
		t.Errorf("msg = %q, want %q (fields must not be in message)", call.msg, "start")
	}
	if call.fields["svc"] != "api" {
		t.Errorf("field svc = %v, want api", call.fields["svc"])
	}
}

// TestWithAttrs_empty verifies that WithAttrs with an empty slice returns
// an equivalent handler (no panic, no change).
func TestWithAttrs_empty(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc).WithAttrs(nil)

	r := newRecord(slog.LevelInfo, "msg")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
}

// TestWithAttrs_doesNotMutateOriginal verifies that calling WithAttrs on a
// handler does not affect the original handler (immutable chaining).
func TestWithAttrs_doesNotMutateOriginal(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	original := sloglog.NewHandler(sc)
	derived := original.WithAttrs([]slog.Attr{slog.String("x", "1")})

	r := newRecord(slog.LevelInfo, "msg")
	_ = original.Handle(context.Background(), r)
	_ = derived.Handle(context.Background(), r)

	if len(*sc.calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(*sc.calls))
	}
	// Original call must not carry the field.
	if _, ok := (*sc.calls)[0].fields["x"]; ok {
		t.Error("original handler's call carried field from derived handler")
	}
	// Derived call must carry the field.
	if (*sc.calls)[1].fields["x"] != "1" {
		t.Errorf("derived handler's call: field x = %v, want 1", (*sc.calls)[1].fields["x"])
	}
}

// --- WithGroup ------------------------------------------------------------

// TestWithGroup_prefixesAttrs verifies that attributes added after
// WithGroup carry a "name." prefix.
func TestWithGroup_prefixesAttrs_textPath(t *testing.T) {
	pc := &plainCapture{lvl: logger.TraceLevel}
	h := sloglog.NewHandler(pc).WithGroup("http").WithAttrs([]slog.Attr{
		slog.String("method", "GET"),
	})

	r := newRecord(slog.LevelInfo, "req")
	_ = h.Handle(context.Background(), r)

	if len(pc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(pc.calls))
	}
	msg := pc.calls[0].msg
	if !strings.Contains(msg, "http.method=GET") {
		t.Errorf("message %q missing %q", msg, "http.method=GET")
	}
}

// TestWithGroup_prefixesFields_structuredPath verifies that attributes added
// after WithGroup carry the group prefix when forwarded as fields.
func TestWithGroup_prefixesFields_structuredPath(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc).WithGroup("http").WithAttrs([]slog.Attr{
		slog.String("method", "GET"),
	})

	r := newRecord(slog.LevelInfo, "req")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	if (*sc.calls)[0].fields["http.method"] != "GET" {
		t.Errorf("field http.method = %v, want GET", (*sc.calls)[0].fields["http.method"])
	}
}

// TestWithGroup_empty verifies that WithGroup("") is a no-op.
func TestWithGroup_empty(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc).WithGroup("")

	r := newRecord(slog.LevelInfo, "msg")
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
}

// --- slog.Group (inline group attr) --------------------------------------

// TestHandle_inlineGroupAttr verifies that an inline slog.Group attr
// (e.g. slog.Group("req", "method", "GET")) is flattened with dot-
// separated keys.
func TestHandle_inlineGroupAttr_textPath(t *testing.T) {
	pc := &plainCapture{lvl: logger.TraceLevel}
	h := sloglog.NewHandler(pc)

	r := newRecord(slog.LevelInfo, "incoming")
	r.AddAttrs(slog.Group("req",
		slog.String("method", "GET"),
		slog.Int("status", 200),
	))
	_ = h.Handle(context.Background(), r)

	if len(pc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(pc.calls))
	}
	msg := pc.calls[0].msg
	for _, want := range []string{"req.method=GET", "req.status=200"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q missing %q", msg, want)
		}
	}
}

// TestHandle_inlineGroupAttr_structuredPath verifies group flattening with
// a structured logger.
func TestHandle_inlineGroupAttr_structuredPath(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	h := sloglog.NewHandler(sc)

	r := newRecord(slog.LevelInfo, "incoming")
	r.AddAttrs(slog.Group("req",
		slog.String("method", "GET"),
	))
	_ = h.Handle(context.Background(), r)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	if (*sc.calls)[0].fields["req.method"] != "GET" {
		t.Errorf("field req.method = %v, want GET", (*sc.calls)[0].fields["req.method"])
	}
}

// --- Nil / null logger ----------------------------------------------------

// TestNewHandler_nilLogger verifies that passing nil is safe and produces
// a handler that silently discards all records.
func TestNewHandler_nilLogger(t *testing.T) {
	h := sloglog.NewHandler(nil)
	r := newRecord(slog.LevelError, "should not panic")
	if err := h.Handle(context.Background(), r); err != nil {
		t.Errorf("Handle returned unexpected error: %v", err)
	}
}

// TestNewHandler_nullLogger verifies that a null logger discards all records
// silently.
func TestNewHandler_nullLogger(t *testing.T) {
	h := sloglog.NewHandler(logger.Null())
	r := newRecord(slog.LevelError, "discarded")
	_ = h.Handle(context.Background(), r) // must not panic
}

// --- slog integration -----------------------------------------------------

// TestSlogSetDefault verifies end-to-end that slog.New wrapping a Handler
// can be used as the default slog logger without panicking.
func TestSlogSetDefault(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	sl := slog.New(sloglog.NewHandler(sc))

	sl.Info("service ready", "port", 8080)

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	call := (*sc.calls)[0]
	if call.level != logger.InfoLevel {
		t.Errorf("level = %v, want Info", call.level)
	}
	if !strings.Contains(call.msg, "service ready") {
		t.Errorf("msg = %q, want to contain %q", call.msg, "service ready")
	}
}

// TestSlogWith verifies that slog.Logger.With forwards attributes to the
// underlying cwlog backend.
func TestSlogWith(t *testing.T) {
	sc := newStructCapture(logger.TraceLevel)
	sl := slog.New(sloglog.NewHandler(sc)).With("component", "cache")

	sl.Warn("eviction")

	if len(*sc.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(*sc.calls))
	}
	if (*sc.calls)[0].fields["component"] != "cache" {
		t.Errorf("field component = %v, want cache", (*sc.calls)[0].fields["component"])
	}
}
