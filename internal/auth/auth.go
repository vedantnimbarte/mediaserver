// Package auth handles password hashing, JWT issuing and verification.
//
// Sessions are stateless: the token carries the user ID, admin flag and a token
// version. Bumping a user's TokenVersion invalidates every token they hold, which is
// how "log out everywhere" and forced re-login after a password change work without
// the server keeping a session table.
package auth

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"kino/internal/models"
)

// TokenTTL is how long an issued token stays valid.
const TokenTTL = 7 * 24 * time.Hour

// MinPasswordLength is the shortest password accepted at registration.
const MinPasswordLength = 8

var (
	// ErrInvalidCredentials is returned for both a bad username and a bad password, so
	// the response cannot be used to enumerate accounts.
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrTokenExpired       = errors.New("token expired")
	ErrTokenInvalid       = errors.New("token invalid")
	ErrTokenStale         = errors.New("token has been revoked")
)

// Claims is the JWT payload.
type Claims struct {
	UserID       string `json:"uid"`
	Username     string `json:"usr"`
	IsAdmin      bool   `json:"adm"`
	TokenVersion int    `json:"tv"`
	jwt.RegisteredClaims
}

// Service issues and verifies tokens against a signing secret.
type Service struct {
	secret []byte
}

// NewService builds a token service. The secret comes from config and is generated
// on first boot.
func NewService(secret string) *Service {
	return &Service{secret: []byte(secret)}
}

// HashPassword produces a bcrypt hash suitable for storage.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// CheckPassword reports whether password matches the stored hash. It always performs
// the full bcrypt comparison so timing does not reveal whether the user exists.
func CheckPassword(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// dummyHash is compared against when a login names a user that does not exist, so
// that a missing account costs the same wall-clock time as a wrong password.
var dummyHash = "$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy"

// BurnTime performs a throwaway bcrypt comparison to equalize login timing.
func BurnTime() { bcrypt.CompareHashAndPassword([]byte(dummyHash), []byte("not-a-password")) }

// Issue mints a signed token for the user.
func (s *Service) Issue(u models.User) (string, time.Time, error) {
	now := time.Now()
	expires := now.Add(TokenTTL)

	claims := Claims{
		UserID:       u.ID,
		Username:     u.Username,
		IsAdmin:      u.IsAdmin,
		TokenVersion: u.TokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now.Add(-30 * time.Second)), // tolerate small clock skew
			ExpiresAt: jwt.NewNumericDate(expires),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(s.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expires, nil
}

// Verify parses and validates a token, returning its claims.
func (s *Service) Verify(tokenStr string) (*Claims, error) {
	claims := &Claims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (any, error) {
		// Reject any algorithm other than the one we sign with; accepting "none" or an
		// RSA-vs-HMAC confusion here would let anyone forge a token.
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return s.secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))

	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if claims.UserID == "" {
		return nil, ErrTokenInvalid
	}
	return claims, nil
}

// TokenFromRequest extracts a bearer token from the Authorization header, falling
// back to an `api_key` query parameter.
//
// The query fallback exists because <video>, <img> and <track> elements cannot send
// custom headers; media and image URLs therefore have to carry the token in the URL.
func TokenFromRequest(authHeader, queryToken string) string {
	if authHeader != "" {
		if after, found := strings.CutPrefix(authHeader, "Bearer "); found {
			return strings.TrimSpace(after)
		}
		if after, found := strings.CutPrefix(authHeader, "bearer "); found {
			return strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(queryToken)
}

// ValidateUsername enforces the username rules, returning a user-facing reason.
func ValidateUsername(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 {
		return errors.New("Username must be at least 2 characters.")
	}
	if len(name) > 32 {
		return errors.New("Username must be at most 32 characters.")
	}
	for _, r := range name {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '.' {
			return errors.New("Username may only contain letters, digits, and _ - . characters.")
		}
	}
	return nil
}

// ValidatePassword enforces the password rules, returning a user-facing reason.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return errors.New("Password must be at least " + strconv.Itoa(MinPasswordLength) + " characters.")
	}
	if len(pw) > 256 {
		return errors.New("Password must be at most 256 characters.")
	}
	return nil
}

// NormalizeUsername lowercases and trims a username for uniqueness comparison.
// The original casing is preserved for display.
func NormalizeUsername(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
