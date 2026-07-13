package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jmoiron/sqlx"
	"github.com/praetordev/praetor/pkg/auth"
	"github.com/praetordev/crypto"
	"github.com/praetordev/models"
	"github.com/praetordev/render"
	"github.com/praetordev/store"
	"golang.org/x/crypto/bcrypt"
)

// AuthResource handles login/token issuance. It holds the users store and the
// LDAP config path (LDAP login is enabled when the path is set). Extracted from
// the former ContentHandler god-object (B6/#85).
type AuthResource struct {
	DB *sqlx.DB
	*Authorizer
	store UserStore

	// LDAPConfigPath, when set, enables LDAP login (AAP group→role mapping) in
	// Login. Empty means local auth only. Set by the router from the API config.
	LDAPConfigPath string
}

func NewAuthResource(db *sqlx.DB, authz *Authorizer) *AuthResource {
	return &AuthResource{DB: db, Authorizer: authz, store: store.NewUserStore(db)}
}

var jwtSecret = []byte(getJWTSecret())

func getJWTSecret() string {
	secret, _ := crypto.JWTSecret()
	return secret
}

// dummyPasswordHash is compared against on every failed/non-local login so the
// LDAP and local paths take comparable time — "user not found" must not be
// distinguishable from "bad password" (no enumeration/timing oracle).
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("praetor-login-timing-floor"), bcrypt.DefaultCost)

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
//
// Two account kinds, in order:
//  1. Break-glass LOCAL account — a row with a non-empty password_hash and
//     ldap_dn IS NULL — always authenticates locally, regardless of LDAP. It is
//     never LDAP-managed, so a broken/unreachable directory can't lock it out.
//  2. Everything else, when LDAP is configured — bind to the directory and apply
//     the AAP group→role mapping (auth.Authenticate). No fallback to a local hash.
func (h *AuthResource) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		render.ErrInvalidRequest(err).Render(w, r)
		return
	}

	// Look up the local row (may not exist). ByUsernameWithHash includes ldap_dn.
	user, err := h.store.ByUsernameWithHash(r.Context(), req.Username)
	isLocalAccount := err == nil && user.PasswordHash != "" && user.LdapDN == nil

	// 1. Local break-glass account.
	if isLocalAccount {
		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
			render.ErrUnauthorized(nil).Render(w, r)
			return
		}
		if !user.IsActive {
			render.ErrUnauthorized(nil).Render(w, r)
			return
		}
		h.issueToken(w, r, user)
		return
	}

	// 2. LDAP path (only when the AAP login mapping is configured).
	if h.LDAPConfigPath != "" {
		if cfg, cerr := auth.LoadConfig(h.LDAPConfigPath); cerr == nil && cfg.UsesLoginMapping() {
			ldapUser, lerr := auth.Authenticate(r.Context(), h.DB, cfg, auth.NewLDAPClient(cfg), req.Username, req.Password)
			if lerr == nil {
				if !ldapUser.IsActive {
					render.ErrUnauthorized(nil).Render(w, r)
					return
				}
				h.issueToken(w, r, *ldapUser)
				return
			}
			// Fall through to the generic failure below (never leak the reason).
		}
	}

	// Constant-time floor, then a generic 401 for all failures.
	_ = bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
	render.ErrUnauthorized(nil).Render(w, r)
}

// issueToken signs a JWT for an authenticated user and writes the login response.
func (h *AuthResource) issueToken(w http.ResponseWriter, r *http.Request, user models.User) {
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

	user.PasswordHash = ""
	render.JSON(w, r, LoginResponse{Token: tokenString, User: user})
}
