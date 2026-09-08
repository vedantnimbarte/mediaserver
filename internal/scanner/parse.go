// Package scanner walks library folders, works out what each file is, and turns the
// result into the movie, show, artist and photo records the rest of the server serves.
package scanner

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// MovieName is what we could work out about a film from its path alone.
type MovieName struct {
	Title   string
	Year    int
	Edition string // "Director's Cut", "Extended", ... when the filename says so
}

// EpisodeName is what we could work out about a TV episode from its path alone.
type EpisodeName struct {
	Show    string
	Season  int
	Episode int
	Title   string
	// Extra holds the additional episode numbers of a multi-episode file
	// (S01E01E02 covers two episodes in one file).
	Extra []int
}

// releaseTags are the scene/release annotations that are never part of a title.
// Everything from the first one onwards is dropped when cleaning a name.
var releaseTags = map[string]bool{
	// Resolution and scan
	"2160p": true, "1440p": true, "1080p": true, "1080i": true, "720p": true,
	"576p": true, "480p": true, "480i": true, "360p": true, "4k": true, "8k": true,
	"uhd": true, "fhd": true, "hd": true, "sd": true,

	// Source
	"bluray": true, "blu": true, "ray": true, "bdrip": true, "brrip": true, "bdremux": true,
	"remux": true, "webrip": true, "webdl": true, "web": true, "hdtv": true, "pdtv": true,
	"dvdrip": true, "dvdscr": true, "dvd": true, "hdrip": true, "camrip": true, "cam": true,
	"telesync": true, "ts": true, "tc": true, "r5": true, "vhsrip": true, "hdcam": true,
	"amzn": true, "nf": true, "hulu": true, "dsnp": true, "hmax": true, "atvp": true, "pcok": true,

	// Video codec
	"x264": true, "x265": true, "h264": true, "h265": true, "hevc": true, "avc": true,
	"xvid": true, "divx": true, "av1": true, "vp9": true, "mpeg2": true,
	"10bit": true, "8bit": true, "12bit": true, "hi10p": true,

	// Audio
	"aac": true, "ac3": true, "eac3": true, "dd": true, "ddp": true, "dts": true,
	"dtshd": true, "truehd": true, "atmos": true, "flac": true, "mp3": true, "opus": true,
	"lpcm": true, "pcm": true, "mp2": true,

	// HDR
	"hdr": true, "hdr10": true, "hdr10plus": true, "dv": true, "dovi": true,
	"dolbyvision": true, "sdr": true, "hlg": true,

	// Misc release noise
	"proper": true, "repack": true, "internal": true, "limited": true, "festival": true,
	"subbed": true, "dubbed": true, "subs": true, "multi": true, "dual": true,
	"complete": true, "readnfo": true, "rerip": true, "uncut": true, "hybrid": true,
}

// editionTags mark a specific cut of a film. These are worth keeping as an annotation
// rather than silently discarding, because a user may hold several cuts of one film.
var editionTags = map[string]string{
	"extended":    "Extended",
	"unrated":     "Unrated",
	"remastered":  "Remastered",
	"imax":        "IMAX",
	"theatrical":  "Theatrical",
	"uncensored":  "Uncensored",
	"anniversary": "Anniversary Edition",
	"criterion":   "Criterion",
}

