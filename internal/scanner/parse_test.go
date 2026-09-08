package scanner

import (
	"reflect"
	"testing"
)

func TestParseMovieName(t *testing.T) {
	cases := []struct {
		path      string
		wantTitle string
		wantYear  int
	}{
		// Clean, well-organized libraries
		{`Movies/The Matrix (1999)/The Matrix (1999).mkv`, "The Matrix", 1999},
		{`Movies/Inception (2010).mp4`, "Inception", 2010},
		{`Movies/Spirited Away (2001)/Spirited Away.mkv`, "Spirited Away", 2001},

		// Scene releases
		{`The.Matrix.1999.1080p.BluRay.x264-GROUP.mkv`, "The Matrix", 1999},
		{`Inception.2010.2160p.UHD.BluRay.x265.HDR.Atmos.TrueHD.7.1-TERMiNAL.mkv`, "Inception", 2010},
		{`Mad.Max.Fury.Road.2015.1080p.WEB-DL.DD5.1.H264-RARBG.mp4`, "Mad Max Fury Road", 2015},
		{`Arrival_2016_1080p_BluRay_x264.mkv`, "Arrival", 2016},

		// The folder carries the information, the file does not
		{`The.Godfather.1972.1080p.BluRay.x264-AMIABLE/amiable-godfather.mkv`, "The Godfather", 1972},
		{`Blade Runner (1982)/VIDEO_TS.mkv`, "Blade Runner", 1982},

		// Titles that contain numbers which look like years
		{`Movies/2012 (2009)/2012 (2009).mkv`, "2012", 2009},
		{`Blade.Runner.2049.2017.1080p.BluRay.x264.mkv`, "Blade Runner 2049", 2017},
		{`Movies/1917 (2019).mkv`, "1917", 2019},
		{`Movies/1984 (1984).mkv`, "1984", 1984},

		// Punctuation-heavy titles
		{`S.W.A.T.2003.1080p.BluRay.x264.mkv`, "S.W.A.T", 2003},
		{`Movies/WALL-E (2008).mkv`, "WALL-E", 2008},
		{`Movies/Amelie (2001).mkv`, "Amelie", 2001},

		// Tracker and checksum noise
		{`The.Prestige.2006.1080p.BluRay.x264.[YTS.MX].mp4`, "The Prestige", 2006},
		{`[Group] Akira (1988) [1080p][BDRip].mkv`, "Akira", 1988},

		// No year at all
		{`Movies/Some Obscure Documentary.mkv`, "Some Obscure Documentary", 0},
		{`Home Videos/birthday party 2019 1080p.mp4`, "birthday party", 2019},

		// Bracket variants
		{`Movies/Dune [2021].mkv`, "Dune", 2021},
		{`Movies/Parasite {2019}.mkv`, "Parasite", 2019},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := ParseMovieName(tc.path)
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Year != tc.wantYear {
				t.Errorf("year = %d, want %d", got.Year, tc.wantYear)
			}
		})
	}
}

func TestParseMovieEdition(t *testing.T) {
	cases := []struct {
		path        string
		wantTitle   string
		wantEdition string
	}{
		{`Blade.Runner.1982.Directors.Cut.1080p.BluRay.mkv`, "Blade Runner", "Director's Cut"},
		{`The.Lord.of.the.Rings.2001.Extended.1080p.mkv`, "The Lord of the Rings", "Extended"},
		{`Aliens.1986.Remastered.1080p.BluRay.mkv`, "Aliens", "Remastered"},
		{`Interstellar.2014.IMAX.1080p.mkv`, "Interstellar", "IMAX"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := ParseMovieName(tc.path)
			if got.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", got.Title, tc.wantTitle)
			}
			if got.Edition != tc.wantEdition {
				t.Errorf("edition = %q, want %q", got.Edition, tc.wantEdition)
			}
		})
	}
}

