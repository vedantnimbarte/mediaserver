// Package metadata enriches scanned items with information from TMDb.
//
// Everything fetched — text and images alike — is cached locally, so a library only
// needs the network the first time it is scanned.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	tmdbBaseURL  = "https://api.themoviedb.org/3"
	tmdbImageURL = "https://image.tmdb.org/t/p"
)

var (
	// ErrNoAPIKey means metadata lookups are disabled.
	ErrNoAPIKey = errors.New("no TMDb API key is configured")
	// ErrBadAPIKey means the key was rejected, which is worth surfacing loudly rather
	// than retrying for every item in the library.
	ErrBadAPIKey = errors.New("the TMDb API key was rejected")
	// ErrNoMatch means the lookup succeeded but found nothing.
	ErrNoMatch = errors.New("no match found")
)

// Client talks to TMDb, respecting its rate limit.
type Client struct {
	apiKey   string
	language string
	http     *http.Client
	limiter  *rateLimiter
}

// NewClient builds a TMDb client. An empty key produces a client that reports
// Enabled() == false rather than failing at every call site.
func NewClient(apiKey, language string) *Client {
	if language == "" {
		language = "en-US"
	}
	return &Client{
		apiKey:   strings.TrimSpace(apiKey),
		language: language,
		http:     &http.Client{Timeout: 20 * time.Second},
		// TMDb allows roughly 50 requests per second, but a scan is not latency
		// sensitive and staying well under the limit avoids ever being throttled.
		limiter: newRateLimiter(20, time.Second),
	}
}

// Enabled reports whether a key is configured.
func (c *Client) Enabled() bool { return c != nil && c.apiKey != "" }

// get performs a rate-limited GET against the API and decodes the response.
func (c *Client) get(ctx context.Context, path string, params url.Values, out any) error {
	if !c.Enabled() {
		return ErrNoAPIKey
	}
	if err := c.limiter.wait(ctx); err != nil {
		return err
	}

	if params == nil {
		params = url.Values{}
	}
	params.Set("api_key", c.apiKey)
	if params.Get("language") == "" {
		params.Set("language", c.language)
	}

	endpoint := tmdbBaseURL + path + "?" + params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("contact TMDb: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return ErrBadAPIKey
	case http.StatusNotFound:
		return ErrNoMatch
	case http.StatusTooManyRequests:
		// Honour Retry-After and try once more; TMDb sends this rarely but the whole
		// remaining scan would otherwise fail in a cascade.
		delay := 2 * time.Second
		if v := resp.Header.Get("Retry-After"); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 && secs < 60 {
				delay = time.Duration(secs) * time.Second
			}
		}
		select {
		case <-time.After(delay):
			return c.get(ctx, path, params, out)
		case <-ctx.Done():
			return ctx.Err()
		}
	default:
		return fmt.Errorf("TMDb returned %s", resp.Status)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode TMDb response: %w", err)
	}
	return nil
}

// ImageURL builds a full artwork URL for a TMDb image path.
func ImageURL(path, size string) string {
	if path == "" {
		return ""
	}
	if size == "" {
		size = "original"
	}
	return tmdbImageURL + "/" + size + path
}

// ---- response shapes ----

type searchResponse struct {
	Results []searchResult `json:"results"`
}

type searchResult struct {
	ID           int     `json:"id"`
	Title        string  `json:"title"`         // movies
	Name         string  `json:"name"`          // shows
	ReleaseDate  string  `json:"release_date"`  // movies
	FirstAirDate string  `json:"first_air_date"` // shows
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	VoteAverage  float64 `json:"vote_average"`
	Popularity   float64 `json:"popularity"`
}

func (r searchResult) name() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r searchResult) year() int {
	date := r.ReleaseDate
	if date == "" {
		date = r.FirstAirDate
	}
	return yearOf(date)
}

