package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"kino/internal/models"
)

type artistCard struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	ImageID    string `json:"imageId,omitempty"`
	AlbumCount int    `json:"albumCount"`
	TrackCount int    `json:"trackCount"`
}

func (s *Server) handleListArtists(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("libraryId")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	artists := s.db.Artists.Filter(func(a models.Artist) bool {
		if libraryID != "" && a.LibraryID != libraryID {
			return false
		}
		return query == "" || strings.Contains(strings.ToLower(a.Name), query)
	})
	sort.Slice(artists, func(i, j int) bool {
		return strings.ToLower(artists[i].SortName) < strings.ToLower(artists[j].SortName)
	})

	cards := make([]artistCard, 0, len(artists))
	for _, a := range artists {
		card := artistCard{
			ID:         a.ID,
			Name:       a.Name,
			ImageID:    a.ImageID,
			AlbumCount: len(a.Albums),
			TrackCount: a.TrackCount(),
		}
		// Fall back to an album cover so the artist grid is not a wall of blanks.
		if card.ImageID == "" {
			for _, al := range a.Albums {
				if al.CoverID != "" {
					card.ImageID = al.CoverID
					break
				}
			}
		}
		cards = append(cards, card)
	}

	offset := queryIntClamped(r, "offset", 0, 0, 1<<30)
	limit := queryIntClamped(r, "limit", 200, 1, 1000)
	writeJSON(w, http.StatusOK, paginate(cards, offset, limit))
}

func (s *Server) handleGetArtist(w http.ResponseWriter, r *http.Request) {
	artist, err := s.db.Artists.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such artist.")
		return
	}
	writeJSON(w, http.StatusOK, artist)
}

// handleListAlbums returns every album across all artists, for an album-first view.
func (s *Server) handleListAlbums(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("libraryId")

	type albumCard struct {
		ID         string `json:"id"`
		ArtistID   string `json:"artistId"`
		Title      string `json:"title"`
		Artist     string `json:"artist"`
		Year       int    `json:"year,omitempty"`
		CoverID    string `json:"coverId,omitempty"`
		TrackCount int    `json:"trackCount"`
	}

	var cards []albumCard
	for _, artist := range s.db.Artists.All() {
		if libraryID != "" && artist.LibraryID != libraryID {
			continue
		}
		for _, al := range artist.Albums {
			cards = append(cards, albumCard{
				ID: al.ID, ArtistID: artist.ID, Title: al.Title,
				Artist: artist.Name, Year: al.Year, CoverID: al.CoverID,
				TrackCount: len(al.Tracks),
			})
		}
	}

	switch r.URL.Query().Get("sort") {
	case "year":
		sort.Slice(cards, func(i, j int) bool { return cards[i].Year > cards[j].Year })
	case "artist":
		sort.Slice(cards, func(i, j int) bool {
			if cards[i].Artist != cards[j].Artist {
				return strings.ToLower(cards[i].Artist) < strings.ToLower(cards[j].Artist)
			}
			return cards[i].Year < cards[j].Year
		})
	default:
		sort.Slice(cards, func(i, j int) bool {
			return strings.ToLower(cards[i].Title) < strings.ToLower(cards[j].Title)
		})
	}

	offset := queryIntClamped(r, "offset", 0, 0, 1<<30)
	limit := queryIntClamped(r, "limit", 200, 1, 1000)
	writeJSON(w, http.StatusOK, paginate(cards, offset, limit))
}

// handleGetAlbum returns one album with its full track list.
func (s *Server) handleGetAlbum(w http.ResponseWriter, r *http.Request) {
	albumID := chi.URLParam(r, "id")

	for _, artist := range s.db.Artists.All() {
		album, ok := artist.FindAlbum(albumID)
		if !ok {
			continue
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"album":      album,
			"artistId":   artist.ID,
			"artistName": artist.Name,
		})
		return
	}

	writeError(w, http.StatusNotFound, "No such album.")
}

// ---- photos ----

type photoAlbumCard struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CoverID    string `json:"coverId,omitempty"`
	PhotoCount int    `json:"photoCount"`
	FolderPath string `json:"folderPath,omitempty"`
}

func (s *Server) handleListPhotoAlbums(w http.ResponseWriter, r *http.Request) {
	libraryID := r.URL.Query().Get("libraryId")

	albums := s.db.PhotoAlbums.Filter(func(a models.PhotoAlbum) bool {
		return libraryID == "" || a.LibraryID == libraryID
	})
	sort.Slice(albums, func(i, j int) bool {
		return strings.ToLower(albums[i].Name) < strings.ToLower(albums[j].Name)
	})

	cards := make([]photoAlbumCard, 0, len(albums))
	for _, a := range albums {
		cards = append(cards, photoAlbumCard{
			ID: a.ID, Name: a.Name, CoverID: a.CoverID,
			PhotoCount: len(a.Photos), FolderPath: a.FolderPath,
		})
	}

	writeJSON(w, http.StatusOK, cards)
}

// photoView adds the aspect ratio the masonry grid needs to lay out before the
// images have loaded.
type photoView struct {
	models.Photo
	AspectRatio float64 `json:"aspectRatio"`
}

func (s *Server) handleGetPhotoAlbum(w http.ResponseWriter, r *http.Request) {
	album, err := s.db.PhotoAlbums.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such album.")
		return
	}

	views := make([]photoView, 0, len(album.Photos))
	for _, p := range album.Photos {
		views = append(views, photoView{Photo: p, AspectRatio: p.AspectRatio()})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"id":     album.ID,
		"name":   album.Name,
		"photos": views,
	})
}

// handlePhotoFile serves a photo, resized when a width is requested.
//
// Photos are served from their original location rather than the image cache, so a
// 40 MB raw-ish JPEG is not duplicated into the data directory. Resized copies are
// cached, because a grid of full-size photos would be unusable over a network.
func (s *Server) handlePhotoFile(w http.ResponseWriter, r *http.Request) {
	photoID := chi.URLParam(r, "id")

	photo, ok := s.findPhoto(photoID)
	if !ok {
		http.NotFound(w, r)
		return
	}

	width := queryIntClamped(r, "w", 0, 0, 4000)
	if width == 0 || s.images == nil {
		http.ServeFile(w, r, photo.Path)
		return
	}

	// Import the original into the cache once, then serve resized variants from it.
	key, err := s.images.StoreFile(photo.Path)
	if err != nil {
		http.ServeFile(w, r, photo.Path)
		return
	}
	path, err := s.images.Open(key, width)
	if err != nil {
		http.ServeFile(w, r, photo.Path)
		return
	}

	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) findPhoto(id string) (models.Photo, bool) {
	for _, album := range s.db.PhotoAlbums.All() {
		if p, ok := album.FindPhoto(id); ok {
			return p, true
		}
	}
	return models.Photo{}, false
}