func TestParseEpisodeName(t *testing.T) {
	cases := []struct {
		path    string
		show    string
		season  int
		episode int
		title   string
	}{
		// Standard layouts
		{`TV/Breaking Bad/Season 01/Breaking Bad - S01E01 - Pilot.mkv`, "Breaking Bad", 1, 1, "Pilot"},
		{`TV/The Office/Season 02/The Office S02E05 Halloween.mkv`, "The Office", 2, 5, "Halloween"},
		{`TV/Firefly/Firefly.S01E14.Objects.in.Space.1080p.BluRay.x264.mkv`, "Firefly", 1, 14, "Objects in Space"},

		// Season comes from the folder, filename has only the marker
		{`TV/Breaking Bad/Season 03/S03E07.mkv`, "Breaking Bad", 3, 7, ""},
		{`TV/Chernobyl/Season 1/E04.mkv`, "Chernobyl", 1, 4, ""},
		{`TV/Planet Earth/Season 1/03.mkv`, "Planet Earth", 1, 3, ""},

		// Alternative markers
		{`TV/Lost/Lost 1x02 Pilot Part 2.avi`, "Lost", 1, 2, "Pilot Part 2"},
		{`TV/Sherlock/Sherlock Season 2 Episode 3.mkv`, "Sherlock", 2, 3, ""},
		{`TV/Show/show.s01e03.mkv`, "show", 1, 3, ""},

		// Specials
		{`TV/Doctor Who/Specials/E01.mkv`, "Doctor Who", 0, 1, ""},
		{`TV/Doctor Who/Doctor.Who.S00E05.Christmas.Special.mkv`, "Doctor Who", 0, 5, "Christmas Special"},

		// Three-digit episode numbers (anime and long-running shows)
		{`TV/One Piece/One Piece S01E1024.mkv`, "One Piece", 1, 1024, ""},

		// Show name recovered from the grandparent folder, with the year stripped out
		// (the scanner records it separately via ShowYear).
		{`TV/Better Call Saul (2015)/Season 04/S04E09.mkv`, "Better Call Saul", 4, 9, ""},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := ParseEpisodeName(tc.path)
			if !ok {
				t.Fatalf("failed to recognize an episode")
			}
			if got.Show != tc.show {
				t.Errorf("show = %q, want %q", got.Show, tc.show)
			}
			if got.Season != tc.season {
				t.Errorf("season = %d, want %d", got.Season, tc.season)
			}
			if got.Episode != tc.episode {
				t.Errorf("episode = %d, want %d", got.Episode, tc.episode)
			}
			if got.Title != tc.title {
				t.Errorf("title = %q, want %q", got.Title, tc.title)
			}
		})
	}
}

func TestParseMultiEpisodeFiles(t *testing.T) {
	cases := []struct {
		path      string
		episode   int
		wantExtra []int
	}{
		{`TV/Show/Season 1/Show.S01E01E02.mkv`, 1, []int{2}},
		{`TV/Show/Season 1/Show.S01E01-E02.mkv`, 1, []int{2}},
		{`TV/Show/Season 1/Show.S01E05E06E07.mkv`, 5, []int{6, 7}},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got, ok := ParseEpisodeName(tc.path)
			if !ok {
				t.Fatal("failed to recognize an episode")
			}
			if got.Episode != tc.episode {
				t.Errorf("episode = %d, want %d", got.Episode, tc.episode)
			}
			if !reflect.DeepEqual(got.Extra, tc.wantExtra) {
				t.Errorf("extra = %v, want %v", got.Extra, tc.wantExtra)
			}
		})
	}
}

// TestNonEpisodesAreRejected guards the boundary between the movie and TV parsers:
// a movie in a TV library must not be silently turned into episode 0.
func TestNonEpisodesAreRejected(t *testing.T) {
	paths := []string{
		`Movies/The Matrix (1999).mkv`,
		`Movies/Inception.2010.1080p.BluRay.mkv`,
		`Random/notes.txt`,
	}
	for _, p := range paths {
		if ep, ok := ParseEpisodeName(p); ok {
			t.Errorf("%s was parsed as an episode: %+v", p, ep)
		}
	}
}

func TestSortTitle(t *testing.T) {
	cases := map[string]string{
		"The Matrix":    "Matrix, The",
		"A Quiet Place": "Quiet Place, A",
		"An Education":  "Education, An",
		"Inception":     "Inception",
		"Theodore Rex":  "Theodore Rex", // must not treat "Theodore" as the article "The"
	}
	for in, want := range cases {
		if got := SortTitle(in); got != want {
			t.Errorf("SortTitle(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeSeparatorsKeepsInitialisms(t *testing.T) {
	cases := map[string]string{
		"S.W.A.T.2003":       "S.W.A.T 2003",
		"The.Matrix.1999":    "The Matrix 1999",
		"Agents.of.S.H.I.E.L.D": "Agents of S.H.I.E.L.D",
		"under_score_name":   "under score name",
	}
	for in, want := range cases {
		if got := normalizeSeparators(in); got != want {
			t.Errorf("normalizeSeparators(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShowYear(t *testing.T) {
	cases := map[string]int{
		"Better Call Saul (2015)": 2015,
		"The Office (US) (2005)":  2005,
		"Breaking Bad":            0,
		"24":                      0, // a title that is a number, not a year
	}
	for in, want := range cases {
		if got := ShowYear(in); got != want {
			t.Errorf("ShowYear(%q) = %d, want %d", in, got, want)
		}
	}
}
