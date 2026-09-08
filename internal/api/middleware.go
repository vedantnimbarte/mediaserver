package api

import (
	"context"
	"errors"
	"net/http"

	"kino/internal/auth"
	"kino/internal/models"
	"kino/internal/store"
)

type ctxKey int

const userCtxKey ctxKey = iota

// userFrom returns the authenticated user attached to the request context.
func userFrom(r *http.Request) (models.User, bool) {
	u, ok := r.Context().Value(userCtxKey).(models.User)
	return u, ok
}

// mustUser returns the authenticated user. It is only valid inside handlers mounted
// behind requireAuth, which guarantees the user is present.
func mustUser(r *http.Request) models.User {
	u, ok := userFrom(r)
	if !ok {
		panic("api: mustUser called on an unauthenticated route")
	}
	return u
}

// requireAuth rejects requests without a valid token and loads the user record.
//
// Loading the user on every request (rather than trusting the token payload) is what
// makes revocation immediate: a deleted user or a bumped TokenVersion takes effect on
// the very next call instead of whenever the token happens to expire.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := auth.TokenFromRequest(r.Header.Get("Authorization"), r.URL.Query().Get("api_key"))
		if token == "" {
			writeErrorCode(w, http.StatusUnauthorized, "NO_TOKEN", "Authentication required.")
			return
		}

		claims, err := s.tokens.Verify(token)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrTokenExpired):
				writeErrorCode(w, http.StatusUnauthorized, "TOKEN_EXPIRED", "Your session has expired. Please sign in again.")
			default:
				writeErrorCode(w, http.StatusUnauthorized, "TOKEN_INVALID", "Invalid authentication token.")
			}
			return
		}

		user, err := s.db.Users.Get(claims.UserID)
		if err != nil {
			// The account was deleted while a token was still outstanding.
			writeErrorCode(w, http.StatusUnauthorized, "TOKEN_INVALID", "This account no longer exists.")
			return
		}
		if user.TokenVersion != claims.TokenVersion {
			writeErrorCode(w, http.StatusUnauthorized, "TOKEN_REVOKED", "Your session was ended. Please sign in again.")
			return
		}

		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAdmin rejects non-admin users. It must be mounted inside requireAuth.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := userFrom(r)
		if !ok {
			writeErrorCode(w, http.StatusUnauthorized, "NO_TOKEN", "Authentication required.")
			return
		}
		if !user.IsAdmin {
			writeError(w, http.StatusForbidden, "This action requires an administrator account.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// lookupUserByName finds a user by case-insensitive username.
func (s *Server) lookupUserByName(username string) (models.User, bool) {
	norm := auth.NormalizeUsername(username)
	return s.db.Users.Find(func(u models.User) bool {
		return auth.NormalizeUsername(u.Username) == norm
	})
}

// usernameTaken reports whether a username is already in use by a different account.
func (s *Server) usernameTaken(username, exceptID string) bool {
	u, ok := s.lookupUserByName(username)
	return ok && u.ID != exceptID
}

// storeErrStatus maps a store error to an HTTP status.
func storeErrStatus(err error) int {
	if errors.Is(err, store.ErrNotFound) {
		return http.StatusNotFound
	}
	return http.StatusInternalServerError
}
