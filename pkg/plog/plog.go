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
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/praetordev/praetor/pkg/env"
)

// Configure installs the process-wide slog default handler from the environment
// and returns a logger tagged with the service name. Call it once, early, in each
// service's main. Because Go routes the stdlib `log` package through the slog
// default, existing log.Printf calls also become structured records at Info level
// after this runs.
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

// New returns a logger tagged with a component name. Its handler resolves the
// process default LAZILY (at log time), so a package-level `var logger =
// plog.New(...)` — initialized at import, before main runs Configure — still
// emits through the configured handler rather than capturing slog's bootstrap
// default (which would double-wrap text inside the JSON handler).
func New(component string) *slog.Logger {
	return slog.New(&lazyHandler{attrs: []slog.Attr{slog.String("component", component)}})
}

// lazyHandler delegates to slog.Default()'s handler at Handle time, re-applying
// its own accumulated groups/attrs so it always reflects the currently-installed
// default handler.
type lazyHandler struct {
	groups []string
	attrs  []slog.Attr
}

func (l *lazyHandler) resolved() slog.Handler {
	h := slog.Default().Handler()
	for _, g := range l.groups {
		h = h.WithGroup(g)
	}
	if len(l.attrs) > 0 {
		h = h.WithAttrs(l.attrs)
	}
	return h
}

func (l *lazyHandler) Enabled(ctx context.Context, lvl slog.Level) bool {
	return slog.Default().Handler().Enabled(ctx, lvl)
}

func (l *lazyHandler) Handle(ctx context.Context, r slog.Record) error {
	return l.resolved().Handle(ctx, r)
}

func (l *lazyHandler) WithAttrs(as []slog.Attr) slog.Handler {
	na := make([]slog.Attr, 0, len(l.attrs)+len(as))
	na = append(na, l.attrs...)
	na = append(na, as...)
	return &lazyHandler{groups: l.groups, attrs: na}
}

func (l *lazyHandler) WithGroup(g string) slog.Handler {
	ng := make([]string, 0, len(l.groups)+1)
	ng = append(ng, l.groups...)
	ng = append(ng, g)
	return &lazyHandler{groups: ng, attrs: l.attrs}
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
