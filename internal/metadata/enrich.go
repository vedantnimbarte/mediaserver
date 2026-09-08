package metadata

import (
	"context"
	"errors"
	"log"
	"sync"

	"kino/internal/images"
	"kino/internal/models"
)

// posterSize and backdropSize keep downloads reasonable while staying sharp on a
// high-DPI display.
const (
	posterSize   = "w780"
	backdropSize = "w1280"
	stillSize    = "w300"
	profileSize  = "w185"
)

// maxCast limits how many actors are stored. Full TMDb cast lists run to hundreds of
// entries, which would bloat every library file for information no UI shows.
const maxCast = 20

// Enricher fills in provider metadata and caches the artwork it references.
// It satisfies scanner.MetadataEnricher.
type Enricher struct {
	client *Client
	images *images.Cache

	// keyBad latches a rejected API key so a whole library scan does not make one
	// doomed request per item.
	mu     sync.Mutex
	keyBad bool
}

// NewEnricher builds an enricher. A nil or key-less client makes Enabled() false and
// every Enrich call a no-op.
func NewEnricher(client *Client, imageCache *images.Cache) *Enricher {
	return &Enricher{client: client, images: imageCache}
}

// Enabled reports whether metadata lookups can run.
func (e *Enricher) Enabled() bool {
	if e == nil || e.client == nil || !e.client.Enabled() {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return !e.keyBad
}

func (e *Enricher) noteError(err error) {
	if errors.Is(err, ErrBadAPIKey) {
		e.mu.Lock()
		if !e.keyBad {
			log.Printf("metadata: the TMDb API key was rejected; metadata lookups are disabled until it is fixed")
		}
		e.keyBad = true
		e.mu.Unlock()
	}
}

// EnrichMovie looks up a film and fills in its metadata in place.
func (e *Enricher) EnrichMovie(ctx context.Context, movie *models.Movie) error {
	if !e.Enabled() {
		movie.MetaStatus = models.MetaLocal
		return nil
	}

	id := movie.TMDbID
	if id == 0 {
		results, err := e.client.searchMovie(ctx, movie.Title, movie.Year)
		if err != nil {
			e.noteError(err)
			movie.MetaStatus = models.MetaError
			return err
		}
		match, ok := bestMatch(results, movie.Title, movie.Year)
		if !ok {
			movie.MetaStatus = models.MetaNoMatch
			return nil
		}
		id = match.ID
	}

	details, err := e.client.movieDetails(ctx, id)
	if err != nil {
		e.noteError(err)
		if errors.Is(err, ErrNoMatch) {
			movie.MetaStatus = models.MetaNoMatch
			return nil
		}
		movie.MetaStatus = models.MetaError
		return err
	}

	movie.TMDbID = details.ID
	movie.IMDbID = details.IMDbID
	if details.Title != "" {
		movie.Title = details.Title
	}
	movie.Overview = details.Overview
	if details.Tagline != "" {
		movie.Tagline = details.Tagline
	}
	movie.ReleaseDate = details.ReleaseDate
	if y := yearOf(details.ReleaseDate); y > 0 {
		movie.Year = y
	}
	movie.RuntimeMins = details.Runtime
	movie.Rating = details.VoteAverage

	movie.Genres = nil
	for _, g := range details.Genres {
		movie.Genres = append(movie.Genres, g.Name)
	}
	movie.Studios = nil
	for _, s := range details.ProductionCompanies {
		movie.Studios = append(movie.Studios, s.Name)
	}

	movie.Directors = nil
	for _, c := range details.Credits.Crew {
		if c.Job == "Director" {
			movie.Directors = append(movie.Directors, c.Name)
		}
	}
	movie.Cast = e.buildCast(ctx, details.Credits)

	movie.PosterID = e.cacheImage(ctx, details.PosterPath, posterSize)
	movie.BackdropID = e.cacheImage(ctx, details.BackdropPath, backdropSize)

	movie.MetaStatus = models.MetaMatched
	return nil
}

// EnrichShow looks up a series, then walks its seasons to fill in episode details.
func (e *Enricher) EnrichShow(ctx context.Context, show *models.Show) error {
	if !e.Enabled() {
		show.MetaStatus = models.MetaLocal
		return nil
	}

	id := show.TMDbID
	if id == 0 {
		results, err := e.client.searchShow(ctx, show.Title, show.Year)
		if err != nil {
			e.noteError(err)
			show.MetaStatus = models.MetaError
			return err
		}
		match, ok := bestMatch(results, show.Title, show.Year)
		if !ok {
			show.MetaStatus = models.MetaNoMatch
			return nil
		}
		id = match.ID
	}

	details, err := e.client.showDetails(ctx, id)
	if err != nil {
		e.noteError(err)
		if errors.Is(err, ErrNoMatch) {
			show.MetaStatus = models.MetaNoMatch
			return nil
		}
		show.MetaStatus = models.MetaError
		return err
	}

	show.TMDbID = details.ID
	show.IMDbID = details.ExternalIDs.IMDbID
	if details.Name != "" {
		show.Title = details.Name
	}
	show.Overview = details.Overview
	show.FirstAired = details.FirstAirDate
	if y := yearOf(details.FirstAirDate); y > 0 {
		show.Year = y
	}
	show.Status = details.Status
	show.Rating = details.VoteAverage

	show.Genres = nil
	for _, g := range details.Genres {
		show.Genres = append(show.Genres, g.Name)
	}
	show.Networks = nil
	for _, n := range details.Networks {
		show.Networks = append(show.Networks, n.Name)
	}
	show.Cast = e.buildCast(ctx, details.Credits)

	show.PosterID = e.cacheImage(ctx, details.PosterPath, posterSize)
	show.BackdropID = e.cacheImage(ctx, details.BackdropPath, backdropSize)

	// Only fetch seasons we actually hold files for; a long-running show would
	// otherwise cost dozens of requests for episodes the user does not have.
	for i := range show.Seasons {
		season := &show.Seasons[i]
		if err := ctx.Err(); err != nil {
			return err
		}

		seasonInfo, err := e.client.seasonDetails(ctx, details.ID, season.Number)
		if err != nil {
			if !errors.Is(err, ErrNoMatch) {
				log.Printf("metadata: season %d of %q: %v", season.Number, show.Title, err)
			}
			continue
		}

		if season.Name == "" {
			season.Name = seasonInfo.Name
		}
		season.Overview = seasonInfo.Overview
		if season.PosterID == "" {
			season.PosterID = e.cacheImage(ctx, seasonInfo.PosterPath, posterSize)
		}

		byNumber := make(map[int]int, len(seasonInfo.Episodes))
		for idx, ep := range seasonInfo.Episodes {
			byNumber[ep.EpisodeNumber] = idx
		}

		for j := range season.Episodes {
			ep := &season.Episodes[j]
			idx, ok := byNumber[ep.Episode]
			if !ok {
				continue
			}
			remote := seasonInfo.Episodes[idx]

			// The provider title is better than one scraped from a filename, but an
			// empty provider title must not wipe out what we already had.
			if remote.Name != "" {
				ep.Title = remote.Name
			}
			ep.Overview = remote.Overview
			ep.AirDate = remote.AirDate
			ep.Rating = remote.VoteAverage
			if ep.StillID == "" {
				ep.StillID = e.cacheImage(ctx, remote.StillPath, stillSize)
			}
		}
	}

	show.MetaStatus = models.MetaMatched
	return nil
}

func (e *Enricher) buildCast(ctx context.Context, c credits) []models.CastMember {
	var out []models.CastMember
	for _, member := range c.Cast {
		if len(out) >= maxCast {
			break
		}
		out = append(out, models.CastMember{
			Name:      member.Name,
			Character: member.Character,
			Order:     member.Order,
			ProfileID: e.cacheImage(ctx, member.ProfilePath, profileSize),
		})
	}
	return out
}

// cacheImage downloads artwork and returns its cache key, or "" if unavailable.
// A failed image download is never fatal: text metadata is still worth keeping.
func (e *Enricher) cacheImage(ctx context.Context, path, size string) string {
	if path == "" || e.images == nil {
		return ""
	}
	key, err := e.images.Download(ctx, ImageURL(path, size))
	if err != nil {
		log.Printf("metadata: could not cache image %s: %v", path, err)
		return ""
	}
	return key
}

// SearchMovies exposes raw search results so the admin UI can offer a manual match.
func (e *Enricher) SearchMovies(ctx context.Context, title string, year int) ([]Candidate, error) {
	if !e.Enabled() {
		return nil, ErrNoAPIKey
	}
	results, err := e.client.searchMovie(ctx, title, year)
	if err != nil {
		e.noteError(err)
		return nil, err
	}
	return toCandidates(results), nil
}

// SearchShows exposes raw series search results for manual matching.
func (e *Enricher) SearchShows(ctx context.Context, title string, year int) ([]Candidate, error) {
	if !e.Enabled() {
		return nil, ErrNoAPIKey
	}
	results, err := e.client.searchShow(ctx, title, year)
	if err != nil {
		e.noteError(err)
		return nil, err
	}
	return toCandidates(results), nil
}

// Candidate is one option in the manual-match picker.
type Candidate struct {
	TMDbID    int     `json:"tmdbId"`
	Title     string  `json:"title"`
	Year      int     `json:"year,omitempty"`
	Overview  string  `json:"overview,omitempty"`
	PosterURL string  `json:"posterUrl,omitempty"`
	Rating    float64 `json:"rating,omitempty"`
}

func toCandidates(results []searchResult) []Candidate {
	out := make([]Candidate, 0, len(results))
	for _, r := range results {
		out = append(out, Candidate{
			TMDbID:    r.ID,
			Title:     r.name(),
			Year:      r.year(),
			Overview:  r.Overview,
			PosterURL: ImageURL(r.PosterPath, "w185"),
			Rating:    r.VoteAverage,
		})
	}
	return out
}

// VerifyKey checks the configured key.
func (e *Enricher) VerifyKey(ctx context.Context) error {
	if e == nil || e.client == nil {
		return ErrNoAPIKey
	}
	err := e.client.VerifyKey(ctx)
	if err == nil {
		e.mu.Lock()
		e.keyBad = false
		e.mu.Unlock()
	}
	return err
}
