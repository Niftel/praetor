package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"os"

	"github.com/golang-jwt/jwt/v5"
	"github.com/praetordev/praetor/pkg/models"
	"github.com/praetordev/praetor/services/api/render"
	"golang.org/x/crypto/bcrypt"
)

var jwtSecret = []byte(getJWTSecret())

func getJWTSecret() string {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return "praetor-secret-key-change-me"
	}
	return secret
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string      `json:"token"`
	User  models.User `json:"user"`
}

type AuthClaims struct {
	UserID          int64  `json:"user_id"`
	Username        string `json:"username"`
	IsSuperuser     bool   `json:"is_superuser"`
	IsSystemAuditor bool   `json:"is_system_auditor"`
	jwt.RegisteredClaims
}

// Login POST /api/v1/auth/login
func (h *ContentHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	var user models.User
	// Get user including password_hash
	query := `SELECT id, username, password_hash, first_name, last_name, email, is_superuser, is_system_auditor, is_active FROM users WHERE username = $1`
	err := h.DB.Get(&user, query, req.Username)
	if err != nil {
		// Generic error for security
		render.ErrUnauthorized(nil).Render(w, r)
		return
	}

	// Verify Password
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		render.ErrUnauthorized(nil).Render(w, r)
		return
	}

	if !user.IsActive {
		render.ErrUnauthorized(nil).Render(w, r) // Account inactive
		return
	}

	// Generate JWT
	claims := AuthClaims{
		UserID:          user.ID,
		Username:        user.Username,
		IsSuperuser:     user.IsSuperuser,
		IsSystemAuditor: user.IsSystemAuditor,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "praetor-api",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, err := token.SignedString(jwtSecret)
	if err != nil {
		render.ErrInternal(err).Render(w, r)
		return
	}

	// Clear hash before returning
	user.PasswordHash = ""

	render.JSON(w, r, LoginResponse{
		Token: tokenString,
		User:  user,
	})
}