var (
	// Matches a year in parentheses, brackets or braces: the most reliable signal.
	reBracketedYear = regexp.MustCompile(`[\(\[\{]\s*((?:19|20)\d{2})\s*[\)\]\}]`)
	// Matches a bare four-digit year as a standalone token.
	reBareYear = regexp.MustCompile(`(?:^|[\s\.\-_])((?:19|20)\d{2})(?:$|[\s\.\-_])`)

	// S01E02, s1e2, S01E02E03, S01E02-E03, S01E02-03
	reSeasonEpisode = regexp.MustCompile(`(?i)\bs(\d{1,3})[\s\.\-_]*e(\d{1,4})((?:[\s\.\-_]*(?:e|-)\s*\d{1,4})*)`)
	// 1x02, 01x02
	reCrossEpisode = regexp.MustCompile(`(?i)(?:^|[\s\.\-_\[])(\d{1,3})x(\d{1,4})\b`)
	// "Season 1 Episode 2" spelled out
	reWordyEpisode = regexp.MustCompile(`(?i)\bseason[\s\.\-_]*(\d{1,3})[\s\.\-_]*episode[\s\.\-_]*(\d{1,4})`)
	// A "Season 03" / "S03" / "Series 3" folder name
	reSeasonFolder = regexp.MustCompile(`(?i)^(?:season|series|saison|staffel|s)[\s\.\-_]*(\d{1,3})$`)
	// A bare "E05" / "Ep05" / "Episode 5" when the season comes from the folder
	reBareEpisode = regexp.MustCompile(`(?i)(?:^|[\s\.\-_])(?:e|ep|episode)[\s\.\-_]*(\d{1,4})(?:$|[\s\.\-_])`)
	// Additional episode numbers inside a multi-episode marker
	reExtraEpisode = regexp.MustCompile(`\d{1,4}`)

	// Trailing "-GROUP" release-team suffix
	reGroupSuffix = regexp.MustCompile(`(?i)[\-\s]([a-z0-9]{2,20})$`)
	// Runs of whitespace left behind after cleaning
	reMultiSpace = regexp.MustCompile(`\s{2,}`)
)

// genericNames are filenames that carry no information, so the parent folder must be
// consulted instead. Disc rips and torrent conventions produce a lot of these.
var genericNames = map[string]bool{
	"video_ts": true, "vts_01_1": true, "index": true, "movie": true,
	"film": true, "main": true, "title00": true, "title01": true, "playlist": true,
}

// ParseMovieName extracts a title, year and edition from a movie file path.
//
// The filename is tried first; if it yields no year and the parent folder does, the
// folder wins. That handles the very common release layout where the folder is
// "The.Matrix.1999.1080p.BluRay.x264-GROUP" and the file inside is "group-matrix.mkv".
func ParseMovieName(path string) MovieName {
	base := stemOf(path)
	parent := filepath.Base(filepath.Dir(path))

	fromFile := parseMovieString(base)
	if genericNames[strings.ToLower(strings.TrimSpace(base))] || fromFile.Title == "" {
		if fromParent := parseMovieString(parent); fromParent.Title != "" {
			return fromParent
		}
	}
	// A year in the folder is more trustworthy than no year at all.
	if fromFile.Year == 0 {
		if fromParent := parseMovieString(parent); fromParent.Year != 0 {
			if titlesAgree(fromFile.Title, fromParent.Title) {
				fromFile.Year = fromParent.Year
				if fromFile.Edition == "" {
					fromFile.Edition = fromParent.Edition
				}
			} else {
				// The names disagree and only the folder has a year. That is the scene
				// layout, where the folder carries the real title and the file inside is
				// named after the release group. Trust the folder outright.
				return fromParent
			}
		}
	}
	return fromFile
}

// titlesAgree reports whether two candidate titles are close enough that metadata
// from one can be applied to the other. Release folders and their inner files often
// differ in punctuation and casing only.
func titlesAgree(a, b string) bool {
	na, nb := normalizeForCompare(a), normalizeForCompare(b)
	if na == "" || nb == "" {
		return true // nothing to contradict
	}
	return na == nb || strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na)
}

