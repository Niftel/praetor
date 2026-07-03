package middleware

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
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

// PATPrefix marks a personal access token (vs. a JWT). Tokens are opaque random
// strings; only their SHA-256 hash is stored server-side.
const PATPrefix = "prtr_pat_"

// HashToken returns the hex SHA-256 of a token — what's stored and looked up, so
// the plaintext never touches the database.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type AuthContextKey string

const UserContextKey AuthContextKey = "user"

type UserContext struct {
	UserID          int64
	Username        string
	IsSuperuser     bool
	IsSystemAuditor bool
}

// AuthMiddleware authenticates each request by either a personal access token
// (Bearer prtr_pat_…, looked up in api_tokens) or a login JWT. Both resolve to
// the same UserContext, so a PAT acts exactly as its owning user.
func AuthMiddleware(db *sqlx.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
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

			var userCtx UserContext
			var ok bool
			if strings.HasPrefix(tokenString, PATPrefix) {
				userCtx, ok = authenticatePAT(db, r.Context(), tokenString)
			} else {
				userCtx, ok = authenticateJWT(tokenString)
			}
			if !ok {
				render.ErrUnauthorized(nil).Render(w, r)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, userCtx)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticatePAT resolves a personal access token to its user, enforcing
// expiry, and stamps last_used_at (throttled) for auditing.
func authenticatePAT(db *sqlx.DB, ctx context.Context, token string) (UserContext, bool) {
	var row struct {
		TokenID   int64  `db:"token_id"`
		UserID    int64  `db:"id"`
		Username  string `db:"username"`
		Super     bool   `db:"is_superuser"`
		Auditor   bool   `db:"is_system_auditor"`
		Expired   bool   `db:"expired"`
	}
	err := db.GetContext(ctx, &row, `
		SELECT t.id AS token_id, u.id, u.username, u.is_superuser, u.is_system_auditor,
		       (t.expires_at IS NOT NULL AND t.expires_at < now()) AS expired
		FROM api_tokens t JOIN users u ON u.id = t.user_id
		WHERE t.token_hash = $1`, HashToken(token))
	if err != nil || row.Expired {
		return UserContext{}, false
	}
	// Best-effort, throttled last-used stamp (avoids a write on every request).
	_, _ = db.ExecContext(ctx, `
		UPDATE api_tokens SET last_used_at = now()
		WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`, row.TokenID)
	return UserContext{UserID: row.UserID, Username: row.Username, IsSuperuser: row.Super, IsSystemAuditor: row.Auditor}, true
}

// authenticateJWT validates a login JWT and extracts its claims.
func authenticateJWT(tokenString string) (UserContext, bool) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return UserContext{}, false
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return UserContext{}, false
	}
	userIDFloat, _ := claims["user_id"].(float64)
	username, _ := claims["username"].(string)
	isSuperuser, _ := claims["is_superuser"].(bool)
	isSystemAuditor, _ := claims["is_system_auditor"].(bool)
	return UserContext{
		UserID:          int64(userIDFloat),
		Username:        username,
		IsSuperuser:     isSuperuser,
		IsSystemAuditor: isSystemAuditor,
	}, true
}
