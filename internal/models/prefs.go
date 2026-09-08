package models

import "time"

// UserPrefs holds one viewer's playback and appearance settings.
//
// These are per-user rather than server-wide because they are personal taste: one
// household member watching on a laptop over wifi wants a quality cap and subtitles
// on by default, another on a wired desktop wants neither.
type UserPrefs struct {
	// ID is the user ID, so a user's preferences are a direct lookup.
	ID     string `json:"id"`
	UserID string `json:"userId"`

	// ---- playback ----

	// MaxQuality caps the rendition the player will select: "auto" or a rung name
	// such as "720p". Useful on metered or slow connections.
	MaxQuality string `json:"maxQuality"`
	// AutoplayNext advances to the next episode when one finishes.
	AutoplayNext bool `json:"autoplayNext"`
	// AutoplayCountdown is how many seconds the "up next" card waits.
	AutoplayCountdown int `json:"autoplayCountdown"`
	// PreferredAudioLang is an ISO code; the player picks a matching track when present.
	PreferredAudioLang string `json:"preferredAudioLang"`
	// PreferredSubLang is an ISO code used the same way for subtitles.
	PreferredSubLang string `json:"preferredSubLang"`
	// SubtitlesDefaultOn enables subtitles automatically when a matching track exists.
	SubtitlesDefaultOn bool `json:"subtitlesDefaultOn"`
	// SeekStepSec is how far the arrow keys jump.
	SeekStepSec int `json:"seekStepSec"`
	// SkipIntroEnabled shows a skip button during the opening credits window.
	SkipIntroEnabled bool `json:"skipIntroEnabled"`

	// ---- appearance ----

	// AccentColor is a hex colour used for buttons, focus rings and progress bars.
	AccentColor string `json:"accentColor"`
	// PosterSize controls how many posters fit per row: small, medium or large.
	PosterSize string `json:"posterSize"`
	// ShowTitles draws the title under each poster.
	ShowTitles bool `json:"showTitles"`
	// ReducedMotion disables hover expansion and large transitions.
	ReducedMotion bool `json:"reducedMotion"`
	// HeroOnHome shows the full-bleed billboard at the top of the home screen.
	HeroOnHome bool `json:"heroOnHome"`

	UpdatedAt time.Time `json:"updatedAt"`
}

func (p UserPrefs) EntityID() string { return p.ID }

// DefaultPrefs returns the settings a brand-new account starts with.
func DefaultPrefs(userID string) UserPrefs {
	return UserPrefs{
		ID:     userID,
		UserID: userID,

		MaxQuality:         "auto",
		AutoplayNext:       true,
		AutoplayCountdown:  10,
		PreferredAudioLang: "",
		PreferredSubLang:   "",
		SubtitlesDefaultOn: false,
		SeekStepSec:        10,
		SkipIntroEnabled:   true,

		AccentColor:   "#E50914",
		PosterSize:    "medium",
		ShowTitles:    true,
		ReducedMotion: false,
		HeroOnHome:    true,
	}
}

// PosterSizes are the accepted values for PosterSize.
var PosterSizes = []string{"small", "medium", "large"}

// QualityCaps are the accepted values for MaxQuality.
var QualityCaps = []string{"auto", "2160p", "1080p", "720p", "480p", "360p"}

// Normalize repairs any out-of-range or unknown values, so a hand-edited file or an
// older client cannot put the UI into a broken state.
func (p *UserPrefs) Normalize() {
	if !contains(QualityCaps, p.MaxQuality) {
		p.MaxQuality = "auto"
	}
	if !contains(PosterSizes, p.PosterSize) {
		p.PosterSize = "medium"
	}
	if p.AutoplayCountdown < 3 || p.AutoplayCountdown > 60 {
		p.AutoplayCountdown = 10
	}
	if p.SeekStepSec < 5 || p.SeekStepSec > 60 {
		p.SeekStepSec = 10
	}
	if !validHexColor(p.AccentColor) {
		p.AccentColor = "#E50914"
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// validHexColor accepts #RGB and #RRGGBB. The value is injected into a CSS custom
// property, so anything else must be rejected rather than passed through.
func validHexColor(s string) bool {
	if len(s) != 4 && len(s) != 7 {
		return false
	}
	if s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		isHex := (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
		if !isHex {
			return false
		}
	}
	return true
}