func normalizeForCompare(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func parseMovieString(raw string) MovieName {
	name := normalizeSeparators(raw)

	var out MovieName

	// A bracketed year is unambiguous, so take it and cut the title there.
	if m := reBracketedYear.FindStringSubmatchIndex(name); m != nil {
		out.Year, _ = strconv.Atoi(name[m[2]:m[3]])
		out.Edition = findEdition(name[m[1]:])
		out.Title = cleanTitle(name[:m[0]])
		if out.Title != "" {
			return out
		}
	}

	// Otherwise look for a bare year. Scan from the right, because titles can contain
	// year-like numbers ("2012", "Blade Runner 2049") and the release year comes last.
	if idx, year := lastBareYear(name); idx > 0 {
		out.Year = year
		out.Edition = findEdition(name[idx:])
		out.Title = cleanTitle(name[:idx])
		if out.Title != "" {
			return out
		}
	}

	out.Edition = findEdition(name)
	out.Title = cleanTitle(name)
	return out
}

// lastBareYear returns the index and value of the rightmost standalone year token.
// A year at position 0 is ignored: that is a title like "1917", not a release year.
//
// This walks tokens by hand rather than using a regex because adjacent years
// ("Blade Runner 2049 2017 1080p") overlap on their shared separator, and Go's regexp
// has no lookbehind, so a repeated find would silently skip the second one and
// mistake the title's number for the release year.
func lastBareYear(s string) (int, int) {
	bestIdx, bestYear := -1, 0

	for i := 0; i < len(s); {
		for i < len(s) && isSeparatorByte(s[i]) {
			i++
		}
		start := i
		for i < len(s) && !isSeparatorByte(s[i]) {
			i++
		}
		if start >= len(s) {
			break
		}
		if start == 0 {
			continue // a leading year is the title itself
		}
		if year, ok := yearToken(s[start:i]); ok {
			bestIdx, bestYear = start, year
		}
	}
	return bestIdx, bestYear
}

func isSeparatorByte(b byte) bool {
	return b == ' ' || b == '.' || b == '-' || b == '_'
}

// yearToken reports whether a token is a plausible four-digit release year.
func yearToken(tok string) (int, bool) {
	if len(tok) != 4 {
		return 0, false
	}
	for i := 0; i < 4; i++ {
		if tok[i] < '0' || tok[i] > '9' {
			return 0, false
		}
	}
	year, err := strconv.Atoi(tok)
	if err != nil || year < 1900 || year > 2099 {
		return 0, false
	}
	return year, true
}

// ParseEpisodeName extracts show, season and episode from a TV file path.
// It reports false when the path carries no recognizable episode marker.
func ParseEpisodeName(path string) (EpisodeName, bool) {
	base := stemOf(path)
	name := normalizeSeparators(base)

	if ep, ok := matchSeasonEpisode(name); ok {
		ep.Show = resolveShowName(path, ep.Show)
		return ep, true
	}

	// No marker in the filename: fall back to a "Season N" parent folder plus a bare
	// episode number in the file, which is a common minimal layout.
	if season, ok := seasonFromFolder(path); ok {
		if m := reBareEpisode.FindStringSubmatch(name); m != nil {
			num, _ := strconv.Atoi(m[1])
			return EpisodeName{
				Show:    resolveShowName(path, ""),
				Season:  season,
				Episode: num,
				Title:   cleanTitle(strings.Replace(name, m[0], " ", 1)),
			}, true
		}
		// A file whose entire name is just a number, e.g. "Season 1/03.mkv".
		if num, err := strconv.Atoi(strings.TrimSpace(name)); err == nil && num > 0 && num < 2000 {
			return EpisodeName{
				Show:    resolveShowName(path, ""),
				Season:  season,
				Episode: num,
			}, true
		}
	}

	return EpisodeName{}, false
}

// matchSeasonEpisode tries each episode-marker pattern against a cleaned filename.
func matchSeasonEpisode(name string) (EpisodeName, bool) {
	if m := reSeasonEpisode.FindStringSubmatchIndex(name); m != nil {
		season, _ := strconv.Atoi(name[m[2]:m[3]])
		episode, _ := strconv.Atoi(name[m[4]:m[5]])
		ep := EpisodeName{
			Season:  season,
			Episode: episode,
			Show:    cleanTitle(name[:m[0]]),
			Title:   cleanTitle(name[m[1]:]),
		}
		if m[6] >= 0 {
			ep.Extra = parseExtraEpisodes(name[m[6]:m[7]], episode)
		}
		return ep, true
	}

	if m := reWordyEpisode.FindStringSubmatchIndex(name); m != nil {
		season, _ := strconv.Atoi(name[m[2]:m[3]])
		episode, _ := strconv.Atoi(name[m[4]:m[5]])
		return EpisodeName{
			Season:  season,
			Episode: episode,
			Show:    cleanTitle(name[:m[0]]),
			Title:   cleanTitle(name[m[1]:]),
		}, true
	}

	if m := reCrossEpisode.FindStringSubmatchIndex(name); m != nil {
		season, _ := strconv.Atoi(name[m[2]:m[3]])
		episode, _ := strconv.Atoi(name[m[4]:m[5]])
		return EpisodeName{
			Season:  season,
			Episode: episode,
			Show:    cleanTitle(name[:m[0]]),
			Title:   cleanTitle(name[m[1]:]),
		}, true
	}

	return EpisodeName{}, false
}

func parseExtraEpisodes(tail string, first int) []int {
	var extra []int
	for _, numStr := range reExtraEpisode.FindAllString(tail, -1) {
		n, err := strconv.Atoi(numStr)
		if err != nil || n == first {
			continue
		}
		extra = append(extra, n)
	}
	return extra
}

// seasonFromFolder reads a season number out of the file's parent directory.
func seasonFromFolder(path string) (int, bool) {
	parent := filepath.Base(filepath.Dir(path))
	if m := reSeasonFolder.FindStringSubmatch(strings.TrimSpace(parent)); m != nil {
		n, err := strconv.Atoi(m[1])
		if err == nil {
			return n, true
		}
	}
	// "Specials" is the conventional name for season 0.
	if strings.EqualFold(strings.TrimSpace(parent), "specials") {
		return 0, true
	}
	return 0, false
}

// resolveShowName decides what the series is called.
//
// The name embedded in the filename wins when present. Otherwise we climb: the parent
// folder, or its parent when the parent is a "Season N" folder. This covers both
// "Show/Season 1/S01E01.mkv" and "Show/Show - S01E01.mkv".
func resolveShowName(path, fromFilename string) string {
	if fromFilename != "" {
		return fromFilename
	}

	dir := filepath.Dir(path)
	parent := filepath.Base(dir)

	if _, isSeasonFolder := seasonFromFolder(path); isSeasonFolder {
		grandparent := filepath.Base(filepath.Dir(dir))
		if usableFolder(grandparent) {
			return showNameFromFolder(grandparent)
		}
	}
	if usableFolder(parent) {
		return showNameFromFolder(parent)
	}
	return ""
}

func usableFolder(name string) bool {
	return name != "" && name != "." && name != string(filepath.Separator)
}

// showNameFromFolder cleans a series folder name. The year is stripped here because
// the scanner records it separately via ShowYear; leaving it in would make the title
// disagree with the metadata provider and show as "Better Call Saul (2015)" in the UI.
func showNameFromFolder(folder string) string {
	parsed := parseMovieString(folder)
	if parsed.Title != "" {
		return parsed.Title
	}
	return cleanTitle(normalizeSeparators(folder))
}

// ShowYear pulls a year out of a show folder name such as "The Office (2005)".
func ShowYear(folderName string) int {
	name := normalizeSeparators(folderName)
	if m := reBracketedYear.FindStringSubmatch(name); m != nil {
		y, _ := strconv.Atoi(m[1])
		return y
	}
	if idx, year := lastBareYear(name); idx > 0 {
		return year
	}
	return 0
}

// ---- cleaning helpers ----

// stemOf returns a path's filename without its extension.
func stemOf(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}

// normalizeSeparators turns scene-style dot and underscore separators into spaces.
//
// Dots between single letters are preserved so that initialisms survive: "S.W.A.T."
// must not become "S W A T".
func normalizeSeparators(s string) string {
	s = strings.ReplaceAll(s, "_", " ")

	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i, r := range runes {
		if r != '.' {
			b.WriteRune(r)
			continue
		}
		// Keep the dot when it sits between two single characters, as in "S.W.A.T".
		prevIsLoneLetter := i >= 1 && isLetterOrDigit(runes[i-1]) &&
			(i < 2 || !isLetterOrDigit(runes[i-2]))
		nextIsLoneLetter := i+1 < len(runes) && isLetterOrDigit(runes[i+1]) &&
			(i+2 >= len(runes) || !isLetterOrDigit(runes[i+2]))
		if prevIsLoneLetter && nextIsLoneLetter {
			b.WriteRune('.')
			continue
		}
		b.WriteRune(' ')
	}
	return b.String()
}

func isLetterOrDigit(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

// findEdition looks for a cut marker such as "Extended" or "Director's Cut".
func findEdition(s string) string {
	lower := strings.ToLower(s)
	if strings.Contains(lower, "director") && strings.Contains(lower, "cut") {
		return "Director's Cut"
	}
	if strings.Contains(lower, "final cut") {
		return "Final Cut"
	}
	for _, token := range strings.Fields(lower) {
		if edition, ok := editionTags[strings.Trim(token, "()[]{}-")]; ok {
			return edition
		}
	}
	return ""
}

// cleanTitle strips release noise and punctuation from a candidate title.
//
// Everything from the first release tag onwards is dropped, because release names are
// ordered "Title Year Quality Source Codec-Group" and nothing after that first tag is
// ever part of the title.
func cleanTitle(s string) string {
	// trimEdges rather than a blanket Trim: stripping a leading "[" here would break
	// "[Group] Akira" into the token "Group]", which no longer reads as a bracketed tag.
	s = trimEdges(s)
	if s == "" {
		return ""
	}

	fields := strings.Fields(s)
	kept := make([]string, 0, len(fields))

	for _, f := range fields {
		token := strings.ToLower(strings.Trim(f, "()[]{}-.,"))
		if token == "" {
			continue
		}
		if _, isEdition := editionTags[token]; releaseTags[token] || isEdition {
			break
		}
		// Channel layouts like "5.1" and "7.1" mark the audio section.
		if isChannelLayout(token) {
			break
		}
		// A fully bracketed token is a release-group, tracker or checksum tag:
		// "[YTS.MX]", "[1A2B3C4D]", "[BDRip]". Drop it and keep reading, because such
		// tags appear before the title as often as after it.
		if fullyBracketed(f) {
			continue
		}
		kept = append(kept, f)
	}

	if len(kept) == 0 {
		// Everything looked like noise; fall back to the raw string so the item is at
		// least identifiable in the UI rather than showing an empty card.
		kept = fields
	}

	title := trimEdges(strings.Join(kept, " "))
	title = reMultiSpace.ReplaceAllString(title, " ")

	// Restore the article convention: "Matrix, The" reads better as "The Matrix".
	if before, after, found := strings.Cut(title, ", "); found {
		switch strings.ToLower(strings.TrimSpace(after)) {
		case "the", "a", "an":
			title = strings.TrimSpace(after) + " " + before
		}
	}

	return strings.TrimSpace(title)
}

func isChannelLayout(token string) bool {
	switch token {
	case "5.1", "7.1", "2.0", "1.0", "6.1":
		return true
	}
	return false
}

// fullyBracketed reports whether a token is entirely wrapped in square brackets or
// braces, which in release naming always marks an annotation rather than the title.
// Parentheses are deliberately excluded: those do appear inside real titles, as in
// "The Office (US)".
func fullyBracketed(f string) bool {
	if len(f) < 3 {
		return false
	}
	return (f[0] == '[' && f[len(f)-1] == ']') || (f[0] == '{' && f[len(f)-1] == '}')
}

// trimEdges strips separator punctuation from both ends of a title, removing brackets
// only when they are unbalanced. Trimming them unconditionally would turn
// "The Office (US)" into "The Office (US", which is worse than leaving them alone.
func trimEdges(s string) string {
	const filler = " -._,"

	for {
		before := s
		s = strings.Trim(s, filler)

		for _, pair := range []struct{ open, close byte }{{'(', ')'}, {'[', ']'}, {'{', '}'}} {
			opens := strings.Count(s, string(pair.open))
			closes := strings.Count(s, string(pair.close))
			if len(s) > 0 && s[len(s)-1] == pair.close && closes > opens {
				s = s[:len(s)-1]
			}
			if len(s) > 0 && s[0] == pair.open && opens > closes {
				s = s[1:]
			}
		}

		if s == before {
			return s
		}
	}
}

// SortTitle produces the string a library sorts on, moving a leading article to the
// end so "The Matrix" files under M.
func SortTitle(title string) string {
	lower := strings.ToLower(title)
	for _, article := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, article) {
			return strings.TrimSpace(title[len(article):]) + ", " + strings.TrimSpace(title[:len(article)-1])
		}
	}
	return title
}
