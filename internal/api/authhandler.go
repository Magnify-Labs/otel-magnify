package api

import (
	"net/http"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func hashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	return string(hash), err
}

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	setNoStoreAuthHeaders(w)

	var req loginRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.Email == "" || req.Password == "" {
		respondError(w, 400, "email and password are required")
		return
	}
	limitKey := loginRateLimitKey(r)
	if a.loginLimiter != nil && !a.loginLimiter.allow(limitKey) {
		respondError(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}

	user, err := a.db.GetUserByEmail(req.Email)
	if err != nil {
		if auditErr := audit.Emit(r.Context(), a.audit, "auth.login.failure", "user", "", req.Email); auditErr != nil {
			respondAuditUnavailable(w, sideEffectNone)
			return
		}
		if a.loginLimiter != nil {
			a.loginLimiter.recordFailure(limitKey)
		}
		respondError(w, 401, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		if auditErr := audit.Emit(r.Context(), a.audit, "auth.login.failure", "user", "", req.Email); auditErr != nil {
			respondAuditUnavailable(w, sideEffectNone)
			return
		}
		if a.loginLimiter != nil {
			a.loginLimiter.recordFailure(limitKey)
		}
		respondError(w, 401, "invalid credentials")
		return
	}
	if a.loginLimiter != nil {
		a.loginLimiter.reset(limitKey)
	}

	groups, err := a.db.GetUserGroups(user.ID)
	if err != nil {
		respondError(w, 500, "failed to load user groups")
		return
	}
	groupNames := make([]string, 0, len(groups))
	for _, g := range groups {
		groupNames = append(groupNames, g.Name)
	}

	token, err := a.auth.GenerateToken(user.ID, user.Email, groupNames)
	if err != nil {
		respondError(w, 500, "failed to generate token")
		return
	}
	var expiresAt time.Time
	if expProvider, ok := a.auth.(extTokenExpirationProvider); ok {
		expiresAt = tokenExpiresAt(expProvider, token)
	}

	// Login success: token is minted but not yet returned. If audit fails we
	// 503 with side_effect_status=none — the user retries, gets a fresh
	// token, and the audit is re-emitted. JWT is stateless so nothing leaks.
	authedCtx := ext.ContextWithUserInfo(r.Context(), &ext.UserInfo{UserID: user.ID, Email: user.Email})
	if err := audit.Emit(authedCtx, a.audit, "auth.login.success", "user", user.ID, ""); err != nil {
		respondAuditUnavailable(w, sideEffectNone)
		return
	}
	setSessionCookie(w, r, token, expiresAt)
	respondJSON(w, 200, map[string]string{"token": token})
}

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	setNoStoreAuthHeaders(w)
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func setNoStoreAuthHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	h.Set("Pragma", "no-cache")
}
