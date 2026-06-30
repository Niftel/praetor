package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/praetordev/praetor/pkg/crypto"
	"github.com/praetordev/praetor/services/api/render"
)

// jwtSecret is resolved at package init. A misconfiguration (unset JWT_SECRET
// without PRAETOR_ALLOW_INSECURE_DEFAULTS) yields an empty secret here, but the
// API's main() calls crypto.ValidateSecrets and exits before serving, so an
// insecure value is never actually used to sign or verify tokens.
var jwtSecret = []byte(getJWTSecret())

func getJWTSecret() string {
	secret, _ := crypto.JWTSecret()
	return secret
}

type AuthContextKey string

const UserContextKey AuthContextKey = "user"

type UserContext struct {
	UserID          int64
	Username        string
	IsSuperuser     bool
	IsSystemAuditor bool
}

// AuthMiddleware validates JWT tokens
func AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			render.ErrUnauthorized(nil).Render(w, r)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			render.ErrUnauthorized(fmt.Errorf("invalid auth header format")).Render(w, r)
			return
		}
		tokenString := parts[1]

		token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
			if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return jwtSecret, nil
		})

		if err != nil || !token.Valid {
			render.ErrUnauthorized(err).Render(w, r)
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			render.ErrUnauthorized(fmt.Errorf("invalid token claims")).Render(w, r)
			return
		}

		// Safely extract claims. jwt.MapClaims numbers are float64 by default.
		userIDFloat, _ := claims["user_id"].(float64)
		username, _ := claims["username"].(string)
		isSuperuser, _ := claims["is_superuser"].(bool)
		isSystemAuditor, _ := claims["is_system_auditor"].(bool)

		userCtx := UserContext{
			UserID:          int64(userIDFloat),
			Username:        username,
			IsSuperuser:     isSuperuser,
			IsSystemAuditor: isSystemAuditor,
		}

		ctx := context.WithValue(r.Context(), UserContextKey, userCtx)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
