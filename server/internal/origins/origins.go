// Package origins names countries and languages, and reads the words people use for them in
// prompts ("Korean", "British", "from Iceland"), for Muse and the description documents.
package origins

import (
	"sort"
	"strings"
)

// Origin is what a word in a prompt points to: a country (ISO 3166-1) and, when the word
// also names a distinctive language, that language (ISO 639-1). "Korean" is KR and ko;
// "British" is GB alone (English says nothing about where a film is from).
type Origin struct {
	Country, Language string
}

type country struct {
	code, name string
	lang       string   // distinctive language, "" when none
	words      []string // names and demonyms, lower case
}

var countries = []country{
	{"US", "the United States", "", []string{"american", "usa", "united states", "the us", "u.s."}},
	{"GB", "the United Kingdom", "", []string{"british", "uk", "united kingdom", "britain", "great britain"}},
	{"IE", "Ireland", "", []string{"irish", "ireland"}},
	{"CA", "Canada", "", []string{"canadian", "canada"}},
	{"AU", "Australia", "", []string{"australian", "australia", "aussie"}},
	{"NZ", "New Zealand", "", []string{"new zealand"}},
	{"FR", "France", "fr", []string{"french", "france"}},
	{"DE", "Germany", "de", []string{"german", "germany"}},
	{"AT", "Austria", "", []string{"austrian", "austria"}},
	{"CH", "Switzerland", "", []string{"swiss", "switzerland"}},
	{"IT", "Italy", "it", []string{"italian", "italy"}},
	{"ES", "Spain", "es", []string{"spanish", "spain"}},
	{"PT", "Portugal", "pt", []string{"portuguese", "portugal"}},
	{"BR", "Brazil", "", []string{"brazilian", "brazil"}},
	{"MX", "Mexico", "", []string{"mexican", "mexico"}},
	{"AR", "Argentina", "", []string{"argentinian", "argentine", "argentina"}},
	{"CL", "Chile", "", []string{"chilean", "chile"}},
	{"CO", "Colombia", "", []string{"colombian", "colombia"}},
	{"CU", "Cuba", "", []string{"cuban", "cuba"}},
	{"JM", "Jamaica", "", []string{"jamaican", "jamaica"}},
	{"NL", "the Netherlands", "nl", []string{"dutch", "netherlands", "holland"}},
	{"BE", "Belgium", "", []string{"belgian", "belgium"}},
	{"SE", "Sweden", "sv", []string{"swedish", "sweden"}},
	{"NO", "Norway", "no", []string{"norwegian", "norway"}},
	{"DK", "Denmark", "da", []string{"danish", "denmark"}},
	{"FI", "Finland", "fi", []string{"finnish", "finland"}},
	{"IS", "Iceland", "is", []string{"icelandic", "iceland"}},
	{"PL", "Poland", "pl", []string{"polish", "poland"}},
	{"CZ", "the Czech Republic", "cs", []string{"czech", "czechia", "czech republic"}},
	{"HU", "Hungary", "hu", []string{"hungarian", "hungary"}},
	{"RO", "Romania", "ro", []string{"romanian", "romania"}},
	{"GR", "Greece", "el", []string{"greek", "greece"}},
	{"TR", "Turkey", "tr", []string{"turkish", "turkey"}},
	{"RU", "Russia", "ru", []string{"russian", "russia"}},
	{"UA", "Ukraine", "uk", []string{"ukrainian", "ukraine"}},
	{"IL", "Israel", "he", []string{"israeli", "israel"}},
	{"IR", "Iran", "fa", []string{"iranian", "persian", "iran"}},
	{"EG", "Egypt", "", []string{"egyptian", "egypt"}},
	{"NG", "Nigeria", "", []string{"nigerian", "nigeria"}},
	{"ZA", "South Africa", "", []string{"south african", "south africa"}},
	{"SN", "Senegal", "", []string{"senegalese", "senegal"}},
	{"ML", "Mali", "", []string{"malian", "mali"}},
	{"IN", "India", "hi", []string{"indian", "india", "bollywood"}},
	{"PK", "Pakistan", "", []string{"pakistani", "pakistan"}},
	{"CN", "China", "zh", []string{"chinese", "china", "mandarin"}},
	{"HK", "Hong Kong", "cn", []string{"hong kong", "cantonese"}},
	{"TW", "Taiwan", "", []string{"taiwanese", "taiwan"}},
	{"JP", "Japan", "ja", []string{"japanese", "japan"}},
	{"KR", "South Korea", "ko", []string{"korean", "south korean", "korea", "south korea", "k-pop", "kpop"}},
	{"TH", "Thailand", "th", []string{"thai", "thailand"}},
	{"VN", "Vietnam", "vi", []string{"vietnamese", "vietnam"}},
	{"ID", "Indonesia", "id", []string{"indonesian", "indonesia"}},
	{"PH", "the Philippines", "tl", []string{"filipino", "philippines"}},
}

