// Package logx is the library's diagnostics sink: a leveled, zero-dependency
// logger writing to stderr. The default level is Info; the dispatcher picks
// up --xyz.log-level (code-side: logx.SetLevel).
//
// The library only ever writes diagnostics here — command results and usage
// errors stay on their own output/error streams and are unaffected by the
// log level.
package logx

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

// Level is a verbosity level. Higher = quieter.
type Level int32

const (
	// LevelUnset 排在 0 位：它是 Config.LogLevel 的 Go 零值，语义为「保持默认」。
	LevelUnset Level = iota
	// LevelDebug enables dispatch/negotiation tracing (handy for MCP).
	LevelDebug
	// LevelInfo is the default: startup notices, mode chosen.
	LevelInfo
	// LevelWarn keeps warnings only (disabled channels, unprotected transports).
	LevelWarn
	// LevelError keeps error diagnostics only.
	LevelError
)

var current atomic.Int32

func init() {
	current.Store(int32(LevelInfo))
}

// SetLevel sets the process-wide verbosity level.
func SetLevel(l Level) {
	if l >= LevelDebug && l <= LevelError {
		current.Store(int32(l))
	}
}

// Enabled reports whether messages at level l would be printed.
func Enabled(l Level) bool {
	return int32(l) >= current.Load()
}

// ParseLevel maps a flag value ("debug"|"info"|"warn"|"error") to a Level.
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return LevelDebug, nil
	case "info":
		return LevelInfo, nil
	case "warn", "warning":
		return LevelWarn, nil
	case "error":
		return LevelError, nil
	default:
		return LevelUnset, fmt.Errorf("unknown log level %q (want debug|info|warn|error)", s)
	}
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "debug"
	case LevelWarn:
		return "warn"
	case LevelError:
		return "error"
	default:
		return "info"
	}
}

// Debugf logs at debug level.
func Debugf(format string, a ...any) { emit(LevelDebug, format, a...) }

// Infof logs at info level.
func Infof(format string, a ...any) { emit(LevelInfo, format, a...) }

// Warnf logs at warn level.
func Warnf(format string, a ...any) { emit(LevelWarn, format, a...) }

// Errorf logs at error level.
func Errorf(format string, a ...any) { emit(LevelError, format, a...) }

func emit(l Level, format string, a ...any) {
	if !Enabled(l) {
		return
	}
	fmt.Fprintf(os.Stderr, "xyz[%s]: %s\n", l, fmt.Sprintf(format, a...))
}
