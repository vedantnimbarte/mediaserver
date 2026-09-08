package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kino/internal/config"
	"kino/internal/models"
	"kino/internal/store"
)

// playStateFixture builds a minimal watch-history record for a user/media pair.
func playStateFixture(userID, mediaID string) models.PlayState {
	return models.PlayState{
		ID:          models.PlayStateID(userID, mediaID),
		UserID:      userID,
		MediaID:     mediaID,
		Kind:        models.PlayableMovie,
		PositionSec: 120,
		DurationSec: 6000,
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()

	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	db, err := store.OpenDB(dir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	return New(Options{Config: cfg, DB: db})
}

// do issues a request against the server and returns the recorder.
func do(t *testing.T, s *Server, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()

	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	return rec
}

func decodeBody[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return out
}

// registerAdmin completes first-run setup and returns the admin's token.
func registerAdmin(t *testing.T, s *Server) string {
	t.Helper()
	rec := do(t, s, "POST", "/api/auth/register", "", credentials{
		Username: "admin", Password: "supersecret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("register: got %d, body %s", rec.Code, rec.Body.String())
	}
	return decodeBody[authResponse](t, rec).Token
}

func TestFirstRunRegistrationCreatesAdmin(t *testing.T) {
	s := newTestServer(t)

	rec := do(t, s, "GET", "/api/server-info", "", nil)
	info := decodeBody[map[string]any](t, rec)
	if info["setupRequired"] != true {
		t.Fatal("expected setupRequired to be true before any user exists")
	}

	resp := decodeBody[authResponse](t, do(t, s, "POST", "/api/auth/register", "", credentials{
		Username: "admin", Password: "supersecret",
	}))
	if !resp.User.IsAdmin {
		t.Fatal("the first account must be an administrator")
	}
	if resp.Token == "" {
		t.Fatal("registration should return a token")
	}

	rec = do(t, s, "GET", "/api/server-info", "", nil)
	info = decodeBody[map[string]any](t, rec)
	if info["setupRequired"] != false {
		t.Fatal("setupRequired should be false once an account exists")
	}
}

// TestRegistrationClosesAfterSetup is the guard that stops a LAN-exposed server from
// letting anyone mint themselves an admin account.
func TestRegistrationClosesAfterSetup(t *testing.T) {
	s := newTestServer(t)
	registerAdmin(t, s)

	rec := do(t, s, "POST", "/api/auth/register", "", credentials{
		Username: "intruder", Password: "supersecret",
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("second registration returned %d, want 403", rec.Code)
	}
}

func TestRegistrationRejectsWeakInput(t *testing.T) {
	cases := []struct {
		name     string
		username string
		password string
	}{
		{"short password", "admin", "short"},
		{"short username", "a", "supersecret"},
		{"invalid characters", "bad name!", "supersecret"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t)
			rec := do(t, s, "POST", "/api/auth/register", "", credentials{
				Username: tc.username, Password: tc.password,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want 400 (body: %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLoginSucceedsAndFails(t *testing.T) {
	s := newTestServer(t)
	registerAdmin(t, s)

	rec := do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "admin", Password: "supersecret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("valid login returned %d", rec.Code)
	}

	// Usernames are matched case-insensitively.
	rec = do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "ADMIN", Password: "supersecret",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("case-insensitive login returned %d", rec.Code)
	}

	rec = do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "admin", Password: "wrong",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password returned %d, want 401", rec.Code)
	}

	// A missing user must produce the same response as a wrong password.
	missing := do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "ghost", Password: "supersecret",
	})
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing user returned %d, want 401", missing.Code)
	}
	if missing.Body.String() != rec.Body.String() {
		t.Fatal("login responses must not distinguish a missing user from a wrong password")
	}
}

func TestProtectedRoutesRequireToken(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	if rec := do(t, s, "GET", "/api/auth/me", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("no token returned %d, want 401", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/auth/me", "garbage.token.here", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad token returned %d, want 401", rec.Code)
	}
	if rec := do(t, s, "GET", "/api/auth/me", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("valid token returned %d", rec.Code)
	}
}

// TestQueryTokenIsAccepted covers the path <video>/<img> elements depend on: they
// cannot set an Authorization header, so the token must work as a query parameter.
func TestQueryTokenIsAccepted(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	rec := do(t, s, "GET", "/api/auth/me?api_key="+token, "", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("query token returned %d, want 200", rec.Code)
	}
}

// TestLogoutRevokesEveryToken is the point of the TokenVersion field.
func TestLogoutRevokesEveryToken(t *testing.T) {
	s := newTestServer(t)
	first := registerAdmin(t, s)

	second := decodeBody[authResponse](t, do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "admin", Password: "supersecret",
	})).Token

	if rec := do(t, s, "POST", "/api/auth/logout", first, nil); rec.Code != http.StatusOK {
		t.Fatalf("logout returned %d", rec.Code)
	}

	// Both the token used to log out and the one issued to the "other device" die.
	for name, tok := range map[string]string{"logged-out token": first, "other device": second} {
		rec := do(t, s, "GET", "/api/auth/me", tok, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s still works after logout (got %d)", name, rec.Code)
		}
	}
}

func TestChangePasswordRequiresCurrentAndRotates(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	rec := do(t, s, "POST", "/api/auth/password", token, changePasswordRequest{
		CurrentPassword: "wrong", NewPassword: "brandnewpassword",
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password returned %d, want 401", rec.Code)
	}

	rec = do(t, s, "POST", "/api/auth/password", token, changePasswordRequest{
		CurrentPassword: "supersecret", NewPassword: "brandnewpassword",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("password change returned %d: %s", rec.Code, rec.Body.String())
	}
	fresh := decodeBody[authResponse](t, rec).Token

	// The old token is dead, the newly issued one works, and the new password logs in.
	if r := do(t, s, "GET", "/api/auth/me", token, nil); r.Code != http.StatusUnauthorized {
		t.Fatalf("old token still valid after password change (got %d)", r.Code)
	}
	if r := do(t, s, "GET", "/api/auth/me", fresh, nil); r.Code != http.StatusOK {
		t.Fatalf("token issued by password change does not work (got %d)", r.Code)
	}
	if r := do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "admin", Password: "brandnewpassword",
	}); r.Code != http.StatusOK {
		t.Fatalf("login with the new password returned %d", r.Code)
	}
}

func TestAdminOnlyRoutes(t *testing.T) {
	s := newTestServer(t)
	adminToken := registerAdmin(t, s)

	rec := do(t, s, "POST", "/api/users", adminToken, createUserRequest{
		Username: "viewer", Password: "viewerpassword", IsAdmin: false,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create user returned %d: %s", rec.Code, rec.Body.String())
	}

	viewerToken := decodeBody[authResponse](t, do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "viewer", Password: "viewerpassword",
	})).Token

	// A regular user may read their own profile but not manage accounts.
	if r := do(t, s, "GET", "/api/auth/me", viewerToken, nil); r.Code != http.StatusOK {
		t.Fatalf("viewer cannot read their own profile (got %d)", r.Code)
	}
	if r := do(t, s, "GET", "/api/users", viewerToken, nil); r.Code != http.StatusForbidden {
		t.Fatalf("viewer listing users returned %d, want 403", r.Code)
	}
	if r := do(t, s, "POST", "/api/users", viewerToken, createUserRequest{
		Username: "sneaky", Password: "sneakypassword", IsAdmin: true,
	}); r.Code != http.StatusForbidden {
		t.Fatalf("viewer creating a user returned %d, want 403", r.Code)
	}
}

