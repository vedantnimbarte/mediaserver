package metadata

import "testing"

func result(id int, name, date string, popularity float64) searchResult {
	return searchResult{
		ID: id, Title: name, ReleaseDate: date,
		Popularity: popularity, PosterPath: "/p.jpg",
	}
}

// TestBestMatchPrefersTheRightYear is the case that motivates scoring at all: TMDb
// orders by popularity, so searching "Alien" puts the 2017 sequel ahead of the 1979
// original that the user actually has on disk.
func TestBestMatchPrefersTheRightYear(t *testing.T) {
	results := []searchResult{
		result(126889, "Alien: Covenant", "2017-05-09", 90),
		result(348, "Alien", "1979-05-25", 40),
		result(8077, "Aliens", "1986-07-18", 50),
	}

	match, ok := bestMatch(results, "Alien", 1979)
	if !ok {
		t.Fatal("no match found")
	}
	if match.ID != 348 {
		t.Errorf("matched %q (%d), want the 1979 Alien", match.name(), match.ID)
	}
}

func TestBestMatchExactTitleBeatsPopularity(t *testing.T) {
	results := []searchResult{
		result(1, "The Batman Returns Again", "2022-01-01", 500),
		result(2, "Batman", "1989-06-23", 10),
	}

	match, ok := bestMatch(results, "Batman", 1989)
	if !ok {
		t.Fatal("no match found")
	}
	if match.ID != 2 {
		t.Errorf("matched %q, want the exact title", match.name())
	}
}

func TestBestMatchHandlesArticlesAndPunctuation(t *testing.T) {
	cases := []struct {
		query   string
		results []searchResult
		wantID  int
	}{
		{
			query:   "Lord of the Rings The Fellowship of the Ring",
			results: []searchResult{result(120, "The Lord of the Rings: The Fellowship of the Ring", "2001-12-18", 60)},
			wantID:  120,
		},
		{
			query:   "WALL-E",
			results: []searchResult{result(10681, "WALL·E", "2008-06-22", 60)},
			wantID:  10681,
		},
		{
			query:   "Spider Man Into the Spider Verse",
			results: []searchResult{result(324857, "Spider-Man: Into the Spider-Verse", "2018-12-06", 60)},
			wantID:  324857,
		},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			match, ok := bestMatch(tc.results, tc.query, 0)
			if !ok {
				t.Fatalf("no match for %q", tc.query)
			}
			if match.ID != tc.wantID {
				t.Errorf("matched %d, want %d", match.ID, tc.wantID)
			}
		})
	}
}

// TestBestMatchRejectsNonsense stops a mis-parsed filename from attaching a wildly
// wrong poster and description to an item.
func TestBestMatchRejectsNonsense(t *testing.T) {
	results := []searchResult{
		result(1, "Completely Unrelated Documentary", "2015-01-01", 5),
	}
	if match, ok := bestMatch(results, "Zzzz Qqqq Wwww", 1999); ok {
		t.Errorf("expected no match, got %q", match.name())
	}
}

func TestBestMatchEmptyResults(t *testing.T) {
	if _, ok := bestMatch(nil, "Anything", 2000); ok {
		t.Error("expected no match from an empty result set")
	}
}

// TestYearOffByOneIsTolerated covers festival premiere vs general release, which
// disagree by a year often enough that a hard year filter would break real libraries.
func TestYearOffByOneIsTolerated(t *testing.T) {
	results := []searchResult{
		result(1, "Parasite", "2019-05-30", 60),
		result(2, "Parasite Zero", "2020-01-01", 90),
	}
	match, ok := bestMatch(results, "Parasite", 2020)
	if !ok {
		t.Fatal("no match")
	}
	if match.ID != 1 {
		t.Errorf("matched %q, want the real Parasite despite the year being one off", match.name())
	}
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"The Matrix":                    "matrix",
		"Spider-Man: Into the Spider-Verse": "spider man into the spider verse",
		"WALL·E":                        "wall e",
		"A Quiet Place":                 "quiet place",
		// Only a leading article is dropped; interior ones are part of the title.
		"The Day the Earth Stood Still": "day the earth stood still",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestYearOf(t *testing.T) {
	cases := map[string]int{
		"1999-03-31": 1999,
		"2021":       2021,
		"":           0,
		"bad":        0,
	}
	for in, want := range cases {
		if got := yearOf(in); got != want {
			t.Errorf("yearOf(%q) = %d, want %d", in, got, want)
		}
	}
}
