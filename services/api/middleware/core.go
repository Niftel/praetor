package middleware

import (
	"net/http"
)

// LoggerMiddleware is a basic request logger (using standard lib or simple print for now).
func LoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// In a real app, use structured logging (zap/zerolog)
		// fmt.Printf("Request: %s %s\n", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
