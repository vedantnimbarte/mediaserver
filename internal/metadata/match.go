package metadata

import (
	"strings"
	"unicode"
)

// scoreMatch rates how well a search result matches what we parsed from the filename.
//
// TMDb orders by popularity, which is wrong often enough to matter: searching
// "Alien" returns the 2017 Covenant sequel above the 1979 original. Scoring on title
// similarity and year proximity fixes that.
func scoreMatch(result searchResult, wantTitle string, wantYear int) float64 {
	got := normalize(result.name())
	want := normalize(wantTitle)

	var score float64

	switch {
	case got == want:
		score += 100
	case strings.HasPrefix(got, want) || strings.HasPrefix(want, got):
		score += 70
	case strings.Contains(got, want) || strings.Contains(want, got):
		score += 45
	default:
		// Fall back to token overlap so "The Lord of the Rings Fellowship" still
		// matches "The Lord of the Rings: The Fellowship of the Ring".
		score += 40 * tokenOverlap(got, want)
	}

	if wantYear > 0 {
		resultYear := result.year()
		switch {
		case resultYear == 0:
			// Unknown year: no evidence either way.
		case resultYear == wantYear:
			score += 50
		case abs(resultYear-wantYear) == 1:
			// Release-year disagreements of a single year are common between a
			// festival premiere and general release, so only a small penalty.
			score += 30
		case abs(resultYear-wantYear) <= 3:
			score += 5
		default:
			score -= 40
		}
	}

	// Popularity is a weak tiebreaker only, capped so it can never outweigh the title.
	score += minFloat(result.Popularity/100, 5)

	// Prefer entries that actually have artwork; a match with no poster leaves a
	// blank card in the UI.
	if result.PosterPath != "" {
		score += 3
	}

	return score
}

// bestMatch picks the highest-scoring result, or reports false when nothing is
// convincing enough to use.
func bestMatch(results []searchResult, title string, year int) (searchResult, bool) {
	const minimumScore = 35

	best := searchResult{}
	bestScore := 0.0
	found := false

	for _, r := range results {
		score := scoreMatch(r, title, year)
		if !found || score > bestScore {
			best, bestScore, found = r, score, true
		}
	}

	if !found || bestScore < minimumScore {
		return searchResult{}, false
	}
	return best, true
}

// normalize reduces a title to comparable form: lowercase, no punctuation, no
// articles, single spaces.
func normalize(s string) string {
	// Every non-alphanumeric rune becomes a space, including exotic punctuation.
	// Treating them inconsistently is what makes "WALL-E" fail to match "WALL·E":
	// dropping one separator and spacing the other yields "walle" versus "wall e".
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}

	fields := strings.Fields(b.String())
	out := make([]string, 0, len(fields))
	for i, f := range fields {
		// Drop leading articles only; "The Day the Earth Stood Still" must keep the
		// second "the" or it stops matching itself.
		if i == 0 && (f == "the" || f == "a" || f == "an") {
			continue
		}
		out = append(out, f)
	}
	return strings.Join(out, " ")
}

// tokenOverlap is the fraction of the shorter title's words present in the longer one.
func tokenOverlap(a, b string) float64 {
	at := strings.Fields(a)
	bt := strings.Fields(b)
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}

	set := make(map[string]bool, len(bt))
	for _, t := range bt {
		set[t] = true
	}

	matched := 0
	for _, t := range at {
		if set[t] {
			matched++
		}
	}

	shorter := len(at)
	if len(bt) < shorter {
		shorter = len(bt)
	}
	return float64(matched) / float64(shorter)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
