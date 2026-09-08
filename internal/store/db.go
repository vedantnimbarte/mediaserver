package store

import (
	"errors"
	"fmt"
	"path/filepath"

	"kino/internal/models"
)

// DB bundles every collection the server persists.
type DB struct {
	Users       *Collection[models.User]
	Libraries   *Collection[models.Library]
	Movies      *Collection[models.Movie]
	Shows       *Collection[models.Show]
	Artists     *Collection[models.Artist]
	PhotoAlbums *Collection[models.PhotoAlbum]
	PlayStates  *Collection[models.PlayState]
	Prefs       *Collection[models.UserPrefs]

	dir string
}

// OpenDB loads every collection from dataDir. On any failure the collections opened
// so far are closed before the error is returned, so no flusher goroutine is leaked.
func OpenDB(dataDir string) (*DB, error) {
	db := &DB{dir: dataDir}

	var opened []interface{ Close() error }
	fail := func(err error) (*DB, error) {
		for _, c := range opened {
			_ = c.Close()
		}
		return nil, err
	}

	var err error
	if db.Users, err = Open[models.User](dataDir, "users"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.Users)

	if db.Libraries, err = Open[models.Library](dataDir, "libraries"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.Libraries)

	if db.Movies, err = Open[models.Movie](dataDir, "movies"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.Movies)

	if db.Shows, err = Open[models.Show](dataDir, "shows"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.Shows)

	if db.Artists, err = Open[models.Artist](dataDir, "music"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.Artists)

	if db.PhotoAlbums, err = Open[models.PhotoAlbum](dataDir, "photos"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.PhotoAlbums)

	if db.PlayStates, err = Open[models.PlayState](dataDir, "playstate"); err != nil {
		return fail(err)
	}
	opened = append(opened, db.PlayStates)

	if db.Prefs, err = Open[models.UserPrefs](dataDir, "preferences"); err != nil {
		return fail(err)
	}

	return db, nil
}

// PrefsFor returns a user's preferences, falling back to the defaults for an account
// that has never changed anything.
func (db *DB) PrefsFor(userID string) models.UserPrefs {
	prefs, err := db.Prefs.Get(userID)
	if err != nil {
		return models.DefaultPrefs(userID)
	}
	prefs.Normalize()
	return prefs
}

// Dir returns the directory backing this database.
func (db *DB) Dir() string { return db.dir }

// Close flushes and shuts down every collection, returning the first error seen.
func (db *DB) Close() error {
	var firstErr error
	closers := []interface{ Close() error }{
		db.Users, db.Libraries, db.Movies, db.Shows,
		db.Artists, db.PhotoAlbums, db.PlayStates, db.Prefs,
	}
	for _, c := range closers {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Flush forces every collection to disk immediately.
func (db *DB) Flush() error {
	var firstErr error
	flushers := []interface{ Flush() error }{
		db.Users, db.Libraries, db.Movies, db.Shows,
		db.Artists, db.PhotoAlbums, db.PlayStates, db.Prefs,
	}
	for _, f := range flushers {
		if err := f.Flush(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// Playable is a uniform handle on anything that can be streamed, regardless of which
// collection it came from. Everything downstream of playback â€” direct play, the
// transcoder, subtitles, playstate â€” works in terms of this rather than the concrete type.
type Playable struct {
	ID    string
	Kind  models.PlayableKind
	Title string
	// ParentID is the show ID for episodes and the album ID for tracks; empty for movies.
	ParentID string
	Media    models.MediaInfo
}

// ErrNotPlayable is returned when an ID matches nothing streamable.
var ErrNotPlayable = errors.New("no playable media with that id")

// ResolvePlayable finds the media behind an ID, searching movies, then episodes,
// then music tracks. Libraries are small enough that a linear scan is cheaper than
// maintaining a separate index, and this is only called on playback start and
// detail views â€” never on the segment hot path.
func (db *DB) ResolvePlayable(id string) (Playable, error) {
	if mv, err := db.Movies.Get(id); err == nil {
		return Playable{
			ID:    mv.ID,
			Kind:  models.PlayableMovie,
			Title: mv.Title,
			Media: mv.Media,
		}, nil
	}

	for _, show := range db.Shows.All() {
		if ep, ok := show.FindEpisode(id); ok {
			return Playable{
				ID:       ep.ID,
				Kind:     models.PlayableEpisode,
				Title:    episodeLabel(show, ep),
				ParentID: show.ID,
				Media:    ep.Media,
			}, nil
		}
	}

	for _, artist := range db.Artists.All() {
		if tr, album, ok := artist.FindTrack(id); ok {
			return Playable{
				ID:       tr.ID,
				Kind:     models.PlayableTrack,
				Title:    tr.Title,
				ParentID: album.ID,
				Media:    tr.Media,
			}, nil
		}
	}

	return Playable{}, ErrNotPlayable
}

func episodeLabel(show models.Show, ep models.Episode) string {
	label := fmt.Sprintf("%s - S%02dE%02d", show.Title, ep.Season, ep.Episode)
	if ep.Title != "" {
		label += " - " + ep.Title
	}
	return label
}

// ShowOfEpisode returns the show containing the given episode ID.
func (db *DB) ShowOfEpisode(episodeID string) (models.Show, models.Episode, bool) {
	for _, show := range db.Shows.All() {
		if ep, ok := show.FindEpisode(episodeID); ok {
			return show, ep, true
		}
	}
	return models.Show{}, models.Episode{}, false
}

// ArtistOfTrack returns the artist and album containing the given track ID.
func (db *DB) ArtistOfTrack(trackID string) (models.Artist, models.Album, models.Track, bool) {
	for _, artist := range db.Artists.All() {
		if tr, album, ok := artist.FindTrack(trackID); ok {
			return artist, album, tr, true
		}
	}
	return models.Artist{}, models.Album{}, models.Track{}, false
}

// UpdateEpisode mutates one episode in place within its show and persists the show.
func (db *DB) UpdateEpisode(episodeID string, fn func(*models.Episode)) error {
	for _, show := range db.Shows.All() {
		for si := range show.Seasons {
			for ei := range show.Seasons[si].Episodes {
				if show.Seasons[si].Episodes[ei].ID != episodeID {
					continue
				}
				_, err := db.Shows.Update(show.ID, func(s *models.Show) {
					// Re-find inside the locked copy: the snapshot above may be stale.
					for i := range s.Seasons {
						for j := range s.Seasons[i].Episodes {
							if s.Seasons[i].Episodes[j].ID == episodeID {
								fn(&s.Seasons[i].Episodes[j])
								return
							}
						}
					}
				})
				return err
			}
		}
	}
	return ErrNotFound
}

// FlushPath reports where a named collection is stored, for diagnostics.
func (db *DB) FlushPath(name string) string { return filepath.Join(db.dir, name+".json") }
