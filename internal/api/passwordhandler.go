package api

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"github.com/magnify-labs/otel-magnify/internal/audit"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

const passwordMinLen = 12

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (a *API) handlePutPassword(w http.ResponseWriter, r *http.Request) {
	info := ext.UserInfoFromContext(r.Context())
	if info == nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req changePasswordRequest
	if !decodeJSONBody(w, r, &req) {
		return
	}
	if req.CurrentPassword == "" || req.NewPassword == "" {
		respondError(w, http.StatusBadRequest, "current_password and new_password are required")
		return
	}
	if len(req.NewPassword) < passwordMinLen {
		respondError(w, http.StatusBadRequest, "new_password must be at least 12 characters")
		return
	}
	if req.NewPassword == req.CurrentPassword {
		respondError(w, http.StatusBadRequest, "new_password must differ from current_password")
		return
	}

	user, err := a.db.GetUserByEmail(info.Email)
	if err != nil {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword)) != nil {
		respondError(w, http.StatusUnauthorized, "current password does not match")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 12)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	user.PasswordHash = string(newHash)
	if err := a.db.UpdateUser(user); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update password")
		return
	}
	// Password row is already updated. If audit fails we 503 with
	// side_effect_status=applied so the caller knows the new password is
	// active despite the error.
	if err := audit.Emit(r.Context(), a.audit, "auth.password.change", "user", info.UserID, ""); err != nil {
		respondAuditUnavailable(w, sideEffectApplied)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
