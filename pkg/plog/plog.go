// Package plog is Praetor's small structured-logging seam over log/slog. A
// composition root (cmd/*/main.go) calls Configure once to install the process
// default handler (format + level from the environment); core packages then take
// a *slog.Logger from New(component) so every record is tagged with its source.
//
// It deliberately does NOT do request-id/trace propagation — that is a separate
// concern. It is intentionally ~1 file: one place to choose JSON vs text and the
// level, so the whole platform logs consistently.
package plog

import (
	"log/slog"
	"os"
	"strings"

	"github.com/praetordev/praetor/pkg/env"
)

// Configure installs the process-wide slog default handler from the environment
// and returns a base logger tagged with the service name. Call it once, early,
// in each service's main. Because Go routes the stdlib `log` package through the
// slog default, existing log.Printf calls also become structured records at Info
// level after this runs.
//
//	PRAETOR_LOG_FORMAT = json (default) | text
//	PRAETOR_LOG_LEVEL  = debug | info (default) | warn | error
func Configure(service string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: levelFromEnv()}
	var h slog.Handler
	if strings.ToLower(env.String("PRAETOR_LOG_FORMAT", "json")) == "text" {
		h = slog.NewTextHandler(os.Stderr, opts)
	} else {
		h = slog.NewJSONHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
	return New(service)
}

// New returns a logger tagged with a component name, derived from the configured
// default handler. Safe to call before Configure (falls back to slog.Default()).
func New(component string) *slog.Logger {
	return slog.Default().With("component", component)
}

func levelFromEnv() slog.Level {
	switch strings.ToLower(env.String("PRAETOR_LOG_LEVEL", "info")) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
