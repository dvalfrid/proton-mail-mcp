package mail

import (
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
)

// LogPanic logs a recovered panic with its stack. A runtime error's value
// is logged (it describes the bug, e.g. a nil map write); any other value
// only by its type, because it could carry message content.
func LogPanic(logger *slog.Logger, where string, r any) {
	value := fmt.Sprintf("%T", r)
	if rerr, ok := r.(runtime.Error); ok {
		value = rerr.Error()
	}
	logger.Error("recovered from a panic", "in", where, "panic", value, "stack", string(debug.Stack()))
}
