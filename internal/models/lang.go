package models

import "strings"

// languageNames maps the ISO 639-1 and 639-2 codes ffprobe emits to display names.
// This covers the languages that realistically appear in media containers; anything
// unlisted falls back to the raw code, which is still better than nothing.
var languageNames = map[string]string{
	"aar": "Afar", "abk": "Abkhazian", "afr": "Afrikaans", "amh": "Amharic",
	"ara": "Arabic", "arm": "Armenian", "hye": "Armenian", "asm": "Assamese",
	"aze": "Azerbaijani", "bak": "Bashkir", "bel": "Belarusian", "ben": "Bengali",
	"bos": "Bosnian", "bul": "Bulgarian", "bur": "Burmese", "mya": "Burmese",
	"cat": "Catalan", "ces": "Czech", "cze": "Czech", "chi": "Chinese", "zho": "Chinese",
	"cmn": "Mandarin", "yue": "Cantonese", "cym": "Welsh", "wel": "Welsh",
	"dan": "Danish", "deu": "German", "ger": "German", "dut": "Dutch", "nld": "Dutch",
	"ell": "Greek", "gre": "Greek", "eng": "English", "epo": "Esperanto",
	"est": "Estonian", "eus": "Basque", "baq": "Basque", "fao": "Faroese",
	"fas": "Persian", "per": "Persian", "fin": "Finnish", "fra": "French", "fre": "French",
	"gla": "Scottish Gaelic", "gle": "Irish", "glg": "Galician", "guj": "Gujarati",
	"heb": "Hebrew", "hin": "Hindi", "hrv": "Croatian", "hun": "Hungarian",
	"ice": "Icelandic", "isl": "Icelandic", "ind": "Indonesian", "ita": "Italian",
	"jpn": "Japanese", "jav": "Javanese", "kan": "Kannada", "kat": "Georgian",
	"geo": "Georgian", "kaz": "Kazakh", "khm": "Khmer", "kor": "Korean",
	"kur": "Kurdish", "lao": "Lao", "lat": "Latin", "lav": "Latvian",
	"lit": "Lithuanian", "ltz": "Luxembourgish", "mac": "Macedonian", "mkd": "Macedonian",
	"mal": "Malayalam", "mar": "Marathi", "may": "Malay", "msa": "Malay",
	"mlt": "Maltese", "mon": "Mongolian", "nep": "Nepali", "nor": "Norwegian",
	"nob": "Norwegian Bokmal", "nno": "Norwegian Nynorsk", "ori": "Odia",
	"pan": "Punjabi", "pol": "Polish", "por": "Portuguese", "pus": "Pashto",
	"ron": "Romanian", "rum": "Romanian", "rus": "Russian", "san": "Sanskrit",
	"sin": "Sinhala", "slk": "Slovak", "slo": "Slovak", "slv": "Slovenian",
	"som": "Somali", "spa": "Spanish", "sqi": "Albanian", "alb": "Albanian",
	"srp": "Serbian", "swa": "Swahili", "swe": "Swedish", "tam": "Tamil",
	"tel": "Telugu", "tgl": "Tagalog", "tha": "Thai", "tur": "Turkish",
	"ukr": "Ukrainian", "urd": "Urdu", "uzb": "Uzbek", "vie": "Vietnamese",
	"yid": "Yiddish", "zul": "Zulu",

	// Two-letter forms that also show up in sidecar filenames.
	"aa": "Afar", "af": "Afrikaans", "am": "Amharic", "ar": "Arabic", "as": "Assamese",
	"az": "Azerbaijani", "be": "Belarusian", "bg": "Bulgarian", "bn": "Bengali",
	"bs": "Bosnian", "ca": "Catalan", "cs": "Czech", "cy": "Welsh", "da": "Danish",
	"de": "German", "el": "Greek", "en": "English", "eo": "Esperanto", "es": "Spanish",
	"et": "Estonian", "eu": "Basque", "fa": "Persian", "fi": "Finnish", "fo": "Faroese",
	"fr": "French", "ga": "Irish", "gd": "Scottish Gaelic", "gl": "Galician",
	"gu": "Gujarati", "he": "Hebrew", "hi": "Hindi", "hr": "Croatian", "hu": "Hungarian",
	"hy": "Armenian", "id": "Indonesian", "is": "Icelandic", "it": "Italian",
	"ja": "Japanese", "jv": "Javanese", "ka": "Georgian", "kk": "Kazakh",
	"km": "Khmer", "kn": "Kannada", "ko": "Korean", "ku": "Kurdish", "la": "Latin",
	"lb": "Luxembourgish", "lo": "Lao", "lt": "Lithuanian", "lv": "Latvian",
	"mk": "Macedonian", "ml": "Malayalam", "mn": "Mongolian", "mr": "Marathi",
	"ms": "Malay", "mt": "Maltese", "my": "Burmese", "nb": "Norwegian Bokmal",
	"ne": "Nepali", "nl": "Dutch", "nn": "Norwegian Nynorsk", "no": "Norwegian",
	"or": "Odia", "pa": "Punjabi", "pl": "Polish", "ps": "Pashto", "pt": "Portuguese",
	"ro": "Romanian", "ru": "Russian", "sa": "Sanskrit", "si": "Sinhala",
	"sk": "Slovak", "sl": "Slovenian", "so": "Somali", "sq": "Albanian",
	"sr": "Serbian", "sv": "Swedish", "sw": "Swahili", "ta": "Tamil", "te": "Telugu",
	"th": "Thai", "tl": "Tagalog", "tr": "Turkish", "uk": "Ukrainian", "ur": "Urdu",
	"uz": "Uzbek", "vi": "Vietnamese", "yi": "Yiddish", "zh": "Chinese", "zu": "Zulu",
}

// LanguageName resolves an ISO language code to a display name. Unknown codes are
// returned title-cased so the UI still shows something meaningful.
func LanguageName(code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	if c == "" || c == "und" || c == "unknown" {
		return ""
	}
	// Strip region suffixes such as pt-BR or zh_Hans.
	if i := strings.IndexAny(c, "-_"); i > 0 {
		if name, ok := languageNames[c[:i]]; ok {
			return name
		}
	}
	if name, ok := languageNames[c]; ok {
		return name
	}
	return strings.ToUpper(c[:1]) + c[1:]
}

// IsLanguageCode reports whether s looks like a language code, used when parsing
// subtitle sidecar filenames such as "Movie.en.srt".
func IsLanguageCode(s string) bool {
	c := strings.ToLower(strings.TrimSpace(s))
	if len(c) != 2 && len(c) != 3 {
		return false
	}
	_, ok := languageNames[c]
	return ok
}
