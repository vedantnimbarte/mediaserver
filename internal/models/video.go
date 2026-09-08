package models

import (
	"sort"
	"time"
)

// Movie is a single film backed by one media file.
type Movie struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`

	Title     string `json:"title"`
	SortTitle string `json:"sortTitle"`
	Year      int    `json:"year,omitempty"`

	Overview    string       `json:"overview,omitempty"`
	Tagline     string       `json:"tagline,omitempty"`
	Genres      []string     `json:"genres,omitempty"`
	RuntimeMins int          `json:"runtimeMins,omitempty"`
	Rating      float64      `json:"rating,omitempty"`
	ReleaseDate string       `json:"releaseDate,omitempty"`
	Studios     []string     `json:"studios,omitempty"`
	Directors   []string     `json:"directors,omitempty"`
	Cast        []CastMember `json:"cast,omitempty"`

	PosterID   string `json:"posterId,omitempty"`   // image cache key
	BackdropID string `json:"backdropId,omitempty"` // image cache key

	TMDbID int    `json:"tmdbId,omitempty"`
	IMDbID string `json:"imdbId,omitempty"`

	// MatchLocked marks a manual metadata correction, which rescans must not overwrite.
	MatchLocked bool           `json:"matchLocked,omitempty"`
	MetaStatus  MetadataStatus `json:"metaStatus"`

	Media MediaInfo `json:"media"`

	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (m Movie) EntityID() string { return m.ID }

// Show is a television series. Seasons and episodes are nested rather than stored in
// their own collections: a show is always loaded as a whole, and nesting keeps
// shows.json readable and consistent without cross-file transactions.
type Show struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`

	Title     string `json:"title"`
	SortTitle string `json:"sortTitle"`
	Year      int    `json:"year,omitempty"`

	Overview   string       `json:"overview,omitempty"`
	Genres     []string     `json:"genres,omitempty"`
	Rating     float64      `json:"rating,omitempty"`
	Status     string       `json:"status,omitempty"` // Returning Series, Ended, ...
	Networks   []string     `json:"networks,omitempty"`
	Cast       []CastMember `json:"cast,omitempty"`
	FirstAired string       `json:"firstAired,omitempty"`

	PosterID   string `json:"posterId,omitempty"`
	BackdropID string `json:"backdropId,omitempty"`

	TMDbID int    `json:"tmdbId,omitempty"`
	IMDbID string `json:"imdbId,omitempty"`

	MatchLocked bool           `json:"matchLocked,omitempty"`
	MetaStatus  MetadataStatus `json:"metaStatus"`

	// FolderPath is the show's root directory, used to re-attach files on rescan.
	FolderPath string `json:"folderPath,omitempty"`

	Seasons []Season `json:"seasons"`

	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (s Show) EntityID() string { return s.ID }

// Season groups episodes. Season 0 is the conventional "Specials" bucket.
type Season struct {
	Number   int       `json:"number"`
	Name     string    `json:"name,omitempty"`
	Overview string    `json:"overview,omitempty"`
	PosterID string    `json:"posterId,omitempty"`
	Episodes []Episode `json:"episodes"`
}

// Episode is one playable installment of a show.
type Episode struct {
	ID     string `json:"id"`
	ShowID string `json:"showId"`

	Season  int `json:"season"`
	Episode int `json:"episode"`

	Title    string  `json:"title,omitempty"`
	Overview string  `json:"overview,omitempty"`
	AirDate  string  `json:"airDate,omitempty"`
	Rating   float64 `json:"rating,omitempty"`
	StillID  string  `json:"stillId,omitempty"` // episode thumbnail image key

	Media MediaInfo `json:"media"`

	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (e Episode) EntityID() string { return e.ID }

// EpisodeCount totals the episodes across every season.
func (s Show) EpisodeCount() int {
	n := 0
	for _, se := range s.Seasons {
		n += len(se.Episodes)
	}
	return n
}

// FindEpisode locates an episode by ID anywhere in the show.
func (s Show) FindEpisode(id string) (Episode, bool) {
	for _, se := range s.Seasons {
		for _, ep := range se.Episodes {
			if ep.ID == id {
				return ep, true
			}
		}
	}
	return Episode{}, false
}

// EpisodesInOrder returns every episode flattened and sorted by season then number,
// which is the order used for "next episode" resolution.
func (s Show) EpisodesInOrder() []Episode {
	var out []Episode
	for _, se := range s.Seasons {
		out = append(out, se.Episodes...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Season != out[j].Season {
			return out[i].Season < out[j].Season
		}
		return out[i].Episode < out[j].Episode
	})
	return out
}

// NextEpisode returns the episode that follows the given one in airing order.
// Specials (season 0) are skipped unless the current episode is itself a special.
func (s Show) NextEpisode(afterID string) (Episode, bool) {
	ordered := s.EpisodesInOrder()
	idx := -1
	for i, ep := range ordered {
		if ep.ID == afterID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Episode{}, false
	}
	current := ordered[idx]
	for _, ep := range ordered[idx+1:] {
		if ep.Season == 0 && current.Season != 0 {
			continue
		}
		if !ep.Media.Available {
			continue
		}
		return ep, true
	}
	return Episode{}, false
}

// SortSeasons orders seasons by number and episodes within each season, with
// Specials (season 0) pushed to the end where viewers expect them.
func (s *Show) SortSeasons() {
	sort.Slice(s.Seasons, func(i, j int) bool {
		a, b := s.Seasons[i].Number, s.Seasons[j].Number
		if a == 0 {
			return false
		}
		if b == 0 {
			return true
		}
		return a < b
	})
	for i := range s.Seasons {
		eps := s.Seasons[i].Episodes
		sort.Slice(eps, func(x, y int) bool { return eps[x].Episode < eps[y].Episode })
	}
}
