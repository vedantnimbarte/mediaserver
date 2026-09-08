package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"kino/internal/auth"
	"kino/internal/config"
	"kino/internal/models"
)

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type authResponse struct {
	Token     string             `json:"token"`
	ExpiresAt time.Time          `json:"expiresAt"`
	User      models.PublicUser  `json:"user"`
}

// handleLogin authenticates a user and returns a token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var creds credentials
	if !decodeJSON(w, r, &creds) {
		return
	}

	user, found := s.lookupUserByName(creds.Username)
	if !found {
		// Spend the same time a real bcrypt comparison would, so response latency
		// does not reveal which usernames exist.
		auth.BurnTime()
		writeError(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}
	if !auth.CheckPassword(user.PasswordHash, creds.Password) {
		writeError(w, http.StatusUnauthorized, "Invalid username or password.")
		return
	}

	updated, err := s.db.Users.Update(user.ID, func(u *models.User) {
		u.LastLoginAt = time.Now()
	})
	if err == nil {
		user = updated
	}

	s.issueToken(w, user)
}

// handleRegister creates the first account. It is only available while no users
// exist; afterwards, accounts are created by an admin through /api/users.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if s.db.Users.Len() > 0 {
		writeErrorCode(w, http.StatusForbidden, "SETUP_COMPLETE",
			"Setup has already been completed. Ask an administrator to create your account.")
		return
	}

	var creds credentials
	if !decodeJSON(w, r, &creds) {
		return
	}
	if err := auth.ValidateUsername(creds.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(creds.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	user, err := s.createUser(strings.TrimSpace(creds.Username), creds.Password, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the account: "+err.Error())
		return
	}

	// Persist immediately: losing the admin account to an unclean shutdown seconds
	// after setup would lock the user out of their own server.
	_ = s.db.Users.Flush()
	_ = s.cfg.Update(func(c *config.Config) { c.SetupComplete = true })

	s.issueToken(w, user)
}

// handleMe returns the current user.
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, mustUser(r).Public())
}

// handleLogout invalidates every token for the current user.
//
// Because sessions are stateless there is nothing to delete server-side; bumping the
// token version is what actually ends the session, and it ends it on every device.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	if _, err := s.db.Users.Update(user.ID, func(u *models.User) {
		u.TokenVersion++
	}); err != nil {
		writeError(w, storeErrStatus(err), "Could not end the session.")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "signed out"})
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleChangePassword updates the caller's own password and re-issues a token.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.CurrentPassword) {
		writeError(w, http.StatusUnauthorized, "Your current password is incorrect.")
		return
	}
	if err := auth.ValidatePassword(req.NewPassword); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not update the password.")
		return
	}

	// Bumping the version signs out other devices, which is the expected behaviour
	// after a password change. The caller gets a fresh token below.
	updated, err := s.db.Users.Update(user.ID, func(u *models.User) {
		u.PasswordHash = hash
		u.TokenVersion++
	})
	if err != nil {
		writeError(w, storeErrStatus(err), "Could not update the password.")
		return
	}
	s.issueToken(w, updated)
}

// ---- admin user management ----

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users := s.db.Users.All()
	out := make([]models.PublicUser, 0, len(users))
	for _, u := range users {
		out = append(out, u.Public())
	}
	sortByCreatedAt(out)
	writeJSON(w, http.StatusOK, out)
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	IsAdmin  bool   `json:"isAdmin"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := auth.ValidateUsername(req.Username); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(req.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.usernameTaken(req.Username, "") {
		writeError(w, http.StatusConflict, "That username is already taken.")
		return
	}

	user, err := s.createUser(strings.TrimSpace(req.Username), req.Password, req.IsAdmin)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create the account.")
		return
	}
	writeJSON(w, http.StatusCreated, user.Public())
}

type updateUserRequest struct {
	Password *string `json:"password,omitempty"`
	IsAdmin  *bool   `json:"isAdmin,omitempty"`
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	caller := mustUser(r)

	target, err := s.db.Users.Get(targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such user.")
		return
	}

	var req updateUserRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	// Refuse to remove the last administrator: doing so would leave the server with
	// no way to manage libraries or users ever again.
	if req.IsAdmin != nil && !*req.IsAdmin && target.IsAdmin && s.adminCount() <= 1 {
		writeError(w, http.StatusConflict, "This is the only administrator account; promote another user first.")
		return
	}

	var newHash string
	if req.Password != nil {
		if err := auth.ValidatePassword(*req.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if newHash, err = auth.HashPassword(*req.Password); err != nil {
			writeError(w, http.StatusInternalServerError, "Could not update the password.")
			return
		}
	}

	updated, err := s.db.Users.Update(targetID, func(u *models.User) {
		if newHash != "" {
			u.PasswordHash = newHash
			u.TokenVersion++ // an admin-forced password reset must sign the user out
		}
		if req.IsAdmin != nil {
			u.IsAdmin = *req.IsAdmin
			u.TokenVersion++ // the admin flag is baked into the token, so refresh it
		}
	})
	if err != nil {
		writeError(w, storeErrStatus(err), "Could not update the account.")
		return
	}

	_ = caller // retained for clarity: admins may edit themselves, which is allowed
	writeJSON(w, http.StatusOK, updated.Public())
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	caller := mustUser(r)

	if targetID == caller.ID {
		writeError(w, http.StatusConflict, "You cannot delete your own account.")
		return
	}
	target, err := s.db.Users.Get(targetID)
	if err != nil {
		writeError(w, http.StatusNotFound, "No such user.")
		return
	}
	if target.IsAdmin && s.adminCount() <= 1 {
		writeError(w, http.StatusConflict, "This is the only administrator account.")
		return
	}

	s.db.Users.Delete(targetID)
	// Their watch history is meaningless without the account, and leaving it behind
	// would slowly grow playstate.json with unreachable records.
	s.db.PlayStates.DeleteWhere(func(p models.PlayState) bool { return p.UserID == targetID })

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ---- helpers ----

func (s *Server) createUser(username, password string, isAdmin bool) (models.User, error) {
	hash, err := auth.HashPassword(password)
	if err != nil {
		return models.User{}, err
	}
	user := models.User{
		ID:           uuid.NewString(),
		Username:     username,
		PasswordHash: hash,
		IsAdmin:      isAdmin,
		CreatedAt:    time.Now(),
	}
	if err := s.db.Users.Put(user); err != nil {
		return models.User{}, err
	}
	return user, nil
}

func (s *Server) issueToken(w http.ResponseWriter, user models.User) {
	token, expires, err := s.tokens.Issue(user)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Could not create a session.")
		return
	}
	writeJSON(w, http.StatusOK, authResponse{
		Token:     token,
		ExpiresAt: expires,
		User:      user.Public(),
	})
}

func (s *Server) adminCount() int {
	n := 0
	for _, u := range s.db.Users.All() {
		if u.IsAdmin {
			n++
		}
	}
	return n
}

func sortByCreatedAt(users []models.PublicUser) {
	for i := 1; i < len(users); i++ {
		for j := i; j > 0 && users[j].CreatedAt.Before(users[j-1].CreatedAt); j-- {
			users[j], users[j-1] = users[j-1], users[j]
		}
	}
}
