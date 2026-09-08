package api

import (
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"kino/internal/models"
)

// movieCard is the trimmed projection used in grid views. Sending the full record
// (which carries every ffprobe stream and the whole cast list) would make a
// 2000-item library response many megabytes for data the grid never renders.
type movieCard struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Year        int     `json:"year,omitempty"`
	PosterID    string  `json:"posterId,omitempty"`
	Rating      float64 `json:"rating,omitempty"`
	RuntimeMins int     `json:"runtimeMins,omitempty"`
	Genres      []string `json:"genres,omitempty"`
	Available   bool     `json:"available"`
	Progress    float64  `json:"progress,omitempty"`
	Watched     bool     `json:"watched,omitempty"`
	// Height lets the client filter by quality without fetching every stream list.
	Height      int     `json:"height,omitempty"`
	DurationSec float64 `json:"durationSec,omitempty"`
}

type showCard struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Year         int      `json:"year,omitempty"`
	PosterID     string   `json:"posterId,omitempty"`
	Rating       float64  `json:"rating,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	SeasonCount  int      `json:"seasonCount"`
	EpisodeCount int      `json:"episodeCount"`
	Unwatched    int      `json:"unwatched"`
}

func (s *Server) handleListMovies(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	libraryID := r.URL.Query().Get("libraryId")
	genre := r.URL.Query().Get("genre")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	includeMissing := queryBool(r, "includeMissing")

	movies := s.db.Movies.Filter(func(m models.Movie) bool {
		if libraryID != "" && m.LibraryID != libraryID {
			return false
		}
		if !m.Media.Available && !includeMissing {
			return false
		}
		if genre != "" && !containsFold(m.Genres, genre) {
			return false
		}
		if query != "" && !strings.Contains(strings.ToLower(m.Title), query) {
			return false
		}
		return true
	})

	sortMovies(movies, r.URL.Query().Get("sort"))

	cards := make([]movieCard, 0, len(movies))
	for _, m := range movies {
		card := movieCard{
			ID:          m.ID,
			Title:       m.Title,
			Year:        m.Year,
			PosterID:    m.PosterID,
			Rating:      m.Rating,
			RuntimeMins: m.RuntimeMins,
			Genres:      m.Genres,
			Available:   m.Media.Available,
			DurationSec: m.Media.DurationSec,
		}
		if v := m.Media.VideoStream(); v != nil {
			card.Height = v.Height
		}
		if st, err := s.db.PlayStates.Get(models.PlayStateID(user.ID, m.ID)); err == nil {
			card.Progress = st.Progress()
			card.Watched = st.Watched
		}
		cards = append(cards, card)
	}

	offset := queryIntClamped(r, "offset", 0, 0, 1<<30)
	limit := queryIntClamped(r, "limit", 100, 1, 500)
	writeJSON(w, http.StatusOK, paginate(cards, offset, limit))
}

func sortMovies(movies []models.Movie, mode string) {
	switch mode {
	case "year":
		sort.Slice(movies, func(i, j int) bool { return movies[i].Year > movies[j].Year })
	case "rating":
		sort.Slice(movies, func(i, j int) bool { return movies[i].Rating > movies[j].Rating })
	case "added":
		sort.Slice(movies, func(i, j int) bool { return movies[i].AddedAt.After(movies[j].AddedAt) })
	case "runtime":
		sort.Slice(movies, func(i, j int) bool { return movies[i].RuntimeMins > movies[j].RuntimeMins })
	default: // title
		sort.Slice(movies, func(i, j int) bool {
			a, b := movies[i].SortTitle, movies[j].SortTitle
			if a == "" {
				a = movies[i].Title
			}
			if b == "" {
				b = movies[j].Title
			}
			return strings.ToLower(a) < strings.ToLower(b)
		})
	}
}

// movieDetail is the full record plus the viewer's playback state and the stream
// choices the player needs.
type movieDetail struct {
	models.Movie
	Progress   float64        `json:"progress"`
	ResumeSec  float64        `json:"resumeSec"`
	Watched    bool           `json:"watched"`
	AudioTracks []trackOption `json:"audioTracks"`
	Library    string         `json:"library,omitempty"`
}

type trackOption struct {
	Index    int    `json:"index"` // the N in ffmpeg's -map 0:a:N
	Label    string `json:"label"`
	Language string `json:"language,omitempty"`
	Codec    string `json:"codec,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Default  bool   `json:"default,omitempty"`
}

