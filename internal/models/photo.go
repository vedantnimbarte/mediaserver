package models

import "time"

// PhotoAlbum is a folder of images. Photos are nested because an album is always
// browsed as a unit and this keeps photos.json to one record per folder.
type PhotoAlbum struct {
	ID        string `json:"id"`
	LibraryID string `json:"libraryId"`

	Name       string `json:"name"`
	FolderPath string `json:"folderPath"`
	CoverID    string `json:"coverId,omitempty"` // photo ID used as the album thumbnail

	Photos []Photo `json:"photos"`

	AddedAt   time.Time `json:"addedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (a PhotoAlbum) EntityID() string { return a.ID }

// Photo is a single image file plus whatever EXIF told us about it.
type Photo struct {
	ID      string `json:"id"`
	AlbumID string `json:"albumId"`

	Path     string    `json:"path"`
	Filename string    `json:"filename"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"modTime"`

	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// Orientation is the raw EXIF orientation tag (1-8); 1 means no rotation needed.
	Orientation int `json:"orientation,omitempty"`

	TakenAt  time.Time `json:"takenAt,omitempty"`
	Camera   string    `json:"camera,omitempty"`
	Lens     string    `json:"lens,omitempty"`
	ISO      int       `json:"iso,omitempty"`
	Aperture string    `json:"aperture,omitempty"`
	Shutter  string    `json:"shutter,omitempty"`
	FocalLen string    `json:"focalLen,omitempty"`

	// GPS coordinates, zero when the image carries no location.
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`

	Available bool `json:"available"`
}

func (p Photo) EntityID() string { return p.ID }

// Unchanged reports whether the file on disk still matches the scanned record.
func (p Photo) Unchanged(size int64, modTime time.Time) bool {
	return p.Size == size && p.ModTime.Equal(modTime)
}

// AspectRatio returns width/height, defaulting to 1.5 when dimensions are unknown so
// the masonry grid still has something to lay out with.
func (p Photo) AspectRatio() float64 {
	if p.Width > 0 && p.Height > 0 {
		// Orientations 5-8 involve a 90 degree rotation, swapping the display axes.
		if p.Orientation >= 5 && p.Orientation <= 8 {
			return float64(p.Height) / float64(p.Width)
		}
		return float64(p.Width) / float64(p.Height)
	}
	return 1.5
}

// FindPhoto locates a photo by ID within the album.
func (a PhotoAlbum) FindPhoto(id string) (Photo, bool) {
	for _, p := range a.Photos {
		if p.ID == id {
			return p, true
		}
	}
	return Photo{}, false
}
