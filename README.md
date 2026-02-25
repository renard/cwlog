# cwlog

A minimal, backend-agnostic logging interface for Go libraries and
applications.

## Objectives

- Provide a single `Logger` interface that any project can depend on
  without importing a specific logging library.
- Keep the interface as small as possible: five log levels plus an
  `Enabled` check.
- Offer an optional `StructuredLogger` extension for backends that
  support key-value fields, without breaking compatibility with simple
  loggers.
- Expose two ready-to-use implementations: a null logger (safe
  default) and a stdlib fallback.
- Make it trivial to swap logging backends (zerolog, slog, logrus…)
  without touching library code.

## Motivation

Go has no standard logging interface. Every library that picks a
concrete logger (zerolog, logrus, slog…) forces that dependency on its
consumers. The result is either a tangle of adapter glue or a
proliferation of `logger.Logger` parameters that are `nil`-checked
everywhere.

This module solves that by defining the interface once and keeping it
in a package with zero non-stdlib dependencies. Libraries import
`logger`, applications wire in whichever backend they prefer. The null
logger means a library never needs to guard against a nil logger —
`logger.Safe(nil)` always returns something safe to call.

The `StructuredLogger` extension follows the same principle:
structured fields are opt-in. Code that does not need them stays
simple; code that does can assert the interface at runtime without
requiring every backend to support it.

## Usage

### 1. Library — accept a logger, use a safe default

```go
package parser

import "github.com/renard/cwlog/logger"

type Parser struct {
    log logger.Logger
}

func New(log logger.Logger) *Parser {
    // Safe returns Null() if log is nil, so the library never panics.
    return &Parser{log: logger.Safe(log)}
}

func (p *Parser) Parse(input string) error {
    p.log.Debug("parsing input of %d bytes", len(input))
    // ...
    p.log.Info("parse complete")
    return nil
}
```

### 2. Application — wire in zerolog and adjust verbosity at runtime

```go
package main

import (
    "flag"
    "myproject/parser"
    log "github.com/renard/log"
)

func main() {
    verbose := flag.Int("v", 0, "verbosity: 1=info 2=debug 3=trace")
    flag.Parse()

    l := log.New()
    l.SetLevel(*verbose)

    p := parser.New(l)
    p.Parse("...")
}
```

### 3. Application — with package variable


```go
# parser/logger.go
package parser

import (
	"github.com/renard/cwlog/logger"
)

var (
	log logger.Logger = logger.StdWithLevel(3)
)

func SetLog(l logger.Logger) {
	log = l
}

```

```go
# parser/parser.go
package parser


func DoSomething() {
	log.Info("In DoSomething")
}

```

```go
package main

import (
	"parser"

	log "github.com/renard/cwlog"
)

main () {
	l := log.New()
	l.SetLevel(1)
	parser.SetLog(l)
	parser.DoSomething()
}
```

### 4. Structured fields — opt-in, no change to library code

```go
package main

import (
    log "github.com/renard/cwlog"
    "github.com/renard/cwlog/logger"
)

func handleRequest(baseLog logger.Logger, requestID string) {
    // Attach a field to every log entry within this request scope,
    // only if the backend supports it.
    log := baseLog
    if sl, ok := baseLog.(logger.StructuredLogger); ok {
        log = sl.WithField("requestID", requestID)
    }

    log.Info("handling request")
    // All entries from this point carry requestID automatically.
}

func main() {
    l := log.New()
    l.SetLevel(1)
    handleRequest(l, "abc-123")
}
```

### 5. fclog — file and console with independent levels and rotation

`fclog` is a self-contained backend that writes to stderr, a file, or
both. Everything is configured through a single `Options` struct. Set a
level to `logger.Disabled` to turn off that target entirely.

```go
package main

import (
    "flag"
    "github.com/renard/cwlog/fclog"
    "github.com/renard/cwlog/logger"
    "myproject/parser"
)

func main() {
    verbose := flag.Int("v", 0, "verbosity: 1=info 2=debug 3=trace")
    flag.Parse()

    // Console at Warn, file at Debug in plain text, daily rotation,
    // keep the last 7 files.
    l, err := fclog.New(fclog.Options{
        ConsoleLevel: logger.WarnLevel,
        FileLevel:    logger.DebugLevel,
        FilePath:     "/var/log/myapp.log",
        Daily:        true,
        MaxBackups:   7,
    })
    if err != nil {
        // Fall back to a null logger; the application keeps running.
        l = logger.Null()
    }

    // Override the file level via the verbosity flag at startup.
    l.SetFileLevel(*verbose)

    p := parser.New(l)
    p.Parse("...")
}
```

To get JSON in the file instead of plain text (useful for log
aggregators such as Loki or Datadog), set `FileJSON: true`:

```go
l, err := fclog.New(fclog.Options{
    ConsoleLevel: logger.WarnLevel,
    FileLevel:    logger.DebugLevel,
    FilePath:     "/var/log/myapp.log",
    FileJSON:     true,
    MaxSizeMB:    100,
    MaxBackups:   5,
})
```

To write to a file only, set `ConsoleLevel` to `logger.Disabled`:

```go
l, err := fclog.New(fclog.Options{
    ConsoleLevel: logger.Disabled,
    FileLevel:    logger.InfoLevel,
    FilePath:     "/var/log/myapp.log",
})
```

## Package layout

```
cwlog/
├── logger/        interface, Disabled, Null, Std — no external deps
├── log.go         zerolog console logger (stderr only)
└── fclog/
    └── log.go     zerolog file + console logger with optional rotation
```

## Implementing a custom backend

Any type that satisfies `logger.Logger` can be passed wherever a
logger is expected:

```go
type MyLogger struct{ /* ... */ }

func (m MyLogger) Trace(f string, v ...any) { /* ... */ }
func (m MyLogger) Debug(f string, v ...any) { /* ... */ }
func (m MyLogger) Info(f string, v ...any)  { /* ... */ }
func (m MyLogger) Warn(f string, v ...any)  { /* ... */ }
func (m MyLogger) Error(f string, v ...any) { /* ... */ }
func (m MyLogger) Enabled(l logger.Level) bool { /* ... */ }
```

To support structured fields, also implement `WithField`:

```go
func (m MyLogger) WithField(key string, value any) logger.Logger {
    // return a derived logger with the field attached
}
```

## License

Copyright © Sébastien Gross

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful, but
WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public
License along with this program. If not, see
<http://www.gnu.org/licenses/>.