func (s *Server) handleGetMovie(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	movie, err := s.db.Movies.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such movie.")
		return
	}

	detail := movieDetail{Movie: movie, AudioTracks: audioOptions(movie.Media)}
	if st, err := s.db.PlayStates.Get(models.PlayStateID(user.ID, movie.ID)); err == nil {
		detail.Progress = st.Progress()
		detail.Watched = st.Watched
		if st.Resumable() {
			detail.ResumeSec = st.PositionSec
		}
	}
	if lib, err := s.db.Libraries.Get(movie.LibraryID); err == nil {
		detail.Library = lib.Name
	}

	writeJSON(w, http.StatusOK, detail)
}

func audioOptions(media models.MediaInfo) []trackOption {
	streams := media.AudioStreams()
	out := make([]trackOption, 0, len(streams))
	for _, st := range streams {
		out = append(out, trackOption{
			Index:    st.TypeIndex,
			Label:    st.DisplayName(),
			Language: models.LanguageName(st.Language),
			Codec:    st.Codec,
			Channels: st.Channels,
			Default:  st.Default,
		})
	}
	return out
}

func (s *Server) handleListShows(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	libraryID := r.URL.Query().Get("libraryId")
	genre := r.URL.Query().Get("genre")
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))

	shows := s.db.Shows.Filter(func(sh models.Show) bool {
		if libraryID != "" && sh.LibraryID != libraryID {
			return false
		}
		if genre != "" && !containsFold(sh.Genres, genre) {
			return false
		}
		if query != "" && !strings.Contains(strings.ToLower(sh.Title), query) {
			return false
		}
		return true
	})

	switch r.URL.Query().Get("sort") {
	case "year":
		sort.Slice(shows, func(i, j int) bool { return shows[i].Year > shows[j].Year })
	case "added":
		sort.Slice(shows, func(i, j int) bool { return shows[i].AddedAt.After(shows[j].AddedAt) })
	default:
		sort.Slice(shows, func(i, j int) bool {
			return strings.ToLower(shows[i].SortTitle) < strings.ToLower(shows[j].SortTitle)
		})
	}

	cards := make([]showCard, 0, len(shows))
	for _, sh := range shows {
		card := showCard{
			ID:          sh.ID,
			Title:       sh.Title,
			Year:        sh.Year,
			PosterID:    sh.PosterID,
			Rating:      sh.Rating,
			Genres:      sh.Genres,
			SeasonCount: len(sh.Seasons),
		}
		for _, ep := range sh.EpisodesInOrder() {
			if !ep.Media.Available {
				continue
			}
			card.EpisodeCount++
			st, err := s.db.PlayStates.Get(models.PlayStateID(user.ID, ep.ID))
			if err != nil || !st.Watched {
				card.Unwatched++
			}
		}
		cards = append(cards, card)
	}

	offset := queryIntClamped(r, "offset", 0, 0, 1<<30)
	limit := queryIntClamped(r, "limit", 100, 1, 500)
	writeJSON(w, http.StatusOK, paginate(cards, offset, limit))
}

type episodeView struct {
	models.Episode
	Progress    float64       `json:"progress"`
	ResumeSec   float64       `json:"resumeSec"`
	Watched     bool          `json:"watched"`
	DurationSec float64       `json:"durationSec"`
	AudioTracks []trackOption `json:"audioTracks,omitempty"`
}

type seasonView struct {
	Number   int           `json:"number"`
	Name     string        `json:"name,omitempty"`
	Overview string        `json:"overview,omitempty"`
	PosterID string        `json:"posterId,omitempty"`
	Episodes []episodeView `json:"episodes"`
}

type showDetail struct {
	models.Show
	Seasons []seasonView `json:"seasons"`
	Library string       `json:"library,omitempty"`
	// NextUp is the episode the viewer should watch next: their first unwatched or
	// partially watched episode in airing order.
	NextUp *episodeView `json:"nextUp,omitempty"`
}

