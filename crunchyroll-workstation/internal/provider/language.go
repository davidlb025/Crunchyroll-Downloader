package provider

import "strings"

func AudioLanguageTag(language string) string {
	base := strings.ToLower(strings.Split(language, "-")[0])
	code := map[string]string{"en": "eng", "es": "spa", "fr": "fra", "de": "deu", "it": "ita", "pt": "por", "ja": "jpn", "ko": "kor", "zh": "zho", "ar": "ara", "hi": "hin", "tr": "tur", "ru": "rus", "pl": "pol", "nl": "nld", "sv": "swe", "da": "dan", "no": "nor", "fi": "fin", "cs": "ces", "hu": "hun", "ro": "ron", "th": "tha", "ta": "tam", "te": "tel", "id": "ind", "vi": "vie"}[base]
	if code == "" {
		return "und"
	}
	return code
}
