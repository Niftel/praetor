// Package env holds small helpers for reading configuration from the process
// environment in one consistent way. It is intended for use in cmd/*/main.go
// (the composition root), so that core packages receive plain values rather
// than reaching into os.Getenv themselves — which keeps a service's config
// surface visible and its core logic testable without mutating process env.
package env

import (
	"os"
	"strconv"
)

// String returns the value of environment variable key, or def if it is unset
// or empty.
func String(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Int returns the integer value of environment variable key, or def if it is
// unset, empty, or not a valid integer.
func Int(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
