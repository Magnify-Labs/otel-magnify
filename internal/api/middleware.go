package api

import (
	"net/http"

	"github.com/magnify-labs/otel-magnify/internal/perm"
	"github.com/magnify-labs/otel-magnify/pkg/ext"
)

// RequirePerm returns an http middleware that rejects requests whose
// authenticated user does not have p. It assumes a.Middleware has already
// placed a UserInfo in the context.
func (a *API) RequirePerm(p perm.Permission) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info := ext.UserInfoFromContext(r.Context())
			if info == nil {
				respondError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if !perm.Has(*info, p) {
				respondError(w, http.StatusForbidden, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
