package models

import "time"

// User is an account that can log in and play media.
type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	IsAdmin      bool   `json:"isAdmin"`

	// TokenVersion is embedded in every issued JWT. Incrementing it invalidates all
	// outstanding tokens for this user, which is how logout-everywhere and forced
	// re-login after a password change are implemented without server-side sessions.
	TokenVersion int `json:"tokenVersion"`

	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt,omitempty"`
}

func (u User) EntityID() string { return u.ID }

// PublicUser is the safe projection sent to clients; it never carries the hash.
type PublicUser struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	IsAdmin     bool      `json:"isAdmin"`
	CreatedAt   time.Time `json:"createdAt"`
	LastLoginAt time.Time `json:"lastLoginAt,omitempty"`
}

// Public strips the password hash for transport.
func (u User) Public() PublicUser {
	return PublicUser{
		ID:          u.ID,
		Username:    u.Username,
		IsAdmin:     u.IsAdmin,
		CreatedAt:   u.CreatedAt,
		LastLoginAt: u.LastLoginAt,
	}
}

// PlayableKind identifies which collection a playable ID belongs to.
type PlayableKind string

const (
	PlayableMovie   PlayableKind = "movie"
	PlayableEpisode PlayableKind = "episode"
	PlayableTrack   PlayableKind = "track"
)

// PlayState is one user's progress through one playable item. The ID is
// "<userID>:<mediaID>" so that per-user state lives in a single flat collection.
type PlayState struct {
	ID      string       `json:"id"`
	UserID  string       `json:"userId"`
	MediaID string       `json:"mediaId"`
	Kind    PlayableKind `json:"kind"`

	PositionSec float64 `json:"positionSec"`
	DurationSec float64 `json:"durationSec"`
	Watched     bool    `json:"watched"`
	PlayCount   int     `json:"playCount"`

	UpdatedAt time.Time `json:"updatedAt"`
}

func (p PlayState) EntityID() string { return p.ID }

// PlayStateID builds the composite key for a user/media pair.
func PlayStateID(userID, mediaID string) string { return userID + ":" + mediaID }

// WatchedThreshold is the fraction of runtime past which an item is auto-marked
// watched and its resume point cleared.
const WatchedThreshold = 0.90

// ResumeFloor is the minimum progress before an item shows up in Continue Watching.
// Below this the user has effectively just sampled it.
const ResumeFloor = 0.02

// Resumable reports whether this state should surface a resume point.
func (p PlayState) Resumable() bool {
	if p.Watched || p.DurationSec <= 0 {
		return false
	}
	frac := p.PositionSec / p.DurationSec
	return frac >= ResumeFloor && frac < WatchedThreshold
}

// Progress returns completion as a 0..1 fraction.
func (p PlayState) Progress() float64 {
	if p.DurationSec <= 0 {
		return 0
	}
	f := p.PositionSec / p.DurationSec
	if f > 1 {
		return 1
	}
	if f < 0 {
		return 0
	}
	return f
}
