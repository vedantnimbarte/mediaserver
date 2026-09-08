package models

import (
	"sort"
	"time"
)

// Artist is the top level of the music hierarchy. Albums and tracks are nested for
// the same reason seasons and episodes are: a small library is always read whole.
type Artist struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`

	Name     string `json:"name"`
	SortName string `json:"sortName"`
	Bio      string `json:"bio,omitempty"`
	ImageID  string `json:"imageId,omitempty"`
	Genres   []string `json:"genres,omitempty"`

	Albums []Album `json:"albums"`

	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a Artist) EntityID() string { return a.ID }

// Album is a release by an artist.
type Album struct {
	ID       string `json:"id"`
	ArtistID string `json:"artistId"`

	Title       string `json:"title"`
	AlbumArtist string `json:"albumArtist,omitempty"`
	Year        int    `json:"year,omitempty"`
	Genre       string `json:"genre,omitempty"`
	CoverID     string `json:"coverId,omitempty"` // image cache key, usually extracted cover art

	// FolderPath is the directory the album was assembled from, used on rescan.
	FolderPath string `json:"folderPath,omitempty"`

	Tracks []Track `json:"tracks"`

	AddedAt time.Time `json:"addedAt"`
}

// Track is one playable song.
type Track struct {
	ID       string `json:"id"`
	AlbumID  string `json:"albumId"`
	ArtistID string `json:"artistId"`

	Title       string  `json:"title"`
	Artist      string  `json:"artist,omitempty"` // track artist, may differ on compilations
	TrackNo     int     `json:"trackNo,omitempty"`
	DiscNo      int     `json:"discNo,omitempty"`
	Year        int     `json:"year,omitempty"`
	Genre       string  `json:"genre,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`

	Media MediaInfo `json:"media"`

	AddedAt time.Time `json:"addedAt"`
}

func (t Track) EntityID() string { return t.ID }

// TrackCount totals tracks across every album.
func (a Artist) TrackCount() int {
	n := 0
	for _, al := range a.Albums {
		n += len(al.Tracks)
	}
	return n
}

// FindTrack locates a track by ID anywhere in the artist's discography.
func (a Artist) FindTrack(id string) (Track, Album, bool) {
	for _, al := range a.Albums {
		for _, t := range al.Tracks {
			if t.ID == id {
				return t, al, true
			}
		}
	}
	return Track{}, Album{}, false
}

// FindAlbum locates an album by ID.
func (a Artist) FindAlbum(id string) (Album, bool) {
	for _, al := range a.Albums {
		if al.ID == id {
			return al, true
		}
	}
	return Album{}, false
}

// SortDiscography orders albums newest-last and tracks by disc then track number.
func (a *Artist) SortDiscography() {
	sort.Slice(a.Albums, func(i, j int) bool {
		if a.Albums[i].Year != a.Albums[j].Year {
			return a.Albums[i].Year < a.Albums[j].Year
		}
		return a.Albums[i].Title < a.Albums[j].Title
	})
	for i := range a.Albums {
		tr := a.Albums[i].Tracks
		sort.Slice(tr, func(x, y int) bool {
			if tr[x].DiscNo != tr[y].DiscNo {
				return tr[x].DiscNo < tr[y].DiscNo
			}
			if tr[x].TrackNo != tr[y].TrackNo {
				return tr[x].TrackNo < tr[y].TrackNo
			}
			return tr[x].Title < tr[y].Title
		})
	}
}

// OrderedTracks returns every track in the artist's albums in play order.
func (a Artist) OrderedTracks() []Track {
	var out []Track
	for _, al := range a.Albums {
		out = append(out, al.Tracks...)
	}
	return out
}