func (s *Server) handleGetShow(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	show, err := s.db.Shows.Get(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "No such show.")
		return
	}

	detail := showDetail{Show: show}
	detail.Show.Seasons = nil // replaced by the enriched view below

	for _, season := range show.Seasons {
		sv := seasonView{
			Number:   season.Number,
			Name:     season.Name,
			Overview: season.Overview,
			PosterID: season.PosterID,
			Episodes: make([]episodeView, 0, len(season.Episodes)),
		}
		for _, ep := range season.Episodes {
			sv.Episodes = append(sv.Episodes, s.episodeViewFor(user.ID, ep))
		}
		detail.Seasons = append(detail.Seasons, sv)
	}

	if next, ok := s.nextUpFor(user.ID, show); ok {
		detail.NextUp = &next
	}
	if lib, err := s.db.Libraries.Get(show.LibraryID); err == nil {
		detail.Library = lib.Name
	}

	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) episodeViewFor(userID string, ep models.Episode) episodeView {
	ev := episodeView{
		Episode:     ep,
		DurationSec: ep.Media.DurationSec,
		AudioTracks: audioOptions(ep.Media),
	}
	if st, err := s.db.PlayStates.Get(models.PlayStateID(userID, ep.ID)); err == nil {
		ev.Progress = st.Progress()
		ev.Watched = st.Watched
		if st.Resumable() {
			ev.ResumeSec = st.PositionSec
		}
	}
	return ev
}

// nextUpFor picks the episode to resume a series on: the first one in airing order
// that is either partly watched or not watched at all.
func (s *Server) nextUpFor(userID string, show models.Show) (episodeView, bool) {
	for _, ep := range show.EpisodesInOrder() {
		if !ep.Media.Available || ep.Season == 0 {
			continue
		}
		st, err := s.db.PlayStates.Get(models.PlayStateID(userID, ep.ID))
		if err != nil || !st.Watched {
			return s.episodeViewFor(userID, ep), true
		}
	}
	return episodeView{}, false
}

func (s *Server) handleGetEpisode(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)
	id := chi.URLParam(r, "id")

	show, ep, ok := s.db.ShowOfEpisode(id)
	if !ok {
		writeError(w, http.StatusNotFound, "No such episode.")
		return
	}

	view := s.episodeViewFor(user.ID, ep)

	resp := map[string]any{
		"episode":   view,
		"showId":    show.ID,
		"showTitle": show.Title,
		"posterId":  show.PosterID,
	}
	if next, ok := show.NextEpisode(ep.ID); ok {
		resp["nextEpisode"] = map[string]any{
			"id":      next.ID,
			"season":  next.Season,
			"episode": next.Episode,
			"title":   next.Title,
			"stillId": next.StillID,
		}
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleSearch looks across every library at once.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if query == "" {
		writeError(w, http.StatusBadRequest, "A search term is required.")
		return
	}
	limit := queryIntClamped(r, "limit", 30, 1, 100)

	type hit struct {
		Kind     string `json:"kind"`
		ID       string `json:"id"`
		Title    string `json:"title"`
		Subtitle string `json:"subtitle,omitempty"`
		PosterID string `json:"posterId,omitempty"`
		Year     int    `json:"year,omitempty"`
	}

	var hits []hit

	for _, m := range s.db.Movies.All() {
		if !m.Media.Available || !strings.Contains(strings.ToLower(m.Title), query) {
			continue
		}
		hits = append(hits, hit{Kind: "movie", ID: m.ID, Title: m.Title, PosterID: m.PosterID, Year: m.Year})
	}

	for _, sh := range s.db.Shows.All() {
		if strings.Contains(strings.ToLower(sh.Title), query) {
			hits = append(hits, hit{Kind: "show", ID: sh.ID, Title: sh.Title, PosterID: sh.PosterID, Year: sh.Year})
		}
		for _, ep := range sh.EpisodesInOrder() {
			if ep.Title == "" || !ep.Media.Available || !strings.Contains(strings.ToLower(ep.Title), query) {
				continue
			}
			hits = append(hits, hit{
				Kind:     "episode",
				ID:       ep.ID,
				Title:    ep.Title,
				Subtitle: sh.Title,
				PosterID: sh.PosterID,
			})
		}
	}

	// Prefix matches are what the user usually means, so float them to the top.
	sort.SliceStable(hits, func(i, j int) bool {
		pi := strings.HasPrefix(strings.ToLower(hits[i].Title), query)
		pj := strings.HasPrefix(strings.ToLower(hits[j].Title), query)
		if pi != pj {
			return pi
		}
		return len(hits[i].Title) < len(hits[j].Title)
	})

	if len(hits) > limit {
		hits = hits[:limit]
	}
	if hits == nil {
		hits = []hit{}
	}
	writeJSON(w, http.StatusOK, hits)
}