var languages = map[string]string{
	"en": "English", "fr": "French", "de": "German", "it": "Italian", "es": "Spanish", "pt": "Portuguese", "nl": "Dutch",
	"sv": "Swedish", "no": "Norwegian", "nb": "Norwegian", "da": "Danish", "fi": "Finnish", "is": "Icelandic", "pl": "Polish",
	"cs": "Czech", "hu": "Hungarian", "ro": "Romanian", "el": "Greek", "tr": "Turkish", "ru": "Russian", "uk": "Ukrainian",
	"he": "Hebrew", "fa": "Persian", "ar": "Arabic", "hi": "Hindi", "ta": "Tamil", "te": "Telugu", "zh": "Mandarin",
	"cn": "Cantonese", "ja": "Japanese", "ko": "Korean", "th": "Thai", "vi": "Vietnamese", "id": "Indonesian", "tl": "Tagalog",
}

var (
	byCode = map[string]country{}
	byWord = map[string]country{}
	words  []string // longest first
)

func init() {
	for _, c := range countries {
		byCode[c.code] = c
		for _, w := range c.words {
			byWord[w] = c
			words = append(words, w)
		}
	}
	sort.Slice(words, func(i, j int) bool {
		if len(words[i]) != len(words[j]) {
			return len(words[i]) > len(words[j])
		}
		return words[i] < words[j]
	})
}

// CountryName is the English name of an ISO country code ("" when unknown).
func CountryName(code string) string { return byCode[strings.ToUpper(code)].name }

// LanguageName is the English name of an ISO 639-1 code ("" when unknown).
func LanguageName(code string) string { return languages[strings.ToLower(code)] }

// Lookup reads one word or phrase ("korean", "Iceland", "the UK").
func Lookup(word string) (Origin, bool) {
	w := strings.ToLower(strings.TrimSpace(word))
	w = strings.TrimSuffix(w, "s") // "Brazilians", "Danes" won't all work, but plurals of demonyms mostly do
	c, ok := byWord[w]
	if !ok {
		c, ok = byWord[strings.ToLower(strings.TrimSpace(word))]
	}
	if !ok {
		// A country code ("KR") or a country's own name with "the".
		if cc, isCode := byCode[strings.ToUpper(strings.TrimSpace(word))]; isCode {
			return Origin{cc.code, cc.lang}, true
		}
		return Origin{}, false
	}
	return Origin{c.code, c.lang}, true
}

// Find returns the origins named in text (lower case, words separated by single spaces),
// longest phrase first, and text with them cut out. Short ambiguous words ("us", "uk" in
// another sense) are only taken in their usual spelling.
func Find(text string) ([]Origin, string) {
	s := " " + text + " "
	var out []Origin
	seen := map[string]bool{}
	for _, w := range words {
		for {
			i := strings.Index(s, " "+w+" ")
			if i < 0 {
				break
			}
			c := byWord[w]
			if !seen[c.code] {
				seen[c.code] = true
				out = append(out, Origin{c.code, c.lang})
			}
			s = s[:i] + " " + s[i+len(w)+1:]
		}
	}
	return out, strings.Join(strings.Fields(s), " ")
}