type movieDetails struct {
	ID          int     `json:"id"`
	IMDbID      string  `json:"imdb_id"`
	Title       string  `json:"title"`
	Overview    string  `json:"overview"`
	Tagline     string  `json:"tagline"`
	ReleaseDate string  `json:"release_date"`
	Runtime     int     `json:"runtime"`
	VoteAverage float64 `json:"vote_average"`
	PosterPath  string  `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
	Genres      []struct {
		Name string `json:"name"`
	} `json:"genres"`
	ProductionCompanies []struct {
		Name string `json:"name"`
	} `json:"production_companies"`
	Credits credits `json:"credits"`
}

type showDetails struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	Overview     string  `json:"overview"`
	FirstAirDate string  `json:"first_air_date"`
	Status       string  `json:"status"`
	VoteAverage  float64 `json:"vote_average"`
	PosterPath   string  `json:"poster_path"`
	BackdropPath string  `json:"backdrop_path"`
	Genres       []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Networks []struct {
		Name string `json:"name"`
	} `json:"networks"`
	Seasons []struct {
		SeasonNumber int    `json:"season_number"`
		Name         string `json:"name"`
		Overview     string `json:"overview"`
		PosterPath   string `json:"poster_path"`
	} `json:"seasons"`
	ExternalIDs struct {
		IMDbID string `json:"imdb_id"`
	} `json:"external_ids"`
	Credits credits `json:"credits"`
}

type seasonDetails struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	Episodes     []struct {
		EpisodeNumber int     `json:"episode_number"`
		Name          string  `json:"name"`
		Overview      string  `json:"overview"`
		AirDate       string  `json:"air_date"`
		VoteAverage   float64 `json:"vote_average"`
		StillPath     string  `json:"still_path"`
	} `json:"episodes"`
}

type credits struct {
	Cast []struct {
		Name        string `json:"name"`
		Character   string `json:"character"`
		ProfilePath string `json:"profile_path"`
		Order       int    `json:"order"`
	} `json:"cast"`
	Crew []struct {
		Name string `json:"name"`
		Job  string `json:"job"`
	} `json:"crew"`
}

// ---- API calls ----

func (c *Client) searchMovie(ctx context.Context, title string, year int) ([]searchResult, error) {
	params := url.Values{}
	params.Set("query", title)
	if year > 0 {
		params.Set("year", strconv.Itoa(year))
	}

	var resp searchResponse
	if err := c.get(ctx, "/search/movie", params, &resp); err != nil {
		return nil, err
	}

	// A year filter that returns nothing is often a bad parse rather than a missing
	// film, so retry without it before giving up.
	if len(resp.Results) == 0 && year > 0 {
		params.Del("year")
		if err := c.get(ctx, "/search/movie", params, &resp); err != nil {
			return nil, err
		}
	}
	return resp.Results, nil
}

func (c *Client) searchShow(ctx context.Context, title string, year int) ([]searchResult, error) {
	params := url.Values{}
	params.Set("query", title)
	if year > 0 {
		params.Set("first_air_date_year", strconv.Itoa(year))
	}

	var resp searchResponse
	if err := c.get(ctx, "/search/tv", params, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) == 0 && year > 0 {
		params.Del("first_air_date_year")
		if err := c.get(ctx, "/search/tv", params, &resp); err != nil {
			return nil, err
		}
	}
	return resp.Results, nil
}

func (c *Client) movieDetails(ctx context.Context, id int) (*movieDetails, error) {
	params := url.Values{}
	params.Set("append_to_response", "credits")

	var details movieDetails
	if err := c.get(ctx, "/movie/"+strconv.Itoa(id), params, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

func (c *Client) showDetails(ctx context.Context, id int) (*showDetails, error) {
	params := url.Values{}
	params.Set("append_to_response", "credits,external_ids")

	var details showDetails
	if err := c.get(ctx, "/tv/"+strconv.Itoa(id), params, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

func (c *Client) seasonDetails(ctx context.Context, showID, season int) (*seasonDetails, error) {
	var details seasonDetails
	path := "/tv/" + strconv.Itoa(showID) + "/season/" + strconv.Itoa(season)
	if err := c.get(ctx, path, nil, &details); err != nil {
		return nil, err
	}
	return &details, nil
}

// VerifyKey checks that the configured key works, so Settings can report the problem
// immediately rather than after a failed scan.
func (c *Client) VerifyKey(ctx context.Context) error {
	if !c.Enabled() {
		return ErrNoAPIKey
	}
	var out struct {
		Images struct {
			BaseURL string `json:"base_url"`
		} `json:"images"`
	}
	return c.get(ctx, "/configuration", nil, &out)
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y, err := strconv.Atoi(date[:4])
	if err != nil {
		return 0
	}
	return y
}