func TestDuplicateUsernameRejected(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	rec := do(t, s, "POST", "/api/users", token, createUserRequest{
		Username: "ADMIN", Password: "anotherpassword",
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate username returned %d, want 409", rec.Code)
	}
}

// TestLastAdminIsProtected stops the server from becoming unmanageable.
func TestLastAdminIsProtected(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	admin := decodeBody[authResponse](t, do(t, s, "POST", "/api/auth/login", "", credentials{
		Username: "admin", Password: "supersecret",
	})).User

	demote := false
	rec := do(t, s, "PATCH", "/api/users/"+admin.ID, token, updateUserRequest{IsAdmin: &demote})
	if rec.Code != http.StatusConflict {
		t.Fatalf("demoting the only admin returned %d, want 409", rec.Code)
	}

	rec = do(t, s, "DELETE", "/api/users/"+admin.ID, token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("deleting your own account returned %d, want 409", rec.Code)
	}
}

func TestDeletingUserRemovesTheirPlayState(t *testing.T) {
	s := newTestServer(t)
	token := registerAdmin(t, s)

	created := decodeBody[map[string]any](t, do(t, s, "POST", "/api/users", token, createUserRequest{
		Username: "viewer", Password: "viewerpassword",
	}))
	viewerID, _ := created["id"].(string)
	if viewerID == "" {
		t.Fatal("created user has no id")
	}

	// Seed some watch history for the account we are about to delete.
	_ = s.db.PlayStates.Put(playStateFixture(viewerID, "movie-1"))
	_ = s.db.PlayStates.Put(playStateFixture("someone-else", "movie-1"))

	if rec := do(t, s, "DELETE", "/api/users/"+viewerID, token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete user returned %d: %s", rec.Code, rec.Body.String())
	}

	if s.db.PlayStates.Has(models.PlayStateID(viewerID, "movie-1")) {
		t.Fatal("deleted user's playstate should be gone")
	}
	if s.db.PlayStates.Len() != 1 {
		t.Fatalf("expected the other user's playstate to survive, have %d records", s.db.PlayStates.Len())
	}
}

func TestMalformedJSONRejected(t *testing.T) {
	s := newTestServer(t)

	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBufferString(`{"username":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed JSON returned %d, want 400", rec.Code)
	}
}
