package api

import (
	"encoding/json"
	"net/http"

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
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, 400, "invalid JSON")
		return
	}
	if req.Email == "" || req.Password == "" {
		respondError(w, 400, "email and password are required")
		return
	}

	user, err := a.db.GetUserByEmail(req.Email)
	if err != nil {
		audit.Emit(r.Context(), a.audit, "auth.login.failure", "user", "", req.Email)
		respondError(w, 401, "invalid credentials")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
		audit.Emit(r.Context(), a.audit, "auth.login.failure", "user", "", req.Email)
		respondError(w, 401, "invalid credentials")
		return
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

	// Inject the freshly authenticated principal into ctx so audit.Emit can
	// pull UserID/Email — the public login endpoint runs before the auth
	// middleware, so r.Context() has no UserInfo yet.
	authedCtx := ext.ContextWithUserInfo(r.Context(), &ext.UserInfo{UserID: user.ID, Email: user.Email})
	audit.Emit(authedCtx, a.audit, "auth.login.success", "user", user.ID, "")
	respondJSON(w, 200, map[string]string{"token": token})
}