// handleHome builds the landing page: what to continue, and what is new.
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	user := mustUser(r)

	type row struct {
		Kind        string  `json:"kind"`
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Subtitle    string  `json:"subtitle,omitempty"`
		PosterID    string  `json:"posterId,omitempty"`
		BackdropID  string  `json:"backdropId,omitempty"`
		Year        int     `json:"year,omitempty"`
		Progress    float64 `json:"progress,omitempty"`
		ResumeSec   float64 `json:"resumeSec,omitempty"`
		DurationSec float64 `json:"durationSec,omitempty"`
	}

	// Continue Watching, most recently played first.
	states := s.db.PlayStates.Filter(func(p models.PlayState) bool {
		return p.UserID == user.ID && p.Resumable()
	})
	sort.Slice(states, func(i, j int) bool { return states[i].UpdatedAt.After(states[j].UpdatedAt) })

	continueRow := make([]row, 0, len(states))
	for _, st := range states {
		if len(continueRow) >= 20 {
			break
		}
		switch st.Kind {
		case models.PlayableMovie:
			m, err := s.db.Movies.Get(st.MediaID)
			if err != nil || !m.Media.Available {
				continue
			}
			continueRow = append(continueRow, row{
				Kind: "movie", ID: m.ID, Title: m.Title, Year: m.Year,
				PosterID: m.PosterID, BackdropID: m.BackdropID,
				Progress: st.Progress(), ResumeSec: st.PositionSec, DurationSec: st.DurationSec,
			})
		case models.PlayableEpisode:
			show, ep, ok := s.db.ShowOfEpisode(st.MediaID)
			if !ok || !ep.Media.Available {
				continue
			}
			continueRow = append(continueRow, row{
				Kind: "episode", ID: ep.ID, Title: show.Title,
				Subtitle: episodeLabel(ep), PosterID: show.PosterID, BackdropID: show.BackdropID,
				Progress: st.Progress(), ResumeSec: st.PositionSec, DurationSec: st.DurationSec,
			})
		}
	}

	// Recently added movies.
	movies := s.db.Movies.Filter(func(m models.Movie) bool { return m.Media.Available })
	sort.Slice(movies, func(i, j int) bool { return movies[i].AddedAt.After(movies[j].AddedAt) })
	recentMovies := make([]row, 0, 20)
	for _, m := range movies {
		if len(recentMovies) >= 20 {
			break
		}
		recentMovies = append(recentMovies, row{
			Kind: "movie", ID: m.ID, Title: m.Title, Year: m.Year,
			PosterID: m.PosterID, BackdropID: m.BackdropID,
		})
	}

	// Recently added shows.
	shows := s.db.Shows.All()
	sort.Slice(shows, func(i, j int) bool { return shows[i].AddedAt.After(shows[j].AddedAt) })
	recentShows := make([]row, 0, 20)
	for _, sh := range shows {
		if len(recentShows) >= 20 {
			break
		}
		recentShows = append(recentShows, row{
			Kind: "show", ID: sh.ID, Title: sh.Title, Year: sh.Year,
			PosterID: sh.PosterID, BackdropID: sh.BackdropID,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"continueWatching": continueRow,
		"recentMovies":     recentMovies,
		"recentShows":      recentShows,
	})
}

func episodeLabel(ep models.Episode) string {
	label := "S" + pad2(ep.Season) + "E" + pad2(ep.Episode)
	if ep.Title != "" {
		label += " - " + ep.Title
	}
	return label
}

func pad2(n int) string {
	if n < 10 && n >= 0 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func containsFold(list []string, want string) bool {
	for _, v := range list {
		if strings.EqualFold(v, want) {
			return true
		}
	}
	return false
}
